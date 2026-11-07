package contract_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"go-backend/internal/auth"
	"go-backend/internal/http/response"
	"go-backend/internal/store/repo"
)

func TestFederationForwardCardFlowLinkageContract(t *testing.T) {
	secret := "federation-forward-flow-contract-jwt"
	router, r := setupContractRouter(t, secret)

	adminToken, err := auth.GenerateToken(1, "admin_user", 0, secret)
	if err != nil {
		t.Fatalf("generate admin token: %v", err)
	}

	now := time.Now().UnixMilli()

	if err := r.DB().Exec(`
		INSERT INTO node(name, secret, server_ip, server_ip_v4, server_ip_v6, port, interface_name, version, http, tls, socks, created_time, updated_time, status, tcp_listen_addr, udp_listen_addr, inx, is_remote, remote_url, remote_token, remote_config)
		VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, "flow-local-node", "flow-local-secret", "10.20.30.40", "10.20.30.40", "", "32000-32020", "", "v1", 1, 1, 1, now, now, 1, "[::]", "[::]", 0, 0, "", "", "").Error; err != nil {
		t.Fatalf("insert local node: %v", err)
	}
	nodeID := mustLastInsertID(t, r, "flow-local-node")

	shareToken := "flow-linkage-share-token"
	if err := r.CreatePeerShare(&repo.PeerShare{
		Name:           "flow-linkage-share",
		NodeID:         nodeID,
		Token:          shareToken,
		MaxBandwidth:   0,
		CurrentFlow:    1536,
		ExpiryTime:     0,
		PortRangeStart: 32000,
		PortRangeEnd:   32020,
		IsActive:       1,
		CreatedTime:    now,
		UpdatedTime:    now,
	}); err != nil {
		t.Fatalf("create share: %v", err)
	}
	share, err := r.GetPeerShareByToken(shareToken)
	if err != nil || share == nil {
		t.Fatalf("load share: %v", err)
	}

	tunnelName := fmt.Sprintf("Share-%d-Port-%d", share.ID, 32001)
	if err := r.DB().Exec(`
		INSERT INTO tunnel(name, type, protocol, flow, created_time, updated_time, status, in_ip, inx)
		VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, tunnelName, 1, "tcp", 1, now, now, 1, "", 0).Error; err != nil {
		t.Fatalf("insert share tunnel: %v", err)
	}
	tunnelID := mustLastInsertID(t, r, "flow-share-tunnel")

	if err := r.DB().Exec(`
		INSERT INTO forward(user_id, user_name, name, tunnel_id, remote_addr, strategy, in_flow, out_flow, created_time, updated_time, status, inx)
		VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, 1, "admin_user", "flow-linkage-forward", tunnelID, "1.1.1.1:443", "fifo", 0, 0, now, now, 1, 0).Error; err != nil {
		t.Fatalf("insert forward: %v", err)
	}
	forwardID := mustLastInsertID(t, r, "flow-linkage-forward")

	if err := r.DB().Exec(`INSERT INTO forward_port(forward_id, node_id, port) VALUES(?, ?, ?)`, forwardID, nodeID, 32001).Error; err != nil {
		t.Fatalf("insert forward_port: %v", err)
	}

	forwardOut := requestContractEnvelope(t, router, adminToken, "/api/v1/forward/list", nil)
	if forwardOut.Code != 0 {
		t.Fatalf("forward list failed: code=%d msg=%q", forwardOut.Code, forwardOut.Msg)
	}
	forwardRows := mustContractSlice(t, forwardOut.Data, "forward list data")

	var targetForward map[string]interface{}
	for _, row := range forwardRows {
		m, ok := row.(map[string]interface{})
		if !ok {
			continue
		}
		if contractValueAsInt64(m["id"]) == forwardID {
			targetForward = m
			break
		}
	}
	if targetForward == nil {
		t.Fatalf("target forward %d not found in /forward/list response", forwardID)
	}

	shareOut := requestContractEnvelope(t, router, adminToken, "/api/v1/federation/share/list", nil)
	if shareOut.Code != 0 {
		t.Fatalf("share list failed: code=%d msg=%q", shareOut.Code, shareOut.Msg)
	}
	localShareRows := mustContractSlice(t, shareOut.Data, "share list data")

	remoteUsageOut := requestContractEnvelope(t, router, adminToken, "/api/v1/federation/share/remote-usage/list", nil)
	if remoteUsageOut.Code != 0 {
		t.Fatalf("remote usage list failed: code=%d msg=%q", remoteUsageOut.Code, remoteUsageOut.Msg)
	}
	remoteUsageRows := mustContractSlice(t, remoteUsageOut.Data, "remote usage data")
	if len(remoteUsageRows) != 0 {
		t.Fatalf("expected no remote usage rows in local-only fixture, got %d", len(remoteUsageRows))
	}

	flowByShare := make(map[int64]int64)
	for _, row := range remoteUsageRows {
		m, ok := row.(map[string]interface{})
		if !ok {
			continue
		}
		shareID := contractValueAsInt64(m["shareId"])
		currentFlow := contractValueAsInt64(m["currentFlow"])
		if shareID > 0 && currentFlow > 0 {
			if currentFlow > flowByShare[shareID] {
				flowByShare[shareID] = currentFlow
			}
		}
	}
	for _, row := range localShareRows {
		m, ok := row.(map[string]interface{})
		if !ok {
			continue
		}
		shareID := contractValueAsInt64(m["id"])
		currentFlow := contractValueAsInt64(m["currentFlow"])
		if shareID > 0 && currentFlow > 0 {
			if currentFlow > flowByShare[shareID] {
				flowByShare[shareID] = currentFlow
			}
		}
	}

	parsedShareID := contractParseShareIDFromTunnelName(contractValueAsString(targetForward["tunnelName"]))
	if parsedShareID != share.ID {
		t.Fatalf("expected parsed shareID=%d, got %d (tunnelName=%q)", share.ID, parsedShareID, contractValueAsString(targetForward["tunnelName"]))
	}

	forwardCountByShare := make(map[int64]int)
	for _, row := range forwardRows {
		m, ok := row.(map[string]interface{})
		if !ok {
			continue
		}
		sid := contractParseShareIDFromTunnelName(contractValueAsString(m["tunnelName"]))
		if sid > 0 && flowByShare[sid] > 0 {
			forwardCountByShare[sid] = forwardCountByShare[sid] + 1
		}
	}

	directFlow := contractValueAsInt64(targetForward["inFlow"]) + contractValueAsInt64(targetForward["outFlow"])
	if directFlow != 0 {
		t.Fatalf("fixture expectation failed: directFlow should be 0, got %d", directFlow)
	}

	shareFlow := flowByShare[parsedShareID]
	if shareFlow <= 0 {
		t.Fatalf("expected merged share flow > 0 for share %d", parsedShareID)
	}

	count := forwardCountByShare[parsedShareID]
	if count <= 0 {
		count = 1
	}
	estimated := shareFlow / int64(count)
	if estimated < 1 {
		estimated = 1
	}
	displayFlow := estimated

	if displayFlow <= 0 {
		t.Fatalf("expected displayFlow > 0 after frontend-style merge, got %d", displayFlow)
	}
	if displayFlow != share.CurrentFlow {
		t.Fatalf("expected displayFlow=%d, got %d", share.CurrentFlow, displayFlow)
	}
}

func TestFederationForwardCardFlowLinkageContractSplitShareFlowAcrossMultipleForwards(t *testing.T) {
	secret := "federation-forward-split-flow-contract-jwt"
	router, r := setupContractRouter(t, secret)

	adminToken, err := auth.GenerateToken(1, "admin_user", 0, secret)
	if err != nil {
		t.Fatalf("generate admin token: %v", err)
	}

	now := time.Now().UnixMilli()

	if err := r.DB().Exec(`
		INSERT INTO node(name, secret, server_ip, server_ip_v4, server_ip_v6, port, interface_name, version, http, tls, socks, created_time, updated_time, status, tcp_listen_addr, udp_listen_addr, inx, is_remote, remote_url, remote_token, remote_config)
		VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, "flow-split-local-node", "flow-split-local-secret", "10.21.31.41", "10.21.31.41", "", "32100-32120", "", "v1", 1, 1, 1, now, now, 1, "[::]", "[::]", 0, 0, "", "", "").Error; err != nil {
		t.Fatalf("insert local node: %v", err)
	}
	nodeID := mustLastInsertID(t, r, "flow-split-local-node")

	shareToken := "flow-split-share-token"
	if err := r.CreatePeerShare(&repo.PeerShare{
		Name:           "flow-split-share",
		NodeID:         nodeID,
		Token:          shareToken,
		MaxBandwidth:   0,
		CurrentFlow:    4097,
		ExpiryTime:     0,
		PortRangeStart: 32100,
		PortRangeEnd:   32120,
		IsActive:       1,
		CreatedTime:    now,
		UpdatedTime:    now,
	}); err != nil {
		t.Fatalf("create share: %v", err)
	}
	share, err := r.GetPeerShareByToken(shareToken)
	if err != nil || share == nil {
		t.Fatalf("load share: %v", err)
	}

	createShareForward := func(name string, port int) int64 {
		t.Helper()

		tunnelName := fmt.Sprintf("Share-%d-Port-%d", share.ID, port)
		if err := r.DB().Exec(`
			INSERT INTO tunnel(name, type, protocol, flow, created_time, updated_time, status, in_ip, inx)
			VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?)
		`, tunnelName, 1, "tcp", 1, now, now, 1, "", 0).Error; err != nil {
			t.Fatalf("insert share tunnel: %v", err)
		}
		tunnelID := mustLastInsertID(t, r, "flow-split-tunnel")

		if err := r.DB().Exec(`
			INSERT INTO forward(user_id, user_name, name, tunnel_id, remote_addr, strategy, in_flow, out_flow, created_time, updated_time, status, inx)
			VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		`, 1, "admin_user", name, tunnelID, "1.1.1.1:443", "fifo", 0, 0, now, now, 1, 0).Error; err != nil {
			t.Fatalf("insert forward: %v", err)
		}
		forwardID := mustLastInsertID(t, r, name)

		if err := r.DB().Exec(`INSERT INTO forward_port(forward_id, node_id, port) VALUES(?, ?, ?)`, forwardID, nodeID, port).Error; err != nil {
			t.Fatalf("insert forward_port: %v", err)
		}

		return forwardID
	}

	forwardIDA := createShareForward("flow-split-forward-a", 32101)
	forwardIDB := createShareForward("flow-split-forward-b", 32102)

	forwardOut := requestContractEnvelope(t, router, adminToken, "/api/v1/forward/list", nil)
	if forwardOut.Code != 0 {
		t.Fatalf("forward list failed: code=%d msg=%q", forwardOut.Code, forwardOut.Msg)
	}
	forwardRows := mustContractSlice(t, forwardOut.Data, "forward list data")

	shareOut := requestContractEnvelope(t, router, adminToken, "/api/v1/federation/share/list", nil)
	if shareOut.Code != 0 {
		t.Fatalf("share list failed: code=%d msg=%q", shareOut.Code, shareOut.Msg)
	}
	localShareRows := mustContractSlice(t, shareOut.Data, "share list data")

	remoteUsageOut := requestContractEnvelope(t, router, adminToken, "/api/v1/federation/share/remote-usage/list", nil)
	if remoteUsageOut.Code != 0 {
		t.Fatalf("remote usage list failed: code=%d msg=%q", remoteUsageOut.Code, remoteUsageOut.Msg)
	}
	remoteUsageRows := mustContractSlice(t, remoteUsageOut.Data, "remote usage data")
	if len(remoteUsageRows) != 0 {
		t.Fatalf("expected no remote usage rows in local-only fixture, got %d", len(remoteUsageRows))
	}

	flowByShare := make(map[int64]int64)
	for _, row := range remoteUsageRows {
		m, ok := row.(map[string]interface{})
		if !ok {
			continue
		}
		shareID := contractValueAsInt64(m["shareId"])
		currentFlow := contractValueAsInt64(m["currentFlow"])
		if shareID > 0 && currentFlow > 0 {
			if currentFlow > flowByShare[shareID] {
				flowByShare[shareID] = currentFlow
			}
		}
	}
	for _, row := range localShareRows {
		m, ok := row.(map[string]interface{})
		if !ok {
			continue
		}
		shareID := contractValueAsInt64(m["id"])
		currentFlow := contractValueAsInt64(m["currentFlow"])
		if shareID > 0 && currentFlow > 0 {
			if currentFlow > flowByShare[shareID] {
				flowByShare[shareID] = currentFlow
			}
		}
	}

	shareFlow := flowByShare[share.ID]
	if shareFlow <= 0 {
		t.Fatalf("expected merged share flow > 0 for share %d", share.ID)
	}

	forwardCountByShare := make(map[int64]int)
	for _, row := range forwardRows {
		m, ok := row.(map[string]interface{})
		if !ok {
			continue
		}
		sid := contractParseShareIDFromTunnelName(contractValueAsString(m["tunnelName"]))
		if sid > 0 && flowByShare[sid] > 0 {
			forwardCountByShare[sid] = forwardCountByShare[sid] + 1
		}
	}

	count := forwardCountByShare[share.ID]
	if count != 2 {
		t.Fatalf("expected 2 forwards sharing share %d, got %d", share.ID, count)
	}

	expectedEach := shareFlow / int64(count)
	if expectedEach < 1 {
		expectedEach = 1
	}

	findForward := func(forwardID int64) map[string]interface{} {
		t.Helper()
		for _, row := range forwardRows {
			m, ok := row.(map[string]interface{})
			if !ok {
				continue
			}
			if contractValueAsInt64(m["id"]) == forwardID {
				return m
			}
		}
		t.Fatalf("forward %d not found in /forward/list response", forwardID)
		return nil
	}

	for _, forwardID := range []int64{forwardIDA, forwardIDB} {
		forward := findForward(forwardID)
		sid := contractParseShareIDFromTunnelName(contractValueAsString(forward["tunnelName"]))
		if sid != share.ID {
			t.Fatalf("expected parsed shareID=%d, got %d for forward %d", share.ID, sid, forwardID)
		}

		directFlow := contractValueAsInt64(forward["inFlow"]) + contractValueAsInt64(forward["outFlow"])
		if directFlow != 0 {
			t.Fatalf("fixture expectation failed: directFlow should be 0 for forward %d, got %d", forwardID, directFlow)
		}

		displayFlow := int64(0)
		if directFlow > 0 {
			displayFlow = directFlow
		} else {
			shareFlowForForward := flowByShare[sid]
			if shareFlowForForward > 0 {
				cnt := forwardCountByShare[sid]
				if cnt <= 0 {
					cnt = 1
				}
				estimated := shareFlowForForward / int64(cnt)
				if estimated < 1 {
					estimated = 1
				}
				displayFlow = estimated
			}
		}

		if displayFlow <= 0 {
			t.Fatalf("expected displayFlow > 0 for forward %d, got %d", forwardID, displayFlow)
		}
		if displayFlow != expectedEach {
			t.Fatalf("expected displayFlow=%d for forward %d, got %d", expectedEach, forwardID, displayFlow)
		}
	}
}

