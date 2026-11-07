# Per-IP Rule Limits Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add per-client-IP max connection and bandwidth limits to forward rules while preserving existing total rule/user limit behavior.

**Status:** Completed in PR #479 and tagged `3.0.0-beta2`.

**Architecture:** Persist per-IP rule settings on `forward`, expose them through the existing forward APIs, and translate them into GOST's built-in `$$`, `0.0.0.0/0`, and `::/0` limiter syntax during runtime sync. Runtime service JSON should only receive final limiter names, while helper functions own the payload construction. UDP listener wrapping must match TCP listener behavior so per-client limiters apply consistently.

**Tech Stack:** Go `net/http`, GORM, SQLite/PostgreSQL, GOST/x listener and limiter packages, React, TypeScript, Vite.

---

## File Map

- Modify `go-backend/internal/store/model/model.go`: add `IPMaxConn` and `IPSpeedID` to `Forward`, `ForwardRecord`, and backup structs.
- Modify `go-backend/internal/store/repo/repository_flow.go`: populate per-IP fields in active forward records.
- Modify `go-backend/internal/store/repo/repository_control.go`: populate per-IP fields in tunnel forward records.
- Modify `go-backend/internal/store/repo/repository_mutations.go`: persist per-IP fields in `CreateForwardTx` and `UpdateForward`.
- Modify `go-backend/internal/store/repo/repository.go`: include per-IP fields in `/forward/list`, backup export, and backup import.
- Modify `go-backend/internal/store/repo/repository_forward_proxy_protocol_test.go`: add repository tests for new fields.
- Modify `go-backend/internal/http/handler/mutations.go`: parse API fields and enforce non-admin `ipSpeedId` restrictions.
- Modify `go-backend/internal/http/handler/control_plane.go`: build combined limiter payloads and pass final limiter names to service configs.
- Modify `go-backend/internal/http/handler/control_plane_test.go`: add unit tests for limiter helper output and service config references.
- Modify `go-backend/tests/contract/max_conn_limit_contract_test.go`: cover `$$` per-IP connection limiter payloads.
- Create `go-backend/tests/contract/per_ip_speed_limit_contract_test.go`: cover per-IP traffic limiter payloads and non-admin permission behavior.
- Modify `go-gost/x/listener/udp/listener.go`: wrap UDP pseudo-connections with connection and connection-scope traffic limiters.
- Create `go-gost/x/limiter/conn/conn_test.go`: prove `$$` creates independent per-IP connection limiters.
- Create `go-gost/x/limiter/traffic/traffic_test.go`: prove CIDR traffic limiters create independent per-IP bandwidth buckets.
- Create `go-gost/x/listener/udp/listener_test.go`: prove UDP connection limiter counts are released on close.
- Modify `vite-frontend/src/api/types.ts`: add API payload fields.
- Modify `vite-frontend/src/pages/forward.tsx`: add form state, mapping, submit payload, and advanced controls.

---

### Task 1: Backend Data Model And Repository

**Files:**

- Modify: `go-backend/internal/store/model/model.go`
- Modify: `go-backend/internal/store/repo/repository_flow.go`
- Modify: `go-backend/internal/store/repo/repository_control.go`
- Modify: `go-backend/internal/store/repo/repository_mutations.go`
- Modify: `go-backend/internal/store/repo/repository.go`
- Modify: `go-backend/internal/store/repo/repository_forward_proxy_protocol_test.go`

- [x] **Step 1: Write failing repository tests**

Add `database/sql` to `repository_forward_proxy_protocol_test.go` imports, then append this test:

```go
func TestForwardRepositoryPersistsPerIPLimits(t *testing.T) {
	r, err := Open(":memory:")
	if err != nil {
		t.Fatalf("open repo: %v", err)
	}
	defer r.Close()

	now := time.Now().UnixMilli()
	forwardID, err := r.CreateForwardTx(1, "admin", "per-ip-forward", 2, "1.1.1.1:443", "fifo", now, 1, []int64{3}, 24000, "", nil, 0, 5, int64(21), 0)
	if err != nil {
		t.Fatalf("CreateForwardTx: %v", err)
	}
	record, err := r.GetForwardRecord(forwardID)
	if err != nil {
		t.Fatalf("GetForwardRecord after create: %v", err)
	}
	if record.IPMaxConn != 5 {
		t.Fatalf("expected created ipMaxConn 5, got %d", record.IPMaxConn)
	}
	if !record.IPSpeedID.Valid || record.IPSpeedID.Int64 != 21 {
		t.Fatalf("expected created ipSpeedId 21, got %+v", record.IPSpeedID)
	}

	if err := r.UpdateForward(forwardID, "per-ip-forward", 2, "2.2.2.2:443", "fifo", now+1, nil, 0, 9, int64(22), 0); err != nil {
		t.Fatalf("UpdateForward: %v", err)
	}
	record, err = r.GetForwardRecord(forwardID)
	if err != nil {
		t.Fatalf("GetForwardRecord after update: %v", err)
	}
	if record.IPMaxConn != 9 {
		t.Fatalf("expected updated ipMaxConn 9, got %d", record.IPMaxConn)
	}
	if !record.IPSpeedID.Valid || record.IPSpeedID.Int64 != 22 {
		t.Fatalf("expected updated ipSpeedId 22, got %+v", record.IPSpeedID)
	}

	if err := r.DB().Create(&model.Forward{
		UserID:      4,
		UserName:    "user",
		Name:        "listed-per-ip-forward",
		TunnelID:    8,
		RemoteAddr:  "3.3.3.3:443",
		Strategy:    "fifo",
		CreatedTime: now,
		UpdatedTime: now,
		Status:      1,
		IPMaxConn:   11,
		IPSpeedID:   sql.NullInt64{Int64: 33, Valid: true},
	}).Error; err != nil {
		t.Fatalf("create listed forward: %v", err)
	}
	records, err := r.ListForwardsByTunnel(8)
	if err != nil {
		t.Fatalf("ListForwardsByTunnel: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("expected 1 listed record, got %d", len(records))
	}
	if records[0].IPMaxConn != 11 || !records[0].IPSpeedID.Valid || records[0].IPSpeedID.Int64 != 33 {
		t.Fatalf("expected listed per-IP limits 11/33, got ipMaxConn=%d ipSpeedId=%+v", records[0].IPMaxConn, records[0].IPSpeedID)
	}
}
```

