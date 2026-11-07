package handler

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"go-backend/internal/store/repo"
)

// Issue #208: normal users get the listen IP choices of their tunnels from
// the user tunnel list, built like the admin form builds them.
func TestTunnelListenOptions(t *testing.T) {
	r, err := repo.Open(t.TempDir() + "/listen-ip.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = r.Close() })
	h := New(r, "unit-test-secret")
	db := r.DB()
	now := time.Now().UnixMilli()

	addNode := func(name, ip, v4, v6, extra string) int64 {
		if err := db.Exec(`
			INSERT INTO node(name, secret, server_ip, server_ip_v4, server_ip_v6, extra_ips, port, interface_name, version, http, tls, socks, created_time, updated_time, status, tcp_listen_addr, udp_listen_addr, inx)
			VALUES(?, ?, ?, ?, ?, ?, '30000-30010', '', 'v1', 0, 0, 0, ?, ?, 1, '[::]', '[::]', 0)`,
			name, name+"-secret", ip, v4, v6, extra, now, now).Error; err != nil {
			t.Fatal(err)
		}
		var id int64
		db.Raw(`SELECT id FROM node WHERE name = ?`, name).Scan(&id)
		return id
	}
	addTunnel := func(name, inIP string, entries ...int64) int64 {
		if err := db.Exec(`
			INSERT INTO tunnel(name, traffic_ratio, type, protocol, flow, created_time, updated_time, status, in_ip, inx)
			VALUES(?, 1.0, 1, 'tls', 99999, ?, ?, 1, ?, 0)`, name, now, now, inIP).Error; err != nil {
			t.Fatal(err)
		}
		var id int64
		db.Raw(`SELECT id FROM tunnel WHERE name = ?`, name).Scan(&id)
		for i, n := range entries {
			if err := db.Exec(`INSERT INTO chain_tunnel(tunnel_id, chain_type, node_id, port, strategy, inx, protocol) VALUES(?, 1, ?, NULL, 'round', ?, 'tcp')`, id, n, i+1).Error; err != nil {
				t.Fatal(err)
			}
		}
		return id
	}

	dual := addNode("dual", "198.51.100.1", "198.51.100.1", "2001:db8::1", "10.0.0.5, 10.0.0.6")
	bare := addNode("bare", "", "", "", "")
	withNodes := addTunnel("with-nodes", "nat.example.com", dual)
	fallback := addTunnel("fallback", "203.0.113.50", bare)
	multi := addTunnel("multi", "", dual, bare)

	opts, err := r.TunnelListenOptions([]int64{withNodes, fallback, multi})
	if err != nil {
		t.Fatal(err)
	}
	if got := opts[withNodes]; got.EntryCount != 1 || !reflect.DeepEqual(got.IPs, []string{"198.51.100.1", "2001:db8::1", "10.0.0.5", "10.0.0.6"}) {
		t.Fatalf("entry server addresses, without the tunnel's own entry address: %+v", got)
	}
	if got := opts[fallback]; !reflect.DeepEqual(got.IPs, []string{"203.0.113.50"}) {
		t.Fatalf("tunnel entry address is the fallback when servers have none: %+v", got)
	}
	if got := opts[multi]; got.EntryCount != 2 {
		t.Fatalf("multi-entry count: %+v", got)
	}

	if err := h.validateUserListenIP(withNodes, "2001:db8::1", nil); err != nil {
		t.Fatalf("an entry server address must be accepted: %v", err)
	}
	if err := h.validateUserListenIP(withNodes, "nat.example.com", nil); err == nil || !strings.Contains(err.Error(), "nat.example.com") {
		t.Fatalf("an address no entry server has must be rejected: %v", err)
	}
	if err := h.validateUserListenIP(withNodes, "192.0.2.9", []forwardPortRecord{{InIP: "192.0.2.9"}}); err != nil {
		t.Fatalf("the forward's current listen IP must stay accepted: %v", err)
	}
}
