package contract_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"go-backend/internal/auth"
	"go-backend/internal/http/response"
)

func TestForwardBatchRedeployReturnsFailureReasonsContract(t *testing.T) {
	secret := "contract-jwt-secret"
	router, repo := setupContractRouter(t, secret)
	now := time.Now().UnixMilli()

	adminToken, err := auth.GenerateToken(1, "admin_user", 0, secret)
	if err != nil {
		t.Fatalf("generate admin token: %v", err)
	}

	if err := repo.DB().Exec(`
		INSERT INTO user(id, user, pwd, role_id, exp_time, flow, in_flow, out_flow, flow_reset_time, num, created_time, updated_time, status)
		VALUES(2, 'batch_redeploy_user', '3c85cdebade1c51cf64ca9f3c09d182d', 1, 2727251700000, 99999, 0, 0, 1, 99999, ?, ?, 1)
	`, now, now).Error; err != nil {
		t.Fatalf("insert user: %v", err)
	}

	if err := repo.DB().Exec(`
		INSERT INTO tunnel(name, traffic_ratio, type, protocol, flow, created_time, updated_time, status, in_ip, inx)
		VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, "batch-redeploy-tunnel", 1.0, 1, "tls", 99999, now, now, 1, nil, 0).Error; err != nil {
		t.Fatalf("insert tunnel: %v", err)
	}
	tunnelID := mustLastInsertID(t, repo, "batch-redeploy-tunnel")

	if err := repo.DB().Exec(`
		INSERT INTO forward(user_id, user_name, name, tunnel_id, remote_addr, strategy, in_flow, out_flow, created_time, updated_time, status, inx)
		VALUES(?, ?, ?, ?, ?, ?, 0, 0, ?, ?, 1, 0)
	`, 2, "batch_redeploy_user", "redeploy-forward", tunnelID, "1.1.1.1:443", "fifo", now, now).Error; err != nil {
		t.Fatalf("insert forward: %v", err)
	}
	forwardID := mustLastInsertID(t, repo, "redeploy-forward")

	req := httptest.NewRequest(http.MethodPost, "/api/v1/forward/batch-redeploy", bytes.NewBufferString(`{"ids":[`+jsonNumber(forwardID)+`]}`))
	req.Header.Set("Authorization", adminToken)
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	var out response.R
	if err := json.NewDecoder(res.Body).Decode(&out); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if out.Code != 0 {
		t.Fatalf("expected API success envelope, got code=%d msg=%q", out.Code, out.Msg)
	}

	result, ok := out.Data.(map[string]interface{})
	if !ok {
		t.Fatalf("expected map result, got %T", out.Data)
	}
	if int(result["failCount"].(float64)) != 1 {
		t.Fatalf("expected failCount=1, got %v", result["failCount"])
	}
	if int(result["successCount"].(float64)) != 0 {
		t.Fatalf("expected successCount=0, got %v", result["successCount"])
	}

	failures, ok := result["failures"].([]interface{})
	if !ok || len(failures) != 1 {
		t.Fatalf("expected exactly one failure detail, got %#v", result["failures"])
	}
	first, ok := failures[0].(map[string]interface{})
	if !ok {
		t.Fatalf("expected failure detail object, got %T", failures[0])
	}
	if gotName := strings.TrimSpace(first["name"].(string)); gotName != "redeploy-forward" {
		t.Fatalf("expected failure name redeploy-forward, got %q", gotName)
	}
	reason, _ := first["reason"].(string)
	if !strings.Contains(reason, "转发入口端口不存在") {
		t.Fatalf("expected forward failure reason to mention missing entry port, got %q", reason)
	}
}

func TestTunnelBatchRedeployReturnsFailureReasonsContract(t *testing.T) {
	secret := "contract-jwt-secret"
	router, repo := setupContractRouter(t, secret)
	now := time.Now().UnixMilli()

	adminToken, err := auth.GenerateToken(1, "admin_user", 0, secret)
	if err != nil {
		t.Fatalf("generate admin token: %v", err)
	}

	if err := repo.DB().Exec(`
		INSERT INTO tunnel(name, traffic_ratio, type, protocol, flow, created_time, updated_time, status, in_ip, inx)
		VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, "broken-redeploy-tunnel", 1.0, 2, "tls", 99999, now, now, 1, nil, 0).Error; err != nil {
		t.Fatalf("insert tunnel: %v", err)
	}
	tunnelID := mustLastInsertID(t, repo, "broken-redeploy-tunnel")

	if err := repo.DB().Exec(`
		INSERT INTO node(name, secret, server_ip, server_ip_v4, server_ip_v6, port, interface_name, version, http, tls, socks, created_time, updated_time, status, tcp_listen_addr, udp_listen_addr, inx)
		VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, "entry-only-node", "entry-only-secret", "10.0.0.20", "10.0.0.20", "", "20000-20010", "", "v1", 1, 1, 1, now, now, 1, "[::]", "[::]", 0).Error; err != nil {
		t.Fatalf("insert node: %v", err)
	}
	entryNodeID := mustLastInsertID(t, repo, "entry-only-node")

	if err := repo.DB().Exec(`
		INSERT INTO chain_tunnel(tunnel_id, chain_type, node_id, port, strategy, inx, protocol)
		VALUES(?, 1, ?, 20001, 'round', 1, 'tls')
	`, tunnelID, entryNodeID).Error; err != nil {
		t.Fatalf("insert chain_tunnel: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/tunnel/batch-redeploy", bytes.NewBufferString(`{"ids":[`+jsonNumber(tunnelID)+`]}`))
	req.Header.Set("Authorization", adminToken)
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	var out response.R
	if err := json.NewDecoder(res.Body).Decode(&out); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if out.Code != 0 {
		t.Fatalf("expected API success envelope, got code=%d msg=%q", out.Code, out.Msg)
	}

	result, ok := out.Data.(map[string]interface{})
	if !ok {
		t.Fatalf("expected map result, got %T", out.Data)
	}
	if int(result["failCount"].(float64)) != 1 {
		t.Fatalf("expected failCount=1, got %v", result["failCount"])
	}

	failures, ok := result["failures"].([]interface{})
	if !ok || len(failures) != 1 {
		t.Fatalf("expected exactly one failure detail, got %#v", result["failures"])
	}
	first, ok := failures[0].(map[string]interface{})
	if !ok {
		t.Fatalf("expected failure detail object, got %T", failures[0])
	}
	if gotName := strings.TrimSpace(first["name"].(string)); gotName != "broken-redeploy-tunnel" {
		t.Fatalf("expected failure name broken-redeploy-tunnel, got %q", gotName)
	}
	reason, _ := first["reason"].(string)
	if !strings.Contains(reason, "转发链目标不能为空") {
		t.Fatalf("expected tunnel failure reason to mention missing target, got %q", reason)
	}
}