- [x] **Step 2: Run the repository test to verify failure**

Run: `(cd go-backend && go test ./internal/store/repo -run TestForwardRepositoryPersistsPerIPLimits)`

Expected: FAIL because the new fields and method signatures do not exist.

- [x] **Step 3: Add model fields**

In `go-backend/internal/store/model/model.go`, add these fields to `Forward` after `MaxConn`:

```go
	IPMaxConn     int           `gorm:"column:ip_max_conn;not null;default:0"`
	IPSpeedID     sql.NullInt64 `gorm:"column:ip_speed_id"`
```

Add these fields to `ForwardBackup` after `SpeedID`:

```go
	IPMaxConn     int                  `json:"ipMaxConn,omitempty"`
	IPSpeedID     *int64               `json:"ipSpeedId,omitempty"`
```

Add these fields to `ForwardRecord` after `MaxConn`:

```go
	IPMaxConn     int
	IPSpeedID     sql.NullInt64
```

- [x] **Step 4: Populate repository records**

In each `model.ForwardRecord` composite literal in `repository_flow.go` and `repository_control.go`, add:

```go
			IPMaxConn:     f.IPMaxConn,
			IPSpeedID:     f.IPSpeedID,
```

- [x] **Step 5: Persist create/update fields**

Change `UpdateForward` in `repository_mutations.go` to accept `ipMaxConn int, ipSpeedID interface{}` between `maxConn` and `proxyProtocol`, and add these update keys:

```go
			"ip_max_conn":    ipMaxConn,
			"ip_speed_id":    nullInt64FromInterface(ipSpeedID),
```

Change `CreateForwardTx` in `repository_mutations.go` to accept `ipMaxConn int, ipSpeedID interface{}` between `maxConn` and `proxyProtocol`, and add these fields to the `model.Forward` literal:

```go
			IPMaxConn:     ipMaxConn,
			IPSpeedID:     nullInt64FromInterface(ipSpeedID),
```

- [x] **Step 6: Include fields in list and backup paths**

In `repository.go` `ListForwards`, add `IPMaxConn int`, `IPSpeedID sql.NullInt64`, and `IPSpeedLimitName string` to `fwdRow`. Update the select and joins to include:

```go
		Select("forward.id, forward.user_id, forward.user_name, forward.name, forward.tunnel_id, COALESCE(tunnel.name, '') AS tunnel_name, COALESCE(tunnel.traffic_ratio, 1.0) AS traffic_ratio, forward.remote_addr, COALESCE(forward.strategy, 'fifo') AS strategy, forward.in_flow, forward.out_flow, forward.created_time, forward.status, forward.inx, forward.speed_id, forward.max_conn, forward.ip_max_conn, forward.ip_speed_id, COALESCE(ip_speed_limit.name, '') AS ip_speed_limit_name, forward.proxy_protocol").
		Joins("LEFT JOIN tunnel ON tunnel.id = forward.tunnel_id").
		Joins("LEFT JOIN speed_limit AS ip_speed_limit ON ip_speed_limit.id = forward.ip_speed_id").
```

Add these response fields:

```go
			"ipMaxConn": row.IPMaxConn,
```

Add this block after the existing `speedId` block:

```go
		if row.IPSpeedID.Valid {
			item["ipSpeedId"] = row.IPSpeedID.Int64
		}
		if strings.TrimSpace(row.IPSpeedLimitName) != "" {
			item["ipSpeedLimitName"] = row.IPSpeedLimitName
		}
```

In `exportForwards`, set `IPMaxConn: f.IPMaxConn` in the backup literal and set nullable IDs:

```go
		if f.SpeedID.Valid {
			v := f.SpeedID.Int64
			b.SpeedID = &v
		}
		if f.IPSpeedID.Valid {
			v := f.IPSpeedID.Int64
			b.IPSpeedID = &v
		}
```

In `importForwards`, set:

```go
			SpeedID:   sql.NullInt64{Int64: nullableBackupInt64(f.SpeedID), Valid: f.SpeedID != nil && *f.SpeedID > 0},
			IPMaxConn: f.IPMaxConn,
			IPSpeedID: sql.NullInt64{Int64: nullableBackupInt64(f.IPSpeedID), Valid: f.IPSpeedID != nil && *f.IPSpeedID > 0},
```

Add this helper near import helpers in `repository.go`:

```go
func nullableBackupInt64(v *int64) int64 {
	if v == nil {
		return 0
	}
	return *v
}
```

Add `"speed_id", "ip_max_conn", "ip_speed_id"` to the `importForwards` `DoUpdates` assignment list.

- [x] **Step 7: Update call sites to compile**

Update every `CreateForwardTx` call to pass `ipMaxConn` and `ipSpeedID` before `proxyProtocol`. Existing call sites without per-IP values should pass `0, nil`.

