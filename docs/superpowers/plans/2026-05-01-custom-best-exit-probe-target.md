# Custom Best-Exit Probe Target Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let each tunnel define the TCP host/port used for exit-side quality probing and `best` exit scoring, defaulting to `www.bing.com:443` when unset.

**Architecture:** Add small backend probe-target normalization helpers, persist configured target fields on `tunnel`, and thread the effective target through tunnel quality probing and best-exit scoring. Frontend adds compact tunnel-form inputs and replaces Bing-specific monitoring labels with target-aware text while keeping existing API fields compatible.

**Tech Stack:** Go `net/http` handlers + GORM models/repository, SQLite/PostgreSQL auto-migration, Vite/React/TypeScript frontend with shadcn bridge components.

---

## File Structure

- Create `go-backend/internal/http/handler/tunnel_probe_target.go`: target constants, value type, validation, request parsing, formatting, and effective default helpers.
- Create `go-backend/internal/http/handler/tunnel_probe_target_test.go`: TDD coverage for normalization, defaulting, invalid inputs, and formatting.
- Modify `go-backend/internal/store/model/model.go`: add `probe_target_host` and `probe_target_port` columns to `model.Tunnel`.
- Modify `go-backend/internal/store/repo/repository.go`: include configured target fields in `ListTunnels()`.
- Modify `go-backend/internal/store/repo/repository_mutations.go`: persist target fields in tunnel create/update repository helpers.
- Modify `go-backend/internal/http/handler/mutations.go`: validate request target and save it during `tunnelCreate` / `tunnelUpdate`.
- Create `go-backend/internal/http/handler/tunnel_probe_target_api_test.go`: handler-level persistence/list/get tests.
- Modify `go-backend/internal/http/handler/tunnel_best_exit.go` and `tunnel_best_exit_test.go`: make exit-to-public scoring use the configured target.
- Modify `go-backend/internal/http/handler/tunnel_quality_prober.go` and add tests in `tunnel_quality_prober_test.go`: make quality snapshots use and expose the effective target.
- Modify `go-backend/internal/http/handler/monitoring.go`: include effective target metadata in DB fallback quality responses.
- Modify `vite-frontend/src/pages/tunnel.tsx`: tunnel types, edit/create payload, form inputs, list display for target.
- Modify `vite-frontend/src/pages/tunnel/form.ts`: frontend validation/defaults for target fields.
- Modify `vite-frontend/src/api/types.ts`: tunnel quality target metadata.
- Modify `vite-frontend/src/pages/node/tunnel-monitor-view.tsx`: target-aware monitoring labels and chart names.

## Task 1: Backend Probe Target Helpers

**Files:**
- Create: `go-backend/internal/http/handler/tunnel_probe_target.go`
- Create: `go-backend/internal/http/handler/tunnel_probe_target_test.go`

- [ ] **Step 1: Write failing helper tests**

Create `go-backend/internal/http/handler/tunnel_probe_target_test.go`:

```go
package handler

import "testing"

func TestNormalizeTunnelProbeTargetDefaultsWhenEmpty(t *testing.T) {
	target, configured, err := normalizeTunnelProbeTarget("", 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if configured {
		t.Fatalf("expected empty input to be default, not configured")
	}
	if target.Host != defaultTunnelProbeTargetHost || target.Port != defaultTunnelProbeTargetPort {
		t.Fatalf("unexpected default target: %+v", target)
	}
}

func TestNormalizeTunnelProbeTargetAcceptsHostPortAndIPv6(t *testing.T) {
	target, configured, err := normalizeTunnelProbeTarget(" [2001:db8::1] ", 8443)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !configured {
		t.Fatalf("expected explicit target")
	}
	if target.Host != "2001:db8::1" || target.Port != 8443 {
		t.Fatalf("unexpected normalized target: %+v", target)
	}
	if got := formatTunnelProbeTarget(target); got != "[2001:db8::1]:8443" {
		t.Fatalf("unexpected formatted target: %s", got)
	}
}

func TestNormalizeTunnelProbeTargetRejectsPartialAndInvalidInputs(t *testing.T) {
	tests := []struct {
		name string
		host string
		port int
	}{
		{name: "missing host", host: "", port: 443},
		{name: "missing port", host: "example.com", port: 0},
		{name: "port too high", host: "example.com", port: 70000},
		{name: "scheme", host: "https://example.com", port: 443},
		{name: "path", host: "example.com/ping", port: 443},
		{name: "space", host: "example .com", port: 443},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, _, err := normalizeTunnelProbeTarget(tt.host, tt.port); err == nil {
				t.Fatalf("expected validation error")
			}
		})
	}
}

func TestParseTunnelProbeTargetFromRequest(t *testing.T) {
	req := map[string]interface{}{
		"probeTargetHost": "speed.example.com",
		"probeTargetPort": float64(1443),
	}
	target, configured, err := parseTunnelProbeTargetFromRequest(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !configured || target.Host != "speed.example.com" || target.Port != 1443 {
		t.Fatalf("unexpected request target: %+v configured=%v", target, configured)
	}
}
```

