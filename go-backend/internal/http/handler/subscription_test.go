package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"go-backend/internal/kernel"
	"go-backend/internal/store/model"
	"go-backend/internal/store/repo"
)

func TestSSSubscriptionsSharePortAndRevokeOnlyOverQuotaUser(t *testing.T) {
	repository, err := repo.Open(t.TempDir() + "/subscription-ss.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = repository.Close() })
	h := New(repository, "unit-test-secret")
	nodeID := seedRelayTargetNode(t, repository, "ss-node", "203.0.113.1")
	xray := seedChainKernel(t, repository, nodeID, kernel.TypeXray)
	inbound := seedChainInbound(t, repository, xray, "metered-ss", kernel.Inbound{
		Protocol: kernel.ProtocolShadowsocks, Port: 8388,
		Settings: kernel.Settings{Method: "aes-128-gcm", Password: "old-shared", SSSubscriptionUsers: true},
	}, 0)
	now := time.Now().UnixMilli()
	first := model.Subscription{Name: "first", Kind: model.SubscriptionShare, Token: strings.Repeat("a", 32),
		Password: "first-password", SourceAll: 1, Enabled: 1, TrafficLimit: 1000, CreatedTime: now, UpdatedTime: now}
	second := model.Subscription{Name: "second", Kind: model.SubscriptionShare, Token: strings.Repeat("b", 32),
		Password: "second-password", SourceAll: 1, Enabled: 1, CreatedTime: now, UpdatedTime: now}
	group := model.NodeGroup{Name: "shared-ss", SourceAll: 1, CreatedTime: now, UpdatedTime: now}
	if err := repository.SaveNodeGroup(&group); err != nil {
		t.Fatal(err)
	}
	for _, sub := range []*model.Subscription{&first, &second} {
		sub.GroupIDs = repo.FormatGroupIDs([]int64{group.ID})
		if err := repository.SaveSubscription(sub); err != nil {
			t.Fatal(err)
		}
	}
	context, err := h.loadSubContext()
	if err != nil {
		t.Fatal(err)
	}
	draft := &model.Subscription{GroupIDs: first.GroupIDs}
	if endpoints := h.subNodes(context, draft); len(endpoints) != 1 || endpoints[0].OrderKey == "" {
		t.Fatal("the metered SS node must appear in the subscription preview")
	}
	for _, sub := range []*model.Subscription{&first, &second} {
		endpoints := h.subNodes(context, sub)
		if len(endpoints) != 1 || endpoints[0].Port != inbound.Port || endpoints[0].Inbound.Settings.Password != sub.Password {
			t.Fatalf("subscription %d did not get its own password on the same port: %+v", sub.ID, endpoints)
		}
		if uri, ok := kernel.ShareURI(endpoints[0]); !ok || !strings.HasPrefix(uri, "ss://") {
			t.Errorf("subscription %d did not get a normal SS URI", sub.ID)
		}
	}
	if endpoints, _ := h.inboundEndpoints([]model.NodeInbound{*inbound}); len(endpoints) != 0 {
		t.Fatal("metered SS must not expose a shared endpoint")
	}
	if _, err := h.chainEndpoint(inbound, nodeID); err == nil {
		t.Fatal("metered SS must not be used as a shared chain target")
	}
	config := func() string {
		t.Helper()
		payload, _, err := h.buildKernelApplyPayload(xray)
		if err != nil {
			t.Fatal(err)
		}
		return payload["files"].([]kernel.File)[0].Content
	}
	if rendered := config(); strings.Contains(rendered, "old-shared") ||
		!strings.Contains(rendered, first.Password) || !strings.Contains(rendered, second.Password) {
		t.Fatal("both subscription passwords and no shared password should be accepted")
	}
	h.applyUserCounters(nodeID, "epoch", []agentUserCounter{
		{ID: first.ID, Dir: "down", Bytes: 1001}, {ID: second.ID, Dir: "down", Bytes: 100},
	}, now)
	for _, check := range []struct {
		token string
		info  string
	}{
		{first.Token, "upload=0; download=1001; total=1000; expire=0"},
		{second.Token, "upload=0; download=100; total=0; expire=0"},
	} {
		export := httptest.NewRecorder()
		h.openSubscription(export, httptest.NewRequest(http.MethodGet, "/api/v1/open_api/sub/"+check.token, nil))
		if export.Code != http.StatusOK || export.Header().Get("Subscription-Userinfo") != check.info {
			t.Fatalf("each subscription must export its own usage: %d %v", export.Code, export.Header())
		}
	}
	if rendered := config(); strings.Contains(rendered, first.Password) || !strings.Contains(rendered, second.Password) {
		t.Fatal("quota should revoke only the first password")
	}
	second.Enabled = 0
	if err := repository.SaveSubscription(&second); err != nil {
		t.Fatal(err)
	}
	defaultSub, err := repository.DefaultSubscription(now)
	if err != nil {
		t.Fatal(err)
	}
	defaultSub.Enabled = 0
	if err := repository.SaveSubscription(defaultSub); err != nil {
		t.Fatal(err)
	}
	if rendered := config(); strings.Contains(rendered, `"protocol": "shadowsocks"`) {
		t.Fatal("without active subscriptions, the SS port should not listen")
	}
}

func TestSubscriptionPreviewOrderMatchesSavedExportAndDoesNotChangeGroup(t *testing.T) {
	repository, err := repo.Open(t.TempDir() + "/subscription-order.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = repository.Close() })
	h := New(repository, "unit-test-secret")
	nodeID := seedRelayTargetNode(t, repository, "ordered-node", "203.0.113.1")
	xray := seedChainKernel(t, repository, nodeID, kernel.TypeXray)
	for index, name := range []string{"first", "second", "third"} {
		seedChainInbound(t, repository, xray, name, kernel.Inbound{
			Protocol: kernel.ProtocolShadowsocks, Port: 8388 + index, Settings: kernel.Settings{Method: "aes-128-gcm", Password: "owner"},
		}, 0)
	}
	group := model.NodeGroup{Name: "sorted", SourceAll: 1, Options: `{"sort":"name"}`}
	if err := repository.SaveNodeGroup(&group); err != nil {
		t.Fatal(err)
	}
	preview := func(order []string) []subPreviewItem {
		t.Helper()
		payload, err := json.Marshal(subscriptionRequest{GroupIDs: []int64{group.ID}, Options: subPipeline{NodeOrder: order}})
		if err != nil {
			t.Fatal(err)
		}
		response := httptest.NewRecorder()
		h.subscriptionPreview(response, httptest.NewRequest(http.MethodPost, "/subscription/preview", strings.NewReader(string(payload))))
		var result struct {
			Code int
			Data struct{ Items []subPreviewItem }
		}
		if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil || result.Code != 0 || len(result.Data.Items) != 3 {
			t.Fatalf("bad preview: %s (%v)", response.Body.String(), err)
		}
		return result.Data.Items
	}
	defaultItems := preview(nil)
	order := []string{defaultItems[1].OrderKey, defaultItems[0].OrderKey}
	sorted := preview(order)
	if !slices.Equal([]string{sorted[0].Name, sorted[1].Name, sorted[2].Name}, []string{"second", "first", "third"}) {
		t.Fatalf("preview order mismatch: %+v", sorted)
	}
	pipe := subPipeline{NodeOrder: order}
	if err := pipe.normalize(); err != nil {
		t.Fatal(err)
	}
	options, err := json.Marshal(pipe)
	if err != nil {
		t.Fatal(err)
	}
	subscription := &model.Subscription{
		Name: "ordered", Kind: model.SubscriptionShare, Password: "own", Token: strings.Repeat("a", 32),
		GroupIDs: repo.FormatGroupIDs([]int64{group.ID}), Options: string(options), Enabled: 1,
	}
	if err := repository.SaveSubscription(subscription); err != nil {
		t.Fatal(err)
	}
	export := httptest.NewRecorder()
	h.openSubscription(export, httptest.NewRequest(http.MethodGet, "/api/v1/open_api/sub/"+subscription.Token+"?format=clash", nil))
	body := export.Body.String()
	second := strings.Index(body, `name: "second"`)
	first := strings.Index(body, `name: "first"`)
	third := strings.Index(body, `name: "third"`)
	if export.Code != http.StatusOK || !(0 <= second && second < first && first < third) {
		t.Fatalf("export order mismatch: %d\n%s", export.Code, body)
	}
	if after := preview(nil); !slices.Equal(after, defaultItems) {
		t.Fatalf("saving the subscription must not reorder its node group: %+v", after)
	}
}

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
