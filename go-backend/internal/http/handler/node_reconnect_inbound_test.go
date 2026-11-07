package handler

import (
	"context"
	"errors"
	"net"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"

	"go-backend/internal/store/model"
)

func TestReconnectRefreshesBuiltInboundsAndClearsCachedPortFailure(t *testing.T) {
	h, r := setupLatencyHandler(t, `[{"name":"电信","host":"ct.example.com","port":80,"enabled":true}]`)
	node := mustCreateRelayNode(t, r, "reconnected", "198.51.100.1")
	other := mustCreateRelayNode(t, r, "other", "198.51.100.2")
	k := model.NodeKernel{NodeID: node, KernelType: "xray", Status: kernelStatusRunning}
	if err := r.DB().Create(&k).Error; err != nil {
		t.Fatal(err)
	}
	active := model.NodeInbound{NodeID: node, KernelID: k.ID, Name: "active", Protocol: "vless", Port: 443, Enabled: 1}
	second := model.NodeInbound{NodeID: node, KernelID: k.ID, Name: "second", Protocol: "trojan", Port: 8443, Enabled: 1}
	whitelist := model.NodeInbound{NodeID: node, KernelID: k.ID, Name: "whitelist", Protocol: "vless", Port: 10443, Enabled: 1, RelayWhitelist: 1}
	udp := model.NodeInbound{NodeID: node, KernelID: k.ID, Name: "udp", Protocol: "hysteria2", Port: 11443, Enabled: 1}
	loopback := model.NodeInbound{NodeID: node, KernelID: k.ID, Name: "loopback", Protocol: "vless", Listen: "127.0.0.1", Port: 12443, Enabled: 1}
	disabled := model.NodeInbound{NodeID: node, KernelID: k.ID, Name: "disabled", Protocol: "vless", Port: 13443}
	imported := model.NodeInbound{NodeID: node, Name: "imported", Protocol: "ss", Listen: "203.0.113.9", Port: 8388, Enabled: 1}
	unrelated := model.NodeInbound{NodeID: other, KernelID: k.ID, Name: "unrelated", Protocol: "vless", Port: 14443, Enabled: 1}
	rows := []*model.NodeInbound{&active, &second, &whitelist, &udp, &loopback, &disabled, &imported, &unrelated}
	old := make(map[int64]reachProbe)
	for _, row := range rows {
		if err := r.CreateNodeInbound(row); err != nil {
			t.Fatal(err)
		}
		old[row.ID] = reachProbe{CheckedAt: 123, Error: "无法连接"}
	}
	// GORM applies the model's enabled=1 default when creating a zero value.
	disabled.Enabled = 0
	if err := r.SaveNodeInbound(&disabled); err != nil {
		t.Fatal(err)
	}
	h.reachProbes.replace(old, true)
	h.nodeLatency.replace(map[int64]reachProbe{node: {CheckedAt: 123, Points: []latencyPoint{{Name: "电信", Error: "节点离线"}}}}, false)

	oldDial := reachDial
	t.Cleanup(func() { reachDial = oldDial })
	var mu sync.Mutex
	addresses := make(map[string]int)
	reachDial = func(_ context.Context, addr string) (net.Conn, error) {
		mu.Lock()
		addresses[addr]++
		mu.Unlock()
		a, b := net.Pipe()
		_ = b.Close()
		return a, nil
	}
	stubLatencyPing(t, func(id int64, host string) (map[string]interface{}, error) {
		if id != node || host != "ct.example.com" {
			t.Errorf("unrelated latency probe: node=%d host=%s", id, host)
		}
		return okPing(25), nil
	})
	_, nodes, inbounds, err := h.reconnectProbeTargets([]int64{node, node})
	if err != nil || !reflect.DeepEqual(nodes, []int64{node}) || !reflect.DeepEqual(inbounds, []int64{active.ID, second.ID, whitelist.ID, udp.ID, loopback.ID}) {
		t.Fatalf("targets: nodes=%v inbounds=%v err=%v", nodes, inbounds, err)
	}
	if err := h.probeReconnectedNodes(context.Background(), []int64{node}); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(addresses, map[string]int{"198.51.100.1:443": 1, "198.51.100.1:8443": 1}) {
		t.Fatalf("unexpected port probes: %v", addresses)
	}
	for _, row := range []*model.NodeInbound{&active, &second} {
		p, _ := h.reachProbes.get(row.ID)
		view := h.inboundProbeView(row)
		if !p.OK || p.CheckedAt <= 123 || view == nil || !view.OK || len(view.Points) != 1 || view.Points[0].LatencyMs != 25 {
			t.Fatalf("stale port failure still hides node latency: port=%+v view=%+v", p, view)
		}
	}
	for _, row := range []*model.NodeInbound{&whitelist, &udp, &loopback} {
		if p, _ := h.reachProbes.get(row.ID); !p.Skipped {
			t.Fatalf("inbound %s should retain skip rules: %+v", row.Name, p)
		}
	}
	for _, row := range []*model.NodeInbound{&disabled, &imported, &unrelated} {
		if p, _ := h.reachProbes.get(row.ID); p.CheckedAt != 123 {
			t.Fatalf("unrelated/disabled/imported inbound %s was measured: %+v", row.Name, p)
		}
	}
}