- [ ] **Step 2: Run tests to verify RED**

Run from `go-backend`:

```bash
go test ./internal/http/handler -run 'TestNormalizeTunnelProbeTarget|TestParseTunnelProbeTarget' -count=1
```

Expected: FAIL with undefined symbols such as `normalizeTunnelProbeTarget`, `defaultTunnelProbeTargetHost`, and `parseTunnelProbeTargetFromRequest`.

- [ ] **Step 3: Implement helper**

Create `go-backend/internal/http/handler/tunnel_probe_target.go`:

```go
package handler

import (
	"errors"
	"fmt"
	"net/netip"
	"strings"

	"go-backend/internal/store/model"
)

const (
	defaultTunnelProbeTargetHost = "www.bing.com"
	defaultTunnelProbeTargetPort = 443
)

type tunnelProbeTarget struct {
	Host string
	Port int
}

func defaultTunnelProbeTarget() tunnelProbeTarget {
	return tunnelProbeTarget{Host: defaultTunnelProbeTargetHost, Port: defaultTunnelProbeTargetPort}
}

func normalizeTunnelProbeTarget(host string, port int) (tunnelProbeTarget, bool, error) {
	host = strings.TrimSpace(host)
	if strings.HasPrefix(host, "[") && strings.HasSuffix(host, "]") {
		host = strings.TrimPrefix(strings.TrimSuffix(host, "]"), "[")
	}

	if host == "" && port == 0 {
		return defaultTunnelProbeTarget(), false, nil
	}
	if host == "" {
		return tunnelProbeTarget{}, false, errors.New("测试目标 Host 不能为空")
	}
	if port <= 0 || port > 65535 {
		return tunnelProbeTarget{}, false, errors.New("测试目标端口必须是 1-65535")
	}
	if strings.Contains(host, "://") || strings.ContainsAny(host, "/?#") || strings.ContainsAny(host, " \t\r\n") {
		return tunnelProbeTarget{}, false, errors.New("测试目标 Host 不能包含协议或路径")
	}

	return tunnelProbeTarget{Host: host, Port: port}, true, nil
}

func parseTunnelProbeTargetFromRequest(req map[string]interface{}) (tunnelProbeTarget, bool, error) {
	if req == nil {
		return defaultTunnelProbeTarget(), false, nil
	}
	return normalizeTunnelProbeTarget(asString(req["probeTargetHost"]), asInt(req["probeTargetPort"], 0))
}

func effectiveTunnelProbeTarget(tunnel *model.Tunnel) tunnelProbeTarget {
	if tunnel == nil {
		return defaultTunnelProbeTarget()
	}
	return effectiveTunnelProbeTargetValues(tunnel.ProbeTargetHost, tunnel.ProbeTargetPort)
}

func effectiveTunnelProbeTargetValues(host string, port int) tunnelProbeTarget {
	target, configured, err := normalizeTunnelProbeTarget(host, port)
	if err != nil || !configured {
		return defaultTunnelProbeTarget()
	}
	return target
}

func formatTunnelProbeTarget(target tunnelProbeTarget) string {
	if addr, err := netip.ParseAddr(target.Host); err == nil && addr.Is6() {
		return fmt.Sprintf("[%s]:%d", target.Host, target.Port)
	}
	return fmt.Sprintf("%s:%d", target.Host, target.Port)
}
```

- [ ] **Step 4: Run helper tests to verify GREEN**

Run from `go-backend`:

```bash
go test ./internal/http/handler -run 'TestNormalizeTunnelProbeTarget|TestParseTunnelProbeTarget' -count=1
```

Expected: PASS.

- [ ] **Step 5: Format and commit**

```bash
gofmt -w internal/http/handler/tunnel_probe_target.go internal/http/handler/tunnel_probe_target_test.go
git add internal/http/handler/tunnel_probe_target.go internal/http/handler/tunnel_probe_target_test.go
git commit -m "feat: add tunnel probe target normalization"
```

## Task 2: Persist Probe Target On Tunnels

**Files:**
- Modify: `go-backend/internal/store/model/model.go`
- Modify: `go-backend/internal/store/repo/repository.go`
- Modify: `go-backend/internal/store/repo/repository_mutations.go`
- Modify: `go-backend/internal/http/handler/mutations.go`
- Create: `go-backend/internal/http/handler/tunnel_probe_target_api_test.go`

- [ ] **Step 1: Write failing API persistence tests**

Create `go-backend/internal/http/handler/tunnel_probe_target_api_test.go`:

```go
package handler

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"go-backend/internal/store/repo"
)

func TestTunnelCreatePersistsProbeTargetAndListReturnsConfiguredValue(t *testing.T) {
	h := setupProbeTargetTunnelHandler(t)
	body := bytes.NewReader([]byte(`{
		"name":"custom-target",
		"type":1,
		"flow":1,
		"trafficRatio":1,
		"status":1,
		"inNodeId":[{"nodeId":10,"protocol":"tls"}],
		"probeTargetHost":"speed.example.com",
		"probeTargetPort":8443
	}`))

	res := httptest.NewRecorder()
	h.tunnelCreate(res, httptest.NewRequest(http.MethodPost, "/api/v1/tunnel/create", body))
	assertProbeTargetSuccess(t, res)

	listRes := httptest.NewRecorder()
	h.tunnelList(listRes, httptest.NewRequest(http.MethodPost, "/api/v1/tunnel/list", nil))
	var payload struct {
		Code int              `json:"code"`
		Data []map[string]any `json:"data"`
	}
	decodeProbeTargetResponse(t, listRes, &payload)
	if payload.Code != 0 {
		t.Fatalf("expected success, got code %d", payload.Code)
	}
	item := payload.Data[0]
	if item["probeTargetHost"] != "speed.example.com" || item["probeTargetPort"] != float64(8443) {
		t.Fatalf("unexpected probe target in list response: %+v", item)
	}
}

func TestTunnelUpdatePersistsDefaultProbeTargetAsEmpty(t *testing.T) {
	h := setupProbeTargetTunnelHandler(t)
	seedProbeTargetTunnel(t, h, 77, "existing", "old.example.com", 9443)
	body := bytes.NewReader([]byte(`{
		"id":77,
		"name":"existing",
		"type":1,
		"flow":1,
		"trafficRatio":1,
		"status":1,
		"inNodeId":[{"nodeId":10,"protocol":"tls"}],
		"probeTargetHost":"",
		"probeTargetPort":0
	}`))

	res := httptest.NewRecorder()
	h.tunnelUpdate(res, httptest.NewRequest(http.MethodPost, "/api/v1/tunnel/update", body))
	assertProbeTargetSuccess(t, res)

	items, err := h.repo.ListTunnels()
	if err != nil {
		t.Fatalf("list tunnels: %v", err)
	}
	item := findProbeTargetTunnelItem(t, items, 77)
	if item["probeTargetHost"] != "" || item["probeTargetPort"] != 0 {
		t.Fatalf("expected default target to round-trip as empty/0, got %+v", item)
	}
}

func TestTunnelCreateRejectsInvalidProbeTarget(t *testing.T) {
	h := setupProbeTargetTunnelHandler(t)
	body := bytes.NewReader([]byte(`{
		"name":"bad-target",
		"type":1,
		"flow":1,
		"trafficRatio":1,
		"status":1,
		"inNodeId":[{"nodeId":10,"protocol":"tls"}],
		"probeTargetHost":"https://example.com",
		"probeTargetPort":443
	}`))

	res := httptest.NewRecorder()
	h.tunnelCreate(res, httptest.NewRequest(http.MethodPost, "/api/v1/tunnel/create", body))
	var payload struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
	}
	decodeProbeTargetResponse(t, res, &payload)
	if payload.Code == 0 || payload.Msg == "" {
		t.Fatalf("expected validation failure, got %+v", payload)
	}
}

func setupProbeTargetTunnelHandler(t *testing.T) *Handler {
	t.Helper()
	r, err := repo.Open(filepath.Join(t.TempDir(), "panel.db"))
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = r.Close() })
	h := New(r, "secret")
	now := time.Now().UnixMilli()
	if err := r.DB().Exec(`
		INSERT INTO node(id, name, secret, server_ip, server_ip_v4, server_ip_v6, port, interface_name, version, http, tls, socks, created_time, updated_time, status, tcp_listen_addr, udp_listen_addr, inx)
		VALUES(10, 'entry-a', 'entry-secret', '10.0.0.1', '10.0.0.1', '', '30000-30010', '', 'v1', 1, 1, 1, ?, ?, 1, '[::]', '[::]', 0)
	`, now, now).Error; err != nil {
		t.Fatalf("insert node: %v", err)
	}
	return h
}

func seedProbeTargetTunnel(t *testing.T, h *Handler, id int64, name string, host string, port int) {
	t.Helper()
	now := time.Now().UnixMilli()
	if err := h.repo.DB().Exec(`
		INSERT INTO tunnel(id, name, traffic_ratio, type, protocol, flow, created_time, updated_time, status, inx, ip_preference, probe_target_host, probe_target_port)
		VALUES(?, ?, 1, 1, 'tls', 1, ?, ?, 1, ?, '', ?, ?)
	`, id, name, now, now, id, host, port).Error; err != nil {
		t.Fatalf("insert tunnel: %v", err)
	}
	if err := h.repo.DB().Exec(`
		INSERT INTO chain_tunnel(tunnel_id, chain_type, node_id, port, strategy, inx, protocol)
		VALUES(?, '1', 10, 30001, 'round', 1, 'tls')
	`, id).Error; err != nil {
		t.Fatalf("insert chain: %v", err)
	}
}

func assertProbeTargetSuccess(t *testing.T, res *httptest.ResponseRecorder) {
	t.Helper()
	var payload struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
	}
	decodeProbeTargetResponse(t, res, &payload)
	if payload.Code != 0 {
		t.Fatalf("expected success, got %+v", payload)
	}
}

func decodeProbeTargetResponse(t *testing.T, res *httptest.ResponseRecorder, v any) {
	t.Helper()
	if res.Code != http.StatusOK {
		t.Fatalf("expected HTTP %d, got %d", http.StatusOK, res.Code)
	}
	if err := json.NewDecoder(res.Body).Decode(v); err != nil {
		t.Fatalf("decode response: %v", err)
	}
}

func findProbeTargetTunnelItem(t *testing.T, items []map[string]interface{}, id int64) map[string]interface{} {
	t.Helper()
	for _, item := range items {
		if asInt64(item["id"], 0) == id {
			return item
		}
	}
	t.Fatalf("tunnel %d not found: %+v", id, items)
	return nil
}
```

