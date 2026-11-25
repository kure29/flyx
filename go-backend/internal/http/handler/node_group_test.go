package handler

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"go-backend/internal/http/middleware"
	"go-backend/internal/http/response"
	"go-backend/internal/store/model"
	"go-backend/internal/store/repo"
)

// A share subscription's credentials follow its node group: changing the
// group's inbounds moves them between kernels.
func TestNodeGroupChangeMovesShareCredentials(t *testing.T) {
	r, err := repo.Open(t.TempDir() + "/groups.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = r.Close() })
	h := New(r, "unit-test-secret")
	mux := http.NewServeMux()
	h.Register(mux)
	router := middleware.JWT(middleware.AuthOptions{JWTSecret: "unit-test-secret", GetUserAuthState: h.GetUserAuthState})(mux)
	admin := mustGenerateConfigAccessToken(t, 1, "admin_user", 0)
	post := func(path, body string) response.R {
		req := httptest.NewRequest(http.MethodPost, path, bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", admin)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		var out response.R
		_ = json.NewDecoder(rec.Body).Decode(&out)
		return out
	}

	now := time.Now().UnixMilli()
	if err := r.DB().Exec(`INSERT INTO node(name, secret, server_ip, port, created_time, status) VALUES('n', 's', '203.0.113.1', '1000-2000', ?, 1)`, now).Error; err != nil {
		t.Fatal(err)
	}
	nodeID := mustLastInsertID(t, r, "node")
	if err := r.DB().Exec(`INSERT INTO node_kernel(node_id, kernel_type, version, status, created_time, updated_time) VALUES(?, 'xray', 'latest', 'pending', ?, ?)`, nodeID, now, now).Error; err != nil {
		t.Fatal(err)
	}
	kernelID := mustLastInsertID(t, r, "kernel")
	inbound := func(port int) int64 {
		if err := r.DB().Exec(`INSERT INTO node_inbound(kernel_id, node_id, name, protocol, listen, port, settings, created_time, updated_time)
			VALUES(?, ?, ?, 'trojan', '::', ?, '{"password":"owner-pw"}', ?, ?)`, kernelID, nodeID, "in"+strconv.Itoa(port), port, now, now).Error; err != nil {
			t.Fatal(err)
		}
		return mustLastInsertID(t, r, "node_inbound")
	}
	a, b := inbound(443), inbound(8443)

	out := post("/api/v1/subscription/group/save", `{"name":"A","inboundIds":[`+strconv.FormatInt(a, 10)+`]}`)
	if out.Code != 0 {
		t.Fatalf("group: %+v", out)
	}
	group := int64(out.Data.(map[string]interface{})["id"].(float64))
	out = post("/api/v1/subscription/save", `{"name":"小明","groupIds":[`+strconv.FormatInt(group, 10)+`]}`)
	if out.Code != 0 {
		t.Fatalf("subscription: %+v", out)
	}
	sub := int64(out.Data.(map[string]interface{})["id"].(float64))
	has := func(inbound int64) bool {
		for _, u := range h.activeUsersByInbound(time.Now())[inbound] {
			if u.ID == sub {
				return true
			}
		}
		return false
	}
	if !has(a) || has(b) {
		t.Fatal("credentials on the group's inbound only")
	}

	out = post("/api/v1/subscription/group/save", `{"id":`+strconv.FormatInt(group, 10)+`,"name":"A","inboundIds":[`+strconv.FormatInt(b, 10)+`]}`)
	if out.Code != 0 {
		t.Fatalf("group update: %+v", out)
	}
	if has(a) || !has(b) {
		t.Fatal("credentials must follow the group to the new inbound")
	}
	if g := mustGroup(t, r, group); g.Inx == 0 || g.CreatedTime == 0 {
		t.Fatalf("an update keeps the group's order and creation time: %+v", g)
	}
	if !strings.Contains(post("/api/v1/subscription/group/delete", `{"id":`+strconv.FormatInt(group, 10)+`}`).Msg, "小明") {
		t.Fatal("deleting a group in use names the subscription")
	}
}

func mustGroup(t *testing.T, r *repo.Repository, id int64) *model.NodeGroup {
	t.Helper()
	g, err := r.GetNodeGroup(id)
	if err != nil || g == nil {
		t.Fatalf("group %d: %v", id, err)
	}
	return g
}

func TestNodeGroupExplicitLineSelection(t *testing.T) {
	r, err := repo.Open(t.TempDir() + "/groups_explicit.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = r.Close() })
	h := New(r, "unit-test-secret")
	mux := http.NewServeMux()
	h.Register(mux)
	router := middleware.JWT(middleware.AuthOptions{JWTSecret: "unit-test-secret", GetUserAuthState: h.GetUserAuthState})(mux)
	admin := mustGenerateConfigAccessToken(t, 1, "admin_user", 0)
	post := func(path, body string) response.R {
		req := httptest.NewRequest(http.MethodPost, path, bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", admin)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		var out response.R
		_ = json.NewDecoder(rec.Body).Decode(&out)
		return out
	}

	now := time.Now().UnixMilli()
	// Landing node + entry relay node.
	if err := r.DB().Exec(`INSERT INTO node(name, secret, server_ip, port, created_time, status) VALUES('jp', 's1', '203.0.113.10', '1000-9000', ?, 1)`, now).Error; err != nil {
		t.Fatal(err)
	}
	landingNodeID := mustLastInsertID(t, r, "node_jp")
	if err := r.DB().Exec(`INSERT INTO node(name, secret, server_ip, port, created_time, status) VALUES('hk', 's2', '198.51.100.20', '1000-9000', ?, 1)`, now).Error; err != nil {
		t.Fatal(err)
	}
	entryNodeID := mustLastInsertID(t, r, "node_hk")
	if err := r.DB().Exec(`INSERT INTO node_kernel(node_id, kernel_type, version, status, created_time, updated_time) VALUES(?, 'xray', 'latest', 'running', ?, ?)`, landingNodeID, now, now).Error; err != nil {
		t.Fatal(err)
	}
	kernelID := mustLastInsertID(t, r, "kernel")
	if err := r.DB().Exec(`INSERT INTO node_inbound(kernel_id, node_id, name, protocol, listen, port, settings, enabled, created_time, updated_time)
		VALUES(?, ?, 'JP-落地', 'trojan', '::', 443, '{"password":"owner-pw"}', 1, ?, ?)`, kernelID, landingNodeID, now, now).Error; err != nil {
		t.Fatal(err)
	}
	inboundID := mustLastInsertID(t, r, "node_inbound")

	if err := r.DB().Exec(`INSERT INTO tunnel(name, type, protocol, flow, status, created_time, updated_time, inx) VALUES('T1', 1, 'tls', 1, 1, ?, ?, 1)`, now, now).Error; err != nil {
		t.Fatal(err)
	}
	tunnelID := mustLastInsertID(t, r, "tunnel")

	addRelay := func(name string, port int) int64 {
		if err := r.DB().Exec(`INSERT INTO forward(user_id, user_name, name, tunnel_id, remote_addr, strategy, status, created_time, updated_time, inx) VALUES(1, 'admin_user', ?, ?, '203.0.113.10:443', 'fifo', 1, ?, ?, 1)`, name, tunnelID, now, now).Error; err != nil {
			t.Fatal(err)
		}
		fwdID := mustLastInsertID(t, r, "forward")
		if err := r.DB().Exec(`INSERT INTO forward_port(forward_id, node_id, port) VALUES(?, ?, ?)`, fwdID, entryNodeID, port).Error; err != nil {
			t.Fatal(err)
		}
		if err := r.DB().Exec(`INSERT INTO node_inbound_relay(inbound_id, forward_id, name, created_time) VALUES(?, ?, ?, ?)`, inboundID, fwdID, name, now).Error; err != nil {
			t.Fatal(err)
		}
		return mustLastInsertID(t, r, "node_inbound_relay")
	}
	relay1 := addRelay("HK-中转1", 20001)
	relay2 := addRelay("SG-中转2", 20002)

	previewNames := func(body string) []string {
		out := post("/api/v1/subscription/group/preview", body)
		if out.Code != 0 {
			t.Fatalf("preview failed: %+v", out)
		}
		items := out.Data.(map[string]interface{})["items"].([]interface{})
		var names []string
		for _, it := range items {
			names = append(names, it.(map[string]interface{})["name"].(string))
		}
		return names
	}

	// 1. Pick ONLY relay1: landing direct and relay2 must NOT appear.
	got := previewNames(`{"name":"only-r1","sourceAll":false,"options":{"explicitLines":true,"directIds":[],"relayIds":[` + strconv.FormatInt(relay1, 10) + `]}}`)
	if len(got) != 1 || got[0] != "HK-中转1" {
		t.Fatalf("expected only [HK-中转1], got %v", got)
	}

	// 2. Pick ONLY direct landing: relays must NOT appear.
	got = previewNames(`{"name":"only-direct","sourceAll":false,"options":{"explicitLines":true,"directIds":[` + strconv.FormatInt(inboundID, 10) + `],"relayIds":[]}}`)
	if len(got) != 1 || got[0] != "JP-落地" {
		t.Fatalf("expected only [JP-落地], got %v", got)
	}

	// 3. Saving a relay-only group still includes its inboundID for share credentials.
	saveOut := post("/api/v1/subscription/group/save", `{"name":"relay-only","sourceAll":false,"options":{"explicitLines":true,"directIds":[],"relayIds":[`+strconv.FormatInt(relay1, 10)+`]}}`)
	if saveOut.Code != 0 {
		t.Fatalf("save relay-only group: %+v", saveOut)
	}
	gid := int64(saveOut.Data.(map[string]interface{})["id"].(float64))
	savedGroup := mustGroup(t, r, gid)
	if savedGroup.InboundIDs != strconv.FormatInt(inboundID, 10) {
		t.Fatalf("expected derived inbound_ids=%d for share credentials, got %q", inboundID, savedGroup.InboundIDs)
	}

	// 4. Legacy group without explicitLines still emits direct + all relays.
	_ = relay2
	got = previewNames(`{"name":"legacy","sourceAll":false,"inboundIds":[` + strconv.FormatInt(inboundID, 10) + `]}`)
	if len(got) != 3 {
		t.Fatalf("expected legacy group to emit direct + 2 relays (3 items), got %v", got)
	}
}