func TestInboundReachOlderRoundCannotOverwriteNewerAndCancelKeepsCache(t *testing.T) {
	h, r := setupLatencyHandler(t, "")
	row := model.NodeInbound{Name: "external", Protocol: "ss", Listen: "203.0.113.1", Port: 443, Enabled: 1}
	if err := r.CreateNodeInbound(&row); err != nil {
		t.Fatal(err)
	}
	h.reachProbes.replace(map[int64]reachProbe{row.ID: {OK: true, CheckedAt: 123}}, false)
	oldDial := reachDial
	t.Cleanup(func() { reachDial = oldDial })
	started := make(chan struct{}, 2)
	release := make(chan struct{})
	var calls atomic.Int32
	reachDial = func(ctx context.Context, _ string) (net.Conn, error) {
		if calls.Add(1) == 1 {
			// The older round still sees the port closed, and finishes last.
			started <- struct{}{}
			select {
			case <-release:
			case <-ctx.Done():
				return nil, ctx.Err()
			}
			return nil, errors.New("connection refused")
		}
		a, b := net.Pipe()
		_ = b.Close()
		return a, nil
	}

	// A cancelled round leaves the cache alone.
	ctx, cancel := context.WithCancel(context.Background())
	cancelled := make(chan error, 1)
	go func() {
		_, err := h.probeInboundReach(ctx, []int64{row.ID})
		cancelled <- err
	}()
	<-started
	cancel()
	if err := <-cancelled; err != context.Canceled {
		t.Fatalf("cancelled probe: %v", err)
	}
	if p, _ := h.reachProbes.get(row.ID); !p.OK || p.CheckedAt != 123 {
		t.Fatalf("cancelled check changed cache: %+v", p)
	}

	// Rounds overlap: the newer one is not held up by the older one, and
	// the older one's stale failure does not replace its result.
	calls.Store(0)
	older := make(chan error, 1)
	go func() {
		_, err := h.probeInboundReach(context.Background(), []int64{row.ID})
		older <- err
	}()
	<-started
	if _, err := h.probeInboundReach(context.Background(), []int64{row.ID}); err != nil {
		t.Fatal(err)
	}
	newer, _ := h.reachProbes.get(row.ID)
	if !newer.OK || newer.CheckedAt <= 123 {
		t.Fatalf("fresh result not stored: %+v", newer)
	}
	close(release)
	if err := <-older; err != nil {
		t.Fatal(err)
	}
	if p, _ := h.reachProbes.get(row.ID); !p.OK || p.CheckedAt != newer.CheckedAt {
		t.Fatalf("older round overwrote the newer result: %+v", p)
	}
}

func TestInboundRecoveryViewDoesNotGoBackToOlderNodeSampleTime(t *testing.T) {
	h, _ := setupLatencyHandler(t, "")
	row := model.NodeInbound{ID: 1, NodeID: 1, KernelID: 1}
	h.nodeLatency.replace(map[int64]reachProbe{1: {OK: true, CheckedAt: 100, Points: []latencyPoint{{Name: "电信", OK: true, LatencyMs: 25}}}}, false)
	h.reachProbes.replace(map[int64]reachProbe{1: {CheckedAt: 200, Error: "无法连接"}}, false)
	if view := h.inboundProbeView(&row); view == nil || view.OK || view.CheckedAt != 200 {
		t.Fatalf("initial port failure: %+v", view)
	}
	h.reachProbes.replace(map[int64]reachProbe{1: {OK: true, CheckedAt: 300}}, false)
	if view := h.inboundProbeView(&row); view == nil || !view.OK || view.CheckedAt != 300 || view.Points[0].LatencyMs != 25 {
		t.Fatalf("recovered view would be rejected as older than the failure: %+v", view)
	}
	// A node sample collected while a port round was still running must not
	// make the subsequent combined failure appear older than the shown view.
	h.nodeLatency.replace(map[int64]reachProbe{1: {OK: true, CheckedAt: 400, Points: []latencyPoint{{Name: "电信", OK: true, LatencyMs: 20}}}}, false)
	h.reachProbes.replace(map[int64]reachProbe{1: {CheckedAt: 350, Error: "无法连接"}}, false)
	if view := h.inboundProbeView(&row); view == nil || view.OK || view.CheckedAt != 400 {
		t.Fatalf("combined port failure: %+v", view)
	}
	if cached, _ := h.nodeLatency.get(1); cached.CheckedAt != 400 {
		t.Fatal("combining a view changed the stored node sample")
	}
}

func TestPartialInboundChecksDoNotCountTowardOutage(t *testing.T) {
	h, r := setupLatencyHandler(t, "")
	row := model.NodeInbound{Name: "external", Protocol: "ss", Listen: "203.0.113.1", Port: 443, Enabled: 1}
	if err := r.CreateNodeInbound(&row); err != nil {
		t.Fatal(err)
	}
	oldDial := reachDial
	t.Cleanup(func() { reachDial = oldDial })
	reachDial = func(context.Context, string) (net.Conn, error) {
		return nil, errors.New("connection refused")
	}
	for i := 0; i < notifyInboundDownStreak+1; i++ {
		if _, err := h.probeInboundReach(context.Background(), []int64{row.ID}); err != nil {
			t.Fatal(err)
		}
	}
	h.reachProbes.mu.RLock()
	streak, down := h.reachProbes.failStreak[row.ID], h.reachProbes.down[row.ID]
	h.reachProbes.mu.RUnlock()
	if streak != 0 || down {
		t.Fatalf("clicked / reconnect checks counted toward an outage: streak=%d down=%v", streak, down)
	}
}