Update every `UpdateForward` call to pass `ipMaxConn` and `ipSpeedID` before `proxyProtocol`. Existing call sites without per-IP values should pass `0, nil` or values from the request in later tasks.

- [x] **Step 8: Run repository tests**

Run: `(cd go-backend && go test ./internal/store/repo -run 'ForwardRepositoryPersistsPerIPLimits|ListForwardsByTunnelIncludes|GetForwardRecordIncludes')`

Expected: PASS.

- [x] **Step 9: Commit**

Run:

```bash
git add go-backend/internal/store/model/model.go go-backend/internal/store/repo/repository_flow.go go-backend/internal/store/repo/repository_control.go go-backend/internal/store/repo/repository_mutations.go go-backend/internal/store/repo/repository.go go-backend/internal/store/repo/repository_forward_proxy_protocol_test.go
git commit -m "feat: persist per-IP forward limits"
```

---

### Task 2: Backend API Parsing And Permissions

**Files:**

- Modify: `go-backend/internal/http/handler/mutations.go`
- Modify: `go-backend/tests/contract/forward_contract_test.go`

- [x] **Step 1: Write failing non-admin permission test**

In `forward_contract_test.go`, add a test next to the existing non-admin `speedId` tests:

```go
func TestForwardIPSpeedLimitPermission(t *testing.T) {
	secret := "contract-jwt-secret"
	router, repo := setupContractRouter(t, secret)
	now := time.Now().UnixMilli()

	if err := repo.DB().Exec(`
		INSERT INTO user(id, user, pwd, role_id, exp_time, flow, in_flow, out_flow, flow_reset_time, num, created_time, updated_time, status)
		VALUES(2, 'normal_user', 'pwd', 1, ?, 99999, 0, 0, 1, 10, ?, ?, 1)
	`, now+86400000, now, now).Error; err != nil {
		t.Fatalf("insert user: %v", err)
	}
	if err := repo.DB().Exec(`
		INSERT INTO tunnel(id, name, traffic_ratio, type, protocol, flow, created_time, updated_time, status, in_ip, inx)
		VALUES(12, 'ip-speed-permission-tunnel', 1.0, 1, 'tls', 99999, ?, ?, 1, NULL, 0)
	`, now, now).Error; err != nil {
		t.Fatalf("insert tunnel: %v", err)
	}
	if err := repo.DB().Exec(`
		INSERT INTO node(id, name, secret, server_ip, server_ip_v4, server_ip_v6, port, interface_name, version, http, tls, socks, created_time, updated_time, status, tcp_listen_addr, udp_listen_addr, inx)
		VALUES(20, 'ip-speed-permission-node', 'ip-speed-permission-secret', '10.22.0.1', '10.22.0.1', '', '32200-32210', '', 'v1', 1, 1, 1, ?, ?, 1, '[::]', '[::]', 0)
	`, now, now).Error; err != nil {
		t.Fatalf("insert node: %v", err)
	}
	if err := repo.DB().Exec(`
		INSERT INTO chain_tunnel(tunnel_id, chain_type, node_id, port, strategy, inx, protocol)
		VALUES(12, 1, 20, 32201, 'round', 1, 'tls')
	`).Error; err != nil {
		t.Fatalf("insert chain_tunnel: %v", err)
	}
	if err := repo.DB().Exec(`
		INSERT INTO speed_limit(id, name, speed, created_time, status)
		VALUES(9, 'per-ip-10m', 10, ?, 1)
	`, now).Error; err != nil {
		t.Fatalf("insert speed limit: %v", err)
	}
	if err := repo.DB().Exec(`
		INSERT INTO user_tunnel(user_id, tunnel_id, num, flow, in_flow, out_flow, flow_reset_time, exp_time, status)
		VALUES(2, 12, 10, 99999, 0, 0, 1, ?, 1)
	`, now+86400000).Error; err != nil {
		t.Fatalf("insert user tunnel: %v", err)
	}

	userToken, err := auth.GenerateToken(2, "normal_user", 1, secret)
	if err != nil {
		t.Fatalf("generate user token: %v", err)
	}
	body, err := json.Marshal(map[string]interface{}{
		"name":      "blocked-ip-speed",
		"tunnelId":  12,
		"remoteAddr": "1.1.1.1:443",
		"strategy":   "fifo",
		"ipSpeedId": 9,
	})
	if err != nil {
		t.Fatalf("marshal create payload: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/forward/create", bytes.NewReader(body))
	req.Header.Set("Authorization", userToken)
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)
	assertCodeMsg(t, res, -1, "普通用户无法设置每 IP 限速规则")
}
```

- [x] **Step 2: Run permission test and confirm failure**

Run: `(cd go-backend && go test ./tests/contract -run TestForwardIPSpeedLimitPermission)`

Expected: FAIL because `ipSpeedId` is not parsed or blocked.

- [x] **Step 3: Parse create fields**

In `forwardCreate`, after `speedID` normalization, add:

```go
	if roleID != 0 {
		if ipSpeedIDVal, ok := req["ipSpeedId"]; ok && ipSpeedIDVal != nil {
			response.WriteJSON(w, response.Err(-1, "普通用户无法设置每 IP 限速规则"))
			return
		}
	}
	ipSpeedID := asAnyToInt64Ptr(req["ipSpeedId"])
	ipSpeedID, err = h.normalizeSpeedLimitReference(ipSpeedID)
	if err != nil {
		response.WriteJSON(w, response.Err(-2, err.Error()))
		return
	}
```

Near `maxConn := asInt(req["maxConn"], 0)`, add:

```go
	ipMaxConn := asInt(req["ipMaxConn"], 0)
	if ipMaxConn < 0 {
		ipMaxConn = 0
	}
```

