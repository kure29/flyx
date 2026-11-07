package handler

import (
	"path/filepath"
	"testing"
	"time"

	"go-backend/internal/store/repo"
)

func TestReconstructTunnelState_PreservesConnectIP(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "reconstruct-connect-ip.db")
	r, err := repo.Open(dbPath)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = r.Close() })

	h := New(r, "secret")
	now := time.Now().UnixMilli()

	if err := r.DB().Exec(`
		INSERT INTO tunnel(id, name, traffic_ratio, type, protocol, flow, created_time, updated_time, status, in_ip, inx)
		VALUES(1, 'reconstruct-tunnel', 1.0, 2, 'tls', 1, ?, ?, 1, NULL, 0)
	`, now, now).Error; err != nil {
		t.Fatalf("insert tunnel: %v", err)
	}

	insertNode := func(id int64, name, ip string) {
		if err := r.DB().Exec(`
			INSERT INTO node(id, name, secret, server_ip, server_ip_v4, server_ip_v6, port, interface_name, version, http, tls, socks, created_time, updated_time, status, tcp_listen_addr, udp_listen_addr, inx)
			VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		`, id, name, name+"-secret", ip, ip, "", "30000-30010", "", "v1", 1, 1, 1, now, now, 1, "[::]", "[::]", 0).Error; err != nil {
			t.Fatalf("insert node %s: %v", name, err)
		}
	}

	insertNode(101, "entry", "10.90.0.10")
	insertNode(102, "middle", "10.90.0.20")
	insertNode(103, "exit", "10.90.0.30")

	if err := r.DB().Exec(`
		INSERT INTO chain_tunnel(tunnel_id, chain_type, node_id, port, strategy, inx, protocol)
		VALUES(1, '1', 101, 30001, 'round', 1, 'tls')
	`).Error; err != nil {
		t.Fatalf("insert entry chain: %v", err)
	}
	if err := r.DB().Exec(`
		INSERT INTO chain_tunnel(tunnel_id, chain_type, node_id, port, strategy, inx, protocol, connect_ip)
		VALUES(1, '2', 102, 30002, 'round', 1, 'tls', '10.99.9.22')
	`).Error; err != nil {
		t.Fatalf("insert middle chain: %v", err)
	}
	if err := r.DB().Exec(`
		INSERT INTO chain_tunnel(tunnel_id, chain_type, node_id, port, strategy, inx, protocol, connect_ip)
		VALUES(1, '3', 103, 30003, 'round', 1, 'tls', '10.99.9.33')
	`).Error; err != nil {
		t.Fatalf("insert exit chain: %v", err)
	}

	state, err := h.reconstructTunnelState(1)
	if err != nil {
		t.Fatalf("reconstructTunnelState: %v", err)
	}

	if len(state.ChainHops) != 1 || len(state.ChainHops[0]) != 1 {
		t.Fatalf("unexpected chain hops: %+v", state.ChainHops)
	}
	if got := state.ChainHops[0][0].ConnectIP; got != "10.99.9.22" {
		t.Fatalf("expected middle connectIp 10.99.9.22, got %q", got)
	}

	if len(state.OutNodes) != 1 {
		t.Fatalf("unexpected out nodes: %+v", state.OutNodes)
	}
	if got := state.OutNodes[0].ConnectIP; got != "10.99.9.33" {
		t.Fatalf("expected exit connectIp 10.99.9.33, got %q", got)
	}
}
