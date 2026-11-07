package contract_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"go-backend/internal/auth"
	"go-backend/internal/http/response"
)

func TestForwardAndTunnelDiagnoseContracts(t *testing.T) {
	secret := "contract-jwt-secret"
	router, repo := setupContractRouter(t, secret)
	now := time.Now().UnixMilli()

	if err := repo.DB().Exec(`
		INSERT INTO user(id, user, pwd, role_id, exp_time, flow, in_flow, out_flow, flow_reset_time, num, created_time, updated_time, status)
		VALUES(2, 'normal_user', '3c85cdebade1c51cf64ca9f3c09d182d', 1, 2727251700000, 99999, 0, 0, 1, 99999, ?, ?, 1)
	`, now, now).Error; err != nil {
		t.Fatalf("insert user: %v", err)
	}

	if err := repo.DB().Exec(`
		INSERT INTO tunnel(name, traffic_ratio, type, protocol, flow, created_time, updated_time, status, in_ip, inx)
		VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, "contract-tunnel", 2.5, 1, "tls", 99999, now, now, 1, nil, 0).Error; err != nil {
		t.Fatalf("insert tunnel: %v", err)
	}
	tunnelID := mustLastInsertID(t, repo, "contract-tunnel")

	if err := repo.DB().Exec(`
		INSERT INTO node(name, secret, server_ip, server_ip_v4, server_ip_v6, port, interface_name, version, http, tls, socks, created_time, updated_time, status, tcp_listen_addr, udp_listen_addr, inx)
		VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, "entry-node", "entry-secret", "10.0.0.10", "10.0.0.10", "", "20000-20010", "", "v1", 1, 1, 1, now, now, 1, "[::]", "[::]", 0).Error; err != nil {
		t.Fatalf("insert node: %v", err)
	}
	entryNodeID := mustLastInsertID(t, repo, "entry-node")

	if err := repo.DB().Exec(`
		INSERT INTO chain_tunnel(tunnel_id, chain_type, node_id, port, strategy, inx, protocol)
		VALUES(?, 1, ?, 20001, 'round', 1, 'tls')
	`, tunnelID, entryNodeID).Error; err != nil {
		t.Fatalf("insert chain_tunnel: %v", err)
	}

	if err := repo.DB().Exec(`
		INSERT INTO forward(user_id, user_name, name, tunnel_id, remote_addr, strategy, in_flow, out_flow, created_time, updated_time, status, inx)
		VALUES(?, ?, ?, ?, ?, ?, 0, 0, ?, ?, 1, ?)
	`, 1, "admin_user", "admin-forward", tunnelID, "1.1.1.1:443", "fifo", now, now, 0).Error; err != nil {
		t.Fatalf("insert admin forward: %v", err)
	}
	_ = mustLastInsertID(t, repo, "admin-forward")

	if err := repo.DB().Exec(`
		INSERT INTO forward(user_id, user_name, name, tunnel_id, remote_addr, strategy, in_flow, out_flow, created_time, updated_time, status, inx)
		VALUES(?, ?, ?, ?, ?, ?, 0, 0, ?, ?, 1, ?)
	`, 2, "normal_user", "user-forward", tunnelID, "8.8.8.8:53", "fifo", now, now, 1).Error; err != nil {
		t.Fatalf("insert user forward: %v", err)
	}
	userForwardID := mustLastInsertID(t, repo, "user-forward")

	adminToken, err := auth.GenerateToken(1, "admin_user", 0, secret)
	if err != nil {
		t.Fatalf("generate admin token: %v", err)
	}

	t.Run("forward diagnose returns structured payload", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/forward/diagnose", bytes.NewBufferString(`{"forwardId":`+jsonNumber(userForwardID)+`}`))
		req.Header.Set("Authorization", adminToken)
		res := httptest.NewRecorder()

		router.ServeHTTP(res, req)

		var out response.R
		if err := json.NewDecoder(res.Body).Decode(&out); err != nil {
			t.Fatalf("decode response: %v", err)
		}
		if out.Code != 0 {
			t.Fatalf("expected code 0, got %d (%s)", out.Code, out.Msg)
		}

		payload, ok := out.Data.(map[string]interface{})
		if !ok {
			t.Fatalf("expected object payload, got %T", out.Data)
		}
		results, ok := payload["results"].([]interface{})
		if !ok || len(results) == 0 {
			t.Fatalf("expected non-empty results, got %v", payload["results"])
		}
		first, ok := results[0].(map[string]interface{})
		if !ok {
			t.Fatalf("expected result object, got %T", results[0])
		}
		if _, ok := first["message"]; !ok {
			t.Fatalf("expected message field in diagnosis result")
		}
		fromChainTypeFloat, ok := first["fromChainType"].(float64)
		if !ok {
			t.Fatalf("expected fromChainType to be float64, got %T", first["fromChainType"])
		}
		if got := int(fromChainTypeFloat); got != 1 {
			t.Fatalf("expected fromChainType=1, got %d", got)
		}
	})

	t.Run("tunnel diagnose returns structured payload", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/tunnel/diagnose", bytes.NewBufferString(`{"tunnelId":`+jsonNumber(tunnelID)+`}`))
		req.Header.Set("Authorization", adminToken)
		res := httptest.NewRecorder()

		router.ServeHTTP(res, req)

		var out response.R
		if err := json.NewDecoder(res.Body).Decode(&out); err != nil {
			t.Fatalf("decode response: %v", err)
		}
		if out.Code != 0 {
			t.Fatalf("expected code 0, got %d (%s)", out.Code, out.Msg)
		}

		payload, ok := out.Data.(map[string]interface{})
		if !ok {
			t.Fatalf("expected object payload, got %T", out.Data)
		}
		results, ok := payload["results"].([]interface{})
		if !ok || len(results) == 0 {
			t.Fatalf("expected non-empty results, got %v", payload["results"])
		}
		first, ok := results[0].(map[string]interface{})
		if !ok {
			t.Fatalf("expected result object, got %T", results[0])
		}
		if _, ok := first["message"]; !ok {
			t.Fatalf("expected message field in tunnel diagnosis result")
		}
	})
}

