package handler

import (
	"context"
	"errors"
	"reflect"
	"sync/atomic"
	"testing"
	"time"
)

func TestReconnectProbeTargetsIncludeEveryTunnelRoleAndLanding(t *testing.T) {
	h, r := setupLatencyHandler(t, "")
	entry := mustCreateRelayNode(t, r, "entry", "198.51.100.1")
	hop := mustCreateRelayNode(t, r, "hop", "198.51.100.2")
	exit := mustCreateRelayNode(t, r, "exit", "198.51.100.3")
	landing := mustCreateRelayNode(t, r, "landing", "198.51.100.4")
	other := mustCreateRelayNode(t, r, "other", "198.51.100.5")
	id := mustCreateRelayLine(t, r, "affected", 2, "198.51.100.4:443", entry, []int64{hop}, exit)
	mustCreateRelayLine(t, r, "unrelated", 1, "203.0.113.1:443", other, nil, 0)
	if err := r.DB().Exec("UPDATE node_inbound SET node_id = ? WHERE id = (SELECT inbound_id FROM node_inbound_relay WHERE id = ?)", landing, id).Error; err != nil {
		t.Fatal(err)
	}
	for _, node := range []int64{entry, hop, exit, landing} {
		relays, nodes, _, err := h.reconnectProbeTargets([]int64{node, node})
		wantNodes := []int64{entry}
		if node != entry {
			wantNodes = append(wantNodes, node)
		}
		if err != nil || !reflect.DeepEqual(relays, []int64{id}) || !reflect.DeepEqual(nodes, wantNodes) {
			t.Fatalf("reconnect %d: relays=%v nodes=%v err=%v", node, relays, nodes, err)
		}
	}
}

func TestReconnectProbeReplacesOfflineReferenceAndRelayWithoutTouchingOtherLines(t *testing.T) {
	h, r := setupLatencyHandler(t, `[{"name":"电信","host":"ct.example.com","port":80,"enabled":true}]`)
	entry := mustCreateRelayNode(t, r, "entry", "198.51.100.1")
	other := mustCreateRelayNode(t, r, "other", "198.51.100.2")
	id := mustCreateRelayLine(t, r, "affected", 1, "203.0.113.1:443", entry, nil, 0)
	unrelated := mustCreateRelayLine(t, r, "unrelated", 1, "203.0.113.2:443", other, nil, 0)
	h.nodeLatency.replace(map[int64]reachProbe{entry: {CheckedAt: 1, Points: []latencyPoint{{Name: "电信", Error: "节点离线"}}}}, false)
	if err := r.UpdateNodeStatus(entry, 0); err != nil {
		t.Fatal(err)
	}
	stubLatencyPing(t, func(node int64, host string) (map[string]interface{}, error) {
		if node != entry {
			t.Errorf("unrelated node measured: %d", node)
		}
		if host == "ct.example.com" {
			return okPing(20), nil
		}
		return okPing(30), nil
	})
	initial, err := h.probeRelayLatency(context.Background(), []int64{id})
	if err != nil || initial[id].OK {
		t.Fatalf("expected offline line: %+v err=%v", initial[id], err)
	}
	h.relayLatency.replace(map[int64]relayLatency{unrelated: {reachProbe: reachProbe{CheckedAt: 123, OK: true}}}, false)
	if err := r.UpdateNodeStatus(entry, 1); err != nil {
		t.Fatal(err)
	}
	if err := h.probeReconnectedNodes(context.Background(), []int64{entry}); err != nil {
		t.Fatal(err)
	}
	got, _ := h.relayLatency.get(id)
	if !got.OK || len(got.Points) != 1 || !got.Points[0].OK || got.Points[0].LatencyMs != 50 || got.CheckedAt < initial[id].CheckedAt {
		t.Fatalf("line did not recover with fresh reference point: %+v", got)
	}
	if p, _ := h.relayLatency.get(unrelated); p.CheckedAt != 123 {
		t.Fatalf("unrelated line changed: %+v", p)
	}
	if p, _ := h.nodeLatency.get(entry); !p.OK || p.LatencyMs != 20 {
		t.Fatalf("node page would still show old latency: %+v", p)
	}
}