- [ ] **Step 2: Run tests to verify RED**

Run from `go-backend`:

```bash
go test ./internal/http/handler -run 'TestTunnel(Create|Update).*ProbeTarget' -count=1
```

Expected: FAIL because `probe_target_host` / `probe_target_port` columns and response fields do not exist yet.

- [ ] **Step 3: Add model and repository fields**

Modify `go-backend/internal/store/model/model.go` in `type Tunnel`:

```go
	IPPreference    string `gorm:"column:ip_preference;type:varchar(10);not null;default:''"`
	ProbeTargetHost string `gorm:"column:probe_target_host;type:text;not null;default:''"`
	ProbeTargetPort int    `gorm:"column:probe_target_port;not null;default:0"`
```

Modify `go-backend/internal/store/repo/repository.go` in `ListTunnels()` tunnel map:

```go
			"ipPreference":    t.IPPreference,
			"probeTargetHost": t.ProbeTargetHost,
			"probeTargetPort": t.ProbeTargetPort,
```

Modify `go-backend/internal/store/repo/repository_mutations.go` signatures and fields:

```go
func (r *Repository) UpdateTunnelTx(tx *gorm.DB, tunnelID int64, name string, typeVal int, flow int64, trafficRatio float64, status int, inIP, ipPreference string, protocol string, probeTargetHost string, probeTargetPort int, now int64) error {
```

Add to the `Updates` map:

```go
			"probe_target_host": probeTargetHost,
			"probe_target_port": probeTargetPort,
```

Update `CreateTunnelTx` for consistency:

```go
func (r *Repository) CreateTunnelTx(tx *gorm.DB, name string, trafficRatio float64, typeVal int, flow int64, now int64, status int, inIP interface{}, inx int, ipPreference string, probeTargetHost string, probeTargetPort int) (int64, error) {
```

Set fields in `model.Tunnel`:

```go
		ProbeTargetHost: probeTargetHost,
		ProbeTargetPort: probeTargetPort,
```

- [ ] **Step 4: Validate and save fields in handlers**

In `tunnelCreate`, after `ipPreference := asString(req["ipPreference"])`, add:

```go
	probeTarget, probeTargetConfigured, err := parseTunnelProbeTargetFromRequest(req)
	if err != nil {
		response.WriteJSON(w, response.ErrDefault(err.Error()))
		return
	}
	probeTargetHost := ""
	probeTargetPort := 0
	if probeTargetConfigured {
		probeTargetHost = probeTarget.Host
		probeTargetPort = probeTarget.Port
	}
```

Set fields on the `model.Tunnel` literal:

```go
		ProbeTargetHost: probeTargetHost,
		ProbeTargetPort: probeTargetPort,
```

In `tunnelUpdate`, after `ipPreference := asString(req["ipPreference"])`, add the same validation block and pass `probeTargetHost`, `probeTargetPort` into `h.repo.UpdateTunnelTx(...)` before `now`.

- [ ] **Step 5: Run API persistence tests to verify GREEN**

Run from `go-backend`:

```bash
go test ./internal/http/handler -run 'TestTunnel(Create|Update).*ProbeTarget' -count=1
```

Expected: PASS.

- [ ] **Step 6: Run broader backend compile check and commit**

```bash
gofmt -w internal/store/model/model.go internal/store/repo/repository.go internal/store/repo/repository_mutations.go internal/http/handler/mutations.go internal/http/handler/tunnel_probe_target_api_test.go
go test ./internal/http/handler -run 'TestTunnel(Create|Update).*ProbeTarget|TestNormalizeTunnelProbeTarget' -count=1
git add internal/store/model/model.go internal/store/repo/repository.go internal/store/repo/repository_mutations.go internal/http/handler/mutations.go internal/http/handler/tunnel_probe_target_api_test.go
git commit -m "feat: persist tunnel probe targets"
```

## Task 3: Use Probe Target In Best-Exit Scoring

**Files:**
- Modify: `go-backend/internal/http/handler/tunnel_best_exit.go`
- Modify: `go-backend/internal/http/handler/tunnel_best_exit_test.go`
- Modify: `go-backend/internal/http/handler/tunnel_quality_prober.go`

- [ ] **Step 1: Write failing best-exit scoring test**

Append to `go-backend/internal/http/handler/tunnel_best_exit_test.go`:

```go
func TestEvaluateBestExitOwnerUsesConfiguredPublicProbeTarget(t *testing.T) {
	owner := chainNodeRecord{NodeID: 10, NodeName: "entry-a"}
	exits := []chainNodeRecord{{NodeID: 30, NodeName: "exit-a", Port: 30001}}
	nodes := map[int64]*nodeRecord{
		10: {ID: 10, Name: "entry-a", ServerIP: "10.0.0.10", ServerIPv4: "10.0.0.10"},
		30: {ID: 30, Name: "exit-a", ServerIP: "10.0.0.30", ServerIPv4: "10.0.0.30"},
	}
	target := tunnelProbeTarget{Host: "speed.example.com", Port: 8443}
	var calls []string
	ping := func(nodeID int64, ip string, port int, options diagnosisExecOptions) (float64, float64, error) {
		calls = append(calls, fmt.Sprintf("%d|%s|%d", nodeID, ip, port))
		return 10, 0, nil
	}

	scores := evaluateBestExitOwner(owner, exits, nodes, "", diagnosisExecOptions{}, target, ping)
	if len(scores) != 1 || !scores[0].Success {
		t.Fatalf("expected successful score, got %+v", scores)
	}
	if !slices.Contains(calls, "30|speed.example.com|8443") {
		t.Fatalf("expected exit public probe to use configured target, calls=%+v", calls)
	}
	for _, call := range calls {
		if strings.Contains(call, defaultTunnelProbeTargetHost) {
			t.Fatalf("did not expect default target call when custom target configured: %+v", calls)
		}
	}
}
```

Ensure the `tunnel_best_exit_test.go` import block contains these imports after adding the test:

```go
import (
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"
)
```

- [ ] **Step 2: Run test to verify RED**

Run from `go-backend`:

```bash
go test ./internal/http/handler -run TestEvaluateBestExitOwnerUsesConfiguredPublicProbeTarget -count=1
```

Expected: FAIL because `evaluateBestExitOwner` does not accept a target and still uses the hardcoded default.

- [ ] **Step 3: Thread target into best-exit scoring**

Modify `evaluateBestExitOwner` signature in `tunnel_best_exit.go`:

```go
func evaluateBestExitOwner(owner chainNodeRecord, exits []chainNodeRecord, nodes map[int64]*nodeRecord, ipPreference string, options diagnosisExecOptions, target tunnelProbeTarget, ping bestExitProbeFunc) []bestExitCandidateScore {
```

Replace the hardcoded public probe call:

```go
		publicLatency, publicLoss, publicErr := ping(exit.NodeID, target.Host, target.Port, options)
```

Update existing tests and callers to pass `defaultTunnelProbeTarget()` unless they are testing custom targets.

In `tunnel_quality_prober.go`, compute and pass the target from `probeTunnel`:

```go
	probeTarget := effectiveTunnelProbeTarget(tunnel)
	p.probeBestExitOwners(tunnelID, inNodes, midNodesGrouped, outNodes, ipPreference, options, probeTarget)
```

Change `probeBestExitOwners` signature:

```go
func (p *tunnelQualityProber) probeBestExitOwners(tunnelID int64, inNodes []chainNodeRecord, chainHops [][]chainNodeRecord, outNodes []chainNodeRecord, ipPreference string, options diagnosisExecOptions, probeTarget tunnelProbeTarget) {
```

Pass it into scoring:

```go
		scores := evaluateBestExitOwner(owner, outNodes, nodeMap, ipPreference, options, probeTarget, roundPinger)
```

- [ ] **Step 4: Make round pinger cache target-safe**

In `tunnel_best_exit.go`, replace `newBestExitRoundPinger` cache key with node+host+port:

```go
type bestExitProbeCacheKey struct {
	NodeID int64
	Host   string
	Port   int
}

func newBestExitRoundPinger(base bestExitProbeFunc) bestExitProbeFunc {
	cache := make(map[bestExitProbeCacheKey]bestExitProbeResult)
	return func(nodeID int64, ip string, port int, options diagnosisExecOptions) (float64, float64, error) {
		key := bestExitProbeCacheKey{NodeID: nodeID, Host: ip, Port: port}
		if cached, ok := cache[key]; ok {
			return cached.latency, cached.loss, cached.err
		}
		lat, loss, err := base(nodeID, ip, port, options)
		cache[key] = bestExitProbeResult{latency: lat, loss: loss, err: err}
		return lat, loss, err
	}
}
```

- [ ] **Step 5: Run best-exit tests to verify GREEN**

Run from `go-backend`:

```bash
go test ./internal/http/handler -run 'TestEvaluateBestExitOwner|TestBestExit|TestBuildTunnelChainConfigUsesBestRuntimeStrategy' -count=1
```

Expected: PASS.

- [ ] **Step 6: Format and commit**

```bash
gofmt -w internal/http/handler/tunnel_best_exit.go internal/http/handler/tunnel_best_exit_test.go internal/http/handler/tunnel_quality_prober.go
git add internal/http/handler/tunnel_best_exit.go internal/http/handler/tunnel_best_exit_test.go internal/http/handler/tunnel_quality_prober.go
git commit -m "feat: use probe target for best exit scoring"
```

