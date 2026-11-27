package handler

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"go-backend/internal/http/response"
	"go-backend/internal/kernel"
	"go-backend/internal/store/model"
	"go-backend/internal/store/repo"
)

func seedIPTestSubscription(t *testing.T, repository *repo.Repository, name, token string, maxOnline int, action string, suspendMin int, maxPull int) (*model.Subscription, *model.NodeKernel) {
	t.Helper()
	now := time.Now().UnixMilli()
	nodeID := seedRelayTargetNode(t, repository, "ip-node-1", "203.0.113.10")
	sb := seedChainKernel(t, repository, nodeID, kernel.TypeSingbox)
	seedChainInbound(t, repository, sb, "vless-test", kernel.Inbound{
		Protocol: kernel.ProtocolVLESS,
		Port:     14430,
		Settings: kernel.Settings{UUID: "11111111-1111-4111-8111-111111111111"},
	}, 0)

	group := model.NodeGroup{Name: "ip-group-" + name, SourceAll: 1, CreatedTime: now, UpdatedTime: now}
	if err := repository.SaveNodeGroup(&group); err != nil {
		t.Fatal(err)
	}
	sub := &model.Subscription{
		Name:             name,
		Kind:             model.SubscriptionShare,
		Token:            token,
		UUID:             "22222222-2222-4222-8222-222222222222",
		Password:         "secret-pass",
		GroupIDs:         repo.FormatGroupIDs([]int64{group.ID}),
		Enabled:          1,
		AppliedActive:    1,
		MaxOnlineIPs:     maxOnline,
		IPAction:         action,
		IPSuspendMinutes: suspendMin,
		MaxPullIPs:       maxPull,
		CreatedTime:      now,
		UpdatedTime:      now,
		TrafficSince:     now,
	}
	if err := repository.SaveSubscription(sub); err != nil {
		t.Fatal(err)
	}
	return sub, sb
}

func TestSubscriptionOnlineIPCrossNodeAggregationAndIPv6Dedup(t *testing.T) {
	store := newSubOnlineIPStore()
	t0 := time.Date(2026, 11, 26, 12, 0, 0, 0, time.UTC)

	store.updateNode(10, []agentUserOnlineIP{
		{ID: 7, IP: "1.2.3.4", LastSeen: t0.UnixMilli()},
		{ID: 7, IP: "2001:db8:1:2::1111", LastSeen: t0.Add(-5 * time.Second).UnixMilli()},
	}, t0)
	store.updateNode(20, []agentUserOnlineIP{
		{ID: 7, IP: "::ffff:1.2.3.4", LastSeen: t0.Add(2 * time.Second).UnixMilli()},
		{ID: 7, IP: "2001:db8:1:2::2222", LastSeen: t0.Add(1 * time.Second).UnixMilli()},
		{ID: 7, IP: "5.6.7.8", LastSeen: t0.Add(-10 * time.Second).UnixMilli()},
	}, t0.Add(2*time.Second))

	got := store.onlineIPsForSub(7, t0.Add(5*time.Second))
	if len(got) != 3 {
		t.Fatalf("expected 3 deduplicated online IPs across nodes, got %d: %+v", len(got), got)
	}
	// Newest first: 1.2.3.4 (+2s) on both nodes [10, 20]
	if got[0].ip != "1.2.3.4" || len(got[0].nodeIDs) != 2 || got[0].nodeIDs[0] != 10 || got[0].nodeIDs[1] != 20 {
		t.Fatalf("expected 1.2.3.4 merged on nodes [10 20], got %+v", got[0])
	}
	// Second: 2001:db8:1:2::2222 (+1s) merged across nodes [10, 20] by /64
	if got[1].ip != "2001:db8:1:2::2222" || len(got[1].nodeIDs) != 2 {
		t.Fatalf("expected IPv6 /64 merged with freshest address, got %+v", got[1])
	}

	counts := store.onlineIPCounts(t0.Add(5 * time.Second))
	if counts[7] != 3 {
		t.Fatalf("expected onlineIPCounts[7] == 3, got %d", counts[7])
	}

	// After TTL elapses, entries expire automatically.
	expired := store.onlineIPsForSub(7, t0.Add(subOnlineIPTTL+time.Minute))
	if len(expired) != 0 {
		t.Fatalf("expected expired entries to clear after TTL, got %+v", expired)
	}
}