Update the `CreateForwardTx` call to pass `ipMaxConn` and `nullableInt(ipSpeedID)` before `proxyProtocol`.

- [x] **Step 4: Parse update fields**

In `forwardUpdate`, after the existing `speedId` block, add:

```go
	rawIPSpeedID, hasIPSpeedID := req["ipSpeedId"]
	requestedIPSpeedID := asAnyToInt64Ptr(rawIPSpeedID)
	if actorRole != 0 && hasIPSpeedID && requestedIPSpeedID != nil && !sameSpeedLimitSelection(forward.IPSpeedID, requestedIPSpeedID) {
		response.WriteJSON(w, response.Err(-1, "普通用户无法修改每 IP 限速规则"))
		return
	}
	ipSpeedID := requestedIPSpeedID
	ipSpeedID, err = h.normalizeSpeedLimitReference(ipSpeedID)
	if err != nil {
		response.WriteJSON(w, response.Err(-2, err.Error()))
		return
	}
	newIPSpeedID := forward.IPSpeedID
	if ipSpeedID != nil {
		newIPSpeedID = sql.NullInt64{Int64: *ipSpeedID, Valid: true}
	} else if _, ok := req["ipSpeedId"]; ok {
		newIPSpeedID = sql.NullInt64{Valid: false}
	}
```

Near `maxConn := asInt(req["maxConn"], forward.MaxConn)`, add:

```go
	ipMaxConn := asInt(req["ipMaxConn"], forward.IPMaxConn)
	if ipMaxConn < 0 {
		ipMaxConn = 0
	}
```

Update the `UpdateForward` call to pass `ipMaxConn` and `newIPSpeedID` before `proxyProtocol`.

- [x] **Step 5: Run permission test**

Run: `(cd go-backend && go test ./tests/contract -run TestForwardIPSpeedLimitPermission)`

Expected: PASS.

- [x] **Step 6: Commit**

Run:

```bash
git add go-backend/internal/http/handler/mutations.go go-backend/tests/contract/forward_contract_test.go
git commit -m "feat: expose per-IP forward limit fields"
```

---

### Task 3: Runtime Limiter Payloads

**Files:**

- Modify: `go-backend/internal/http/handler/control_plane.go`
- Modify: `go-backend/internal/http/handler/control_plane_test.go`
- Modify: `go-backend/tests/contract/max_conn_limit_contract_test.go`
- Create: `go-backend/tests/contract/per_ip_speed_limit_contract_test.go`

- [x] **Step 1: Add failing helper tests**

Append these tests to `control_plane_test.go`:

```go
func TestBuildConnLimiterConfigCombinesTotalAndPerIP(t *testing.T) {
	cfg := buildConnLimiterConfig(&forwardRecord{ID: 42, UserID: 9, MaxConn: 100, IPMaxConn: 5}, 37)
	want := forwardLimiterConfig{Name: "rule_conn_limit_42", Limits: []string{"$ 100", "$$ 5"}}
	if !reflect.DeepEqual(cfg, want) {
		t.Fatalf("expected %+v, got %+v", want, cfg)
	}
}

func TestBuildConnLimiterConfigUsesUserTotalWithRulePerIP(t *testing.T) {
	cfg := buildConnLimiterConfig(&forwardRecord{ID: 42, UserID: 9, IPMaxConn: 5}, 37)
	want := forwardLimiterConfig{Name: "rule_conn_limit_42", Limits: []string{"$ 37", "$$ 5"}}
	if !reflect.DeepEqual(cfg, want) {
		t.Fatalf("expected %+v, got %+v", want, cfg)
	}
}

func TestBuildTrafficLimiterPayloadCombinesTotalAndPerIP(t *testing.T) {
	payload := buildTrafficLimiterPayload("rule_traffic_limit_42", intPtr(80), intPtr(40))
	wantLimits := []string{"$ 10.0MB 10.0MB", "0.0.0.0/0 5.0MB 5.0MB", "::/0 5.0MB 5.0MB"}
	if payload["name"] != "rule_traffic_limit_42" {
		t.Fatalf("expected name rule_traffic_limit_42, got %v", payload["name"])
	}
	if !reflect.DeepEqual(payload["limits"], wantLimits) {
		t.Fatalf("expected limits %v, got %v", wantLimits, payload["limits"])
	}
}

func TestBuildForwardServiceConfigsUsesRuntimeLimiterNames(t *testing.T) {
	forward := &forwardRecord{RemoteAddr: "1.2.3.4:80", Strategy: "fifo", TunnelID: 7}
	node := &nodeRecord{TCPListenAddr: "0.0.0.0", UDPListenAddr: "[::]"}
	services := buildForwardServiceConfigs("1_2_0", forward, nil, node, 22001, "", forwardRuntimeLimiters{TrafficLimiter: "rule_traffic_limit_42", ConnLimiter: "rule_conn_limit_42"})
	if len(services) != 2 {
		t.Fatalf("expected 2 services, got %d", len(services))
	}
	for _, service := range services {
		if service["limiter"] != "rule_traffic_limit_42" {
			t.Fatalf("expected traffic limiter rule_traffic_limit_42, got %v", service["limiter"])
		}
		if service["climiter"] != "rule_conn_limit_42" {
			t.Fatalf("expected conn limiter rule_conn_limit_42, got %v", service["climiter"])
		}
	}
}

func intPtr(v int) *int { return &v }
```

- [x] **Step 2: Run helper tests and confirm failure**

Run: `(cd go-backend && go test ./internal/http/handler -run 'BuildConnLimiterConfig|BuildTrafficLimiterPayload|RuntimeLimiterNames')`

Expected: FAIL because helper types and signatures do not exist.