func TestFederationForwardCardFlowLinkageContractResolvesShareByTunnelBindingWhenTunnelNameIsCustom(t *testing.T) {
	secret := "federation-forward-binding-flow-contract-jwt"
	router, r := setupContractRouter(t, secret)

	adminToken, err := auth.GenerateToken(1, "admin_user", 0, secret)
	if err != nil {
		t.Fatalf("generate admin token: %v", err)
	}

	now := time.Now().UnixMilli()
	remoteShareID := int64(901)
	remoteShareFlow := int64(5000)

	if err := r.DB().Exec(`
		INSERT INTO node(name, secret, server_ip, server_ip_v4, server_ip_v6, port, interface_name, version, http, tls, socks, created_time, updated_time, status, tcp_listen_addr, udp_listen_addr, inx, is_remote, remote_url, remote_token, remote_config)
		VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`,
		"flow-binding-remote-node", "flow-binding-remote-secret", "10.31.41.51", "10.31.41.51", "", "33000-33020", "", "v1", 1, 1, 1, now, now, 1, "[::]", "[::]", 0, 1, "", "", fmt.Sprintf(`{"shareId":%d,"maxBandwidth":0,"currentFlow":%d,"portRangeStart":33000,"portRangeEnd":33020}`, remoteShareID, remoteShareFlow),
	).Error; err != nil {
		t.Fatalf("insert remote node: %v", err)
	}
	remoteNodeID := mustLastInsertID(t, r, "flow-binding-remote-node")

	if err := r.DB().Exec(`
		INSERT INTO tunnel(name, type, protocol, flow, created_time, updated_time, status, in_ip, inx)
		VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, "federation-port-forward-custom-name", 1, "tcp", 1, now, now, 1, "", 0).Error; err != nil {
		t.Fatalf("insert custom tunnel: %v", err)
	}
	tunnelID := mustLastInsertID(t, r, "flow-binding-custom-tunnel")

	if err := r.DB().Exec(`
		INSERT INTO forward(user_id, user_name, name, tunnel_id, remote_addr, strategy, in_flow, out_flow, created_time, updated_time, status, inx)
		VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, 1, "admin_user", "flow-binding-forward", tunnelID, "1.1.1.1:443", "fifo", 0, 0, now, now, 1, 0).Error; err != nil {
		t.Fatalf("insert forward: %v", err)
	}
	forwardID := mustLastInsertID(t, r, "flow-binding-forward")

	if err := r.DB().Exec(`INSERT INTO forward_port(forward_id, node_id, port) VALUES(?, ?, ?)`, forwardID, remoteNodeID, 33001).Error; err != nil {
		t.Fatalf("insert forward_port: %v", err)
	}

	forwardOut := requestContractEnvelope(t, router, adminToken, "/api/v1/forward/list", nil)
	if forwardOut.Code != 0 {
		t.Fatalf("forward list failed: code=%d msg=%q", forwardOut.Code, forwardOut.Msg)
	}
	forwardRows := mustContractSlice(t, forwardOut.Data, "forward list data")

	shareOut := requestContractEnvelope(t, router, adminToken, "/api/v1/federation/share/list", nil)
	if shareOut.Code != 0 {
		t.Fatalf("share list failed: code=%d msg=%q", shareOut.Code, shareOut.Msg)
	}
	localShareRows := mustContractSlice(t, shareOut.Data, "share list data")

	remoteUsageOut := requestContractEnvelope(t, router, adminToken, "/api/v1/federation/share/remote-usage/list", nil)
	if remoteUsageOut.Code != 0 {
		t.Fatalf("remote usage list failed: code=%d msg=%q", remoteUsageOut.Code, remoteUsageOut.Msg)
	}
	remoteUsageRows := mustContractSlice(t, remoteUsageOut.Data, "remote usage data")
	if len(remoteUsageRows) == 0 {
		t.Fatalf("expected non-empty remote usage rows")
	}

	findForward := func(id int64) map[string]interface{} {
		t.Helper()
		for _, row := range forwardRows {
			m, ok := row.(map[string]interface{})
			if !ok {
				continue
			}
			if contractValueAsInt64(m["id"]) == id {
				return m
			}
		}
		t.Fatalf("forward %d not found in /forward/list response", id)
		return nil
	}

	flowByShare := make(map[int64]int64)
	shareIDsByTunnel := make(map[int64]map[int64]struct{})

	for _, row := range remoteUsageRows {
		m, ok := row.(map[string]interface{})
		if !ok {
			continue
		}
		shareID := contractValueAsInt64(m["shareId"])
		currentFlow := contractValueAsInt64(m["currentFlow"])
		if shareID > 0 && currentFlow > 0 {
			if currentFlow > flowByShare[shareID] {
				flowByShare[shareID] = currentFlow
			}
		}

		bindings, _ := m["bindings"].([]interface{})
		for _, bindingRaw := range bindings {
			binding, ok := bindingRaw.(map[string]interface{})
			if !ok {
				continue
			}
			tunnelIDVal := contractValueAsInt64(binding["tunnelId"])
			chainType := contractValueAsInt64(binding["chainType"])
			if shareID <= 0 || tunnelIDVal <= 0 {
				continue
			}
			if chainType != 1 {
				continue
			}
			setByTunnel, ok := shareIDsByTunnel[tunnelIDVal]
			if !ok {
				setByTunnel = make(map[int64]struct{})
				shareIDsByTunnel[tunnelIDVal] = setByTunnel
			}
			setByTunnel[shareID] = struct{}{}
		}
	}

	for _, row := range localShareRows {
		m, ok := row.(map[string]interface{})
		if !ok {
			continue
		}
		shareID := contractValueAsInt64(m["id"])
		currentFlow := contractValueAsInt64(m["currentFlow"])
		if shareID > 0 && currentFlow > 0 {
			if currentFlow > flowByShare[shareID] {
				flowByShare[shareID] = currentFlow
			}
		}
	}

	targetForward := findForward(forwardID)
	parsedByName := contractParseShareIDFromTunnelName(contractValueAsString(targetForward["tunnelName"]))
	if parsedByName != 0 {
		t.Fatalf("expected custom tunnel name cannot be parsed as Share-*-Port-*, got %d", parsedByName)
	}

	resolveShareIDForForward := func(forward map[string]interface{}) int64 {
		candidates := make(map[int64]struct{})

		shareIDFromName := contractParseShareIDFromTunnelName(contractValueAsString(forward["tunnelName"]))
		if shareIDFromName > 0 {
			candidates[shareIDFromName] = struct{}{}
		}

		tunnelIDVal := contractValueAsInt64(forward["tunnelId"])
		if setByTunnel, ok := shareIDsByTunnel[tunnelIDVal]; ok {
			for sid := range setByTunnel {
				candidates[sid] = struct{}{}
			}
		}

		var bestShareID int64
		bestFlow := int64(0)
		for sid := range candidates {
			flow := flowByShare[sid]
			if flow > bestFlow {
				bestFlow = flow
				bestShareID = sid
			}
		}
		return bestShareID
	}

	resolvedShareID := resolveShareIDForForward(targetForward)
	if resolvedShareID != remoteShareID {
		t.Fatalf("expected resolved shareID=%d via tunnel binding, got %d", remoteShareID, resolvedShareID)
	}

	forwardCountByShare := make(map[int64]int)
	resolvedByForwardID := make(map[int64]int64)
	for _, row := range forwardRows {
		m, ok := row.(map[string]interface{})
		if !ok {
			continue
		}
		fid := contractValueAsInt64(m["id"])
		sid := resolveShareIDForForward(m)
		if sid > 0 {
			resolvedByForwardID[fid] = sid
		}
		if sid > 0 && flowByShare[sid] > 0 {
			forwardCountByShare[sid] = forwardCountByShare[sid] + 1
		}
	}

	directFlow := contractValueAsInt64(targetForward["inFlow"]) + contractValueAsInt64(targetForward["outFlow"])
	if directFlow != 0 {
		t.Fatalf("fixture expectation failed: directFlow should be 0, got %d", directFlow)
	}

	shareFlow := flowByShare[resolvedByForwardID[forwardID]]
	if shareFlow <= 0 {
		t.Fatalf("expected merged share flow > 0 for resolved share %d", resolvedByForwardID[forwardID])
	}

	count := forwardCountByShare[resolvedByForwardID[forwardID]]
	if count <= 0 {
		count = 1
	}
	estimated := shareFlow / int64(count)
	if estimated < 1 {
		estimated = 1
	}

	displayFlow := estimated
	if displayFlow <= 0 {
		t.Fatalf("expected displayFlow > 0 after tunnel-binding-based merge, got %d", displayFlow)
	}
	if displayFlow != remoteShareFlow {
		t.Fatalf("expected displayFlow=%d, got %d", remoteShareFlow, displayFlow)
	}
}

