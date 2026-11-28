package contract_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"go-backend/internal/http/response"
)

func TestFederationConnectReturnsIPv6AndForwardIngressPrefersV6(t *testing.T) {
	secret := "contract-jwt-secret"
	router, r := setupContractRouter(t, secret)
	now := time.Now().UnixMilli()

	// Seed a dual-stack node and a peer share pointing to it.
	if err := r.DB().Exec(`
		INSERT INTO node(name, secret, server_ip, server_ip_v4, server_ip_v6, extra_ips, port, interface_name, version, http, tls, socks, created_time, updated_time, status, tcp_listen_addr, udp_listen_addr, inx)
		VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, "dual-stack-peer", "peer-secret", "198.51.100.10", "198.51.100.10", "2001:db8:abcd::10", "198.51.100.11,2001:db8:abcd::11", "10000-20000", "", "v1", 1, 1, 1, now, now, 1, "[::]", "[::]", 0).Error; err != nil {
		t.Fatalf("insert dual-stack node: %v", err)
	}
	nodeID := mustLastInsertID(t, r, "dual-stack-peer")

	if err := r.DB().Exec(`
		INSERT INTO peer_share(name, node_id, token, max_bandwidth, current_flow, expiry_time, port_range_start, port_range_end, is_active, created_time, updated_time)
		VALUES(?, ?, ?, 0, 0, 0, 10000, 10100, 1, ?, ?)
	`, "share-dual", nodeID, "share-token-v6", now, now).Error; err != nil {
		t.Fatalf("insert peer_share: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/federation/connect", bytes.NewBufferString(`{"localDomain":"panel.example.com"}`))
	req.Header.Set("Authorization", "Bearer share-token-v6")
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	var out response.R
	if err := json.NewDecoder(res.Body).Decode(&out); err != nil {
		t.Fatalf("decode federation/connect response: %v", err)
	}
	if out.Code != 0 {
		t.Fatalf("expected federation/connect code 0, got %+v", out)
	}
	data := out.Data.(map[string]interface{})
	if data["serverIp"] != "198.51.100.10" {
		t.Fatalf("unexpected serverIp: %v", data["serverIp"])
	}
	if data["serverIpV4"] != "198.51.100.10" {
		t.Fatalf("expected serverIpV4=198.51.100.10, got %v", data["serverIpV4"])
	}
	if data["serverIpV6"] != "2001:db8:abcd::10" {
		t.Fatalf("expected serverIpV6=2001:db8:abcd::10, got %v", data["serverIpV6"])
	}
	if data["extraIPs"] != "198.51.100.11,2001:db8:abcd::11" {
		t.Fatalf("expected extraIPs preserved, got %v", data["extraIPs"])
	}

	// Verify CreateRemoteNode + UpdateNodeRemoteSync persist IPv4/IPv6/extraIPs.
	if err := r.CreateRemoteNode(
		"remote-dual (Remote)",
		"remote-secret",
		"198.51.100.10",
		"198.51.100.10",
		"2001:db8:abcd::10",
		"2001:db8:abcd::11",
		"10000-10100",
		now,
		1,
		1,
		"https://peer.example.com",
		"share-token-v6",
		"{}",
	); err != nil {
		t.Fatalf("CreateRemoteNode: %v", err)
	}
	remoteID := mustLastInsertID(t, r, "remote-dual (Remote)")
	remoteNode, err := r.GetNodeByID(remoteID)
	if err != nil || remoteNode == nil {
		t.Fatalf("GetNodeByID remote: %v", err)
	}
	if !remoteNode.ServerIPV4.Valid || remoteNode.ServerIPV4.String != "198.51.100.10" {
		t.Fatalf("expected remote server_ip_v4 persisted, got %+v", remoteNode.ServerIPV4)
	}
	if !remoteNode.ServerIPV6.Valid || remoteNode.ServerIPV6.String != "2001:db8:abcd::10" {
		t.Fatalf("expected remote server_ip_v6 persisted, got %+v", remoteNode.ServerIPV6)
	}

	// Verify ForwardEntries respects tunnel ip_preference="v6" for relays/forwards on a remote dual-stack node.
	if err := r.DB().Exec(`
		INSERT INTO tunnel(name, traffic_ratio, type, protocol, flow, created_time, updated_time, status, in_ip, inx, ip_preference)
		VALUES(?, 1.0, 1, 'tls', 99999, ?, ?, 1, ?, 0, 'v6')
	`, "remote-v6-tunnel", now, now, "2001:db8:abcd::10,198.51.100.10").Error; err != nil {
		t.Fatalf("insert v6 tunnel: %v", err)
	}
	tunnelID := mustLastInsertID(t, r, "remote-v6-tunnel")

	if err := r.DB().Exec(`
		INSERT INTO forward(user_id, user_name, name, tunnel_id, remote_addr, strategy, in_flow, out_flow, created_time, updated_time, status, inx)
		VALUES(1, 'admin_user', 'remote-v6-relay', ?, '203.0.113.9:443', 'fifo', 0, 0, ?, ?, 1, 0)
	`, tunnelID, now, now).Error; err != nil {
		t.Fatalf("insert forward: %v", err)
	}
	forwardID := mustLastInsertID(t, r, "remote-v6-relay")

	if err := r.DB().Exec(`
		INSERT INTO forward_port(forward_id, node_id, port, in_ip)
		VALUES(?, ?, 10080, '')
	`, forwardID, remoteID).Error; err != nil {
		t.Fatalf("insert forward_port: %v", err)
	}

	entries, err := r.ForwardEntries(forwardID, tunnelID)
	if err != nil {
		t.Fatalf("ForwardEntries: %v", err)
	}
	if len(entries) != 1 || entries[0].Host != "2001:db8:abcd::10" || entries[0].Port != 10080 {
		t.Fatalf("expected IPv6 forward entry 2001:db8:abcd::10:10080, got %+v", entries)
	}
}