- [x] **Step 3: Add runtime limiter helper types and functions**

In `control_plane.go`, add near the existing type aliases:

```go
type forwardRuntimeLimiters struct {
	TrafficLimiter string
	ConnLimiter    string
}

type forwardLimiterConfig struct {
	Name   string
	Limits []string
}
```

Add these helper functions near `buildLimiterAddPayload`:

```go
func buildConnLimiterConfig(forward *forwardRecord, userMaxConn int) forwardLimiterConfig {
	if forward == nil {
		return forwardLimiterConfig{}
	}
	limits := make([]string, 0, 2)
	if forward.MaxConn > 0 {
		limits = append(limits, fmt.Sprintf("$ %d", forward.MaxConn))
	} else if userMaxConn > 0 {
		limits = append(limits, fmt.Sprintf("$ %d", userMaxConn))
	}
	if forward.IPMaxConn > 0 {
		limits = append(limits, fmt.Sprintf("$$ %d", forward.IPMaxConn))
	}
	if len(limits) == 0 {
		return forwardLimiterConfig{}
	}
	name := fmt.Sprintf("user_conn_limit_%d", forward.UserID)
	if forward.MaxConn > 0 || forward.IPMaxConn > 0 {
		name = fmt.Sprintf("rule_conn_limit_%d", forward.ID)
	}
	return forwardLimiterConfig{Name: name, Limits: limits}
}

func speedToLimitLine(key string, speed int) string {
	rate := float64(speed) / 8.0
	return fmt.Sprintf("%s %.1fMB %.1fMB", key, rate, rate)
}

func buildTrafficLimiterPayload(name string, totalSpeed *int, ipSpeed *int) map[string]interface{} {
	limits := make([]string, 0, 3)
	if totalSpeed != nil && *totalSpeed > 0 {
		limits = append(limits, speedToLimitLine("$", *totalSpeed))
	}
	if ipSpeed != nil && *ipSpeed > 0 {
		limits = append(limits, speedToLimitLine("0.0.0.0/0", *ipSpeed), speedToLimitLine("::/0", *ipSpeed))
	}
	return map[string]interface{}{"name": name, "limits": limits}
}
```

- [x] **Step 4: Update service config signature**

Change `buildForwardServiceConfigs` signature to:

```go
func buildForwardServiceConfigs(baseName string, forward *forwardRecord, tunnel *tunnelRecord, node *nodeRecord, port int, bindIP string, runtimeLimiters forwardRuntimeLimiters) []map[string]interface{} {
```

Replace limiter assignments inside the function with:

```go
		if runtimeLimiters.ConnLimiter != "" {
			service["climiter"] = runtimeLimiters.ConnLimiter
		}
		if runtimeLimiters.TrafficLimiter != "" {
			service["limiter"] = runtimeLimiters.TrafficLimiter
		}
```

Update existing tests and call sites that pass `limiterID, cLimiterName` to pass `forwardRuntimeLimiters{TrafficLimiter: existingTrafficName, ConnLimiter: existingConnName}`.

- [x] **Step 5: Update limiter upsert functions**

Replace `ensureConnLimiterOnNode` with:

```go
func (h *Handler) ensureConnLimiterOnNode(nodeID int64, cfg forwardLimiterConfig) error {
	if cfg.Name == "" || len(cfg.Limits) == 0 {
		return nil
	}
	payload := map[string]interface{}{"name": cfg.Name, "limits": cfg.Limits}
	if _, err := h.sendNodeCommand(nodeID, "AddCLimiters", payload, false, false); err != nil {
		if !isAlreadyExistsMessage(err.Error()) {
			return fmt.Errorf("连接限制器下发失败: %w", err)
		}
		updatePayload := map[string]interface{}{"limiter": cfg.Name, "data": payload}
		if _, updateErr := h.sendNodeCommand(nodeID, "UpdateCLimiters", updatePayload, false, false); updateErr != nil {
			return fmt.Errorf("连接限制器更新失败: %w", updateErr)
		}
	}
	return nil
}
```

Add a rule-level traffic limiter upsert:

```go
func (h *Handler) ensureTrafficLimiterOnNode(nodeID int64, name string, totalSpeed *int, ipSpeed *int) error {
	payload := buildTrafficLimiterPayload(name, totalSpeed, ipSpeed)
	limits, _ := payload["limits"].([]string)
	if name == "" || len(limits) == 0 {
		return nil
	}
	if _, err := h.sendNodeCommand(nodeID, "AddLimiters", payload, false, false); err != nil {
		if !isAlreadyExistsMessage(err.Error()) {
			return fmt.Errorf("限速规则下发失败: %w", err)
		}
		if _, updateErr := h.sendNodeCommand(nodeID, "UpdateLimiters", buildLimiterUpdatePayload(name, payload)); updateErr != nil {
			return fmt.Errorf("限速规则更新失败: %w", updateErr)
		}
	}
	return nil
}
```

Keep `ensureLimiterOnNode` and `upsertLimiterOnNode` for the existing total-only path.

- [x] **Step 6: Update `syncForwardServicesWithWarnings`**

Inside `syncForwardServicesWithWarnings`, resolve total speed as before. Add per-IP speed resolution:

```go
	var ipSpeed *int
	if forward.IPSpeedID.Valid && forward.IPSpeedID.Int64 > 0 {
		if speedVal, err := h.repo.GetSpeedLimitSpeed(forward.IPSpeedID.Int64); err == nil && speedVal > 0 {
			ipSpeed = &speedVal
		}
	}
```

After loading the user, build connection config once:

```go
	userMaxConn := 0
	if user != nil && user.MaxConn > 0 {
		userMaxConn = user.MaxConn
	}
	connLimiterConfig := buildConnLimiterConfig(forward, userMaxConn)
```

In the per-port loop, set `runtimeLimiters`:

```go
		runtimeLimiters := forwardRuntimeLimiters{ConnLimiter: connLimiterConfig.Name}
		if ipSpeed != nil {
			runtimeLimiters.TrafficLimiter = fmt.Sprintf("rule_traffic_limit_%d", forward.ID)
			if err := h.ensureTrafficLimiterOnNode(fp.NodeID, runtimeLimiters.TrafficLimiter, speed, ipSpeed); err != nil {
				if isNodeOfflineOrTimeoutError(err) {
					node, _ := h.getNodeRecord(fp.NodeID)
					nodeName := fmt.Sprintf("%d", fp.NodeID)
					if node != nil && strings.TrimSpace(node.Name) != "" {
						nodeName = strings.TrimSpace(node.Name)
					}
					warnings = append(warnings, fmt.Sprintf("节点 %s 不在线，已跳过下发", nodeName))
					continue
				}
				return nil, err
			}
		} else if limiterID != nil && speed != nil {
			runtimeLimiters.TrafficLimiter = strconv.FormatInt(*limiterID, 10)
			if err := h.ensureLimiterOnNode(fp.NodeID, *limiterID, *speed); err != nil {
				if isNodeOfflineOrTimeoutError(err) {
					node, _ := h.getNodeRecord(fp.NodeID)
					nodeName := fmt.Sprintf("%d", fp.NodeID)
					if node != nil && strings.TrimSpace(node.Name) != "" {
						nodeName = strings.TrimSpace(node.Name)
					}
					warnings = append(warnings, fmt.Sprintf("节点 %s 不在线，已跳过下发", nodeName))
					continue
				}
				return nil, err
			}
		}
		if connLimiterConfig.Name != "" {
			if err := h.ensureConnLimiterOnNode(fp.NodeID, connLimiterConfig); err != nil {
				warnings = append(warnings, fmt.Sprintf("节点 %d 连接限制器下发失败: %v", fp.NodeID, err))
			}
		}
```

Pass `runtimeLimiters` to every `buildForwardServiceConfigs` call in `syncForwardServicesWithWarnings`. Change `fallbackForwardPortToDefaultBind` to accept `runtimeLimiters forwardRuntimeLimiters` and pass it through to its `buildForwardServiceConfigs` call. Keep `rebindForwardServiceOnSelfOccupiedPort` unchanged because it receives already-built service maps.

- [x] **Step 7: Run helper tests**

Run: `(cd go-backend && go test ./internal/http/handler -run 'BuildConnLimiterConfig|BuildTrafficLimiterPayload|RuntimeLimiterNames|BuildForwardServiceConfigs')`

Expected: PASS.

- [x] **Step 8: Add backend contract tests for payloads**

In `max_conn_limit_contract_test.go`, update `TestMaxConnLimit` create payload with `"ipMaxConn": 7` and update expected limiter limits to contain both `"$ 42"` and `"$$ 7"`.

Create `go-backend/tests/contract/per_ip_speed_limit_contract_test.go` with a mock-node contract that seeds two speed limits, creates a forward with `speedId` and `ipSpeedId`, captures `AddLimiters` and `UpdateService`, and asserts:

```go
expectedName := fmt.Sprintf("rule_traffic_limit_%d", forwardID)
expectedLimits := []string{"$ 10.0MB 10.0MB", "0.0.0.0/0 5.0MB 5.0MB", "::/0 5.0MB 5.0MB"}
```

For service assertions, unmarshal `UpdateService` into `[]map[string]interface{}` and require every service has `service["limiter"] == expectedName`.

- [x] **Step 9: Run contract tests**

Run: `(cd go-backend && go test ./tests/contract -run 'MaxConnLimit|PerIPSpeedLimit')`

Expected: PASS.

- [x] **Step 10: Commit**

Run:

```bash
git add go-backend/internal/http/handler/control_plane.go go-backend/internal/http/handler/control_plane_test.go go-backend/tests/contract/max_conn_limit_contract_test.go go-backend/tests/contract/per_ip_speed_limit_contract_test.go
git commit -m "feat: sync per-IP runtime limiters"
```

---

### Task 4: GOST Limiter And UDP Listener Coverage

**Files:**

- Create: `go-gost/x/limiter/conn/conn_test.go`
- Create: `go-gost/x/limiter/traffic/traffic_test.go`
- Create: `go-gost/x/listener/udp/listener_test.go`
- Modify: `go-gost/x/listener/udp/listener.go`

- [x] **Step 1: Add limiter unit tests**

Create `go-gost/x/limiter/conn/conn_test.go`:

```go
package conn

import "testing"

func TestIPLimitKeyCreatesIndependentLimiters(t *testing.T) {
	limiter := NewConnLimiter(LimitsOption("$$ 1"))
	first := limiter.Limiter("192.0.2.1")
	second := limiter.Limiter("192.0.2.2")
	if first == nil || second == nil {
		t.Fatalf("expected non-nil per-IP limiters")
	}
	if !first.Allow(1) {
		t.Fatalf("expected first IP first connection to be allowed")
	}
	if first.Allow(1) {
		t.Fatalf("expected first IP second connection to be rejected")
	}
	if !second.Allow(1) {
		t.Fatalf("expected second IP first connection to be allowed independently")
	}
}
```

Create `go-gost/x/limiter/traffic/traffic_test.go`:

