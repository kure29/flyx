package handler

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"go-backend/internal/store/model"
	"go-backend/internal/store/repo"
)

func mustCreateRelayNode(t *testing.T, r *repo.Repository, name, ip string) int64 {
	t.Helper()
	n := model.Node{Name: name, Secret: name, ServerIP: ip, Status: 1}
	if err := r.DB().Create(&n).Error; err != nil {
		t.Fatal(err)
	}
	return n.ID
}

// mustCreateRelayLine adds a tunnel (entry → hops... → exit, or a port
// forward when exit is 0), a forward to target and the relay line using it.
func mustCreateRelayLine(t *testing.T, r *repo.Repository, name string, tunnelType int, target string, entry int64, hops []int64, exit int64) int64 {
	t.Helper()
	now := time.Now().UnixMilli()
	db := r.DB()
	if err := db.Exec(`
		INSERT INTO tunnel(name, traffic_ratio, type, protocol, flow, created_time, updated_time, status, in_ip, inx)
		VALUES(?, 1.0, ?, 'tcp', 99999, ?, ?, 1, NULL, 0)`, name, tunnelType, now, now).Error; err != nil {
		t.Fatal(err)
	}
	var tunnelID int64
	if err := db.Raw(`SELECT id FROM tunnel WHERE name = ?`, name).Scan(&tunnelID).Error; err != nil {
		t.Fatal(err)
	}
	chain := func(chainType int, node int64, port int, inx int) {
		if err := db.Exec(`INSERT INTO chain_tunnel(tunnel_id, chain_type, node_id, port, strategy, inx, protocol) VALUES(?, ?, ?, ?, 'round', ?, 'tcp')`,
			tunnelID, chainType, node, port, inx).Error; err != nil {
			t.Fatal(err)
		}
	}
	chain(1, entry, 0, 1)
	for i, hop := range hops {
		chain(2, hop, 32000+i, i+1)
	}
	if exit > 0 {
		chain(3, exit, 31000, 1)
	}
	fwd := model.Forward{UserID: 1, UserName: "admin", Name: name, TunnelID: tunnelID, RemoteAddr: target, CreatedTime: now, UpdatedTime: now, Status: 1}
	if err := db.Create(&fwd).Error; err != nil {
		t.Fatal(err)
	}
	in := model.NodeInbound{Name: name + "-landing", Protocol: "vless", Port: 443}
	if err := db.Create(&in).Error; err != nil {
		t.Fatal(err)
	}
	rel := model.NodeInboundRelay{InboundID: in.ID, ForwardID: fwd.ID, Name: name, CreatedTime: now}
	if err := db.Create(&rel).Error; err != nil {
		t.Fatal(err)
	}
	return rel.ID
}

func okPing(ms float64) map[string]interface{} {
	return map[string]interface{}{"success": true, "averageTime": ms, "packetLoss": 0.0}
}

func TestRelayLatencyAddsEveryLegToTheEntryLatency(t *testing.T) {
	h, r := setupLatencyHandler(t, `[{"name":"电信","host":"ct.example.com","port":80,"enabled":true},{"name":"联通","host":"cu.example.com","port":80,"enabled":true}]`)
	front := mustCreateRelayNode(t, r, "front", "198.51.100.1")
	ix := mustCreateRelayNode(t, r, "ix", "198.51.100.2")
	landing := mustCreateRelayNode(t, r, "landing", "203.0.113.10")

	viaIX := mustCreateRelayLine(t, r, "via-ix", 2, "203.0.113.10:443", front, nil, ix)
	port := mustCreateRelayLine(t, r, "port", 1, "203.0.113.10:443", front, nil, 0)
	onLanding := mustCreateRelayLine(t, r, "on-landing", 2, "127.0.0.1:443", front, nil, landing)

	legs := map[string]float64{
		"198.51.100.2:31000": 20, // front → ix
		"203.0.113.10:443":   30, // ix → landing, or front → landing (port forward)
		"203.0.113.10:31000": 25, // front → landing's tunnel port
		"ct.example.com:80":  40,
		"cu.example.com:80":  -1,
	}
	old := nodeLatencyPing
	t.Cleanup(func() { nodeLatencyPing = old })
	var mu sync.Mutex
	var calls []string
	nodeLatencyPing = func(_ *Handler, nodeID int64, host string, port int) (map[string]interface{}, error) {
		addr := fmt.Sprintf("%s:%d", host, port)
		mu.Lock()
		calls = append(calls, fmt.Sprintf("%d>%s", nodeID, addr))
		mu.Unlock()
		ms, ok := legs[addr]
		if !ok {
			return nil, fmt.Errorf("unexpected ping %s", addr)
		}
		if ms < 0 {
			return map[string]interface{}{"success": false, "errorMessage": "timeout"}, nil
		}
		return okPing(ms), nil
	}

	if _, err := h.probeNodeLatency(context.Background(), []int64{front}); err != nil {
		t.Fatal(err)
	}
	calls = nil
	res, err := h.probeRelayLatency(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}

	got := res[viaIX]
	if got.Entry != "front" || got.PathMs != 50 || len(got.Hops) != 2 || got.Hops[0].To != "ix" || got.Hops[1].From != "ix" || got.Hops[1].To != "via-ix-landing" {
		t.Fatalf("via IX = %+v", got)
	}
	if len(got.Points) != 2 || !got.Points[0].OK || got.Points[0].LatencyMs != 90 || got.Points[1].OK || got.Points[1].Error == "" {
		t.Fatalf("via IX points = %+v", got.Points)
	}
	if p := res[port]; p.PathMs != 30 || len(p.Hops) != 1 || p.Hops[0].From != "front" || p.Points[0].LatencyMs != 70 {
		t.Fatalf("port forward = %+v", p)
	}
	// Exiting on the landing dials the inbound locally: one leg, no local ping.
	if p := res[onLanding]; p.PathMs != 25 || len(p.Hops) != 1 || p.Hops[0].To != "landing" {
		t.Fatalf("exit on landing = %+v", p)
	}
	for _, c := range calls {
		if strings.Contains(c, "127.0.0.1") {
			t.Fatalf("pinged the loopback target: %v", calls)
		}
	}

	// The relay list carries the last measurement.
	views, err := h.relayViews(0)
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range views {
		if v.Latency == nil || v.Latency.CheckedAt == 0 {
			t.Fatalf("relay %d has no latency: %+v", v.ID, v)
		}
	}
}