## Task 4: Use Probe Target In Tunnel Quality Monitoring

**Files:**
- Modify: `go-backend/internal/http/handler/tunnel_quality_prober.go`
- Modify: `go-backend/internal/http/handler/monitoring.go`
- Create: `go-backend/internal/http/handler/tunnel_quality_prober_test.go`

- [ ] **Step 1: Write failing quality prober test**

Create `go-backend/internal/http/handler/tunnel_quality_prober_test.go`:

```go
func TestTunnelQualityProberUsesConfiguredProbeTarget(t *testing.T) {
	h := setupProbeTargetTunnelHandler(t)
	seedProbeTargetTunnel(t, h, 77, "quality-target", "speed.example.com", 8443)
	if err := h.repo.DB().Exec(`
		INSERT INTO node(id, name, secret, server_ip, server_ip_v4, server_ip_v6, port, interface_name, version, http, tls, socks, created_time, updated_time, status, tcp_listen_addr, udp_listen_addr, inx)
		VALUES(30, 'exit-a', 'exit-secret', '10.0.0.30', '10.0.0.30', '', '30000-30010', '', 'v1', 1, 1, 1, ?, ?, 1, '[::]', '[::]', 0)
	`, time.Now().UnixMilli(), time.Now().UnixMilli()).Error; err != nil {
		t.Fatalf("insert exit node: %v", err)
	}
	if err := h.repo.DB().Exec(`
		INSERT INTO chain_tunnel(tunnel_id, chain_type, node_id, port, strategy, inx, protocol)
		VALUES(77, '3', 30, 30001, 'round', 1, 'tls')
	`).Error; err != nil {
		t.Fatalf("insert exit chain: %v", err)
	}

	p := newTunnelQualityProber(h)
	var calls []string
	p.probeNode = func(nodeID int64, ip string, port int, options diagnosisExecOptions) (float64, float64, error) {
		calls = append(calls, fmt.Sprintf("%d|%s|%d", nodeID, ip, port))
		return 10, 0, nil
	}
	p.probeTunnel(77)

	if !slices.Contains(calls, "30|speed.example.com|8443") {
		t.Fatalf("expected exit probe to configured target, calls=%+v", calls)
	}
	snaps := p.GetAll()
	if len(snaps) != 1 {
		t.Fatalf("expected one quality snapshot, got %+v", snaps)
	}
	if snaps[0].ProbeTargetHost != "speed.example.com" || snaps[0].ProbeTargetPort != 8443 {
		t.Fatalf("unexpected snapshot target metadata: %+v", snaps[0])
	}
}
```

Use this import block in the new `tunnel_quality_prober_test.go` file:

```go
import (
	"fmt"
	"slices"
	"testing"
	"time"
)
```

- [ ] **Step 2: Run test to verify RED**

Run from `go-backend`:

```bash
go test ./internal/http/handler -run TestTunnelQualityProberUsesConfiguredProbeTarget -count=1
```

Expected: FAIL because `tunnelQualityProber` has no injectable `probeNode` and snapshots do not include target metadata.

- [ ] **Step 3: Add injectable probe function and target metadata**

Modify `tunnelQualitySnapshot` in `tunnel_quality_prober.go`:

```go
	ProbeTargetHost string `json:"probeTargetHost,omitempty"`
	ProbeTargetPort int    `json:"probeTargetPort,omitempty"`
```

Modify `tunnelQualityProber` struct:

```go
	probeNode bestExitProbeFunc
```

Add method:

```go
func (p *tunnelQualityProber) pingNode(nodeID int64, ip string, port int, options diagnosisExecOptions) (float64, float64, error) {
	if p != nil && p.probeNode != nil {
		return p.probeNode(nodeID, ip, port, options)
	}
	return p.tcpPingNode(nodeID, ip, port, options)
}
```

Replace existing `p.tcpPingNode(...)` calls in `probeTunnel` and `probeBestExitOwners` setup with `p.pingNode(...)` / `newBestExitRoundPinger(p.pingNode)`.

- [ ] **Step 4: Use effective target in quality probes**

In `probeTunnel`, after loading `tunnel` and before the switch:

```go
	probeTarget := effectiveTunnelProbeTarget(tunnel)
	snap.ProbeTargetHost = probeTarget.Host
	snap.ProbeTargetPort = probeTarget.Port
```

Replace hardcoded Bing probes:

```go
	lat, loss, err := p.pingNode(inNodes[0].NodeID, probeTarget.Host, probeTarget.Port, options)
```

and:

```go
	lat, loss, err := p.pingNode(outNodes[0].NodeID, probeTarget.Host, probeTarget.Port, options)
```

Use the same replacement in the default case. Keep persisted DB columns unchanged.

- [ ] **Step 5: Include target metadata in DB fallback monitoring response**

In `monitoring.go`, when converting DB `TunnelQuality` rows to `tunnelQualitySnapshot`, fetch tunnel list once and build an effective-target map:

```go
	targetsByTunnelID := map[int64]tunnelProbeTarget{}
	if tunnels, listErr := h.repo.ListTunnels(); listErr == nil {
		for _, item := range tunnels {
			id := asInt64(item["id"], 0)
			if id > 0 {
				targetsByTunnelID[id] = effectiveTunnelProbeTargetValues(asString(item["probeTargetHost"]), asInt(item["probeTargetPort"], 0))
			}
		}
	}
```

Set snapshot metadata:

```go
			target := targetsByTunnelID[q.TunnelID]
			if target.Host == "" {
				target = defaultTunnelProbeTarget()
			}
```

Then include:

```go
			ProbeTargetHost: target.Host,
			ProbeTargetPort: target.Port,
```

- [ ] **Step 6: Run quality prober tests to verify GREEN**

Run from `go-backend`:

```bash
go test ./internal/http/handler -run 'TestTunnelQualityProberUsesConfiguredProbeTarget|TestTunnelCreatePersistsProbeTarget' -count=1
```

Expected: PASS.

- [ ] **Step 7: Format and commit**

```bash
gofmt -w internal/http/handler/tunnel_quality_prober.go internal/http/handler/tunnel_quality_prober_test.go internal/http/handler/monitoring.go
git add internal/http/handler/tunnel_quality_prober.go internal/http/handler/tunnel_quality_prober_test.go internal/http/handler/monitoring.go
git commit -m "feat: use probe target for tunnel quality checks"
```

## Task 5: Frontend Form And Monitoring Display

**Files:**
- Modify: `vite-frontend/src/pages/tunnel.tsx`
- Modify: `vite-frontend/src/pages/tunnel/form.ts`
- Modify: `vite-frontend/src/api/types.ts`
- Modify: `vite-frontend/src/pages/node/tunnel-monitor-view.tsx`

- [ ] **Step 1: Extend frontend tunnel and quality types**

In `vite-frontend/src/pages/tunnel.tsx`, add fields to `Tunnel` and `TunnelForm`:

```ts
probeTargetHost?: string;
probeTargetPort?: number;
```

In `vite-frontend/src/api/types.ts`, extend `TunnelQualityApiItem`:

```ts
probeTargetHost?: string;
probeTargetPort?: number;
```

In `vite-frontend/src/pages/tunnel/form.ts`, extend `TunnelFormInput`:

```ts
probeTargetHost?: string;
probeTargetPort?: number;
```

- [ ] **Step 2: Add frontend defaults and validation**

In `createTunnelFormDefaults()` add:

```ts
probeTargetHost: "",
probeTargetPort: 0,
```

In `validateTunnelForm`, add after traffic ratio validation:

```ts
const probeHost = (form.probeTargetHost || "").trim();
const probePort = Number(form.probeTargetPort || 0);

if (probeHost || probePort > 0) {
  if (!probeHost) {
    errors.probeTargetHost = "请输入测试目标 Host";
  } else if (
    probeHost.includes("://") ||
    /[\s/?#]/.test(probeHost)
  ) {
    errors.probeTargetHost = "Host 不能包含协议、空格或路径";
  }

  if (!Number.isInteger(probePort) || probePort < 1 || probePort > 65535) {
    errors.probeTargetPort = "端口必须是 1-65535";
  }
}
```

- [ ] **Step 3: Round-trip form values**

In `handleEdit`, add:

```ts
probeTargetHost: tunnel.probeTargetHost || "",
probeTargetPort: tunnel.probeTargetPort || 0,
```

Before building submit `data`, normalize:

```ts
const probeTargetHost = (form.probeTargetHost || "").trim();
const probeTargetPort = probeTargetHost ? Number(form.probeTargetPort || 0) : 0;
```

Set in payload:

```ts
probeTargetHost,
probeTargetPort,
```

- [ ] **Step 4: Add form inputs**

In the tunnel modal body near `ipPreference` and before `入口配置`, add:

```tsx
<div className="rounded-xl border border-divider/60 bg-default-50/40 p-3 space-y-3">
  <div>
    <div className="text-sm font-medium">质量检测目标</div>
    <p className="text-xs text-default-500 mt-0.5">
      用于实时隧道质量检测和 best 最优出口评分，留空使用 www.bing.com:443
    </p>
  </div>
  <div className="grid grid-cols-1 md:grid-cols-[1fr_140px] gap-3">
    <Input
      errorMessage={errors.probeTargetHost}
      isInvalid={!!errors.probeTargetHost}
      label="Host"
      placeholder="www.bing.com"
      value={form.probeTargetHost || ""}
      variant="bordered"
      onChange={(e) =>
        setForm((prev) => ({ ...prev, probeTargetHost: e.target.value }))
      }
    />
    <Input
      errorMessage={errors.probeTargetPort}
      isInvalid={!!errors.probeTargetPort}
      label="Port"
      max={65535}
      min={1}
      placeholder="443"
      type="number"
      value={form.probeTargetPort ? String(form.probeTargetPort) : ""}
      variant="bordered"
      onChange={(e) =>
        setForm((prev) => ({
          ...prev,
          probeTargetPort: e.target.value ? Number(e.target.value) : 0,
        }))
      }
    />
  </div>
</div>
```