func requestContractEnvelope(t *testing.T, router http.Handler, token string, path string, body interface{}) response.R {
	t.Helper()

	payload := []byte("{}")
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal request body for %s: %v", path, err)
		}
		payload = raw
	}

	req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(payload))
	req.Header.Set("Authorization", token)
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected http 200 for %s, got %d", path, res.Code)
	}

	var out response.R
	if err := json.NewDecoder(res.Body).Decode(&out); err != nil {
		t.Fatalf("decode response for %s: %v", path, err)
	}

	return out
}

func mustContractSlice(t *testing.T, data interface{}, label string) []interface{} {
	t.Helper()

	rows, ok := data.([]interface{})
	if !ok {
		t.Fatalf("expected %s to be []interface{}, got %T", label, data)
	}
	return rows
}

func contractParseShareIDFromTunnelName(tunnelName string) int64 {
	normalized := strings.TrimSpace(tunnelName)
	if !strings.HasPrefix(normalized, "Share-") {
		return 0
	}
	raw := strings.TrimPrefix(normalized, "Share-")
	idx := strings.Index(raw, "-Port-")
	if idx <= 0 {
		return 0
	}
	shareID, err := strconv.ParseInt(strings.TrimSpace(raw[:idx]), 10, 64)
	if err != nil || shareID <= 0 {
		return 0
	}
	return shareID
}

func contractValueAsInt64(v interface{}) int64 {
	switch n := v.(type) {
	case int64:
		return n
	case int:
		return int64(n)
	case float64:
		return int64(n)
	case json.Number:
		i, err := n.Int64()
		if err == nil {
			return i
		}
		f, err := n.Float64()
		if err == nil {
			return int64(f)
		}
		return 0
	case string:
		i, err := strconv.ParseInt(strings.TrimSpace(n), 10, 64)
		if err == nil {
			return i
		}
		return 0
	default:
		return 0
	}
}

func contractValueAsString(v interface{}) string {
	s, _ := v.(string)
	return s
}