func TestForwardSwitchTunnelRollbackOnSyncFailure(t *testing.T) {
	secret := "contract-jwt-secret"
	router, repo := setupContractRouter(t, secret)
	now := time.Now().UnixMilli()

	adminToken, err := auth.GenerateToken(1, "admin_user", 0, secret)
	if err != nil {
		t.Fatalf("generate admin token: %v", err)
	}

	if err := repo.DB().Exec(`
		INSERT INTO user(id, user, pwd, role_id, exp_time, flow, in_flow, out_flow, flow_reset_time, num, created_time, updated_time, status)
		VALUES(2, 'switch_user', '3c85cdebade1c51cf64ca9f3c09d182d', 1, 2727251700000, 99999, 0, 0, 1, 99999, ?, ?, 1)
	`, now, now).Error; err != nil {
		t.Fatalf("insert user: %v", err)
	}

	insertTunnel := func(name string, inx int) int64 {
		if err := repo.DB().Exec(`
			INSERT INTO tunnel(name, traffic_ratio, type, protocol, flow, created_time, updated_time, status, in_ip, inx)
			VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		`, name, 1.0, 1, "tls", 99999, now, now, 1, nil, inx).Error; err != nil {
			t.Fatalf("insert tunnel %s: %v", name, err)
		}
		return mustLastInsertID(t, repo, name)
	}

	insertNode := func(name, ip, portRange string, inx int) int64 {
		if err := repo.DB().Exec(`
			INSERT INTO node(name, secret, server_ip, server_ip_v4, server_ip_v6, port, interface_name, version, http, tls, socks, created_time, updated_time, status, tcp_listen_addr, udp_listen_addr, inx)
			VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		`, name, name+"-secret", ip, ip, "", portRange, "", "v1", 1, 1, 1, now, now, 1, "[::]", "[::]", inx).Error; err != nil {
			t.Fatalf("insert node %s: %v", name, err)
		}
		return mustLastInsertID(t, repo, name)
	}

	tunnelA := insertTunnel("switch-tunnel-a", 0)
	tunnelB := insertTunnel("switch-tunnel-b", 1)
	nodeA := insertNode("switch-node-a", "10.10.0.1", "21000-21010", 0)
	nodeB := insertNode("switch-node-b", "10.10.0.2", "22000-22010", 1)

	if err := repo.DB().Exec(`
		INSERT INTO chain_tunnel(tunnel_id, chain_type, node_id, port, strategy, inx, protocol)
		VALUES(?, 1, ?, 21001, 'round', 1, 'tls')
	`, tunnelA, nodeA).Error; err != nil {
		t.Fatalf("insert chain_tunnel tunnelA: %v", err)
	}
	if err := repo.DB().Exec(`
		INSERT INTO chain_tunnel(tunnel_id, chain_type, node_id, port, strategy, inx, protocol)
		VALUES(?, 1, ?, 22001, 'round', 1, 'tls')
	`, tunnelB, nodeB).Error; err != nil {
		t.Fatalf("insert chain_tunnel tunnelB: %v", err)
	}

	if err := repo.DB().Exec(`
		INSERT INTO user_tunnel(id, user_id, tunnel_id, speed_id, num, flow, in_flow, out_flow, flow_reset_time, exp_time, status)
		VALUES(10, 2, ?, NULL, 999, 99999, 0, 0, 1, 2727251700000, 1)
	`, tunnelA).Error; err != nil {
		t.Fatalf("insert user_tunnel A: %v", err)
	}
	if err := repo.DB().Exec(`
		INSERT INTO user_tunnel(id, user_id, tunnel_id, speed_id, num, flow, in_flow, out_flow, flow_reset_time, exp_time, status)
		VALUES(11, 2, ?, NULL, 999, 99999, 0, 0, 1, 2727251700000, 1)
	`, tunnelB).Error; err != nil {
		t.Fatalf("insert user_tunnel B: %v", err)
	}

	if err := repo.DB().Exec(`
		INSERT INTO forward(user_id, user_name, name, tunnel_id, remote_addr, strategy, in_flow, out_flow, created_time, updated_time, status, inx)
		VALUES(2, 'switch_user', 'switch-forward', ?, '8.8.8.8:53', 'fifo', 0, 0, ?, ?, 1, 0)
	`, tunnelA, now, now).Error; err != nil {
		t.Fatalf("insert forward: %v", err)
	}
	forwardID := mustLastInsertID(t, repo, "switch-forward")

	if err := repo.DB().Exec(`INSERT INTO forward_port(forward_id, node_id, port) VALUES(?, ?, ?)`, forwardID, nodeA, 21001).Error; err != nil {
		t.Fatalf("insert forward_port: %v", err)
	}

	payload := `{"id":` + jsonNumber(forwardID) + `,"tunnelId":` + jsonNumber(tunnelB) + `}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/forward/update", bytes.NewBufferString(payload))
	req.Header.Set("Authorization", adminToken)
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()

	router.ServeHTTP(res, req)

	var out response.R
	if err := json.NewDecoder(res.Body).Decode(&out); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if out.Code == 0 {
		t.Fatalf("expected update failure when node is offline")
	}

	tunnelAfter := mustQueryInt64(t, repo, `SELECT tunnel_id FROM forward WHERE id = ?`, forwardID)
	if tunnelAfter != tunnelA {
		t.Fatalf("expected tunnel rollback to %d, got %d", tunnelA, tunnelAfter)
	}

	nodeAfter, portAfter := mustQueryInt64Int(t, repo, `SELECT node_id, port FROM forward_port WHERE forward_id = ? LIMIT 1`, forwardID)
	if nodeAfter != nodeA || portAfter != 21001 {
		t.Fatalf("expected forward_port rollback to node=%d port=21001, got node=%d port=%d", nodeA, nodeAfter, portAfter)
	}
}

func TestForwardBatchChangeTunnelRollbackOnSyncFailure(t *testing.T) {
	secret := "contract-jwt-secret"
	router, repo := setupContractRouter(t, secret)
	now := time.Now().UnixMilli()

	adminToken, err := auth.GenerateToken(1, "admin_user", 0, secret)
	if err != nil {
		t.Fatalf("generate admin token: %v", err)
	}

	if err := repo.DB().Exec(`
		INSERT INTO user(id, user, pwd, role_id, exp_time, flow, in_flow, out_flow, flow_reset_time, num, created_time, updated_time, status)
		VALUES(2, 'batch_switch_user', '3c85cdebade1c51cf64ca9f3c09d182d', 1, 2727251700000, 99999, 0, 0, 1, 99999, ?, ?, 1)
	`, now, now).Error; err != nil {
		t.Fatalf("insert user: %v", err)
	}

	if err := repo.DB().Exec(`
		INSERT INTO tunnel(name, traffic_ratio, type, protocol, flow, created_time, updated_time, status, in_ip, inx)
		VALUES('batch-switch-tunnel-a', 1.0, 1, 'tls', 99999, ?, ?, 1, NULL, 0)
	`, now, now).Error; err != nil {
		t.Fatalf("insert tunnel A: %v", err)
	}
	tunnelA := mustLastInsertID(t, repo, "batch-switch-tunnel-a")

	if err := repo.DB().Exec(`
		INSERT INTO tunnel(name, traffic_ratio, type, protocol, flow, created_time, updated_time, status, in_ip, inx)
		VALUES('batch-switch-tunnel-b', 1.0, 1, 'tls', 99999, ?, ?, 1, NULL, 1)
	`, now, now).Error; err != nil {
		t.Fatalf("insert tunnel B: %v", err)
	}
	tunnelB := mustLastInsertID(t, repo, "batch-switch-tunnel-b")

	if err := repo.DB().Exec(`
		INSERT INTO node(name, secret, server_ip, server_ip_v4, server_ip_v6, port, interface_name, version, http, tls, socks, created_time, updated_time, status, tcp_listen_addr, udp_listen_addr, inx)
		VALUES('batch-switch-node-a', 'batch-switch-node-a-secret', '10.11.0.1', '10.11.0.1', '', '23000-23010', '', 'v1', 1, 1, 1, ?, ?, 1, '[::]', '[::]', 0)
	`, now, now).Error; err != nil {
		t.Fatalf("insert node A: %v", err)
	}
	nodeA := mustLastInsertID(t, repo, "batch-switch-node-a")

	if err := repo.DB().Exec(`
		INSERT INTO node(name, secret, server_ip, server_ip_v4, server_ip_v6, port, interface_name, version, http, tls, socks, created_time, updated_time, status, tcp_listen_addr, udp_listen_addr, inx)
		VALUES('batch-switch-node-b', 'batch-switch-node-b-secret', '10.11.0.2', '10.11.0.2', '', '24000-24010', '', 'v1', 1, 1, 1, ?, ?, 1, '[::]', '[::]', 1)
	`, now, now).Error; err != nil {
		t.Fatalf("insert node B: %v", err)
	}
	nodeB := mustLastInsertID(t, repo, "batch-switch-node-b")

	if err := repo.DB().Exec(`INSERT INTO chain_tunnel(tunnel_id, chain_type, node_id, port, strategy, inx, protocol) VALUES(?, 1, ?, 23001, 'round', 1, 'tls')`, tunnelA, nodeA).Error; err != nil {
		t.Fatalf("insert chain_tunnel A: %v", err)
	}
	if err := repo.DB().Exec(`INSERT INTO chain_tunnel(tunnel_id, chain_type, node_id, port, strategy, inx, protocol) VALUES(?, 1, ?, 24001, 'round', 1, 'tls')`, tunnelB, nodeB).Error; err != nil {
		t.Fatalf("insert chain_tunnel B: %v", err)
	}

	if err := repo.DB().Exec(`INSERT INTO user_tunnel(id, user_id, tunnel_id, speed_id, num, flow, in_flow, out_flow, flow_reset_time, exp_time, status) VALUES(20, 2, ?, NULL, 999, 99999, 0, 0, 1, 2727251700000, 1)`, tunnelA).Error; err != nil {
		t.Fatalf("insert user_tunnel A: %v", err)
	}
	if err := repo.DB().Exec(`INSERT INTO user_tunnel(id, user_id, tunnel_id, speed_id, num, flow, in_flow, out_flow, flow_reset_time, exp_time, status) VALUES(21, 2, ?, NULL, 999, 99999, 0, 0, 1, 2727251700000, 1)`, tunnelB).Error; err != nil {
		t.Fatalf("insert user_tunnel B: %v", err)
	}

	if err := repo.DB().Exec(`
		INSERT INTO forward(user_id, user_name, name, tunnel_id, remote_addr, strategy, in_flow, out_flow, created_time, updated_time, status, inx)
		VALUES(2, 'batch_switch_user', 'batch-switch-forward', ?, '1.1.1.1:443', 'fifo', 0, 0, ?, ?, 1, 0)
	`, tunnelA, now, now).Error; err != nil {
		t.Fatalf("insert forward: %v", err)
	}
	forwardID := mustLastInsertID(t, repo, "batch-switch-forward")

	if err := repo.DB().Exec(`INSERT INTO forward_port(forward_id, node_id, port) VALUES(?, ?, ?)`, forwardID, nodeA, 23001).Error; err != nil {
		t.Fatalf("insert forward_port: %v", err)
	}

	payload := `{"forwardIds":[` + jsonNumber(forwardID) + `],"targetTunnelId":` + jsonNumber(tunnelB) + `}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/forward/batch-change-tunnel", bytes.NewBufferString(payload))
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

	tunnelAfter := mustQueryInt64(t, repo, `SELECT tunnel_id FROM forward WHERE id = ?`, forwardID)
	if tunnelAfter != tunnelA {
		t.Fatalf("expected tunnel rollback to %d, got %d", tunnelA, tunnelAfter)
	}

	nodeAfter, portAfter := mustQueryInt64Int(t, repo, `SELECT node_id, port FROM forward_port WHERE forward_id = ? LIMIT 1`, forwardID)
	if nodeAfter != nodeA || portAfter != 23001 {
		t.Fatalf("expected forward_port rollback to node=%d port=23001, got node=%d port=%d", nodeA, nodeAfter, portAfter)
	}
}