func TestRelayLatencyWithoutReferencePointsShowsThePath(t *testing.T) {
	h, r := setupLatencyHandler(t, "")
	front := mustCreateRelayNode(t, r, "front", "198.51.100.1")
	id := mustCreateRelayLine(t, r, "port", 1, "203.0.113.10:443", front, nil, 0)
	stubLatencyPing(t, func(int64, string) (map[string]interface{}, error) { return okPing(33), nil })

	res, err := h.probeRelayLatency(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	got := res[id]
	if !got.OK || len(got.Points) != 1 || got.Points[0].Name != relayPathPointName || got.Points[0].LatencyMs != 33 {
		t.Fatalf("path only = %+v", got)
	}
}

func TestRelayLatencyBrokenLegFailsEveryPoint(t *testing.T) {
	h, r := setupLatencyHandler(t, `[{"name":"电信","host":"ct.example.com","port":80,"enabled":true}]`)
	front := mustCreateRelayNode(t, r, "front", "198.51.100.1")
	ix := mustCreateRelayNode(t, r, "ix", "198.51.100.2")
	id := mustCreateRelayLine(t, r, "via-ix", 2, "203.0.113.10:443", front, nil, ix)
	stubLatencyPing(t, func(_ int64, host string) (map[string]interface{}, error) {
		if host == "203.0.113.10" {
			return nil, errors.New("node gone")
		}
		return okPing(10), nil
	})
	if _, err := h.probeNodeLatency(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	res, err := h.probeRelayLatency(context.Background(), []int64{id})
	if err != nil {
		t.Fatal(err)
	}
	got := res[id]
	if got.OK || got.Points[0].OK || got.Points[0].Error != "线路不通" || !strings.Contains(got.Error, "ix → via-ix-landing") {
		t.Fatalf("broken leg = %+v", got)
	}
}

func TestRelayLatencySkipsOfflineEntry(t *testing.T) {
	h, r := setupLatencyHandler(t, "")
	front := mustCreateRelayNode(t, r, "front", "198.51.100.1")
	if err := r.DB().Model(&model.Node{}).Where("id = ?", front).Update("status", 0).Error; err != nil {
		t.Fatal(err)
	}
	id := mustCreateRelayLine(t, r, "port", 1, "203.0.113.10:443", front, nil, 0)
	calls := stubLatencyPing(t, func(int64, string) (map[string]interface{}, error) { return okPing(1), nil })
	res, _ := h.probeRelayLatency(context.Background(), nil)
	if len(*calls) != 0 || res[id].OK || res[id].Hops[0].Error != "服务器离线" {
		t.Fatalf("offline entry: calls=%v res=%+v", *calls, res[id])
	}
}

func TestLatencyTargetsAllowThirty(t *testing.T) {
	list := func(n int) string {
		parts := make([]string, n)
		for i := range parts {
			parts[i] = fmt.Sprintf(`{"name":"p%d","host":"p%d.example.com","port":80}`, i, i)
		}
		return "[" + strings.Join(parts, ",") + "]"
	}
	if _, err := parseLatencyTargets(list(30)); err != nil {
		t.Fatalf("30 points: %v", err)
	}
	if _, err := parseLatencyTargets(list(31)); err == nil {
		t.Fatal("31 points must be refused")
	}
}