func TestReconnectWorkerCoalescesBurstsAndKeepsEventsDuringProbe(t *testing.T) {
	h, r := setupLatencyHandler(t, "")
	entry := mustCreateRelayNode(t, r, "entry", "198.51.100.1")
	id := mustCreateRelayLine(t, r, "line", 1, "203.0.113.1:443", entry, nil, 0)
	queue := newNodeReconnectProbeQueue()
	started := make(chan struct{}, 4)
	release := make(chan struct{})
	var count atomic.Int32
	stubLatencyPing(t, func(int64, string) (map[string]interface{}, error) {
		n := count.Add(1)
		started <- struct{}{}
		if n == 1 {
			<-release
		}
		return okPing(10), nil
	})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		h.runNodeReconnectProbes(ctx, queue, 10*time.Millisecond)
	}()
	defer func() {
		cancel()
		select {
		case <-release:
		default:
			close(release)
		}
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Error("reconnect worker did not stop")
		}
	}()
	wait := func() {
		t.Helper()
		select {
		case <-started:
		case <-time.After(2 * time.Second):
			t.Fatal("reconnect did not trigger detection")
		}
	}
	for i := 0; i < 100; i++ {
		queue.add(entry)
	}
	wait()
	for i := 0; i < 100; i++ {
		queue.add(entry)
	}
	close(release)
	wait()
	select {
	case <-started:
		t.Fatal("duplicate reconnect probes")
	case <-time.After(40 * time.Millisecond):
	}
	cancel()
	<-done
	if got, _ := h.relayLatency.get(id); !got.OK || count.Load() != 2 {
		t.Fatalf("result=%+v probes=%d", got, count.Load())
	}
}

func TestReconnectNodeWithoutRelayOnlyRefreshesItsOwnLatency(t *testing.T) {
	h, r := setupLatencyHandler(t, `[{"name":"A","host":"a.example.com","port":80,"enabled":true}]`)
	id := mustCreateRelayNode(t, r, "no-relay", "198.51.100.1")
	other := mustCreateRelayNode(t, r, "other", "198.51.100.2")
	relay := mustCreateRelayLine(t, r, "unrelated", 1, "203.0.113.1:443", other, nil, 0)
	h.reachProbes.replace(map[int64]reachProbe{999: {CheckedAt: 123}}, false)
	calls := stubLatencyPing(t, func(node int64, host string) (map[string]interface{}, error) {
		if node != id || host != "a.example.com" {
			t.Errorf("unrelated measurement: node=%d host=%s", node, host)
		}
		return okPing(42), nil
	})
	if err := h.probeReconnectedNodes(context.Background(), []int64{id}); err != nil {
		t.Fatal(err)
	}
	if p, _ := h.nodeLatency.get(id); !p.OK || p.LatencyMs != 42 || len(*calls) != 1 {
		t.Fatalf("node result=%+v calls=%v", p, *calls)
	}
	if _, ok := h.relayLatency.get(relay); ok {
		t.Fatal("empty affected-relay list triggered a full relay round")
	}
	if p, ok := h.reachProbes.get(999); !ok || p.CheckedAt != 123 {
		t.Fatal("empty affected-inbound list triggered a full port check")
	}
}

func TestRelayProbeOlderRoundCannotOverwriteNewer(t *testing.T) {
	h, r := setupLatencyHandler(t, "")
	entry := mustCreateRelayNode(t, r, "entry", "198.51.100.1")
	id := mustCreateRelayLine(t, r, "line", 1, "203.0.113.1:443", entry, nil, 0)
	started := make(chan struct{}, 1)
	release := make(chan struct{})
	var calls atomic.Int32
	stubLatencyPing(t, func(int64, string) (map[string]interface{}, error) {
		if calls.Add(1) == 1 {
			started <- struct{}{}
			<-release
			return nil, errors.New("节点离线")
		}
		return okPing(10), nil
	})
	older := make(chan struct{})
	go func() {
		defer close(older)
		_, _ = h.probeRelayLatency(context.Background(), []int64{id})
	}()
	<-started
	if _, err := h.probeRelayLatency(context.Background(), []int64{id}); err != nil {
		t.Fatal(err)
	}
	if got, _ := h.relayLatency.get(id); !got.OK {
		t.Fatalf("newer result not stored: %+v", got)
	}
	close(release)
	<-older
	if got, _ := h.relayLatency.get(id); !got.OK {
		t.Fatalf("older round overwrote the newer result: %+v", got)
	}
}