- [ ] **Step 5: Add target-aware labels in monitoring view**

In `vite-frontend/src/pages/node/tunnel-monitor-view.tsx`, add helper near constants:

```ts
const DEFAULT_PROBE_TARGET_LABEL = "www.bing.com:443";

const probeTargetLabel = (quality?: TunnelQualityApiItem | null) => {
  if (!quality?.probeTargetHost || !quality.probeTargetPort) {
    return DEFAULT_PROBE_TARGET_LABEL;
  }
  const host = quality.probeTargetHost.includes(":")
    ? `[${quality.probeTargetHost}]`
    : quality.probeTargetHost;
  return `${host}:${quality.probeTargetPort}`;
};
```

Replace visible labels `出口 → Bing 延迟`, `出口 → Bing 丢包`, `出口→Bing`, and table header `出口→Bing` with target-neutral labels:

```tsx
出口 → 测试目标 延迟
```

and add `title={probeTargetLabel(quality)}` to compact labels where space is constrained.

Where the detail view has auto-probe status, append:

```tsx
<span className="text-default-400">· 测试目标: {probeTargetLabel(quality)}</span>
```

- [ ] **Step 6: Run frontend build**

Run from `vite-frontend`:

```bash
pnpm run build
```

Expected: PASS.

- [ ] **Step 7: Commit frontend changes**

```bash
git add src/pages/tunnel.tsx src/pages/tunnel/form.ts src/api/types.ts src/pages/node/tunnel-monitor-view.tsx
git commit -m "feat: add tunnel probe target UI"
```

## Task 6: Full Verification And Final Review

**Files:**
- No planned code changes. Fix only issues found by verification or review.

- [ ] **Step 1: Run backend tests**

Run from `go-backend`:

```bash
go test ./...
```

Expected: PASS.

- [ ] **Step 2: Run frontend build**

Run from `vite-frontend`:

```bash
pnpm run build
```

Expected: PASS.

- [ ] **Step 3: Check diff hygiene**

Run from repo root:

```bash
git diff --check origin/main...HEAD
git status --short
```

Expected: no whitespace errors; only intended branch changes.

- [ ] **Step 4: Manual smoke checklist**

Use local backend/frontend if available:

```bash
# terminal 1
cd go-backend && go run ./cmd/paneld

# terminal 2
cd vite-frontend && pnpm run dev
```

Check:

- Create tunnel with blank quality target; list/get shows empty target fields and monitoring uses `www.bing.com:443` effectively.
- Edit tunnel with `speed.example.com` and `8443`; reopen edit modal and confirm values round-trip.
- Enable tunnel quality detection; monitoring labels show `测试目标`, not hardcoded Bing.
- A `best` multi-exit tunnel eventually leaves `等待探测` when probes succeed against the configured target.

- [ ] **Step 5: Final code review**

Dispatch a final reviewer with this context:

```text
Review custom per-tunnel probe target implementation. Verify configured host/port persists, defaults to www.bing.com:443 when unset, is used by best-exit scoring and tunnel quality prober, preserves existing API fields, and does not change routing/switching thresholds or add frontend dependencies.
```

Fix any Critical or Important findings, then rerun Steps 1-3.

- [ ] **Step 6: Commit review fixes if any**

If review fixes were needed, stage the files touched by those fixes and commit them. Use the exact changed paths from `git status --short`; do not stage unrelated files.

```bash
git status --short
git add go-backend/internal/http/handler/tunnel_probe_target.go go-backend/internal/http/handler/tunnel_probe_target_test.go go-backend/internal/http/handler/tunnel_probe_target_api_test.go go-backend/internal/http/handler/tunnel_best_exit.go go-backend/internal/http/handler/tunnel_best_exit_test.go go-backend/internal/http/handler/tunnel_quality_prober.go go-backend/internal/http/handler/tunnel_quality_prober_test.go go-backend/internal/http/handler/monitoring.go go-backend/internal/http/handler/mutations.go go-backend/internal/store/model/model.go go-backend/internal/store/repo/repository.go go-backend/internal/store/repo/repository_mutations.go vite-frontend/src/pages/tunnel.tsx vite-frontend/src/pages/tunnel/form.ts vite-frontend/src/api/types.ts vite-frontend/src/pages/node/tunnel-monitor-view.tsx
git commit -m "fix: stabilize custom probe target handling"
```

If `git status --short` shows no review-fix changes, do not create an empty commit.

---

## Implementation Notes

- Keep the existing `exitToBingLatency` / `exitToBingLoss` JSON and DB fields for compatibility; change labels, not keys.
- Do not add frontend tests. This repository has no frontend test framework and `AGENTS.md` explicitly says not to add one.
- Backend handlers should continue using repository methods for new persistence paths. Do not introduce new `repo.DB()` usage in production handlers.
- SQLite and PostgreSQL compatibility: use plain string/int GORM tags, no `jsonb` or `serial`.
- Do not edit `install.sh` or `panel_install.sh`.