func TestSubscriptionOnlineIPLimitDebounceSuspendAndAutoResume(t *testing.T) {
	repository, err := repo.Open(t.TempDir() + "/sub-ip-suspend.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = repository.Close() })
	h := New(repository, "unit-test-secret")
	sub, _ := seedIPTestSubscription(t, repository, "alice", strings.Repeat("a", 32), 2, subIPActionSuspend, 10, 0)

	t0 := time.Now().Truncate(time.Second)
	threeIPs := func(at time.Time) []agentUserOnlineIP {
		ms := at.UnixMilli()
		return []agentUserOnlineIP{
			{ID: sub.ID, IP: "198.51.100.1", LastSeen: ms},
			{ID: sub.ID, IP: "198.51.100.2", LastSeen: ms},
			{ID: sub.ID, IP: "198.51.100.3", LastSeen: ms},
		}
	}

	// 1st observation: breach streak = 1, must NOT suspend yet.
	h.applyNodeOnlineIPs(1, threeIPs(t0), t0)
	got, _ := repository.GetSubscription(sub.ID)
	if !subActive(got, t0) || got.IPSuspendedUntil != 0 {
		t.Fatalf("1st breach observation must not suspend immediately, got suspendedUntil=%d", got.IPSuspendedUntil)
	}

	// Parallel report from another node 2s later within the same poll tick must NOT trigger suspend.
	h.applyNodeOnlineIPs(2, []agentUserOnlineIP{{ID: sub.ID, IP: "198.51.100.1", LastSeen: t0.Add(2 * time.Second).UnixMilli()}}, t0.Add(2*time.Second))
	got, _ = repository.GetSubscription(sub.ID)
	if !subActive(got, t0.Add(2*time.Second)) || got.IPSuspendedUntil != 0 {
		t.Fatalf("same-tick parallel node poll must not double-count breach streak")
	}

	// Dropping back to 2 IPs resets the streak cleanly.
	tDrop := t0.Add(60 * time.Second)
	h.applyNodeOnlineIPs(1, []agentUserOnlineIP{
		{ID: sub.ID, IP: "198.51.100.1", LastSeen: tDrop.UnixMilli()},
		{ID: sub.ID, IP: "198.51.100.2", LastSeen: tDrop.UnixMilli()},
	}, tDrop)
	h.applyNodeOnlineIPs(2, nil, tDrop)

	// New breach observation 1 after reset -> streak = 1 (still active).
	t1 := tDrop.Add(60 * time.Second)
	h.applyNodeOnlineIPs(1, threeIPs(t1), t1)
	got, _ = repository.GetSubscription(sub.ID)
	if !subActive(got, t1) {
		t.Fatalf("first breach after reset must remain active")
	}

	// 2nd consecutive breach observation 60s later -> triggers 10-minute suspension!
	t2 := t1.Add(60 * time.Second)
	h.applyNodeOnlineIPs(1, threeIPs(t2), t2)
	got, _ = repository.GetSubscription(sub.ID)
	if subActive(got, t2) {
		t.Fatalf("2nd consecutive breach must suspend subscription")
	}
	wantUntil := t2.Add(10 * time.Minute).UnixMilli()
	if got.IPSuspendedUntil != wantUntil {
		t.Fatalf("IPSuspendedUntil = %d, want %d", got.IPSuspendedUntil, wantUntil)
	}
	if reason := subInactiveReason(got, t2); !strings.Contains(reason, "在线 IP 超限") {
		t.Fatalf("unexpected inactive reason: %q", reason)
	}
	if status := subscriptionStatusString(got, t2); status != "ip_suspended" {
		t.Fatalf("subscriptionStatusString = %q, want ip_suspended", status)
	}

	// Open subscription link while suspended returns notice node instead of real nodes.
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/open_api/sub/"+sub.Token+"?format=clash", nil)
	req.RemoteAddr = "198.51.100.50:43210"
	h.openSubscription(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "在线 IP 超限") {
		t.Fatalf("expected IP suspend notice node in clash output, code=%d body=%s", rec.Code, rec.Body.String())
	}

	// After suspension expires, subActive becomes true again and syncSubscriptions clears IPSuspendedUntil.
	tAfter := t2.Add(11 * time.Minute)
	if !subActive(got, tAfter) {
		t.Fatalf("subscription should auto-resume once IPSuspendedUntil has passed")
	}
	h.syncSubscriptions(tAfter)
	got, _ = repository.GetSubscription(sub.ID)
	if got.IPSuspendedUntil != 0 {
		t.Fatalf("syncSubscriptions should clear expired IPSuspendedUntil, got %d", got.IPSuspendedUntil)
	}
}

func TestSubscriptionOnlineIPLimitDisableAndAlert(t *testing.T) {
	repository, err := repo.Open(t.TempDir() + "/sub-ip-actions.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = repository.Close() })
	h := New(repository, "unit-test-secret")

	subDisable, _ := seedIPTestSubscription(t, repository, "bob", strings.Repeat("b", 32), 1, subIPActionDisable, 0, 0)
	subAlert, _ := seedIPTestSubscription(t, repository, "carol", strings.Repeat("c", 32), 1, subIPActionAlert, 0, 0)

	t0 := time.Now().Truncate(time.Second)
	for _, at := range []time.Time{t0, t0.Add(60 * time.Second)} {
		ms := at.UnixMilli()
		h.applyNodeOnlineIPs(1, []agentUserOnlineIP{
			{ID: subDisable.ID, IP: "198.51.100.1", LastSeen: ms},
			{ID: subDisable.ID, IP: "198.51.100.2", LastSeen: ms},
			{ID: subAlert.ID, IP: "198.51.100.3", LastSeen: ms},
			{ID: subAlert.ID, IP: "198.51.100.4", LastSeen: ms},
		}, at)
	}

	gotDisable, _ := repository.GetSubscription(subDisable.ID)
	if gotDisable.Enabled != 0 {
		t.Fatalf("action=disable must disable subscription after consecutive breach")
	}
	gotAlert, _ := repository.GetSubscription(subAlert.ID)
	if gotAlert.Enabled != 1 || gotAlert.IPSuspendedUntil != 0 {
		t.Fatalf("action=alert must leave subscription enabled and unsuspended")
	}
}

func TestSubscriptionPullLogCapIPActivityAndUnsuspend(t *testing.T) {
	repository, err := repo.Open(t.TempDir() + "/sub-pull-cap.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = repository.Close() })
	h := New(repository, "unit-test-secret")
	sub, sb := seedIPTestSubscription(t, repository, "dave", strings.Repeat("d", 32), 0, subIPActionAlert, 10, 2)

	// Verify sing-box renders log.level=warn when MaxOnlineIPs == 0.
	payload, _, err := h.buildKernelApplyPayload(sb)
	if err != nil {
		t.Fatal(err)
	}
	cfg := payload["files"].([]kernel.File)[0].Content
	if !strings.Contains(cfg, `"level": "warn"`) {
		t.Fatalf("expected sing-box log.level=warn without online IP limit, got:\n%s", cfg)
	}

	// Update subscription via save API to enable MaxOnlineIPs=3 and verify sing-box switches to info.
	maxOnline := 3
	action := subIPActionSuspend
	suspendMins := 15
	maxPull := 2
	saveBody, _ := json.Marshal(subscriptionRequest{
		ID:               sub.ID,
		Name:             sub.Name,
		GroupIDs:         repo.ParseGroupIDs(sub.GroupIDs),
		MaxOnlineIPs:     &maxOnline,
		IPAction:         &action,
		IPSuspendMinutes: &suspendMins,
		MaxPullIPs:       &maxPull,
	})
	saveRec := httptest.NewRecorder()
	h.subscriptionSave(saveRec, httptest.NewRequest(http.MethodPost, "/api/v1/subscription/save", bytes.NewReader(saveBody)))
	var saveReply response.R
	if err := json.Unmarshal(saveRec.Body.Bytes(), &saveReply); err != nil || saveReply.Code != 0 {
		t.Fatalf("subscriptionSave failed: %v %+v", err, saveReply)
	}
	payload, _, err = h.buildKernelApplyPayload(sb)
	if err != nil {
		t.Fatal(err)
	}
	cfg = payload["files"].([]kernel.File)[0].Content
	if !strings.Contains(cfg, `"level": "info"`) {
		t.Fatalf("expected sing-box log.level=info when MaxOnlineIPs > 0, got:\n%s", cfg)
	}

	pull := func(method, xff, ua string) string {
		t.Helper()
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(method, "/api/v1/open_api/sub/"+sub.Token+"?format=clash", nil)
		req.RemoteAddr = "127.0.0.1:50000"
		req.Header.Set("X-Forwarded-For", xff)
		req.Header.Set("User-Agent", ua)
		h.openSubscription(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("unexpected status %d for %s %s", rec.Code, method, xff)
		}
		return rec.Body.String()
	}

	// IP 1 -> allowed
	if body := pull(http.MethodGet, "198.51.100.1", "ClashVerge/2.0"); strings.Contains(body, "拉取 IP 数量超限") {
		t.Fatalf("1st pull IP should be allowed")
	}
	// IP 2 (IPv6) -> allowed
	if body := pull(http.MethodGet, "2001:db8:aaaa:bbbb::1", "Shadowrocket/2000"); strings.Contains(body, "拉取 IP 数量超限") {
		t.Fatalf("2nd pull IP should be allowed")
	}
	// Same IPv6 /64 -> still allowed
	if body := pull(http.MethodGet, "2001:db8:aaaa:bbbb::9999", "Shadowrocket/2000"); strings.Contains(body, "拉取 IP 数量超限") {
		t.Fatalf("same IPv6 /64 should be deduplicated and allowed")
	}
	// IP 3 -> blocked!
	if body := pull(http.MethodGet, "198.51.100.9", "Mihomo/1.19"); !strings.Contains(body, "拉取 IP 数量超限") {
		t.Fatalf("3rd distinct pull IP must be blocked, got:\n%s", body)
	}
	// IP 3 repeated -> must remain blocked (blocked attempts don't enter allowed set!)
	if body := pull(http.MethodGet, "198.51.100.9", "Mihomo/1.19"); !strings.Contains(body, "拉取 IP 数量超限") {
		t.Fatalf("repeated blocked IP must remain blocked")
	}
	// IP 1 again -> still allowed
	if body := pull(http.MethodGet, "198.51.100.1", "ClashVerge/2.0"); strings.Contains(body, "拉取 IP 数量超限") {
		t.Fatalf("existing allowed IP should still be permitted")
	}
	// HEAD request -> ignored for pull logs
	_ = pull(http.MethodHead, "198.51.100.77", "Probe/1.0")

	// Seed one live online IP and call POST /api/v1/subscription/ip-activity
	now := time.Now()
	h.subOnlineIPs.updateNode(sb.NodeID, []agentUserOnlineIP{
		{ID: sub.ID, IP: "198.51.100.1", LastSeen: now.UnixMilli()},
	}, now)

	actBody, _ := json.Marshal(map[string]int64{"id": sub.ID})
	actRec := httptest.NewRecorder()
	h.subscriptionIPActivity(actRec, httptest.NewRequest(http.MethodPost, "/api/v1/subscription/ip-activity", bytes.NewReader(actBody)))
	var actResp struct {
		Code int `json:"code"`
		Data struct {
			OnlineIPs        []subscriptionOnlineIPView `json:"onlineIps"`
			PullLogs         []subscriptionPullLogView  `json:"pullLogs"`
			PullIPCount24h   int                        `json:"pullIpCount24h"`
			MaxOnlineIPs     int                        `json:"maxOnlineIps"`
			IPAction         string                     `json:"ipAction"`
			IPSuspendMinutes int                        `json:"ipSuspendMinutes"`
			MaxPullIPs       int                        `json:"maxPullIps"`
		} `json:"data"`
	}
	if err := json.Unmarshal(actRec.Body.Bytes(), &actResp); err != nil || actResp.Code != 0 {
		t.Fatalf("subscriptionIPActivity failed: %v body=%s", err, actRec.Body.String())
	}
	if len(actResp.Data.OnlineIPs) != 1 || actResp.Data.OnlineIPs[0].IP != "198.51.100.1" {
		t.Fatalf("unexpected onlineIps: %+v", actResp.Data.OnlineIPs)
	}
	if len(actResp.Data.OnlineIPs[0].NodeNames) != 1 || actResp.Data.OnlineIPs[0].NodeNames[0] != "ip-node-1" {
		t.Fatalf("unexpected nodeNames: %+v", actResp.Data.OnlineIPs[0].NodeNames)
	}
	if actResp.Data.PullIPCount24h != 2 {
		t.Fatalf("pullIpCount24h = %d, want 2", actResp.Data.PullIPCount24h)
	}
	if len(actResp.Data.PullLogs) != 6 {
		t.Fatalf("expected 6 GET pull logs (excluding HEAD), got %d", len(actResp.Data.PullLogs))
	}
	blockedCount := 0
	for _, l := range actResp.Data.PullLogs {
		if l.Blocked {
			blockedCount++
		}
	}
	if blockedCount != 2 {
		t.Fatalf("expected 2 blocked pull logs, got %d", blockedCount)
	}

	// Verify subscriptionList populates batch onlineIpCount and pullIpCount24h.
	listRec := httptest.NewRecorder()
	h.subscriptionList(listRec, httptest.NewRequest(http.MethodPost, "/api/v1/subscription/list", nil))
	var listResp struct {
		Code int `json:"code"`
		Data struct {
			Items []subscriptionView `json:"items"`
		} `json:"data"`
	}
	if err := json.Unmarshal(listRec.Body.Bytes(), &listResp); err != nil || listResp.Code != 0 {
		t.Fatalf("subscriptionList failed: %v body=%s", err, listRec.Body.String())
	}
	foundSub := false
	for _, it := range listResp.Data.Items {
		if it.ID == sub.ID {
			foundSub = true
			if it.OnlineIPCount != 1 || it.PullIPCount24h != 2 || it.MaxOnlineIPs != 3 || it.MaxPullIPs != 2 {
				t.Fatalf("unexpected subscriptionList IP stats: %+v", it)
			}
		}
	}
	if !foundSub {
		t.Fatalf("subscription %d missing from subscriptionList", sub.ID)
	}

	// Suspend manually and verify batch "unsuspend" clears it.
	until := time.Now().Add(30 * time.Minute).UnixMilli()
	if err := repository.SetSubscriptionIPSuspendedUntil(sub.ID, until, time.Now().UnixMilli()); err != nil {
		t.Fatal(err)
	}
	batchBody, _ := json.Marshal(subscriptionBatchRequest{IDs: []int64{sub.ID}, Action: "unsuspend"})
	batchRec := httptest.NewRecorder()
	h.subscriptionBatch(batchRec, httptest.NewRequest(http.MethodPost, "/api/v1/subscription/batch", bytes.NewReader(batchBody)))
	var batchReply response.R
	if err := json.Unmarshal(batchRec.Body.Bytes(), &batchReply); err != nil || batchReply.Code != 0 {
		t.Fatalf("batch unsuspend failed: %v %+v", err, batchReply)
	}
	got, _ := repository.GetSubscription(sub.ID)
	if got.IPSuspendedUntil != 0 {
		t.Fatalf("batch unsuspend must clear IPSuspendedUntil, got %d", got.IPSuspendedUntil)
	}
}
