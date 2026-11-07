package contract_test

import (
	"bufio"
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"go-backend/internal/auth"
	"go-backend/internal/http/response"
)

func TestDiagnosisChainCoverageContracts(t *testing.T) {
	secret := "contract-jwt-secret"
	router, r := setupContractRouter(t, secret)
	now := time.Now().UnixMilli()

	if err := r.DB().Exec(`
		INSERT INTO user(id, user, pwd, role_id, exp_time, flow, in_flow, out_flow, flow_reset_time, num, created_time, updated_time, status)
		VALUES(2, 'normal_user', '3c85cdebade1c51cf64ca9f3c09d182d', 0, 2727251700000, 99999, 0, 0, 1, 99999, ?, ?, 1)
	`, now, now).Error; err != nil {
		t.Fatalf("insert user: %v", err)
	}

	if err := r.DB().Exec(`
		INSERT INTO tunnel(name, traffic_ratio, type, protocol, flow, created_time, updated_time, status, in_ip, inx)
		VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, "diagnose-chain-tunnel", 1.0, 2, "tls", 99999, now, now, 1, nil, 0).Error; err != nil {
		t.Fatalf("insert tunnel: %v", err)
	}
	tunnelID := mustLastInsertID(t, r, "diagnose-chain-tunnel")

	insertNode := func(name, ip string) int64 {
		if err := r.DB().Exec(`
			INSERT INTO node(name, secret, server_ip, server_ip_v4, server_ip_v6, port, interface_name, version, http, tls, socks, created_time, updated_time, status, tcp_listen_addr, udp_listen_addr, inx)
			VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		`, name, name+"-secret", ip, ip, "", "30000-30010", "", "v1", 1, 1, 1, now, now, 1, "[::]", "[::]", 0).Error; err != nil {
			t.Fatalf("insert node %s: %v", name, err)
		}
		return mustLastInsertID(t, r, name)
	}

	entryNodeID := insertNode("entry-node", "10.0.1.10")
	chainNodeID := insertNode("chain-node", "10.0.1.20")
	exitNodeID := insertNode("exit-node", "10.0.1.30")

	if err := r.DB().Exec(`
		INSERT INTO chain_tunnel(tunnel_id, chain_type, node_id, port, strategy, inx, protocol)
		VALUES(?, 1, ?, 30001, 'round', 1, 'tls')
	`, tunnelID, entryNodeID).Error; err != nil {
		t.Fatalf("insert entry chain: %v", err)
	}
	if err := r.DB().Exec(`
		INSERT INTO chain_tunnel(tunnel_id, chain_type, node_id, port, strategy, inx, protocol)
		VALUES(?, 2, ?, 30002, 'round', 1, 'tls')
	`, tunnelID, chainNodeID).Error; err != nil {
		t.Fatalf("insert middle chain: %v", err)
	}
	if err := r.DB().Exec(`
		INSERT INTO chain_tunnel(tunnel_id, chain_type, node_id, port, strategy, inx, protocol)
		VALUES(?, 3, ?, 30003, 'round', 1, 'tls')
	`, tunnelID, exitNodeID).Error; err != nil {
		t.Fatalf("insert exit chain: %v", err)
	}

	if err := r.DB().Exec(`
		INSERT INTO forward(user_id, user_name, name, tunnel_id, remote_addr, strategy, in_flow, out_flow, created_time, updated_time, status, inx)
		VALUES(?, ?, ?, ?, ?, ?, 0, 0, ?, ?, 1, ?)
	`, 2, "normal_user", "chain-forward", tunnelID, "8.8.8.8:53", "fifo", now, now, 0).Error; err != nil {
		t.Fatalf("insert forward: %v", err)
	}
	forwardID := mustLastInsertID(t, r, "chain-forward")

	userToken, err := auth.GenerateToken(2, "normal_user", 0, secret)
	if err != nil {
		t.Fatalf("generate user token: %v", err)
	}
	adminToken, err := auth.GenerateToken(1, "admin_user", 0, secret)
	if err != nil {
		t.Fatalf("generate admin token: %v", err)
	}

	t.Run("forward diagnose includes entry chain exit paths", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/forward/diagnose", bytes.NewBufferString(`{"forwardId":`+strconv.FormatInt(forwardID, 10)+`}`))
		req.Header.Set("Authorization", userToken)
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

		hasEntryToChain := false
		hasChainToExit := false
		hasExitToTarget := false
		for _, raw := range results {
			item, ok := raw.(map[string]interface{})
			if !ok {
				t.Fatalf("expected result object, got %T", raw)
			}
			if strings.TrimSpace(valueAsString(item["message"])) == "" {
				t.Fatalf("expected non-empty message field")
			}
			from := valueAsInt(item["fromChainType"])
			to := valueAsInt(item["toChainType"])
			if from == 1 && to == 2 {
				hasEntryToChain = true
			}
			if from == 2 && to == 3 {
				hasChainToExit = true
			}
			if from == 3 {
				hasExitToTarget = true
			}
		}

		if !hasEntryToChain || !hasChainToExit || !hasExitToTarget {
			t.Fatalf("expected entry->chain, chain->exit, exit->target coverage; got entry=%v chain=%v exit=%v", hasEntryToChain, hasChainToExit, hasExitToTarget)
		}
	})

	t.Run("tunnel diagnose includes entry chain exit groups", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/tunnel/diagnose", bytes.NewBufferString(`{"tunnelId":`+strconv.FormatInt(tunnelID, 10)+`}`))
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

		hasEntry := false
		hasChain := false
		hasExit := false
		for _, raw := range results {
			item, ok := raw.(map[string]interface{})
			if !ok {
				t.Fatalf("expected result object, got %T", raw)
			}
			if strings.TrimSpace(valueAsString(item["message"])) == "" {
				t.Fatalf("expected non-empty message field")
			}
			switch valueAsInt(item["fromChainType"]) {
			case 1:
				hasEntry = true
			case 2:
				hasChain = true
			case 3:
				hasExit = true
			}
		}

		if !hasEntry || !hasChain || !hasExit {
			t.Fatalf("expected entry/chain/exit groups, got entry=%v chain=%v exit=%v", hasEntry, hasChain, hasExit)
		}
	})
}

func TestForwardDiagnosisRespectsTunnelIPPreferenceContract(t *testing.T) {
	secret := "contract-jwt-secret"
	router, r := setupContractRouter(t, secret)
	now := time.Now().UnixMilli()

	if err := r.DB().Exec(`
		INSERT INTO user(id, user, pwd, role_id, exp_time, flow, in_flow, out_flow, flow_reset_time, num, created_time, updated_time, status)
		VALUES(2, 'normal_user', '3c85cdebade1c51cf64ca9f3c09d182d', 0, 2727251700000, 99999, 0, 0, 1, 99999, ?, ?, 1)
	`, now, now).Error; err != nil {
		t.Fatalf("insert user: %v", err)
	}

	if err := r.DB().Exec(`
		INSERT INTO tunnel(name, traffic_ratio, type, protocol, flow, created_time, updated_time, status, in_ip, inx, ip_preference)
		VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, "diagnose-ip-pref-forward", 1.0, 2, "tls", 99999, now, now, 1, nil, 0, "v6").Error; err != nil {
		t.Fatalf("insert tunnel: %v", err)
	}
	tunnelID := mustLastInsertID(t, r, "diagnose-ip-pref-forward")

	insertNode := func(name, v4, v6 string) int64 {
		if err := r.DB().Exec(`
			INSERT INTO node(name, secret, server_ip, server_ip_v4, server_ip_v6, port, interface_name, version, http, tls, socks, created_time, updated_time, status, tcp_listen_addr, udp_listen_addr, inx)
			VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		`, name, name+"-secret", v4, v4, v6, "30000-30010", "", "v1", 1, 1, 1, now, now, 1, "[::]", "[::]", 0).Error; err != nil {
			t.Fatalf("insert node %s: %v", name, err)
		}
		return mustLastInsertID(t, r, name)
	}

	entryNodeID := insertNode("entry-node-v6", "10.10.1.10", "2001:db8:10::10")
	chainNodeID := insertNode("chain-node-v6", "10.10.1.20", "2001:db8:10::20")
	exitNodeID := insertNode("exit-node-v6", "10.10.1.30", "2001:db8:10::30")

	if err := r.DB().Exec(`
		INSERT INTO chain_tunnel(tunnel_id, chain_type, node_id, port, strategy, inx, protocol)
		VALUES(?, 1, ?, 30001, 'round', 1, 'tls')
	`, tunnelID, entryNodeID).Error; err != nil {
		t.Fatalf("insert entry chain: %v", err)
	}
	if err := r.DB().Exec(`
		INSERT INTO chain_tunnel(tunnel_id, chain_type, node_id, port, strategy, inx, protocol)
		VALUES(?, 2, ?, 30002, 'round', 1, 'tls')
	`, tunnelID, chainNodeID).Error; err != nil {
		t.Fatalf("insert middle chain: %v", err)
	}
	if err := r.DB().Exec(`
		INSERT INTO chain_tunnel(tunnel_id, chain_type, node_id, port, strategy, inx, protocol)
		VALUES(?, 3, ?, 30003, 'round', 1, 'tls')
	`, tunnelID, exitNodeID).Error; err != nil {
		t.Fatalf("insert exit chain: %v", err)
	}

	if err := r.DB().Exec(`
		INSERT INTO forward(user_id, user_name, name, tunnel_id, remote_addr, strategy, in_flow, out_flow, created_time, updated_time, status, inx)
		VALUES(?, ?, ?, ?, ?, ?, 0, 0, ?, ?, 1, ?)
	`, 2, "normal_user", "ip-pref-forward", tunnelID, "8.8.8.8:53", "fifo", now, now, 0).Error; err != nil {
		t.Fatalf("insert forward: %v", err)
	}
	forwardID := mustLastInsertID(t, r, "ip-pref-forward")

	userToken, err := auth.GenerateToken(2, "normal_user", 0, secret)
	if err != nil {
		t.Fatalf("generate user token: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/forward/diagnose", bytes.NewBufferString(`{"forwardId":`+strconv.FormatInt(forwardID, 10)+`}`))
	req.Header.Set("Authorization", userToken)
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

	hasEntryToChain := false
	hasChainToExit := false
	for _, raw := range results {
		item, ok := raw.(map[string]interface{})
		if !ok {
			continue
		}
		from := valueAsInt(item["fromChainType"])
		to := valueAsInt(item["toChainType"])
		targetIP := strings.TrimSpace(valueAsString(item["targetIp"]))

		if from == 1 && to == 2 {
			hasEntryToChain = true
			if targetIP != "2001:db8:10::20" {
				t.Fatalf("expected entry->chain diagnosis target to use IPv6, got %q", targetIP)
			}
		}

		if from == 2 && to == 3 {
			hasChainToExit = true
			if targetIP != "2001:db8:10::30" {
				t.Fatalf("expected chain->exit diagnosis target to use IPv6, got %q", targetIP)
			}
		}
	}

	if !hasEntryToChain || !hasChainToExit {
		t.Fatalf("expected entry->chain and chain->exit steps, got entry=%v chain=%v", hasEntryToChain, hasChainToExit)
	}
}

func TestDiagnosisUsesFederationRuntimeForRemoteNodes(t *testing.T) {
	secret := "contract-jwt-secret"
	router, r := setupContractRouter(t, secret)
	now := time.Now().UnixMilli()

	remoteToken := "remote-diagnose-token"
	var remoteDiagnoseCalls int32
	remoteServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/federation/runtime/diagnose" {
			http.NotFound(w, r)
			return
		}
		if got := strings.TrimSpace(r.Header.Get("Authorization")); got != "Bearer "+remoteToken {
			w.WriteHeader(http.StatusUnauthorized)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"code": -1, "msg": "unauthorized"})
			return
		}

		var req map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"code": -1, "msg": "bad request"})
			return
		}
		if strings.TrimSpace(valueAsString(req["ip"])) != "10.50.0.30" {
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"code": -1, "msg": "unexpected target ip"})
			return
		}
		if valueAsInt(req["port"]) != 30003 {
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"code": -1, "msg": "unexpected target port"})
			return
		}

		atomic.AddInt32(&remoteDiagnoseCalls, 1)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"code": 0,
			"msg":  "success",
			"data": map[string]interface{}{
				"success":     true,
				"averageTime": 12.5,
				"packetLoss":  0,
				"message":     "remote tcp ok",
			},
		})
	}))
	defer remoteServer.Close()

	insertLocalNode := func(name, ip string) int64 {
		if err := r.DB().Exec(`
			INSERT INTO node(name, secret, server_ip, server_ip_v4, server_ip_v6, port, interface_name, version, http, tls, socks, created_time, updated_time, status, tcp_listen_addr, udp_listen_addr, inx)
			VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		`, name, name+"-secret", ip, ip, "", "30000-30010", "", "v1", 1, 1, 1, now, now, 1, "[::]", "[::]", 0).Error; err != nil {
			t.Fatalf("insert local node %s: %v", name, err)
		}
		return mustLastInsertID(t, r, name)
	}

	insertRemoteNode := func(name, ip string) int64 {
		if err := r.DB().Exec(`
			INSERT INTO node(name, secret, server_ip, server_ip_v4, server_ip_v6, port, interface_name, version, http, tls, socks, created_time, updated_time, status, tcp_listen_addr, udp_listen_addr, inx, is_remote, remote_url, remote_token, remote_config)
			VALUES(?, ?, ?, ?, ?, ?, ?, ?, 0, 0, 0, ?, ?, 1, ?, ?, ?, 1, ?, ?, ?)
		`, name, name+"-secret", ip, "", "", "31000-31010", "", "", now, now, "[::]", "[::]", 1, remoteServer.URL, remoteToken, `{"shareId": 123}`).Error; err != nil {
			t.Fatalf("insert remote node %s: %v", name, err)
		}
		return mustLastInsertID(t, r, name)
	}

	entryNodeID := insertLocalNode("entry-local", "10.50.0.10")
	remoteChainNodeID := insertRemoteNode("middle-remote", "10.50.0.20")
	exitNodeID := insertLocalNode("exit-local", "10.50.0.30")

	if err := r.DB().Exec(`
		INSERT INTO tunnel(name, traffic_ratio, type, protocol, flow, created_time, updated_time, status, in_ip, inx)
		VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, "diagnose-remote-tunnel", 1.0, 2, "tls", 99999, now, now, 1, nil, 0).Error; err != nil {
		t.Fatalf("insert tunnel: %v", err)
	}
	tunnelID := mustLastInsertID(t, r, "diagnose-remote-tunnel")

	if err := r.DB().Exec(`
		INSERT INTO chain_tunnel(tunnel_id, chain_type, node_id, port, strategy, inx, protocol)
		VALUES(?, 1, ?, 30001, 'round', 1, 'tls')
	`, tunnelID, entryNodeID).Error; err != nil {
		t.Fatalf("insert entry chain: %v", err)
	}
	if err := r.DB().Exec(`
		INSERT INTO chain_tunnel(tunnel_id, chain_type, node_id, port, strategy, inx, protocol)
		VALUES(?, 2, ?, 30002, 'round', 1, 'tls')
	`, tunnelID, remoteChainNodeID).Error; err != nil {
		t.Fatalf("insert middle chain: %v", err)
	}
	if err := r.DB().Exec(`
		INSERT INTO chain_tunnel(tunnel_id, chain_type, node_id, port, strategy, inx, protocol)
		VALUES(?, 3, ?, 30003, 'round', 1, 'tls')
	`, tunnelID, exitNodeID).Error; err != nil {
		t.Fatalf("insert exit chain: %v", err)
	}

	adminToken, err := auth.GenerateToken(1, "admin_user", 0, secret)
	if err != nil {
		t.Fatalf("generate admin token: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/tunnel/diagnose", bytes.NewBufferString(`{"tunnelId":`+strconv.FormatInt(tunnelID, 10)+`}`))
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

	remoteStepFound := false
	for _, raw := range results {
		item, ok := raw.(map[string]interface{})
		if !ok {
			continue
		}
		if valueAsInt(item["fromChainType"]) == 2 && valueAsInt(item["toChainType"]) == 3 {
			remoteStepFound = true
			if !valueAsBool(item["success"]) {
				t.Fatalf("expected remote chain->exit diagnosis success, got item=%v", item)
			}
			if strings.TrimSpace(valueAsString(item["message"])) != "remote tcp ok" {
				t.Fatalf("expected remote diagnosis message, got %q", valueAsString(item["message"]))
			}
		}
	}

	if !remoteStepFound {
		t.Fatalf("expected chain->exit diagnosis item for remote node")
	}
	if atomic.LoadInt32(&remoteDiagnoseCalls) == 0 {
		t.Fatalf("expected federation runtime diagnose endpoint to be called")
	}
}

func TestTunnelDiagnosisUsesConfiguredConnectIPContract(t *testing.T) {
	secret := "contract-jwt-secret"
	router, r := setupContractRouter(t, secret)
	now := time.Now().UnixMilli()

	insertNode := func(name, ip string) int64 {
		if err := r.DB().Exec(`
			INSERT INTO node(name, secret, server_ip, server_ip_v4, server_ip_v6, port, interface_name, version, http, tls, socks, created_time, updated_time, status, tcp_listen_addr, udp_listen_addr, inx)
			VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		`, name, name+"-secret", ip, ip, "", "30000-30010", "", "v1", 1, 1, 1, now, now, 1, "[::]", "[::]", 0).Error; err != nil {
			t.Fatalf("insert node %s: %v", name, err)
		}
		return mustLastInsertID(t, r, name)
	}

	entryNodeID := insertNode("entry-connectip", "10.80.0.10")
	middleNodeID := insertNode("middle-connectip", "10.80.0.20")
	exitNodeID := insertNode("exit-connectip", "10.80.0.30")

	if err := r.DB().Exec(`
		INSERT INTO tunnel(name, traffic_ratio, type, protocol, flow, created_time, updated_time, status, in_ip, inx)
		VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, "diagnose-connectip-tunnel", 1.0, 2, "tls", 99999, now, now, 1, nil, 0).Error; err != nil {
		t.Fatalf("insert tunnel: %v", err)
	}
	tunnelID := mustLastInsertID(t, r, "diagnose-connectip-tunnel")

	if err := r.DB().Exec(`
		INSERT INTO chain_tunnel(tunnel_id, chain_type, node_id, port, strategy, inx, protocol)
		VALUES(?, 1, ?, 30001, 'round', 1, 'tls')
	`, tunnelID, entryNodeID).Error; err != nil {
		t.Fatalf("insert entry chain: %v", err)
	}
	if err := r.DB().Exec(`
		INSERT INTO chain_tunnel(tunnel_id, chain_type, node_id, port, strategy, inx, protocol, connect_ip)
		VALUES(?, 2, ?, 30002, 'round', 1, 'tls', ?)
	`, tunnelID, middleNodeID, "10.99.0.22").Error; err != nil {
		t.Fatalf("insert middle chain: %v", err)
	}
	if err := r.DB().Exec(`
		INSERT INTO chain_tunnel(tunnel_id, chain_type, node_id, port, strategy, inx, protocol, connect_ip)
		VALUES(?, 3, ?, 30003, 'round', 1, 'tls', ?)
	`, tunnelID, exitNodeID, "10.99.0.33").Error; err != nil {
		t.Fatalf("insert exit chain: %v", err)
	}

	adminToken, err := auth.GenerateToken(1, "admin_user", 0, secret)
	if err != nil {
		t.Fatalf("generate admin token: %v", err)
	}

	t.Run("normal diagnose should use configured connectIp", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/tunnel/diagnose", bytes.NewBufferString(`{"tunnelId":`+strconv.FormatInt(tunnelID, 10)+`}`))
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

		entryToMiddleOK := false
		middleToExitOK := false
		for _, raw := range results {
			item, ok := raw.(map[string]interface{})
			if !ok {
				continue
			}
			from := valueAsInt(item["fromChainType"])
			to := valueAsInt(item["toChainType"])
			targetIP := strings.TrimSpace(valueAsString(item["targetIp"]))

			if from == 1 && to == 2 && targetIP == "10.99.0.22" {
				entryToMiddleOK = true
			}
			if from == 2 && to == 3 && targetIP == "10.99.0.33" {
				middleToExitOK = true
			}
		}

		if !entryToMiddleOK || !middleToExitOK {
			t.Fatalf("expected connectIp targets 10.99.0.22/10.99.0.33, got entry=%v middle=%v", entryToMiddleOK, middleToExitOK)
		}
	})

	t.Run("stream diagnose start items should use configured connectIp", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/tunnel/diagnose/stream", bytes.NewBufferString(`{"tunnelId":`+strconv.FormatInt(tunnelID, 10)+`}`))
		req.Header.Set("Authorization", adminToken)
		res := httptest.NewRecorder()

		router.ServeHTTP(res, req)

		if res.Code != http.StatusOK {
			t.Fatalf("expected status 200, got %d", res.Code)
		}

		scanner := bufio.NewScanner(bytes.NewReader(res.Body.Bytes()))
		startFound := false
		entryToMiddleOK := false
		middleToExitOK := false
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line == "" {
				continue
			}
			var event map[string]interface{}
			if err := json.Unmarshal([]byte(line), &event); err != nil {
				continue
			}
			if strings.TrimSpace(valueAsString(event["type"])) != "start" {
				continue
			}
			startFound = true
			data, ok := event["data"].(map[string]interface{})
			if !ok {
				break
			}
			items, ok := data["items"].([]interface{})
			if !ok {
				break
			}
			for _, raw := range items {
				item, ok := raw.(map[string]interface{})
				if !ok {
					continue
				}
				from := valueAsInt(item["fromChainType"])
				to := valueAsInt(item["toChainType"])
				targetIP := strings.TrimSpace(valueAsString(item["targetIp"]))
				if from == 1 && to == 2 && targetIP == "10.99.0.22" {
					entryToMiddleOK = true
				}
				if from == 2 && to == 3 && targetIP == "10.99.0.33" {
					middleToExitOK = true
				}
			}
			break
		}
		if err := scanner.Err(); err != nil {
			t.Fatalf("scan stream body: %v", err)
		}
		if !startFound {
			t.Fatalf("expected start event in stream response")
		}
		if !entryToMiddleOK || !middleToExitOK {
			t.Fatalf("expected start items with connectIp targets 10.99.0.22/10.99.0.33, got entry=%v middle=%v", entryToMiddleOK, middleToExitOK)
		}
	})
}