```go
package traffic

import (
	"context"
	"testing"
)

func TestCIDRLimitCreatesIndependentClientLimiters(t *testing.T) {
	limiter := NewTrafficLimiter(LimitsOption("0.0.0.0/0 2B 2B"))
	first := limiter.In(context.Background(), "192.0.2.1:1000")
	second := limiter.In(context.Background(), "192.0.2.2:1000")
	if first == nil || second == nil {
		t.Fatalf("expected non-nil CIDR client limiters")
	}
	if first == second {
		t.Fatalf("expected different clients to receive independent limiter instances")
	}
	if first.Limit() != 2 || second.Limit() != 2 {
		t.Fatalf("expected both limits to be 2, got %d and %d", first.Limit(), second.Limit())
	}
}
```

- [x] **Step 2: Add UDP listener test**

Create `go-gost/x/listener/udp/listener_test.go`:

```go
package udp

import (
	"io"
	"net"
	"testing"
	"time"

	corelistener "github.com/go-gost/core/listener"
	corelogger "github.com/go-gost/core/logger"
	xconn "github.com/go-gost/x/limiter/conn"
	xlogger "github.com/go-gost/x/logger"
)

func TestAcceptAppliesConnLimiterAndReleasesOnClose(t *testing.T) {
	ln := NewListener(
		corelistener.AddrOption("127.0.0.1:0"),
		corelistener.ConnLimiterOption(xconn.NewConnLimiter(xconn.LimitsOption("$$ 1"))),
		corelistener.LoggerOption(xlogger.NewLogger(xlogger.OutputOption(io.Discard), xlogger.LevelOption(corelogger.ErrorLevel))),
	)
	if err := ln.Init(nil); err != nil {
		t.Fatalf("init listener: %v", err)
	}
	defer ln.Close()

	addr := ln.Addr().String()
	client, err := net.Dial("udp", addr)
	if err != nil {
		t.Fatalf("dial udp listener: %v", err)
	}
	defer client.Close()
	if _, err := client.Write([]byte("first")); err != nil {
		t.Fatalf("write first packet: %v", err)
	}
	first, err := acceptWithTimeout(t, ln, time.Second)
	if err != nil {
		t.Fatalf("accept first conn: %v", err)
	}

	blockedClient, err := net.Dial("udp", addr)
	if err != nil {
		t.Fatalf("dial blocked udp client: %v", err)
	}
	defer blockedClient.Close()
	if _, err := blockedClient.Write([]byte("blocked")); err != nil {
		t.Fatalf("write blocked packet: %v", err)
	}
	blocked, err := acceptWithTimeout(t, ln, time.Second)
	if err != nil {
		t.Fatalf("expected blocked same-IP pseudo-connection to be returned closed: %v", err)
	}
	buf := make([]byte, 16)
	if _, err := blocked.Read(buf); err == nil {
		_ = blocked.Close()
		t.Fatalf("expected blocked same-IP pseudo-connection to be closed")
	}
	_ = blocked.Close()
	_ = first.Close()

	reopenedClient, err := net.Dial("udp", addr)
	if err != nil {
		t.Fatalf("dial reopened udp client: %v", err)
	}
	defer reopenedClient.Close()
	if _, err := reopenedClient.Write([]byte("after-close")); err != nil {
		t.Fatalf("write after close packet: %v", err)
	}
	reopened, err := acceptWithTimeout(t, ln, time.Second)
	if err != nil {
		t.Fatalf("expected same client to be accepted after close: %v", err)
	}
	_ = reopened.Close()
}

func acceptWithTimeout(t *testing.T, ln corelistener.Listener, timeout time.Duration) (net.Conn, error) {
	t.Helper()
	type result struct {
		conn net.Conn
		err  error
	}
	ch := make(chan result, 1)
	go func() {
		conn, err := ln.Accept()
		ch <- result{conn: conn, err: err}
	}()
	select {
	case res := <-ch:
		return res.conn, res.err
	case <-time.After(timeout):
		return nil, net.ErrClosed
	}
}
```

- [x] **Step 3: Run GOST tests and confirm UDP failure**

Run: `(cd go-gost/x && go test ./limiter/conn ./limiter/traffic ./listener/udp)`

Expected: limiter tests PASS; UDP listener test FAIL because `Accept()` does not apply `ConnLimiter` yet.

- [x] **Step 4: Patch UDP listener**

In `go-gost/x/listener/udp/listener.go`, add import:

```go
	climiter "github.com/go-gost/x/limiter/conn/wrapper"
```

In `Init`, replace the direct assignment to `l.ln` with a local `ln`, then wrap it with the connection limiter:

```go
	ln := udp.NewListener(conn, &udp.ListenConfig{
		Backlog:        l.md.backlog,
		ReadQueueSize:  l.md.readQueueSize,
		ReadBufferSize: l.md.readBufferSize,
		Keepalive:      l.md.keepalive,
		TTL:            l.md.ttl,
		Logger:         l.logger,
	})
	ln = climiter.WrapListener(l.options.ConnLimiter, ln)
	l.ln = ln
```

Replace `Accept` with:

```go
func (l *udpListener) Accept() (conn net.Conn, err error) {
	conn, err = l.ln.Accept()
	if err != nil {
		return
	}
	conn = limiter_wrapper.WrapConn(
		conn,
		l.options.TrafficLimiter,
		conn.RemoteAddr().String(),
		limiter.ScopeOption(limiter.ScopeConn),
		limiter.ServiceOption(l.options.Service),
		limiter.NetworkOption(conn.LocalAddr().Network()),
		limiter.SrcOption(conn.RemoteAddr().String()),
	)
	return
}
```

- [x] **Step 5: Run GOST tests**

Run: `(cd go-gost/x && go test ./limiter/conn ./limiter/traffic ./listener/udp)`

