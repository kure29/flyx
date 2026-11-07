package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"go-backend/internal/store/model"
)

func TestNodeNotificationSettingsCRUD(t *testing.T) {
	h, r, _ := setupNotifyHandler(t)
	request := func(fn http.HandlerFunc, body map[string]interface{}) int {
		t.Helper()
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		w := httptest.NewRecorder()
		fn(w, httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(raw)))
		var res struct {
			Code int `json:"code"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
			t.Fatal(err)
		}
		return res.Code
	}
	body := map[string]interface{}{
		"name": "custom-notify", "serverIp": "198.51.100.10",
		"notifyEvents": map[string]bool{"latency_bad": false, "node_offline": true},
	}
	if code := request(h.nodeCreate, body); code != 0 {
		t.Fatalf("create code = %d", code)
	}
	items, err := r.ListNodes()
	if err != nil || len(items) != 1 {
		t.Fatalf("nodes = %v, %v", items, err)
	}
	id := asInt64(items[0]["id"], 0)
	body["id"] = id
	assertSaved := func(want string) {
		t.Helper()
		node, err := r.GetNodeByID(id)
		if err != nil || node == nil || node.NotifyEvents != want {
			t.Fatalf("saved node = %+v, %v; want %s", node, err, want)
		}
	}
	const saved = `{"latency_bad":false,"node_offline":true}`
	assertSaved(saved)
	w := httptest.NewRecorder()
	h.nodeList(w, httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{}`)))
	var list struct {
		Data []struct {
			NotifyEvents map[string]bool `json:"notifyEvents"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &list); err != nil || len(list.Data) != 1 {
		t.Fatalf("node list = %s, %v", w.Body.String(), err)
	}
	if enabled, exists := list.Data[0].NotifyEvents[notifyEventLatencyBad]; !exists || enabled || !list.Data[0].NotifyEvents[notifyEventNodeOffline] {
		t.Fatalf("list overrides = %v", list.Data[0].NotifyEvents)
	}

	// Older clients that omit the field must preserve custom notification settings.
	delete(body, "notifyEvents")
	body["name"] = "renamed"
	if code := request(h.nodeUpdate, body); code != 0 {
		t.Fatalf("update code = %d", code)
	}
	assertSaved(saved)
	for _, invalid := range []interface{}{
		"latency_bad", []string{"latency_bad"},
		map[string]interface{}{"latency_bad": "false"},
		map[string]interface{}{"latency_bad": nil},
		map[string]interface{}{"daily_report": false},
	} {
		body["notifyEvents"] = invalid
		if code := request(h.nodeUpdate, body); code == 0 {
			t.Fatalf("accepted invalid settings: %#v", invalid)
		}
		assertSaved(saved)
		if code := request(h.nodeCreate, body); code == 0 {
			t.Fatalf("created node with invalid settings: %#v", invalid)
		}
	}
	for _, reset := range []interface{}{nil, map[string]bool{}} {
		body["notifyEvents"] = map[string]bool{"latency_bad": false}
		if code := request(h.nodeUpdate, body); code != 0 {
			t.Fatalf("set override code = %d", code)
		}
		body["notifyEvents"] = reset
		if code := request(h.nodeUpdate, body); code != 0 {
			t.Fatalf("reset code = %d", code)
		}
		assertSaved("")
	}
}

func TestNodeNotificationOverridesRespectGlobalMaster(t *testing.T) {
	h, r, _ := setupNotifyHandler(t)
	nodeID := mustCreateLatencyNode(t, r, "custom", 1, 0)
	otherID := mustCreateLatencyNode(t, r, "default", 1, 0)
	events := []string{notifyEventNodeOffline, notifyEventInboundDown, notifyEventLatencyBad,
		notifyEventRouteChange, notifyEventKernelError, notifyEventNodeExpiry}
	setConfig := func(key, value string) {
		t.Helper()
		if err := r.UpsertConfig(key, value, time.Now().UnixMilli()); err != nil {
			t.Fatal(err)
		}
	}
	for _, event := range events {
		for _, enabled := range []bool{false, true} {
			// Make the server setting disagree with the global default.
			global := ""
			if enabled {
				global = "none"
			}
			setConfig(notifyConfigEvents, global)
			raw, _ := json.Marshal(map[string]bool{event: enabled})
			if err := r.DB().Model(&model.Node{}).Where("id = ?", nodeID).Update("notify_events", string(raw)).Error; err != nil {
				t.Fatal(err)
			}
			if _, _, ok := h.notifyConfigForNode(nodeID, event); ok != enabled {
				t.Fatalf("%s override %v: enabled = %v", event, enabled, ok)
			}
			if _, _, ok := h.notifyConfigForNode(otherID, event); ok == enabled {
				t.Fatalf("%s changed another server's default", event)
			}
		}
	}
	setConfig(notifyConfigEnabled, "false")
	if _, _, ok := h.notifyConfigForNode(nodeID, notifyEventNodeExpiry); ok {
		t.Fatal("server override must not bypass the master switch")
	}
	setConfig(notifyConfigEnabled, "true")
	setConfig(notifyConfigWebhookURL, "")
	if _, _, ok := h.notifyConfigForNode(nodeID, notifyEventNodeExpiry); ok {
		t.Fatal("server override must still require a delivery channel")
	}
}

func TestLatencyNotificationsCanBeMutedForOneServer(t *testing.T) {
	h, r, sent := setupNotifyHandler(t)
	if err := r.UpsertConfig(latencyTargetsConfigKey, `[{"name":"电信","host":"ct.example.com","port":80,"enabled":true}]`, time.Now().UnixMilli()); err != nil {
		t.Fatal(err)
	}
	mutedID := mustCreateLatencyNode(t, r, "静音服务器", 1, 0)
	mustCreateLatencyNode(t, r, "正常通知服务器", 1, 0)
	if err := r.DB().Model(&model.Node{}).Where("id = ?", mutedID).
		Update("notify_events", `{"latency_bad":false}`).Error; err != nil {
		t.Fatal(err)
	}
	healthy := false
	stubLatencyPing(t, func(int64, string) (map[string]interface{}, error) {
		if !healthy {
			return nil, context.DeadlineExceeded
		}
		return map[string]interface{}{"success": true, "averageTime": 30.0, "packetLoss": 0.0}, nil
	})
	for i := 0; i < latencyAlertStreak; i++ {
		if _, err := h.probeNodeLatency(context.Background(), nil); err != nil {
			t.Fatal(err)
		}
	}
	msgs := sent()
	if len(msgs) != 1 || !strings.Contains(msgs[0].Message, "正常通知服务器") || !strings.Contains(msgs[0].Message, "超时") {
		t.Fatalf("timeout messages = %+v", msgs)
	}
	if samples, err := r.ListNodeLatencySamples(mutedID, 0); err != nil || len(samples) != latencyAlertStreak {
		t.Fatalf("muted server must retain latency history: %v, %v", samples, err)
	}
	healthy = true
	if _, err := h.probeNodeLatency(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	msgs = sent()
	if len(msgs) != 2 || msgs[1].Title != "延迟已恢复" || !strings.Contains(msgs[1].Message, "正常通知服务器") {
		t.Fatalf("recovery messages = %+v", msgs)
	}
	// Muting latency must leave this server's other event notifications enabled.
	h.notifyForNode(mutedID, notifyEventKernelError, "内核运行出错", "静音服务器")
	if msgs := sent(); len(msgs) != 3 || msgs[2].Event != notifyEventKernelError {
		t.Fatalf("unrelated event was muted: %+v", msgs)
	}
}

func TestMutedIncidentsStayQuietOnRecovery(t *testing.T) {
	old := nodeOfflineGrace
	nodeOfflineGrace = 20 * time.Millisecond
	t.Cleanup(func() { nodeOfflineGrace = old })
	h, r, sent := setupNotifyHandler(t)
	nodeID := mustCreateLatencyNode(t, r, "muted", 0, 0)
	setOverrides := func(raw string) {
		t.Helper()
		if err := r.DB().Model(&model.Node{}).Where("id = ?", nodeID).Update("notify_events", raw).Error; err != nil {
			t.Fatal(err)
		}
	}

	// An outage that started muted must not end with a lone recovery.
	setOverrides(`{"node_offline":false,"inbound_down":false}`)
	h.notifyNodeOffline(nodeID)
	time.Sleep(60 * time.Millisecond)
	h.notifyIncident(nodeID, notifyEventInboundDown, []string{"1"}, "节点连不上", "muted")
	setOverrides("")
	h.notifyNodeOnline(nodeID)
	if h.incidentAlerted(notifyEventInboundDown, "1") {
		t.Fatal("muted inbound outage reported as alerted")
	}
	if msgs := sent(); len(msgs) != 0 {
		t.Fatalf("muted incidents sent: %+v", msgs)
	}

	// Alerts that were sent still get their recovery.
	h.notifyNodeOffline(nodeID)
	time.Sleep(60 * time.Millisecond)
	h.notifyIncident(nodeID, notifyEventInboundDown, []string{"1"}, "节点连不上", "sent")
	if msgs := sent(); len(msgs) != 2 {
		t.Fatalf("alerts = %+v", msgs)
	}
	h.notifyNodeOnline(nodeID)
	if !h.incidentAlerted(notifyEventInboundDown, "1") {
		t.Fatal("sent inbound outage not reported as alerted")
	}
	if msgs := sent(); len(msgs) != 3 || msgs[2].Title != "服务器恢复在线" {
		t.Fatalf("messages = %+v", msgs)
	}
}

func TestMutedExpiryReminderFiresOnceEnabled(t *testing.T) {
	h, r, sent := setupNotifyHandler(t)
	now := time.Now()
	node := model.Node{Name: "expiring", Secret: "s", ServerIP: "198.51.100.2", NotifyEvents: `{"node_expiry":false}`}
	node.ExpiryTime.Int64, node.ExpiryTime.Valid = now.Add(48*time.Hour).UnixMilli(), true
	if err := r.DB().Create(&node).Error; err != nil {
		t.Fatal(err)
	}
	h.checkNodeExpiry(now)
	if msgs := sent(); len(msgs) != 0 {
		t.Fatalf("muted reminder sent: %+v", msgs)
	}
	if err := r.DB().Model(&model.Node{}).Where("id = ?", node.ID).Update("notify_events", "").Error; err != nil {
		t.Fatal(err)
	}
	h.checkNodeExpiry(now)
	h.checkNodeExpiry(now)
	if msgs := sent(); len(msgs) != 1 || msgs[0].Event != notifyEventNodeExpiry {
		t.Fatalf("reminder after enabling = %+v", msgs)
	}
}

func TestNodeEventFollowsGlobalWhenNodeIsMissing(t *testing.T) {
	h, _, _ := setupNotifyHandler(t)
	if _, _, ok := h.notifyConfigForNode(404, notifyEventNodeOffline); !ok {
		t.Fatal("missing node must keep the global selection")
	}
}