func TestForwardCreateThenPauseResumeContract(t *testing.T) {
	secret := "contract-jwt-secret"
	router, repo := setupContractRouter(t, secret)

	adminToken, err := auth.GenerateToken(1, "admin_user", 0, secret)
	if err != nil {
		t.Fatalf("generate admin token: %v", err)
	}

	now := time.Now().UnixMilli()
	if err := repo.DB().Exec(`
		INSERT INTO tunnel(name, traffic_ratio, type, protocol, flow, created_time, updated_time, status, in_ip, inx)
		VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, "forward-toggle-tunnel", 1.0, 1, "tls", 99999, now, now, 1, nil, 0).Error; err != nil {
		t.Fatalf("insert tunnel: %v", err)
	}
	tunnelID := mustLastInsertID(t, repo, "forward-toggle-tunnel")

	if err := repo.DB().Exec(`
		INSERT INTO node(name, secret, server_ip, server_ip_v4, server_ip_v6, port, interface_name, version, http, tls, socks, created_time, updated_time, status, tcp_listen_addr, udp_listen_addr, inx)
		VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, "forward-toggle-node", "forward-toggle-secret", "10.31.0.1", "10.31.0.1", "", "41000-41010", "", "v1", 1, 1, 1, now, now, 1, "[::]", "[::]", 0).Error; err != nil {
		t.Fatalf("insert node: %v", err)
	}
	nodeID := mustLastInsertID(t, repo, "forward-toggle-node")

	if err := repo.DB().Exec(`
		INSERT INTO chain_tunnel(tunnel_id, chain_type, node_id, port, strategy, inx, protocol)
		VALUES(?, 1, ?, 41001, 'round', 1, 'tls')
	`, tunnelID, nodeID).Error; err != nil {
		t.Fatalf("insert chain_tunnel: %v", err)
	}

	server := httptest.NewServer(router)
	defer server.Close()
	stopNode := startMockNodeSession(t, server.URL, "forward-toggle-secret")
	defer stopNode()

	createPayload := map[string]interface{}{
		"name":       "forward-toggle-target",
		"tunnelId":   tunnelID,
		"remoteAddr": "1.1.1.1:443",
		"strategy":   "fifo",
	}
	createBody, err := json.Marshal(createPayload)
	if err != nil {
		t.Fatalf("marshal create payload: %v", err)
	}
	createReq := httptest.NewRequest(http.MethodPost, "/api/v1/forward/create", bytes.NewReader(createBody))
	createReq.Header.Set("Authorization", adminToken)
	createReq.Header.Set("Content-Type", "application/json")
	createRes := httptest.NewRecorder()
	router.ServeHTTP(createRes, createReq)
	assertCode(t, createRes, 0)

	forwardID := mustLastInsertID(t, repo, "forward-toggle-target")

	pauseBody, err := json.Marshal(map[string]interface{}{"id": forwardID})
	if err != nil {
		t.Fatalf("marshal pause payload: %v", err)
	}
	pauseReq := httptest.NewRequest(http.MethodPost, "/api/v1/forward/pause", bytes.NewReader(pauseBody))
	pauseReq.Header.Set("Authorization", adminToken)
	pauseReq.Header.Set("Content-Type", "application/json")
	pauseRes := httptest.NewRecorder()
	router.ServeHTTP(pauseRes, pauseReq)
	assertCode(t, pauseRes, 0)

	pausedStatus := mustQueryInt(t, repo, `SELECT status FROM forward WHERE id = ?`, forwardID)
	if pausedStatus != 0 {
		t.Fatalf("expected status=0 after pause, got %d", pausedStatus)
	}

	resumeBody, err := json.Marshal(map[string]interface{}{"id": forwardID})
	if err != nil {
		t.Fatalf("marshal resume payload: %v", err)
	}
	resumeReq := httptest.NewRequest(http.MethodPost, "/api/v1/forward/resume", bytes.NewReader(resumeBody))
	resumeReq.Header.Set("Authorization", adminToken)
	resumeReq.Header.Set("Content-Type", "application/json")
	resumeRes := httptest.NewRecorder()
	router.ServeHTTP(resumeRes, resumeReq)
	assertCode(t, resumeRes, 0)

	resumedStatus := mustQueryInt(t, repo, `SELECT status FROM forward WHERE id = ?`, forwardID)
	if resumedStatus != 1 {
		t.Fatalf("expected status=1 after resume, got %d", resumedStatus)
	}
}

