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
