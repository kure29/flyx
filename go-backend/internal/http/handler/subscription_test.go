package handler

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"go-backend/internal/store/model"
	"go-backend/internal/store/repo"
)

func TestKernelPayloadCarriesActiveShareSubscriptions(t *testing.T) {
	r, err := repo.Open(t.TempDir() + "/subscription.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = r.Close() })
	h := New(r, "unit-test-secret")
	now := time.Now().UnixMilli()

	if err := r.DB().Exec(`INSERT INTO node(name, secret, server_ip, port, created_time, status) VALUES('n', 's', '203.0.113.1', '1000-2000', ?, 1)`, now).Error; err != nil {
		t.Fatal(err)
	}
	nodeID := mustLastInsertID(t, r, "node")
	if err := r.DB().Exec(`INSERT INTO node_kernel(node_id, kernel_type, version, status, created_time, updated_time) VALUES(?, 'xray', 'latest', 'pending', ?, ?)`, nodeID, now, now).Error; err != nil {
		t.Fatal(err)
	}
	kernelID := mustLastInsertID(t, r, "kernel")
	if err := r.DB().Exec(`INSERT INTO node_inbound(kernel_id, node_id, name, protocol, listen, port, settings, created_time, updated_time)
		VALUES(?, ?, 'a', 'trojan', '::', 443, '{"password":"owner-pw"}', ?, ?)`, kernelID, nodeID, now, now).Error; err != nil {
		t.Fatal(err)
	}
	// "All inbounds" also covers inbounds added later.
	s := &model.Subscription{Name: "x", Kind: model.SubscriptionShare, Token: strings.Repeat("a", 32), UUID: "u",
		Password: "user-pw", SourceAll: 1, Enabled: 1, CreatedTime: now, UpdatedTime: now}
	if err := r.SaveSubscription(s); err != nil {
		t.Fatal(err)
	}
	own := &model.Subscription{Name: "mine", Kind: model.SubscriptionOwn, Token: strings.Repeat("b", 32),
		Password: "own-should-not-appear", SourceAll: 1, Enabled: 1, CreatedTime: now, UpdatedTime: now}
	if err := r.SaveSubscription(own); err != nil {
		t.Fatal(err)
	}

	config := func() string {
		k, _ := r.GetNodeKernel(kernelID)
		payload, _, err := h.buildKernelApplyPayload(k)
		if err != nil {
			t.Fatal(err)
		}
		b, _ := json.Marshal(payload["files"])
		return string(b)
	}
	if c := config(); !strings.Contains(c, "owner-pw") || !strings.Contains(c, "user-pw") || strings.Contains(c, "own-should-not-appear") {
		t.Fatalf("active share subscription must be in the config, own ones not: %s", c)
	}
	k, _ := r.GetNodeKernel(kernelID)
	// The default subscription ("我的订阅", own credentials too) and s are
	// counted; a leftover owner-credential row is not.
	if payload, _, _ := h.buildKernelApplyPayload(k); !containsInt64(payload["userMarks"].([]int64), s.ID) ||
		containsInt64(payload["userMarks"].([]int64), own.ID) {
		t.Fatalf("share users must be counted: %v", payload["userMarks"])
	}

	// Per-user counters: deltas within an epoch, the whole value after one.
	s.TrafficLimit = 1000
	_ = r.SaveSubscription(s)
	h.applyUserCounters(nodeID, "e1", []agentUserCounter{{ID: s.ID, Dir: "up", Bytes: 100}, {ID: s.ID, Dir: "down", Bytes: 300}}, now)
	h.applyUserCounters(nodeID, "e1", []agentUserCounter{{ID: s.ID, Dir: "up", Bytes: 150}, {ID: s.ID, Dir: "down", Bytes: 300}}, now)
	h.applyUserCounters(nodeID, "e2", []agentUserCounter{{ID: s.ID, Dir: "down", Bytes: 50}}, now)
	got, _ := r.GetSubscription(s.ID)
	if got.UploadBytes != 150 || got.DownloadBytes != 350 || subOverQuota(got) {
		t.Fatalf("traffic: up=%d down=%d", got.UploadBytes, got.DownloadBytes)
	}
	// A save from a stale read keeps the counted traffic.
	stale := *s
	stale.Name = "renamed"
	_ = r.SaveSubscription(&stale)
	h.applyUserCounters(nodeID, "e2", []agentUserCounter{{ID: s.ID, Dir: "down", Bytes: 600}}, now)
	got, _ = r.GetSubscription(s.ID)
	if got.Name != "renamed" || subUsed(got) != 1050 || !subOverQuota(got) || subActive(got, time.Now()) {
		t.Fatalf("over the limit must cut off: %+v", got)
	}
	if c := config(); strings.Contains(c, "user-pw") {
		t.Fatalf("over-quota share must be removed: %s", c)
	}
	_ = r.ResetSubscriptionTraffic(s.ID, now)
	if c := config(); !strings.Contains(c, "user-pw") {
		t.Fatalf("reset traffic restores the user: %s", c)
	}
	if got, _ = r.GetSubscription(s.ID); subTrafficSince(got) != now {
		t.Fatalf("reset restarts the count: since=%d", subTrafficSince(got))
	}

	s.ExpireTime = now - 1000
	_ = r.SaveSubscription(s)
	if c := config(); strings.Contains(c, "user-pw") || !strings.Contains(c, "owner-pw") {
		t.Fatalf("expired share must be removed, owner kept: %s", c)
	}
}

func TestMonthlyReset(t *testing.T) {
	loc := time.FixedZone("x", 8*3600)
	at := func(s string) time.Time {
		v, _ := time.ParseInLocation("2006-01-02 15:04", s, loc)
		return v
	}
	cases := []struct {
		day       int
		now, want string
	}{
		{1, "2026-03-15 10:00", "2026-03-01 00:00"},
		{20, "2026-03-15 10:00", "2026-02-20 00:00"},
		{28, "2026-02-28 00:00", "2026-02-28 00:00"},
		{28, "2026-01-05 00:00", "2025-12-28 00:00"},
	}
	for _, c := range cases {
		if got := lastResetAt(c.day, at(c.now)); !got.Equal(at(c.want)) {
			t.Fatalf("day %d at %s: %s, want %s", c.day, c.now, got, c.want)
		}
	}
	s := &model.Subscription{Kind: model.SubscriptionShare, ResetDay: 1, TrafficResetTime: at("2026-02-10 00:00").UnixMilli()}
	if !subDueReset(s, at("2026-03-01 08:00")) || subDueReset(s, at("2026-02-27 08:00")) {
		t.Fatal("due on the reset day, not before")
	}
}

func containsInt64(list []int64, v int64) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

func TestSubTrafficSince(t *testing.T) {
	for _, c := range []struct {
		s    model.Subscription
		want int64
	}{
		{model.Subscription{CreatedTime: 1, TrafficResetTime: 5, TrafficSince: 3}, 3},
		{model.Subscription{CreatedTime: 1, TrafficResetTime: 5}, 5},
		{model.Subscription{CreatedTime: 1}, 1},
	} {
		if got := subTrafficSince(&c.s); got != c.want {
			t.Fatalf("%+v: got %d, want %d", c.s, got, c.want)
		}
	}
}