func TestForwardUpdateRecoversFromAddressInUseContract(t *testing.T) {
	secret := "contract-jwt-secret"
	router, repo := setupContractRouter(t, secret)
	server := httptest.NewServer(router)
	defer server.Close()

	adminToken, err := auth.GenerateToken(1, "admin_user", 0, secret)
	if err != nil {
		t.Fatalf("generate admin token: %v", err)
	}

	now := time.Now().UnixMilli()
	if err := repo.DB().Exec(`
		INSERT INTO user(id, user, pwd, role_id, exp_time, flow, in_flow, out_flow, flow_reset_time, num, created_time, updated_time, status)
		VALUES(202, 'forward_bind_retry_user', 'pwd', 1, 2727251700000, 99999, 0, 0, 1, 99999, ?, ?, 1)
	`, now, now).Error; err != nil {
		t.Fatalf("insert user: %v", err)
	}

	if err := repo.DB().Exec(`
		INSERT INTO tunnel(name, traffic_ratio, type, protocol, flow, created_time, updated_time, status, in_ip, inx)
		VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, "forward-bind-retry-tunnel", 1.0, 1, "tls", 99999, now, now, 1, nil, 0).Error; err != nil {
		t.Fatalf("insert tunnel: %v", err)
	}
	tunnelID := mustLastInsertID(t, repo, "forward-bind-retry-tunnel")

	if err := repo.DB().Exec(`
		INSERT INTO node(name, secret, server_ip, server_ip_v4, server_ip_v6, port, interface_name, version, http, tls, socks, created_time, updated_time, status, tcp_listen_addr, udp_listen_addr, inx)
		VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, "forward-bind-retry-node", "forward-bind-retry-secret", "10.42.0.1", "10.42.0.1", "", "44000-44010", "", "v1", 1, 1, 1, now, now, 1, "10.42.0.9", "[::]", 0).Error; err != nil {
		t.Fatalf("insert node: %v", err)
	}
	nodeID := mustLastInsertID(t, repo, "forward-bind-retry-node")

	if err := repo.DB().Exec(`
		INSERT INTO chain_tunnel(tunnel_id, chain_type, node_id, port, strategy, inx, protocol)
		VALUES(?, 1, ?, 44001, 'round', 1, 'tls')
	`, tunnelID, nodeID).Error; err != nil {
		t.Fatalf("insert chain_tunnel: %v", err)
	}

	if err := repo.DB().Exec(`
		INSERT INTO user_tunnel(id, user_id, tunnel_id, speed_id, num, flow, in_flow, out_flow, flow_reset_time, exp_time, status)
		VALUES(41, 202, ?, NULL, 999, 99999, 0, 0, 1, 2727251700000, 1)
	`, tunnelID).Error; err != nil {
		t.Fatalf("insert user_tunnel: %v", err)
	}

	createPayload := map[string]interface{}{
		"name":       "forward-bind-retry-target",
		"tunnelId":   tunnelID,
		"remoteAddr": "1.1.1.1:443",
		"strategy":   "fifo",
	}
	createBody, err := json.Marshal(createPayload)
	if err != nil {
		t.Fatalf("marshal create payload: %v", err)
	}

	// Create the forward while the node is offline (the push is skipped with
	// a warning), so the node's reconnect redeploy is what first pushes it.
	// That redeploy runs in its own goroutine; waiting for it to finish below
	// keeps it from landing in the middle of the update being measured.
	createReq := httptest.NewRequest(http.MethodPost, "/api/v1/forward/create", bytes.NewReader(createBody))
	createReq.Header.Set("Authorization", adminToken)
	createReq.Header.Set("Content-Type", "application/json")
	createRes := httptest.NewRecorder()
	router.ServeHTTP(createRes, createReq)
	assertCode(t, createRes, 0)
	forwardID := mustLastInsertID(t, repo, "forward-bind-retry-target")

	var mu sync.Mutex
	counts := map[string]int{}
	var addServiceAddrs []string
	triggerConflict := false
	stopNode := startMockNodeSessionWithCommandRecorder(t, server.URL, "forward-bind-retry-secret", func(cmdType string, data json.RawMessage) (bool, string) {
		key := strings.ToLower(strings.TrimSpace(cmdType))
		mu.Lock()
		counts[key]++
		attempt := counts[key]
		if strings.EqualFold(strings.TrimSpace(cmdType), "AddService") || strings.EqualFold(strings.TrimSpace(cmdType), "UpdateService") {
			var services []map[string]interface{}
			if err := json.Unmarshal(data, &services); err == nil {
				for _, svc := range services {
					if addr, _ := svc["addr"].(string); strings.TrimSpace(addr) != "" {
						addServiceAddrs = append(addServiceAddrs, addr)
					}
				}
			}
		}
		shouldFail := false
		if triggerConflict {
			if strings.EqualFold(strings.TrimSpace(cmdType), "UpdateService") && attempt == 1 {
				shouldFail = true
			}
			if strings.EqualFold(strings.TrimSpace(cmdType), "AddService") && attempt == 1 {
				shouldFail = true
			}
		}
		mu.Unlock()
		if shouldFail {
			return true, "create service 57_7_7_tcp failed: listen tcp4 0.0.0.0:46222: bind: address alreadyin use"
		}
		return false, ""
	})
	defer stopNode()
	waitNodeStatus(t, repo, nodeID, 1)
	waitForwardPushed(t, &mu, counts)

	mu.Lock()
	counts = map[string]int{}
	addServiceAddrs = nil
	triggerConflict = true
	mu.Unlock()

	updatePayload := map[string]interface{}{
		"id":         forwardID,
		"name":       "forward-bind-retry-target-updated",
		"tunnelId":   tunnelID,
		"remoteAddr": "9.9.9.9:8443",
		"strategy":   "fifo",
	}
	updateBody, err := json.Marshal(updatePayload)
	if err != nil {
		t.Fatalf("marshal update payload: %v", err)
	}
	updateReq := httptest.NewRequest(http.MethodPost, "/api/v1/forward/update", bytes.NewReader(updateBody))
	updateReq.Header.Set("Authorization", adminToken)
	updateReq.Header.Set("Content-Type", "application/json")
	updateRes := httptest.NewRecorder()
	router.ServeHTTP(updateRes, updateReq)
	assertCode(t, updateRes, 0)

	mu.Lock()
	defer mu.Unlock()
	boundPort := mustQueryInt(t, repo, `SELECT port FROM forward_port WHERE forward_id = ? LIMIT 1`, forwardID)
	if counts["updateservice"] != 1 {
		t.Fatalf("expected one UpdateService attempt, got %d (%v)", counts["updateservice"], counts)
	}
	if counts["deleteservice"] == 0 {
		t.Fatalf("expected DeleteService cleanup after address-in-use (%v)", counts)
	}
	if counts["addservice"] < 2 {
		t.Fatalf("expected AddService retry path to run at least twice total, got %d (%v)", counts["addservice"], counts)
	}
	foundBindAddr := false
	for _, addr := range addServiceAddrs {
		if addr == "10.42.0.9:"+strconv.Itoa(boundPort) {
			foundBindAddr = true
			break
		}
	}
	if !foundBindAddr {
		t.Fatalf("expected forward runtime to keep node listen addr 10.42.0.9:%d, got %v", boundPort, addServiceAddrs)
	}

	storedRemoteAddr := mustQueryString(t, repo, `SELECT remote_addr FROM forward WHERE id = ?`, forwardID)
	if storedRemoteAddr != "9.9.9.9:8443" {
		t.Fatalf("expected remote_addr update to persist, got %q", storedRemoteAddr)
	}
}

// waitForwardPushed waits until the node's reconnect redeploy has pushed
// the forward (UpdateService or AddService) and the node has then received
// no command for a while: the redeploy pushes a forward more than once, from
// more than one path, and none of it may overlap the update under test.
func waitForwardPushed(t *testing.T, mu *sync.Mutex, counts map[string]int) {
	t.Helper()
	const quiet = 300 * time.Millisecond
	total := func() (int, bool) {
		mu.Lock()
		defer mu.Unlock()
		n := 0
		for _, c := range counts {
			n += c
		}
		return n, counts["updateservice"]+counts["addservice"] > 0
	}
	deadline := time.Now().Add(10 * time.Second)
	last, _ := total()
	lastChange := time.Now()
	for {
		n, pushed := total()
		if n != last {
			last, lastChange = n, time.Now()
		}
		if pushed && time.Since(lastChange) >= quiet {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("the node's reconnect redeploy did not push the forward and settle")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func jsonNumber(v int64) string {
	return strconv.FormatInt(v, 10)
}
