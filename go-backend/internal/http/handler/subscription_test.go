package handler

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"go-backend/internal/http/response"
	"go-backend/internal/kernel"
	"go-backend/internal/store/model"
	"go-backend/internal/store/repo"
)

func TestSS2022InboundKeyOnEditAndModeChange(t *testing.T) {
	repository, err := repo.Open(t.TempDir() + "/ss2022-edit.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = repository.Close() })
	h := New(repository, "unit-test-secret")
	nodeID := seedRelayTargetNode(t, repository, "ss-node", "203.0.113.1")
	xray := seedChainKernel(t, repository, nodeID, kernel.TypeXray)
	sharedKey := kernel.ShadowsocksPassword(kernel.DefaultSSMethod)
	row := seedChainInbound(t, repository, xray, "ss2022", kernel.Inbound{
		Protocol: kernel.ProtocolShadowsocks, Port: 8388, Settings: kernel.Settings{Method: kernel.DefaultSSMethod, Password: sharedKey},
	}, 0)
	update := func(metered bool, password string) string {
		t.Helper()
		payload, err := json.Marshal(inboundRequest{
			ID: row.ID, Name: row.Name, Protocol: row.Protocol, Port: row.Port,
			Settings: kernel.Settings{Method: kernel.DefaultSSMethod, Password: password, SSSubscriptionUsers: metered},
		})
		if err != nil {
			t.Fatal(err)
		}
		result := httptest.NewRecorder()
		h.inboundUpdate(result, httptest.NewRequest(http.MethodPost, "/api/v1/inbound/update", bytes.NewReader(payload)))
		var reply response.R
		if err := json.Unmarshal(result.Body.Bytes(), &reply); err != nil || reply.Code != 0 {
			t.Fatalf("edit failed: %s, %v", reply.Msg, err)
		}
		stored, err := repository.GetNodeInbound(row.ID)
		if err != nil || stored == nil {
			t.Fatalf("read saved inbound: %v", err)
		}
		inbound, err := inboundFromModel(*stored)
		if err != nil || inbound.Settings.SSSubscriptionUsers != metered || inbound.Settings.Password == "" {
			t.Fatalf("invalid saved credentials: %v", err)
		}
		return inbound.Settings.Password
	}
	serverKey := update(true, sharedKey)
	if serverKey == sharedKey || update(true, "") != serverKey {
		t.Fatal("enabling must rotate the old shared key, but subsequent edits must keep it stable")
	}
	if update(false, serverKey) == serverKey {
		t.Fatal("disabling must not let the old server key become a usable shared password")
	}
}

func TestSSSubscriptionsSharePortAndRevokeOnlyOverQuotaUser(t *testing.T) {
	for _, method := range []string{"aes-128-gcm", "2022-blake3-aes-128-gcm", "2022-blake3-aes-256-gcm"} {
		t.Run(method, func(t *testing.T) { testSSSubscriptionPort(t, method) })
	}
}

func testSSSubscriptionPort(t *testing.T, method string) {
	t.Helper()
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
		Settings: kernel.Settings{Method: method, SSSubscriptionUsers: true},
	}, 0)
	stored, err := inboundFromModel(*inbound)
	if err != nil {
		t.Fatal(err)
	}
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
	passwords := make(map[int64]string, 2)
	for _, sub := range []*model.Subscription{&first, &second} {
		endpoints := h.subNodes(context, sub)
		if len(endpoints) != 1 || endpoints[0].Port != inbound.Port {
			t.Fatalf("subscription %d did not get the shared port", sub.ID)
		}
		password := endpoints[0].Inbound.Settings.Password
		uri, ok := kernel.ShareURI(endpoints[0])
		if !ok || !strings.HasPrefix(uri, "ss://") {
			t.Fatalf("subscription %d did not get a normal SS URI", sub.ID)
		}
		if kernel.SS2022KeyLen(method) > 0 {
			parsed, err := url.Parse(uri)
			if err != nil {
				t.Fatal("SS2022 URI cannot be parsed")
			}
			linkPassword, _ := parsed.User.Password()
			serverKey, userKey, combined := strings.Cut(password, ":")
			if parsed.User.Username() != method || linkPassword != password || !combined || serverKey != stored.Settings.Password || userKey == "" || userKey == sub.Password {
				t.Fatalf("subscription %d did not get the encoded server and user keys", sub.ID)
			}
			password = userKey
		} else if password != sub.Password {
			t.Fatalf("subscription %d did not get its legacy password", sub.ID)
		}
		passwords[sub.ID] = password
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
	if rendered := config(); passwords[first.ID] == passwords[second.ID] ||
		!strings.Contains(rendered, passwords[first.ID]) || !strings.Contains(rendered, passwords[second.ID]) {
		t.Fatal("both independent credentials must be configured")
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
	if rendered := config(); strings.Contains(rendered, passwords[first.ID]) || !strings.Contains(rendered, passwords[second.ID]) {
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

func TestSubscriptionTagsPinAndBatch(t *testing.T) {
	r, err := repo.Open(filepath.Join(t.TempDir(), "sub-batch.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = r.Close() })
	h := New(r, "secret")

	post := func(fn func(http.ResponseWriter, *http.Request), body string) map[string]interface{} {
		t.Helper()
		rec := httptest.NewRecorder()
		fn(rec, httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body)))
		var out map[string]interface{}
		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
			t.Fatalf("decode response %q: %v", rec.Body.String(), err)
		}
		if code, _ := out["code"].(float64); code != 0 {
			t.Fatalf("unexpected error response: %s", rec.Body.String())
		}
		return out
	}

	g := &model.NodeGroup{Name: "拼车组", SourceAll: 1, CreatedTime: 1, UpdatedTime: 1}
	if err := r.SaveNodeGroup(g); err != nil {
		t.Fatalf("save group: %v", err)
	}

	// Create two subscriptions with group, pinned and tags (including Chinese comma + dupes).
	res1 := post(h.subscriptionSave, fmt.Sprintf(`{"name":"车友A","groupIds":[%d],"group":" 拼车客户 ","pinned":true,"tags":["拼车，月付"," 拼车 ","VIP"]}`, g.ID))
	data1 := res1["data"].(map[string]interface{})
	id1 := int64(data1["id"].(float64))
	if pinned, _ := data1["pinned"].(bool); !pinned {
		t.Fatalf("expected pinned=true, got %+v", data1)
	}
	if grp, _ := data1["group"].(string); grp != "拼车客户" {
		t.Fatalf("expected trimmed group=拼车客户, got %q", grp)
	}
	rawTags := data1["tags"].([]interface{})
	if len(rawTags) != 3 || rawTags[0] != "拼车" || rawTags[1] != "月付" || rawTags[2] != "VIP" {
		t.Fatalf("unexpected normalized tags: %+v", rawTags)
	}

	// Partial save omitting group, pinned & tags must preserve existing values.
	res1b := post(h.subscriptionSave, fmt.Sprintf(`{"id":%d,"name":"车友A改名","groupIds":[%d]}`, id1, g.ID))
	data1b := res1b["data"].(map[string]interface{})
	if pinned, _ := data1b["pinned"].(bool); !pinned {
		t.Fatalf("expected pinned preserved on partial save: %+v", data1b)
	}
	if grp, _ := data1b["group"].(string); grp != "拼车客户" {
		t.Fatalf("expected group preserved on partial save, got %q", grp)
	}
	if len(data1b["tags"].([]interface{})) != 3 {
		t.Fatalf("expected tags preserved on partial save: %+v", data1b["tags"])
	}

	res2 := post(h.subscriptionSave, fmt.Sprintf(`{"name":"车友B","groupIds":[%d]}`, g.ID))
	id2 := int64(res2["data"].(map[string]interface{})["id"].(float64))

	// Batch disable, extend-days, add-tag, remove-tag, set-group, pin/unpin.
	post(h.subscriptionBatch, fmt.Sprintf(`{"ids":[%d,%d],"action":"disable"}`, id1, id2))
	s1, _ := r.GetSubscription(id1)
	s2, _ := r.GetSubscription(id2)
	if s1.Enabled != 0 || s2.Enabled != 0 {
		t.Fatalf("batch disable failed: s1=%d s2=%d", s1.Enabled, s2.Enabled)
	}

	post(h.subscriptionBatch, fmt.Sprintf(`{"ids":[%d,%d],"action":"enable"}`, id1, id2))
	post(h.subscriptionBatch, fmt.Sprintf(`{"ids":[%d,%d],"action":"extend-days","days":30}`, id1, id2))
	s1, _ = r.GetSubscription(id1)
	if s1.ExpireTime <= time.Now().Add(29*24*time.Hour).UnixMilli() {
		t.Fatalf("expected expireTime extended by ~30 days, got %d", s1.ExpireTime)
	}
	prevExp := s1.ExpireTime
	post(h.subscriptionBatch, fmt.Sprintf(`{"ids":[%d],"action":"extend-days","days":30}`, id1))
	s1, _ = r.GetSubscription(id1)
	if s1.ExpireTime != prevExp+30*86400*1000 {
		t.Fatalf("expected active expiry extended by exact 30d: got %d want %d", s1.ExpireTime, prevExp+30*86400*1000)
	}

	post(h.subscriptionBatch, fmt.Sprintf(`{"ids":[%d,%d],"action":"set-group","group":" 自用分组 "}`, id1, id2))
	s1, _ = r.GetSubscription(id1)
	s2, _ = r.GetSubscription(id2)
	if s1.GroupName != "自用分组" || s2.GroupName != "自用分组" {
		t.Fatalf("batch set-group failed: s1=%q s2=%q", s1.GroupName, s2.GroupName)
	}
	post(h.subscriptionBatch, fmt.Sprintf(`{"ids":[%d],"action":"set-group","group":""}`, id2))
	s2, _ = r.GetSubscription(id2)
	if s2.GroupName != "" {
		t.Fatalf("expected batch set-group empty to clear group, got %q", s2.GroupName)
	}

	post(h.subscriptionBatch, fmt.Sprintf(`{"ids":[%d,%d],"action":"add-tag","tags":["续费提醒"]}`, id1, id2))
	s2, _ = r.GetSubscription(id2)
	if s2.Tags != "续费提醒" {
		t.Fatalf("expected s2 tags=续费提醒, got %q", s2.Tags)
	}
	post(h.subscriptionBatch, fmt.Sprintf(`{"ids":[%d],"action":"remove-tag","tags":["VIP"]}`, id1))
	s1, _ = r.GetSubscription(id1)
	if strings.Contains(s1.Tags, "VIP") || !strings.Contains(s1.Tags, "续费提醒") {
		t.Fatalf("unexpected s1 tags after add/remove: %q", s1.Tags)
	}

	post(h.subscriptionBatch, fmt.Sprintf(`{"ids":[%d],"action":"unpin"}`, id1))
	post(h.subscriptionBatch, fmt.Sprintf(`{"ids":[%d],"action":"pin"}`, id2))
	s1, _ = r.GetSubscription(id1)
	s2, _ = r.GetSubscription(id2)
	if s1.Pinned != 0 || s2.Pinned != 1 {
		t.Fatalf("batch pin/unpin failed: s1=%d s2=%d", s1.Pinned, s2.Pinned)
	}
}