Expected: PASS.

- [x] **Step 6: Commit**

Run:

```bash
git add go-gost/x/limiter/conn/conn_test.go go-gost/x/limiter/traffic/traffic_test.go go-gost/x/listener/udp/listener.go go-gost/x/listener/udp/listener_test.go
git commit -m "fix: apply per-client limits to udp listener"
```

---

### Task 5: Frontend Rule Form

**Files:**

- Modify: `vite-frontend/src/api/types.ts`
- Modify: `vite-frontend/src/pages/forward.tsx`

- [x] **Step 1: Update API types**

In `ForwardApiItem`, add:

```ts
  ipMaxConn?: number;
  ipSpeedId?: number | null;
  ipSpeedLimitName?: string;
```

In `ForwardMutationPayload`, add:

```ts
  ipMaxConn?: number;
  ipSpeedId?: number | null;
```

- [x] **Step 2: Update page-local types and mapper**

In `Forward`, add:

```ts
  ipMaxConn?: number;
  ipSpeedId?: number | null;
  ipSpeedLimitName?: string;
```

In `ForwardForm`, add:

```ts
  ipMaxConn?: number;
  ipSpeedId: number | null;
```

In `mapForwardApiItems`, add:

```ts
    ipMaxConn:
      typeof forward.ipMaxConn === "number" ? forward.ipMaxConn : undefined,
    ipSpeedId:
      typeof forward.ipSpeedId === "number" || forward.ipSpeedId === null
        ? forward.ipSpeedId
        : undefined,
    ipSpeedLimitName:
      typeof forward.ipSpeedLimitName === "string"
        ? forward.ipSpeedLimitName
        : undefined,
```

- [x] **Step 3: Update form state and submit payloads**

In the default create form state, add:

```ts
      ipMaxConn: 0,
      ipSpeedId: null,
```

In `handleEdit`, add:

```ts
      ipMaxConn: forward.ipMaxConn ?? 0,
      ipSpeedId: normalizeSpeedId(forward.ipSpeedId),
```

In `handleSubmit`, add:

```ts
      const normalizedIPSpeedId = normalizeSpeedId(form.ipSpeedId);
      const ipSpeedLimitAutoCleared = isMissingSpeedLimit(form.ipSpeedId);
```

Add these fields to both `updateData` and `createData`:

```ts
          ipMaxConn: form.ipMaxConn,
          ipSpeedId: normalizedIPSpeedId,
```

After the existing speed-limit auto-cleared toast, add:

```ts
        if (ipSpeedLimitAutoCleared) {
          toast("所选每 IP 限速规则不存在，已自动清除为不限速", {
            icon: "⚠️",
            duration: 5000,
          });
        }
```

- [x] **Step 4: Add selected key and controls**

Near `selectedSpeedId`, add:

```ts
  const selectedIPSpeedId = normalizeSpeedId(form.ipSpeedId);
```

In the advanced settings area after `最大连接数`, add:

```tsx
                        <Input
                          description="每个客户端 IP 可同时建立的最大连接数；0 或空表示不限制。"
                          label="每 IP 最大连接数"
                          min="0"
                          placeholder="0 或空表示不限制"
                          type="number"
                          value={
                            form.ipMaxConn === 0
                              ? ""
                              : String(form.ipMaxConn || "")
                          }
                          variant="bordered"
                          onChange={(e) => {
                            const value = Math.max(
                              Number(e.target.value) || 0,
                              0,
                            );

                            setForm((prev) => ({ ...prev, ipMaxConn: value }));
                          }}
                        />
```

In the admin-only speed section after `规则限速`, add:

```tsx
                          <Select
                            description="每个客户端 IP 独享该限速规则；不选择表示不限制。"
                            label="每 IP 限速"
                            placeholder="不限速"
                            selectedKeys={
                              selectedIPSpeedId !== null
                                ? [selectedIPSpeedId.toString()]
                                : []
                            }
                            variant="bordered"
                            onSelectionChange={(keys) => {
                              const selectedKey = Array.from(keys)[0] as
                                | string
                                | undefined;

                              setForm((prev) => ({
                                ...prev,
                                ipSpeedId: selectedKey
                                  ? Number(selectedKey)
                                  : null,
                              }));
                            }}
                          >
                            {availableSpeedLimits.map((speedLimit) => (
                              <SelectItem
                                key={speedLimit.id.toString()}
                                textValue={speedLimit.name}
                              >
                                {speedLimit.name}
                              </SelectItem>
                            ))}
                          </Select>
```

- [x] **Step 5: Build frontend**

Run: `(cd vite-frontend && pnpm run build)`

Expected: PASS.

- [x] **Step 6: Commit**

Run:

```bash
git add vite-frontend/src/api/types.ts vite-frontend/src/pages/forward.tsx
git commit -m "feat: add per-IP limit controls"
```

---

### Task 6: Final Verification

**Files:**

- Verify only

- [x] **Step 1: Run backend tests**

Run: `(cd go-backend && go test ./...)`

Expected: PASS.

- [x] **Step 2: Run GOST/x focused tests**

Run: `(cd go-gost/x && go test ./limiter/conn ./limiter/traffic ./listener/udp)`

Expected: PASS.

- [x] **Step 3: Run frontend build**

Run: `(cd vite-frontend && pnpm run build)`

Expected: PASS.

- [x] **Step 4: Commit plan updates if this file changed during execution**

Run:

```bash
git add docs/superpowers/plans/2026-04-27-per-ip-rule-limits.md
git commit -m "docs: add per-IP rule limits plan"
```

Only run this commit if the plan file is still uncommitted at the end of execution.
