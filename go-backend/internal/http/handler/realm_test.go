package handler

import (
	"strings"
	"testing"
	"time"

	"go-backend/internal/kernel"
	"go-backend/internal/store/repo"
)

func TestRealmForwardRenderAndTraffic(t *testing.T) {
	r, err := repo.Open(t.TempDir() + "/realm.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = r.Close() })
	h := New(r, "unit-test-secret")

	nodeID := seedRelayTargetNode(t, r, "entry", "198.51.100.7")
	tunnelID := seedTunnelForNftables(t, h, "realm-tunnel", nodeID)
	if err := r.DB().Exec(`UPDATE tunnel SET engine = 'realm', flow = 2 WHERE id = ?`, tunnelID).Error; err != nil {
		t.Fatal(err)
	}
	forward := seedForwardForNftables(t, h, tunnelID, nodeID, "1.1.1.1:443,2.2.2.2:443")

	tunnel, err := h.getTunnelRecord(tunnelID)
	if err != nil || !tunnelIsRealm(tunnel) {
		t.Fatalf("tunnel engine not loaded: %+v %v", tunnel, err)
	}
	k, err := h.ensureRealmKernel(nodeID, true)
	if err != nil || k == nil || k.Version != kernel.RealmVersion {
		t.Fatalf("realm kernel: %+v %v", k, err)
	}

	payload, _, err := h.buildKernelApplyPayload(k)
	if err != nil {
		t.Fatal(err)
	}
	cfg := payload["files"].([]kernel.File)[0].Content
	for _, want := range []string{`listen = "[::]:20000"`, `remote = "1.1.1.1:443"`, `extra_remotes = ["2.2.2.2:443"]`, `balance = "roundrobin: 1, 1"`} {
		if !strings.Contains(cfg, want) {
			t.Fatalf("config misses %s:\n%s", want, cfg)
		}
	}
	ports := payload["ports"].([]map[string]interface{})
	if len(ports) != 1 || ports[0]["inboundId"] != kernel.RealmCounterID(forward.ID) || ports[0]["port"] != 20000 {
		t.Fatalf("ports = %v", ports)
	}
	if payload["enabled"] != true {
		t.Fatal("realm with a forward should run")
	}

	// Pausing renders before the status changes: the override drops it.
	paused, _, err := h.buildKernelApplyPayloadWith(k, map[int64]bool{forward.ID: false})
	if err != nil {
		t.Fatal(err)
	}
	if paused["enabled"] != false || len(paused["ports"].([]map[string]interface{})) != 0 {
		t.Fatalf("paused payload still runs: %v", paused)
	}

	// Counters: the first sample sets the baseline, later ones are credited.
	counter := func(dir string, bytes int64) agentKernelCounter {
		return agentKernelCounter{InboundID: kernel.RealmCounterID(forward.ID), Proto: "tcp", Dir: dir, Bytes: bytes}
	}
	now := time.Now()
	h.applyRealmCounters(nodeID, "e1", []agentKernelCounter{counter("in", 100), counter("out", 1000)}, now)
	h.applyRealmCounters(nodeID, "e1", []agentKernelCounter{counter("in", 300), counter("out", 1500)}, now.Add(time.Minute))
	var flow struct{ InFlow, OutFlow int64 }
	if err := r.DB().Raw(`SELECT in_flow, out_flow FROM forward WHERE id = ?`, forward.ID).Scan(&flow).Error; err != nil {
		t.Fatal(err)
	}
	if flow.InFlow != 200 || flow.OutFlow != 500 {
		t.Fatalf("forward flow = %+v, want 200/500", flow)
	}
	// A known new epoch credits its initial counter from zero.
	h.applyRealmCounters(nodeID, "e2", []agentKernelCounter{counter("in", 50)}, now.Add(2*time.Minute))
	if err := r.DB().Raw(`SELECT in_flow, out_flow FROM forward WHERE id = ?`, forward.ID).Scan(&flow).Error; err != nil {
		t.Fatal(err)
	}
	if flow.InFlow != 250 {
		t.Fatalf("epoch change lost new traffic: %+v", flow)
	}
}

func TestNormalizeTunnelEngine(t *testing.T) {
	for in, want := range map[string]string{"": "", "gost": "", "realm": "realm", " Realm ": "realm"} {
		if got, err := normalizeTunnelEngine(in); err != nil || got != want {
			t.Fatalf("%q -> %q %v", in, got, err)
		}
	}
	if _, err := normalizeTunnelEngine("haproxy"); err == nil {
		t.Fatal("unknown engine accepted")
	}
}
