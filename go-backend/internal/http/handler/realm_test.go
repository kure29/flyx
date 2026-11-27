package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"go-backend/internal/auth"
	"go-backend/internal/http/middleware"
	"go-backend/internal/kernel"
	"go-backend/internal/store/repo"
)

func withAdminClaims(req *http.Request) *http.Request {
	return req.WithContext(context.WithValue(req.Context(), middleware.ClaimsContextKey, auth.Claims{
		Sub:    "1",
		User:   "admin",
		RoleID: 0,
	}))
}

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

func TestRealmForwardCreateConcurrentPortSerialization(t *testing.T) {
	r, err := repo.Open(t.TempDir() + "/realm-concurrent.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = r.Close() })
	h := New(r, "unit-test-secret")

	nodeID := seedRelayTargetNode(t, r, "entry-realm", "198.51.100.8")
	if err := r.DB().Exec(`UPDATE node SET port = '20100-20105' WHERE id = ?`, nodeID).Error; err != nil {
		t.Fatal(err)
	}
	tunnelID := seedTunnelForNftables(t, h, "realm-concurrent-tunnel", nodeID)
	if err := r.DB().Exec(`UPDATE tunnel SET engine = 'realm', flow = 2 WHERE id = ?`, tunnelID).Error; err != nil {
		t.Fatal(err)
	}

	doCreate := func(body map[string]interface{}) (int, string, map[string]interface{}) {
		raw, _ := json.Marshal(body)
		req := withAdminClaims(httptest.NewRequest(http.MethodPost, "/api/v1/forward/create", bytes.NewReader(raw)))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		h.forwardCreate(rec, req)
		var out struct {
			Code int                    `json:"code"`
			Msg  string                 `json:"msg"`
			Data map[string]interface{} `json:"data"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
			t.Fatalf("decode response %q: %v", rec.Body.String(), err)
		}
		return out.Code, out.Msg, out.Data
	}

	// 1) Two concurrent requests targeting the exact same explicit port:
	// exactly one must succeed (returning offline realm warning) and the other
	// must be rejected as occupied, leaving no duplicate forward_port rows.
	type result struct {
		code int
		msg  string
		data map[string]interface{}
	}
	explicitCh := make(chan result, 2)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			<-start
			c, m, d := doCreate(map[string]interface{}{
				"name":       fmt.Sprintf("same-port-%d", idx),
				"tunnelId":   tunnelID,
				"inPort":     20100,
				"remoteAddr": "1.1.1.1:443",
			})
			explicitCh <- result{code: c, msg: m, data: d}
		}(i)
	}
	close(start)
	wg.Wait()
	close(explicitCh)

	okCount, occupiedCount := 0, 0
	for res := range explicitCh {
		if res.code == 0 {
			okCount++
			warnings, _ := res.data["warnings"].([]interface{})
			if len(warnings) == 0 {
				t.Fatalf("expected offline realm warning in forwardCreate response, got data=%v", res.data)
			}
		} else if strings.Contains(res.msg, "已被其他转发占用") {
			occupiedCount++
		} else {
			t.Fatalf("unexpected error response: code=%d msg=%q", res.code, res.msg)
		}
	}
	if okCount != 1 || occupiedCount != 1 {
		t.Fatalf("same-port concurrent creates: ok=%d occupied=%d, want 1/1", okCount, occupiedCount)
	}

	// 2) Five concurrent auto-port creates on the remaining 5 ports (20101-20105):
	// every create must succeed with a unique port.
	autoCh := make(chan result, 5)
	startAuto := make(chan struct{})
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			<-startAuto
			c, m, d := doCreate(map[string]interface{}{
				"name":       fmt.Sprintf("auto-port-%d", idx),
				"tunnelId":   tunnelID,
				"remoteAddr": "1.1.1.1:443",
			})
			autoCh <- result{code: c, msg: m, data: d}
		}(i)
	}
	close(startAuto)
	wg.Wait()
	close(autoCh)

	for res := range autoCh {
		if res.code != 0 {
			t.Fatalf("concurrent auto-port create failed: code=%d msg=%q", res.code, res.msg)
		}
	}

	var portCounts []struct {
		Port int
		Cnt  int
	}
	if err := r.DB().Raw(`SELECT port, COUNT(*) AS cnt FROM forward_port WHERE node_id = ? GROUP BY port`, nodeID).Scan(&portCounts).Error; err != nil {
		t.Fatal(err)
	}
	if len(portCounts) != 6 {
		t.Fatalf("expected 6 distinct allocated ports, got %+v", portCounts)
	}
	for _, pc := range portCounts {
		if pc.Cnt != 1 {
			t.Fatalf("duplicate forward_port allocation detected: %+v", portCounts)
		}
	}
}

func TestForwardUpdateAlwaysConvergesManagedRelayName(t *testing.T) {
	r, err := repo.Open(t.TempDir() + "/forward-relay-converge.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = r.Close() })
	h := New(r, "unit-test-secret")

	nodeID := seedRelayTargetNode(t, r, "entry-converge", "198.51.100.9")
	tunnelID := seedTunnelForNftables(t, h, "realm-converge", nodeID)
	if err := r.DB().Exec(`UPDATE tunnel SET engine = 'realm', flow = 2 WHERE id = ?`, tunnelID).Error; err != nil {
		t.Fatal(err)
	}
	forward := seedForwardForNftables(t, h, tunnelID, nodeID, "1.1.1.1:443")
	// Seed a diverged managed relay row whose name differs from forward.Name.
	if err := r.DB().Exec(`INSERT INTO node_inbound_relay(inbound_id, forward_id, name, share_host, managed, created_time) VALUES(1, ?, 'Stale Diverged Name', '', 1, 100)`, forward.ID).Error; err != nil {
		t.Fatal(err)
	}

	body, _ := json.Marshal(map[string]interface{}{
		"id":         forward.ID,
		"name":       forward.Name, // unchanged vs forward.Name
		"tunnelId":   tunnelID,
		"inPort":     30001,
		"remoteAddr": "2.2.2.2:443",
	})
	req := withAdminClaims(httptest.NewRequest(http.MethodPost, "/api/v1/forward/update", bytes.NewReader(body)))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.forwardUpdate(rec, req)

	var out struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if out.Code != 0 {
		t.Fatalf("forwardUpdate failed: %+v", out)
	}

	var relayName string
	if err := r.DB().Raw(`SELECT name FROM node_inbound_relay WHERE forward_id = ?`, forward.ID).Scan(&relayName).Error; err != nil {
		t.Fatal(err)
	}
	if relayName != forward.Name {
		t.Fatalf("expected managed relay name to converge to %q, got %q", forward.Name, relayName)
	}
}
