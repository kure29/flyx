# nftables Forwarding Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add an `nftables` node forwarding mode that lets FLVX manage pure DNAT/SNAT forwarding rules over SSH without installing the GOST agent.

**Architecture:** Keep the existing node/tunnel/forward business model, but split runtime execution by node `forward_mode`: `agent` continues using WebSocket/GOST commands, while `nftables` uses a focused backend runtime package that plans rules from database state, renders a complete FLVX-owned nftables table, and applies it over SSH. The first implementation deliberately supports only single-target pure port forwarding and rejects unsupported GOST features at the API boundary.

**Tech Stack:** Go `net/http`, GORM, SQLite/PostgreSQL, `golang.org/x/crypto/ssh`, nftables CLI over SSH, React/TypeScript, Vite, existing shadcn bridge components.

---

## File Structure

- Modify `go-backend/internal/store/model/model.go`: add node forwarding mode, SSH config, nft binding models, and record fields.
- Modify `go-backend/internal/store/repo/repository.go`: migrate new tables, include node mode in list output, add mode/config/binding repository methods if a narrower file is not used.
- Modify `go-backend/internal/store/repo/repository_mutations.go`: persist node mode and SSH config on create/update.
- Create `go-backend/internal/store/repo/repository_nftables.go`: focused repository methods for SSH configs and nft rule bindings.
- Create `go-backend/internal/store/repo/repository_nftables_test.go`: repository tests for mode/config/binding persistence.
- Create `go-backend/internal/runtime/nftables/types.go`: runtime constants, request/plan/result types.
- Create `go-backend/internal/runtime/nftables/parser.go`: strict `host:port` target parsing.
- Create `go-backend/internal/runtime/nftables/renderer.go`: render complete `table inet flvx` scripts.
- Create `go-backend/internal/runtime/nftables/runner.go`: SSH runner and command abstraction.
- Create `go-backend/internal/runtime/nftables/manager.go`: Reconcile/Test/Clear orchestration.
- Create `go-backend/internal/runtime/nftables/*_test.go`: parser, renderer, manager tests with fake runner.
- Modify `go-backend/internal/http/handler/handler.go`: initialize nftables manager and register nftables maintenance routes.
- Create `go-backend/internal/http/handler/nftables_runtime.go`: handler helpers for capability checks and runtime sync.
- Modify `go-backend/internal/http/handler/mutations.go`: enforce nftables restrictions in node/tunnel/forward create/update/delete/batch redeploy flows.
- Create `go-backend/internal/http/handler/nftables_runtime_test.go`: API-level validation and rollback tests.
- Modify `vite-frontend/src/api/index.ts`: add nftables node operations.
- Modify `vite-frontend/src/api/types.ts`: add `forwardMode` and `sshConfig` types.
- Modify `vite-frontend/src/pages/node.tsx`: add forwarding mode fields, SSH form section, and hide agent-only operations.
- Modify `vite-frontend/src/pages/tunnel.tsx` and `vite-frontend/src/pages/tunnel/form.ts`: prevent nftables tunnel forwarding/chain configuration.
- Modify `vite-frontend/src/pages/forward.tsx`: hide unsupported controls and enforce single-target nftables rules.

Implementation should commit only when explicitly requested. If commits are requested later, commit after each task.

---

### Task 1: Data Model And Repository

**Files:**
- Modify: `go-backend/internal/store/model/model.go`
- Modify: `go-backend/internal/store/repo/repository.go`
- Modify: `go-backend/internal/store/repo/repository_mutations.go`
- Create: `go-backend/internal/store/repo/repository_nftables.go`
- Create: `go-backend/internal/store/repo/repository_nftables_test.go`

- [ ] **Step 1: Write failing repository tests**

Create `go-backend/internal/store/repo/repository_nftables_test.go`:

```go
package repo

import (
	"strings"
	"testing"
	"time"
)

func TestNftablesNodeModeSSHConfigAndBindingPersistence(t *testing.T) {
	r, err := Open(":memory:")
	if err != nil {
		t.Fatalf("open repo: %v", err)
	}
	defer r.Close()

	now := time.Now().UnixMilli()
	if err := r.CreateNode(
		"nft-node",
		"secret",
		"203.0.113.10",
		nil,
		nil,
		"10000-20000",
		nil,
		nil,
		nil,
		nil,
		nil,
		0,
		0,
		0,
		now,
		1,
		"[::]",
		"[::]",
		1,
		0,
		nil,
		nil,
		nil,
		nil,
		"nftables",
	); err != nil {
		t.Fatalf("CreateNode: %v", err)
	}

	nodes, err := r.ListNodes()
	if err != nil {
		t.Fatalf("ListNodes: %v", err)
	}
	if len(nodes) != 1 {
		t.Fatalf("expected 1 node, got %d", len(nodes))
	}
	nodeID := nodes[0]["id"].(int64)
	if got := nodes[0]["forwardMode"]; got != "nftables" {
		t.Fatalf("expected forwardMode nftables, got %#v", got)
	}

	cfg := NftSSHConfigInput{
		Host:       "203.0.113.10",
		Port:       22,
		Username:   "root",
		AuthType:   "private_key",
		PrivateKey: "encrypted-private-key",
		SudoMode:   "none",
	}
	if err := r.UpsertNodeSSHConfig(nodeID, cfg, now); err != nil {
		t.Fatalf("UpsertNodeSSHConfig: %v", err)
	}
	loaded, err := r.GetNodeSSHConfig(nodeID)
	if err != nil {
		t.Fatalf("GetNodeSSHConfig: %v", err)
	}
	if loaded.Host != cfg.Host || loaded.Port != cfg.Port || loaded.Username != cfg.Username || loaded.AuthType != cfg.AuthType {
		t.Fatalf("unexpected ssh config: %+v", loaded)
	}

	binding := NftRuleBindingInput{
		ForwardID:  42,
		NodeID:     nodeID,
		InPort:    24000,
		Protocols: "tcp,udp",
		TargetAddr: "198.51.100.20:443",
		BindIP:     "",
		RuleHash:   "hash-a",
		Status:     "applied",
		LastError:  "",
	}
	if err := r.UpsertNftRuleBinding(binding, now); err != nil {
		t.Fatalf("UpsertNftRuleBinding: %v", err)
	}
	bindings, err := r.ListNftRuleBindingsByNode(nodeID)
	if err != nil {
		t.Fatalf("ListNftRuleBindingsByNode: %v", err)
	}
	if len(bindings) != 1 {
		t.Fatalf("expected 1 binding, got %d", len(bindings))
	}
	if bindings[0].ForwardID != 42 || bindings[0].RuleHash != "hash-a" || bindings[0].Status != "applied" {
		t.Fatalf("unexpected binding: %+v", bindings[0])
	}

	if err := r.MarkNftRuleBindingError(42, nodeID, "nft failed", now+1); err != nil {
		t.Fatalf("MarkNftRuleBindingError: %v", err)
	}
	bindings, err = r.ListNftRuleBindingsByNode(nodeID)
	if err != nil {
		t.Fatalf("ListNftRuleBindingsByNode after error: %v", err)
	}
	if bindings[0].Status != "error" || !strings.Contains(bindings[0].LastError, "nft failed") {
		t.Fatalf("expected error binding, got %+v", bindings[0])
	}

	if err := r.DeleteNftRuleBindingsByForward(42); err != nil {
		t.Fatalf("DeleteNftRuleBindingsByForward: %v", err)
	}
	bindings, err = r.ListNftRuleBindingsByNode(nodeID)
	if err != nil {
		t.Fatalf("ListNftRuleBindingsByNode after delete: %v", err)
	}
	if len(bindings) != 0 {
		t.Fatalf("expected no bindings after delete, got %+v", bindings)
	}
}
```

- [ ] **Step 2: Run repository test and verify failure**

Run:

```bash
(cd go-backend && go test ./internal/store/repo -run TestNftablesNodeModeSSHConfigAndBindingPersistence -count=1)
```

Expected: FAIL with undefined `NftSSHConfigInput`, `UpsertNodeSSHConfig`, `GetNodeSSHConfig`, `NftRuleBindingInput`, `UpsertNftRuleBinding`, `ListNftRuleBindingsByNode`, `MarkNftRuleBindingError`, `DeleteNftRuleBindingsByForward`, and the old `CreateNode` signature.

- [ ] **Step 3: Add models and record fields**

In `go-backend/internal/store/model/model.go`, add `ForwardMode` to `Node` after `IsRemote`:

```go
	ForwardMode string `gorm:"column:forward_mode;type:varchar(20);not null;default:'agent'"`
```

Add new GORM models after `Node`:

```go
type NodeSSHConfig struct {
	ID          int64          `gorm:"primaryKey;autoIncrement"`
	NodeID      int64          `gorm:"column:node_id;not null;uniqueIndex"`
	Host        string         `gorm:"type:varchar(255);not null"`
	Port        int            `gorm:"not null;default:22"`
	Username    string         `gorm:"type:varchar(100);not null"`
	AuthType    string         `gorm:"column:auth_type;type:varchar(20);not null"`
	Password    sql.NullString `gorm:"type:text"`
	PrivateKey  sql.NullString `gorm:"column:private_key;type:text"`
	Passphrase  sql.NullString `gorm:"type:text"`
	SudoMode    string         `gorm:"column:sudo_mode;type:varchar(20);not null;default:'none'"`
	CreatedTime int64          `gorm:"column:created_time;not null"`
	UpdatedTime int64          `gorm:"column:updated_time;not null"`
}

func (NodeSSHConfig) TableName() string { return "node_ssh_config" }

type NftRuleBinding struct {
	ID          int64  `gorm:"primaryKey;autoIncrement"`
	ForwardID   int64  `gorm:"column:forward_id;not null;uniqueIndex:idx_nft_rule_binding_forward_node;index"`
	NodeID      int64  `gorm:"column:node_id;not null;uniqueIndex:idx_nft_rule_binding_forward_node;index"`
	InPort     int    `gorm:"column:in_port;not null"`
	Protocols  string `gorm:"type:varchar(20);not null;default:'tcp,udp'"`
	TargetAddr string `gorm:"column:target_addr;type:text;not null"`
	BindIP     string `gorm:"column:bind_ip;type:text;not null;default:''"`
	RuleHash   string `gorm:"column:rule_hash;type:varchar(128);not null;default:''"`
	Status     string `gorm:"type:varchar(20);not null;default:'pending'"`
	LastError  string `gorm:"column:last_error;type:text;not null;default:''"`
	AppliedTime int64 `gorm:"column:applied_time;not null;default:0"`
	CreatedTime int64 `gorm:"column:created_time;not null"`
	UpdatedTime int64 `gorm:"column:updated_time;not null"`
}

func (NftRuleBinding) TableName() string { return "nft_rule_binding" }
```

Add `ForwardMode string` to `NodeRecord` in `model.go`.

- [ ] **Step 4: Auto-migrate new tables**

In `go-backend/internal/store/repo/repository.go`, add the new models to `autoMigrateAll` immediately after `&model.Node{}`:

```go
		&model.NodeSSHConfig{},
		&model.NftRuleBinding{},
```

Because SQLite startup currently skips `Node` AutoMigrate when a legacy node table exists, add a legacy column preparation helper that runs before the model loop:

```go
	if db.Dialector.Name() == "sqlite" {
		if err := prepareSQLiteNftablesColumns(db); err != nil {
			return err
		}
	}
```

Add this helper near the existing SQLite legacy helpers:

```go
func prepareSQLiteNftablesColumns(db *gorm.DB) error {
	if db == nil || db.Dialector.Name() != "sqlite" {
		return nil
	}
	if !db.Migrator().HasTable(&model.Node{}) {
		return nil
	}
	if !db.Migrator().HasColumn(&model.Node{}, "forward_mode") {
		if err := db.Exec("ALTER TABLE node ADD COLUMN forward_mode varchar(20) NOT NULL DEFAULT 'agent'").Error; err != nil {
			return err
		}
	}
	return nil
}
```

- [ ] **Step 5: Persist node mode and expose it in list output**

Change `CreateNode` in `repository_mutations.go` to accept `forwardMode string` after `extraIPs interface{}` and set:

```go
		ForwardMode: defaultNodeForwardMode(forwardMode),
```

Change `UpdateNode` to accept `forwardMode string` after `renewalCycle interface{}` and add:

```go
			"forward_mode":              defaultNodeForwardMode(forwardMode),
```

Add this helper in `repository_mutations.go`:

```go
func defaultNodeForwardMode(mode string) string {
	switch strings.TrimSpace(strings.ToLower(mode)) {
	case "nftables":
		return "nftables"
	default:
		return "agent"
	}
}
```

If `repository_mutations.go` does not already import `strings`, add it.

In `ListNodes`, include:

```go
			"forwardMode": defaultNodeForwardMode(n.ForwardMode),
```

- [ ] **Step 6: Add nftables repository methods**

Create `go-backend/internal/store/repo/repository_nftables.go`:

```go
package repo

import (
	"database/sql"
	"errors"
	"strings"

	"go-backend/internal/store/model"

	"gorm.io/gorm"
)

type NftSSHConfigInput struct {
	Host       string
	Port       int
	Username   string
	AuthType   string
	Password   string
	PrivateKey string
	Passphrase string
	SudoMode   string
}

type NftRuleBindingInput struct {
	ForwardID  int64
	NodeID     int64
	InPort     int
	Protocols  string
	TargetAddr string
	BindIP     string
	RuleHash   string
	Status     string
	LastError  string
}

func (r *Repository) UpsertNodeSSHConfig(nodeID int64, cfg NftSSHConfigInput, now int64) error {
	if r == nil || r.db == nil {
		return errors.New("repository not initialized")
	}
	if nodeID <= 0 {
		return errors.New("node id is required")
	}
	port := cfg.Port
	if port <= 0 {
		port = 22
	}
	authType := strings.TrimSpace(strings.ToLower(cfg.AuthType))
	if authType == "" {
		authType = "private_key"
	}
	sudoMode := strings.TrimSpace(strings.ToLower(cfg.SudoMode))
	if sudoMode == "" {
		sudoMode = "none"
	}
	var existing model.NodeSSHConfig
	err := r.db.Where("node_id = ?", nodeID).First(&existing).Error
	row := model.NodeSSHConfig{
		NodeID:      nodeID,
		Host:        strings.TrimSpace(cfg.Host),
		Port:        port,
		Username:    strings.TrimSpace(cfg.Username),
		AuthType:    authType,
		Password:    nullStringFromInterface(cfg.Password),
		PrivateKey:  nullStringFromInterface(cfg.PrivateKey),
		Passphrase:  nullStringFromInterface(cfg.Passphrase),
		SudoMode:    sudoMode,
		CreatedTime: now,
		UpdatedTime: now,
	}
	if err == nil {
		row.ID = existing.ID
		row.CreatedTime = existing.CreatedTime
		return r.db.Save(&row).Error
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return r.db.Create(&row).Error
	}
	return err
}

func (r *Repository) GetNodeSSHConfig(nodeID int64) (*model.NodeSSHConfig, error) {
	if r == nil || r.db == nil {
		return nil, errors.New("repository not initialized")
	}
	var cfg model.NodeSSHConfig
	if err := r.db.Where("node_id = ?", nodeID).First(&cfg).Error; err != nil {
		return nil, normalizeNotFoundErr(err)
	}
	return &cfg, nil
}

func (r *Repository) DeleteNodeSSHConfig(nodeID int64) error {
	if r == nil || r.db == nil {
		return errors.New("repository not initialized")
	}
	return r.db.Where("node_id = ?", nodeID).Delete(&model.NodeSSHConfig{}).Error
}

func (r *Repository) UpsertNftRuleBinding(input NftRuleBindingInput, now int64) error {
	if r == nil || r.db == nil {
		return errors.New("repository not initialized")
	}
	var existing model.NftRuleBinding
	err := r.db.Where("forward_id = ? AND node_id = ?", input.ForwardID, input.NodeID).First(&existing).Error
	row := model.NftRuleBinding{
		ForwardID:   input.ForwardID,
		NodeID:      input.NodeID,
		InPort:      input.InPort,
		Protocols:   defaultString(strings.TrimSpace(input.Protocols), "tcp,udp"),
		TargetAddr:  strings.TrimSpace(input.TargetAddr),
		BindIP:      strings.TrimSpace(input.BindIP),
		RuleHash:    strings.TrimSpace(input.RuleHash),
		Status:      defaultString(strings.TrimSpace(input.Status), "pending"),
		LastError:   strings.TrimSpace(input.LastError),
		AppliedTime: now,
		CreatedTime: now,
		UpdatedTime: now,
	}
	if err == nil {
		row.ID = existing.ID
		row.CreatedTime = existing.CreatedTime
		return r.db.Save(&row).Error
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return r.db.Create(&row).Error
	}
	return err
}

func (r *Repository) MarkNftRuleBindingError(forwardID, nodeID int64, message string, now int64) error {
	if r == nil || r.db == nil {
		return errors.New("repository not initialized")
	}
	return r.db.Model(&model.NftRuleBinding{}).
		Where("forward_id = ? AND node_id = ?", forwardID, nodeID).
		Updates(map[string]interface{}{
			"status":       "error",
			"last_error":   strings.TrimSpace(message),
			"updated_time": now,
		}).Error
}

func (r *Repository) ListNftRuleBindingsByNode(nodeID int64) ([]model.NftRuleBinding, error) {
	if r == nil || r.db == nil {
		return nil, errors.New("repository not initialized")
	}
	var rows []model.NftRuleBinding
	err := r.db.Where("node_id = ?", nodeID).Order("forward_id ASC").Find(&rows).Error
	return rows, err
}

func (r *Repository) DeleteNftRuleBindingsByForward(forwardID int64) error {
	if r == nil || r.db == nil {
		return errors.New("repository not initialized")
	}
	return r.db.Where("forward_id = ?", forwardID).Delete(&model.NftRuleBinding{}).Error
}

func (r *Repository) GetNodeForwardMode(nodeID int64) (string, error) {
	if r == nil || r.db == nil {
		return "", errors.New("repository not initialized")
	}
	var row struct {
		ForwardMode sql.NullString `gorm:"column:forward_mode"`
	}
	err := r.db.Model(&model.Node{}).Select("forward_mode").Where("id = ?", nodeID).First(&row).Error
	if err != nil {
		return "", normalizeNotFoundErr(err)
	}
	return defaultNodeForwardMode(row.ForwardMode.String), nil
}
```

- [ ] **Step 7: Update call sites for CreateNode and UpdateNode**

In `go-backend/internal/http/handler/mutations.go`, update `h.repo.CreateNode(...)` to pass:

```go
		nullableText(asString(req["extraIPs"])),
		defaultNodeForwardMode(asString(req["forwardMode"])),
```

Update `h.repo.UpdateNode(...)` to pass `defaultNodeForwardMode(asString(req["forwardMode"]))` after `renewalCycle`.

Add this handler helper in `mutations.go` near other helpers:

```go
func defaultNodeForwardMode(mode string) string {
	switch strings.TrimSpace(strings.ToLower(mode)) {
	case "nftables":
		return "nftables"
	default:
		return "agent"
	}
}
```

- [ ] **Step 8: Run repository tests**

Run:

```bash
(cd go-backend && go test ./internal/store/repo -run TestNftablesNodeModeSSHConfigAndBindingPersistence -count=1)
```

Expected: PASS.

Run:

```bash
(cd go-backend && go test ./internal/store/repo -count=1)
```

Expected: PASS.

---

### Task 2: nftables Parser And Renderer

**Files:**
- Create: `go-backend/internal/runtime/nftables/types.go`
- Create: `go-backend/internal/runtime/nftables/parser.go`
- Create: `go-backend/internal/runtime/nftables/renderer.go`
- Create: `go-backend/internal/runtime/nftables/parser_test.go`
- Create: `go-backend/internal/runtime/nftables/renderer_test.go`

- [ ] **Step 1: Write failing parser tests**

Create `go-backend/internal/runtime/nftables/parser_test.go`:

```go
package nftables

import "testing"

func TestParseSingleTargetAcceptsHostPortAndIPv6(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		host string
		port int
	}{
		{name: "hostname", raw: "example.com:443", host: "example.com", port: 443},
		{name: "ipv4", raw: "198.51.100.20:8443", host: "198.51.100.20", port: 8443},
		{name: "ipv6", raw: "[2001:db8::1]:443", host: "2001:db8::1", port: 443},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			target, err := ParseSingleTarget(tt.raw)
			if err != nil {
				t.Fatalf("ParseSingleTarget: %v", err)
			}
			if target.Host != tt.host || target.Port != tt.port {
				t.Fatalf("expected %s/%d, got %+v", tt.host, tt.port, target)
			}
		})
	}
}

func TestParseSingleTargetRejectsUnsupportedValues(t *testing.T) {
	for _, raw := range []string{"", "example.com", "example.com:0", "example.com:65536", "a:1,b:2", "http://example.com:443"} {
		t.Run(raw, func(t *testing.T) {
			if _, err := ParseSingleTarget(raw); err == nil {
				t.Fatalf("expected error for %q", raw)
			}
		})
	}
}
```

- [ ] **Step 2: Write failing renderer tests**

Create `go-backend/internal/runtime/nftables/renderer_test.go`:

```go
package nftables

import (
	"strings"
	"testing"
)

func TestRenderTableIncludesDNATAndMasquerade(t *testing.T) {
	script := RenderTable(NodePlan{
		NodeID: 10,
		Rules: []Rule{
			{
				ForwardID:  42,
				InPort:     24000,
				TargetHost: "198.51.100.20",
				TargetPort: 443,
				Protocols:  []string{"tcp", "udp"},
			},
		},
	})

	expectedParts := []string{
		"table inet flvx",
		"type nat hook prerouting priority dstnat; policy accept;",
		"type nat hook postrouting priority srcnat; policy accept;",
		"tcp dport 24000 dnat to 198.51.100.20:443 comment \"flvx forward:42 tcp\"",
		"udp dport 24000 dnat to 198.51.100.20:443 comment \"flvx forward:42 udp\"",
		"masquerade comment \"flvx masquerade\"",
	}
	for _, part := range expectedParts {
		if !strings.Contains(script, part) {
			t.Fatalf("script missing %q:\n%s", part, script)
		}
	}
}

func TestRenderTableBracketsIPv6Target(t *testing.T) {
	script := RenderTable(NodePlan{
		NodeID: 10,
		Rules: []Rule{
			{ForwardID: 42, InPort: 24000, TargetHost: "2001:db8::1", TargetPort: 443, Protocols: []string{"tcp"}},
		},
	})
	if !strings.Contains(script, "dnat to [2001:db8::1]:443") {
		t.Fatalf("expected bracketed IPv6 dnat target, got:\n%s", script)
	}
}

func TestRuleHashIsStable(t *testing.T) {
	rule := Rule{ForwardID: 42, InPort: 24000, TargetHost: "198.51.100.20", TargetPort: 443, Protocols: []string{"tcp", "udp"}}
	if RuleHash(rule) != RuleHash(rule) {
		t.Fatalf("expected stable rule hash")
	}
	if RuleHash(rule) == RuleHash(Rule{ForwardID: 42, InPort: 24001, TargetHost: "198.51.100.20", TargetPort: 443, Protocols: []string{"tcp", "udp"}}) {
		t.Fatalf("expected hash to change when port changes")
	}
}
```

- [ ] **Step 3: Run tests and verify failure**

Run:

```bash
(cd go-backend && go test ./internal/runtime/nftables -count=1)
```

Expected: FAIL because the package and functions do not exist.

- [ ] **Step 4: Implement runtime types**

Create `go-backend/internal/runtime/nftables/types.go`:

```go
package nftables

const (
	ModeAgent    = "agent"
	ModeNftables = "nftables"

	StatusPending = "pending"
	StatusApplied = "applied"
	StatusError   = "error"
)

type Target struct {
	Host string
	Port int
}

type Rule struct {
	ForwardID  int64
	InPort     int
	BindIP     string
	TargetHost string
	TargetPort int
	Protocols  []string
}

type NodePlan struct {
	NodeID int64
	Rules  []Rule
}

type SSHConfig struct {
	Host       string
	Port       int
	Username   string
	AuthType   string
	Password   string
	PrivateKey string
	Passphrase string
	SudoMode   string
}

type ApplyResult struct {
	NodeID int64
	Script string
	Hashes map[int64]string
}
```

- [ ] **Step 5: Implement parser**

Create `go-backend/internal/runtime/nftables/parser.go`:

```go
package nftables

import (
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"
)

func ParseSingleTarget(raw string) (Target, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return Target{}, fmt.Errorf("目标地址不能为空")
	}
	if strings.Contains(value, ",") || strings.Contains(value, "\n") {
		return Target{}, fmt.Errorf("nftables 纯转发第一阶段仅支持单目标")
	}
	if u, err := url.Parse(value); err == nil && u.Scheme != "" {
		return Target{}, fmt.Errorf("目标地址必须是 host:port，不能包含 URL scheme")
	}
	host, portText, err := net.SplitHostPort(value)
	if err != nil {
		return Target{}, fmt.Errorf("目标地址必须是 host:port")
	}
	host = strings.TrimSpace(strings.Trim(host, "[]"))
	if host == "" {
		return Target{}, fmt.Errorf("目标主机不能为空")
	}
	port, err := strconv.Atoi(portText)
	if err != nil || port < 1 || port > 65535 {
		return Target{}, fmt.Errorf("目标端口必须在 1-65535 之间")
	}
	return Target{Host: host, Port: port}, nil
}
```

- [ ] **Step 6: Implement renderer**

Create `go-backend/internal/runtime/nftables/renderer.go`:

```go
package nftables

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net"
	"sort"
	"strings"
)

func RenderTable(plan NodePlan) string {
	var b strings.Builder
	b.WriteString("table inet flvx {\n")
	b.WriteString("  chain prerouting {\n")
	b.WriteString("    type nat hook prerouting priority dstnat; policy accept;\n")
	for _, rule := range sortedRules(plan.Rules) {
		for _, protocol := range normalizedProtocols(rule.Protocols) {
			b.WriteString(fmt.Sprintf("    %s dport %d dnat to %s comment \"flvx forward:%d %s\"\n",
				protocol,
				rule.InPort,
				formatDNATTarget(rule.TargetHost, rule.TargetPort),
				rule.ForwardID,
				protocol,
			))
		}
	}
	b.WriteString("  }\n\n")
	b.WriteString("  chain postrouting {\n")
	b.WriteString("    type nat hook postrouting priority srcnat; policy accept;\n")
	if len(plan.Rules) > 0 {
		b.WriteString("    masquerade comment \"flvx masquerade\"\n")
	}
	b.WriteString("  }\n\n")
	b.WriteString("  chain forward {\n")
	b.WriteString("    type filter hook forward priority filter; policy accept;\n")
	b.WriteString("  }\n")
	b.WriteString("}\n")
	return b.String()
}

func RuleHash(rule Rule) string {
	protocols := normalizedProtocols(rule.Protocols)
	sum := sha256.Sum256([]byte(fmt.Sprintf("%d|%d|%s|%d|%s|%s",
		rule.ForwardID,
		rule.InPort,
		strings.TrimSpace(rule.TargetHost),
		rule.TargetPort,
		strings.TrimSpace(rule.BindIP),
		strings.Join(protocols, ","),
	)))
	return hex.EncodeToString(sum[:])
}

func PlanHashes(plan NodePlan) map[int64]string {
	hashes := make(map[int64]string, len(plan.Rules))
	for _, rule := range plan.Rules {
		hashes[rule.ForwardID] = RuleHash(rule)
	}
	return hashes
}

func sortedRules(rules []Rule) []Rule {
	out := append([]Rule(nil), rules...)
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].InPort == out[j].InPort {
			return out[i].ForwardID < out[j].ForwardID
		}
		return out[i].InPort < out[j].InPort
	})
	return out
}

func normalizedProtocols(protocols []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, 2)
	for _, protocol := range protocols {
		p := strings.ToLower(strings.TrimSpace(protocol))
		if p != "tcp" && p != "udp" {
			continue
		}
		if _, ok := seen[p]; ok {
			continue
		}
		seen[p] = struct{}{}
		out = append(out, p)
	}
	if len(out) == 0 {
		return []string{"tcp", "udp"}
	}
	sort.Strings(out)
	return out
}

func formatDNATTarget(host string, port int) string {
	trimmed := strings.Trim(strings.TrimSpace(host), "[]")
	if ip := net.ParseIP(trimmed); ip != nil && ip.To4() == nil {
		return fmt.Sprintf("[%s]:%d", trimmed, port)
	}
	return fmt.Sprintf("%s:%d", trimmed, port)
}
```

- [ ] **Step 7: Run parser and renderer tests**

Run:

```bash
(cd go-backend && go test ./internal/runtime/nftables -count=1)
```

Expected: PASS.

---

### Task 3: SSH Runner And nftables Manager

**Files:**
- Create: `go-backend/internal/runtime/nftables/runner.go`
- Create: `go-backend/internal/runtime/nftables/manager.go`
- Create: `go-backend/internal/runtime/nftables/manager_test.go`

- [ ] **Step 1: Write failing manager tests with a fake runner**

Create `go-backend/internal/runtime/nftables/manager_test.go`:

```go
package nftables

import (
	"context"
	"errors"
	"strings"
	"testing"
)

type fakeRunner struct {
	scripts []string
	err     error
}

func (f *fakeRunner) ApplyScript(ctx context.Context, cfg SSHConfig, script string) error {
	f.scripts = append(f.scripts, script)
	return f.err
}

func (f *fakeRunner) Test(ctx context.Context, cfg SSHConfig) error {
	return f.err
}

func TestManagerReconcileAppliesRenderedScript(t *testing.T) {
	runner := &fakeRunner{}
	manager := NewManager(runner)
	plan := NodePlan{
		NodeID: 7,
		Rules: []Rule{{ForwardID: 42, InPort: 24000, TargetHost: "198.51.100.20", TargetPort: 443, Protocols: []string{"tcp", "udp"}}},
	}
	result, err := manager.Reconcile(context.Background(), SSHConfig{Host: "203.0.113.10", Port: 22, Username: "root"}, plan)
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if len(runner.scripts) != 1 {
		t.Fatalf("expected 1 script, got %d", len(runner.scripts))
	}
	if !strings.Contains(runner.scripts[0], "flvx forward:42 tcp") {
		t.Fatalf("script missing forward comment:\n%s", runner.scripts[0])
	}
	if result.NodeID != 7 || result.Hashes[42] == "" {
		t.Fatalf("unexpected result: %+v", result)
	}
}

func TestManagerReconcileReturnsRunnerError(t *testing.T) {
	runner := &fakeRunner{err: errors.New("ssh failed")}
	manager := NewManager(runner)
	_, err := manager.Reconcile(context.Background(), SSHConfig{Host: "203.0.113.10", Port: 22, Username: "root"}, NodePlan{NodeID: 7})
	if err == nil || !strings.Contains(err.Error(), "ssh failed") {
		t.Fatalf("expected ssh failed error, got %v", err)
	}
}

func TestManagerClearAppliesEmptyTable(t *testing.T) {
	runner := &fakeRunner{}
	manager := NewManager(runner)
	if err := manager.Clear(context.Background(), SSHConfig{Host: "203.0.113.10", Port: 22, Username: "root"}); err != nil {
		t.Fatalf("Clear: %v", err)
	}
	if len(runner.scripts) != 1 {
		t.Fatalf("expected 1 script, got %d", len(runner.scripts))
	}
	if strings.Contains(runner.scripts[0], "masquerade comment") {
		t.Fatalf("empty table should not include masquerade:\n%s", runner.scripts[0])
	}
}
```

- [ ] **Step 2: Run manager tests and verify failure**

Run:

```bash
(cd go-backend && go test ./internal/runtime/nftables -run 'TestManager' -count=1)
```

Expected: FAIL with undefined `NewManager`.

- [ ] **Step 3: Implement runner interfaces and real SSH runner**

Create `go-backend/internal/runtime/nftables/runner.go`:

```go
package nftables

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"
)

type Runner interface {
	ApplyScript(ctx context.Context, cfg SSHConfig, script string) error
	Test(ctx context.Context, cfg SSHConfig) error
}

type SSHRunner struct {
	Timeout time.Duration
}

func NewSSHRunner() *SSHRunner {
	return &SSHRunner{Timeout: 15 * time.Second}
}

func (r *SSHRunner) Test(ctx context.Context, cfg SSHConfig) error {
	return r.run(ctx, cfg, "command -v nft >/dev/null 2>&1 && nft --version >/dev/null 2>&1")
}

func (r *SSHRunner) ApplyScript(ctx context.Context, cfg SSHConfig, script string) error {
	escaped := strings.ReplaceAll(script, "'", "'\"'\"'")
	command := "tmp=/tmp/flvx-nft-$(date +%s%N).nft; " +
		"cat > \"$tmp\" <<'EOF'\n" + escaped + "\nEOF\n" +
		nftCommand(cfg, "list table inet flvx >/dev/null 2>&1 && nft delete table inet flvx || true") + "; " +
		nftCommand(cfg, " -f \"$tmp\"") + "; " +
		"rm -f \"$tmp\""
	return r.run(ctx, cfg, command)
}

func (r *SSHRunner) run(ctx context.Context, cfg SSHConfig, command string) error {
	timeout := r.Timeout
	if timeout <= 0 {
		timeout = 15 * time.Second
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	clientConfig, err := buildSSHClientConfig(cfg)
	if err != nil {
		return err
	}
	addr := net.JoinHostPort(strings.TrimSpace(cfg.Host), fmt.Sprintf("%d", normalizedSSHPort(cfg.Port)))
	dialer := net.Dialer{Timeout: timeout}
	conn, err := dialer.DialContext(runCtx, "tcp", addr)
	if err != nil {
		return fmt.Errorf("SSH 连接失败: %w", err)
	}
	defer conn.Close()

	sshConn, chans, reqs, err := ssh.NewClientConn(conn, addr, clientConfig)
	if err != nil {
		return fmt.Errorf("SSH 认证失败: %w", err)
	}
	client := ssh.NewClient(sshConn, chans, reqs)
	defer client.Close()

	session, err := client.NewSession()
	if err != nil {
		return fmt.Errorf("SSH 会话创建失败: %w", err)
	}
	defer session.Close()

	var stderr bytes.Buffer
	session.Stderr = &stderr
	if err := session.Run(command); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return fmt.Errorf("nftables 下发失败: %s", sanitizeSSHError(msg))
	}
	return nil
}

func buildSSHClientConfig(cfg SSHConfig) (*ssh.ClientConfig, error) {
	authType := strings.TrimSpace(strings.ToLower(cfg.AuthType))
	var methods []ssh.AuthMethod
	switch authType {
	case "password":
		if strings.TrimSpace(cfg.Password) == "" {
			return nil, fmt.Errorf("SSH 密码不能为空")
		}
		methods = append(methods, ssh.Password(cfg.Password))
	case "private_key", "":
		signer, err := parsePrivateKey(cfg.PrivateKey, cfg.Passphrase)
		if err != nil {
			return nil, err
		}
		methods = append(methods, ssh.PublicKeys(signer))
	default:
		return nil, fmt.Errorf("不支持的 SSH 认证方式: %s", authType)
	}
	if strings.TrimSpace(cfg.Username) == "" {
		return nil, fmt.Errorf("SSH 用户名不能为空")
	}
	return &ssh.ClientConfig{
		User:            strings.TrimSpace(cfg.Username),
		Auth:            methods,
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         15 * time.Second,
	}, nil
}

func parsePrivateKey(key, passphrase string) (ssh.Signer, error) {
	trimmed := strings.TrimSpace(key)
	if trimmed == "" {
		return nil, fmt.Errorf("SSH 私钥不能为空")
	}
	if strings.TrimSpace(passphrase) != "" {
		signer, err := ssh.ParsePrivateKeyWithPassphrase([]byte(trimmed), []byte(passphrase))
		if err != nil {
			return nil, fmt.Errorf("SSH 私钥解析失败")
		}
		return signer, nil
	}
	signer, err := ssh.ParsePrivateKey([]byte(trimmed))
	if err != nil {
		return nil, fmt.Errorf("SSH 私钥解析失败")
	}
	return signer, nil
}

func nftCommand(cfg SSHConfig, command string) string {
	if strings.TrimSpace(strings.ToLower(cfg.SudoMode)) == "sudo" {
		return "sudo nft " + command
	}
	return "nft " + command
}

func normalizedSSHPort(port int) int {
	if port <= 0 {
		return 22
	}
	return port
}

func sanitizeSSHError(message string) string {
	msg := strings.TrimSpace(message)
	if len(msg) > 800 {
		msg = msg[:800]
	}
	return msg
}
```

- [ ] **Step 4: Implement manager**

Create `go-backend/internal/runtime/nftables/manager.go`:

```go
package nftables

import (
	"context"
	"errors"
)

type Manager struct {
	runner Runner
}

func NewManager(runner Runner) *Manager {
	if runner == nil {
		runner = NewSSHRunner()
	}
	return &Manager{runner: runner}
}

func (m *Manager) Test(ctx context.Context, cfg SSHConfig) error {
	if m == nil || m.runner == nil {
		return errors.New("nftables manager not initialized")
	}
	return m.runner.Test(ctx, cfg)
}

func (m *Manager) Reconcile(ctx context.Context, cfg SSHConfig, plan NodePlan) (ApplyResult, error) {
	if m == nil || m.runner == nil {
		return ApplyResult{}, errors.New("nftables manager not initialized")
	}
	script := RenderTable(plan)
	if err := m.runner.ApplyScript(ctx, cfg, script); err != nil {
		return ApplyResult{}, err
	}
	return ApplyResult{NodeID: plan.NodeID, Script: script, Hashes: PlanHashes(plan)}, nil
}

func (m *Manager) Clear(ctx context.Context, cfg SSHConfig) error {
	if m == nil || m.runner == nil {
		return errors.New("nftables manager not initialized")
	}
	return m.runner.ApplyScript(ctx, cfg, RenderTable(NodePlan{}))
}
```

- [ ] **Step 5: Fix shell command if tests expose quoting issues**

If `go test` fails because `runner.go` builds an invalid shell string, replace `ApplyScript` command construction with this safer variant:

```go
func (r *SSHRunner) ApplyScript(ctx context.Context, cfg SSHConfig, script string) error {
	escaped := strings.ReplaceAll(script, "'", "'\"'\"'")
	nftPrefix := "nft"
	if strings.TrimSpace(strings.ToLower(cfg.SudoMode)) == "sudo" {
		nftPrefix = "sudo nft"
	}
	command := fmt.Sprintf(
		"tmp=/tmp/flvx-nft-$(date +%%s%%N).nft; cat > \"$tmp\" <<'EOF'\n%s\nEOF\n%s list table inet flvx >/dev/null 2>&1 && %s delete table inet flvx || true; %s -f \"$tmp\"; rc=$?; rm -f \"$tmp\"; exit $rc",
		escaped,
		nftPrefix,
		nftPrefix,
		nftPrefix,
	)
	return r.run(ctx, cfg, command)
}
```

- [ ] **Step 6: Run runtime tests**

Run:

```bash
(cd go-backend && go test ./internal/runtime/nftables -count=1)
```

Expected: PASS.

---

### Task 4: Handler Integration And Capability Validation

**Files:**
- Modify: `go-backend/internal/http/handler/handler.go`
- Create: `go-backend/internal/http/handler/nftables_runtime.go`
- Create: `go-backend/internal/http/handler/nftables_runtime_test.go`
- Modify: `go-backend/internal/http/handler/mutations.go`
- Modify: `go-backend/internal/http/handler/control_plane.go`

- [ ] **Step 1: Write failing handler validation tests**

Create `go-backend/internal/http/handler/nftables_runtime_test.go`:

```go
package handler

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"go-backend/internal/store/repo"
)

func TestNftablesTunnelRejectsTunnelForwarding(t *testing.T) {
	r, err := repo.Open(":memory:")
	if err != nil {
		t.Fatalf("open repo: %v", err)
	}
	defer r.Close()
	h := New(r, "test-secret")
	now := time.Now().UnixMilli()
	if err := r.CreateNode("nft", "secret", "203.0.113.10", nil, nil, "10000-20000", nil, nil, nil, nil, nil, 0, 0, 0, now, 1, "[::]", "[::]", 1, 0, nil, nil, nil, nil, "nftables"); err != nil {
		t.Fatalf("create node: %v", err)
	}

	body := strings.NewReader(`{"name":"bad-nft-tunnel","type":2,"inNodeId":[{"nodeId":1,"protocol":"tcp"}],"outNodeId":[{"nodeId":1,"protocol":"tcp"}]}`)
	res := httptest.NewRecorder()
	h.tunnelCreate(res, httptest.NewRequest(http.MethodPost, "/api/v1/tunnel/create", body))
	if !strings.Contains(res.Body.String(), "nftables") || !strings.Contains(res.Body.String(), "隧道转发") {
		t.Fatalf("expected nftables tunnel forwarding rejection, got %s", res.Body.String())
	}
}

func TestNftablesForwardRejectsUnsupportedFields(t *testing.T) {
	if err := validateNftablesForwardRequest(map[string]interface{}{
		"remoteAddr":    "198.51.100.20:443",
		"speedId":       float64(1),
		"proxyProtocol": float64(1),
	}); err == nil {
		t.Fatalf("expected unsupported fields error")
	}
	if err := validateNftablesForwardRequest(map[string]interface{}{
		"remoteAddr": "198.51.100.20:443,198.51.100.21:443",
	}); err == nil {
		t.Fatalf("expected multi target error")
	}
	if err := validateNftablesForwardRequest(map[string]interface{}{
		"remoteAddr": "198.51.100.20:443",
	}); err != nil {
		t.Fatalf("expected valid nftables forward request, got %v", err)
	}
}
```

- [ ] **Step 2: Run handler tests and verify failure**

Run:

```bash
(cd go-backend && go test ./internal/http/handler -run 'TestNftables' -count=1)
```

Expected: FAIL because `validateNftablesForwardRequest` and handler integration do not exist.

- [ ] **Step 3: Initialize nftables manager on Handler**

In `go-backend/internal/http/handler/handler.go`, import:

```go
	"go-backend/internal/runtime/nftables"
```

Add to `Handler`:

```go
	nftables *nftables.Manager
```

Initialize in `New`:

```go
		nftables:                 nftables.NewManager(nil),
```

- [ ] **Step 4: Add handler helpers**

Create `go-backend/internal/http/handler/nftables_runtime.go`:

```go
package handler

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"go-backend/internal/http/response"
	runtimenft "go-backend/internal/runtime/nftables"
	"go-backend/internal/store/repo"
)

func isNftablesMode(mode string) bool {
	return strings.TrimSpace(strings.ToLower(mode)) == runtimenft.ModeNftables
}

func (h *Handler) nodeForwardMode(nodeID int64) string {
	mode, err := h.repo.GetNodeForwardMode(nodeID)
	if err != nil {
		return runtimenft.ModeAgent
	}
	return mode
}

func (h *Handler) tunnelUsesNftables(tunnelID int64) bool {
	nodes, err := h.tunnelEntryNodeIDs(tunnelID)
	if err != nil || len(nodes) == 0 {
		return false
	}
	return isNftablesMode(h.nodeForwardMode(nodes[0]))
}

func (h *Handler) validateNftablesTunnelState(state *tunnelCreateState) error {
	if state == nil || len(state.InNodes) == 0 {
		return nil
	}
	mode := h.nodeForwardMode(state.InNodes[0].NodeID)
	for _, n := range state.InNodes {
		if h.nodeForwardMode(n.NodeID) != mode {
			return errors.New("同一隧道不能混用 agent 和 nftables 节点")
		}
	}
	if !isNftablesMode(mode) {
		return nil
	}
	if state.Type != 1 {
		return errors.New("nftables 节点不支持隧道转发")
	}
	if len(state.OutNodes) > 0 || len(state.ChainHops) > 0 {
		return errors.New("nftables 纯转发不支持出口节点或转发链")
	}
	return nil
}

func validateNftablesForwardRequest(req map[string]interface{}) error {
	if req == nil {
		return errors.New("请求参数错误")
	}
	if asAnyToInt64Ptr(req["speedId"]) != nil {
		return errors.New("nftables 纯转发不支持限速")
	}
	if asAnyToInt64Ptr(req["ipSpeedId"]) != nil {
		return errors.New("nftables 纯转发不支持每 IP 限速")
	}
	if asInt(req["maxConn"], 0) > 0 || asInt(req["ipMaxConn"], 0) > 0 {
		return errors.New("nftables 纯转发不支持连接数限制")
	}
	if asInt(req["proxyProtocol"], 0) > 0 {
		return errors.New("nftables 纯转发不支持 Proxy Protocol")
	}
	if _, err := runtimenft.ParseSingleTarget(asString(req["remoteAddr"])); err != nil {
		return err
	}
	return nil
}

func (h *Handler) sshConfigForNode(nodeID int64) (runtimenft.SSHConfig, error) {
	cfg, err := h.repo.GetNodeSSHConfig(nodeID)
	if err != nil {
		return runtimenft.SSHConfig{}, err
	}
	return runtimenft.SSHConfig{
		Host:       cfg.Host,
		Port:       cfg.Port,
		Username:   cfg.Username,
		AuthType:   cfg.AuthType,
		Password:   cfg.Password.String,
		PrivateKey: cfg.PrivateKey.String,
		Passphrase: cfg.Passphrase.String,
		SudoMode:   cfg.SudoMode,
	}, nil
}

func (h *Handler) syncNftablesNode(nodeID int64) error {
	cfg, err := h.sshConfigForNode(nodeID)
	if err != nil {
		return fmt.Errorf("读取 SSH 配置失败: %w", err)
	}
	plan, err := h.buildNftablesNodePlan(nodeID)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	result, err := h.nftables.Reconcile(ctx, cfg, plan)
	now := time.Now().UnixMilli()
	if err != nil {
		for _, rule := range plan.Rules {
			_ = h.repo.MarkNftRuleBindingError(rule.ForwardID, nodeID, err.Error(), now)
		}
		return err
	}
	for _, rule := range plan.Rules {
		_ = h.repo.UpsertNftRuleBinding(repoBindingFromRule(nodeID, rule, result.Hashes[rule.ForwardID]), now)
	}
	return nil
}

func repoBindingFromRule(nodeID int64, rule runtimenft.Rule, hash string) repo.NftRuleBindingInput {
	return repo.NftRuleBindingInput{
		ForwardID:  rule.ForwardID,
		NodeID:     nodeID,
		InPort:    rule.InPort,
		Protocols: strings.Join(rule.Protocols, ","),
		TargetAddr: fmt.Sprintf("%s:%d", rule.TargetHost, rule.TargetPort),
		BindIP:     rule.BindIP,
		RuleHash:   hash,
		Status:     runtimenft.StatusApplied,
		LastError:  "",
	}
}
```

- [ ] **Step 5: Add build plan helper**

Append to `nftables_runtime.go`:

```go
func (h *Handler) buildNftablesNodePlan(nodeID int64) (runtimenft.NodePlan, error) {
	forwards, err := h.repo.ListActiveForwardsByEntryNode(nodeID)
	if err != nil {
		return runtimenft.NodePlan{}, err
	}
	plan := runtimenft.NodePlan{NodeID: nodeID, Rules: make([]runtimenft.Rule, 0, len(forwards))}
	for _, f := range forwards {
		target, err := runtimenft.ParseSingleTarget(f.RemoteAddr)
		if err != nil {
			return runtimenft.NodePlan{}, fmt.Errorf("规则 %d 目标地址无效: %w", f.ID, err)
		}
		port := 0
		bindIP := ""
		ports, err := h.listForwardPorts(f.ID)
		if err != nil {
			return runtimenft.NodePlan{}, err
		}
		for _, p := range ports {
			if p.NodeID == nodeID {
				port = p.Port
				bindIP = p.InIP.String
				break
			}
		}
		if port <= 0 {
			continue
		}
		plan.Rules = append(plan.Rules, runtimenft.Rule{
			ForwardID:  f.ID,
			InPort:     port,
			BindIP:     bindIP,
			TargetHost: target.Host,
			TargetPort: target.Port,
			Protocols:  []string{"tcp", "udp"},
		})
	}
	return plan, nil
}
```

Add `ListActiveForwardsByEntryNode` to `repository_nftables.go`:

```go
func (r *Repository) ListActiveForwardsByEntryNode(nodeID int64) ([]model.ForwardRecord, error) {
	if r == nil || r.db == nil {
		return nil, errors.New("repository not initialized")
	}
	var rows []model.Forward
	err := r.db.
		Joins("JOIN forward_port ON forward_port.forward_id = forward.id").
		Where("forward_port.node_id = ? AND forward.status = ?", nodeID, 1).
		Order("forward.id ASC").
		Find(&rows).Error
	if err != nil {
		return nil, err
	}
	out := make([]model.ForwardRecord, 0, len(rows))
	for _, f := range rows {
		out = append(out, model.ForwardRecord{
			ID:            f.ID,
			UserID:        f.UserID,
			UserName:      f.UserName,
			Name:          f.Name,
			TunnelID:      f.TunnelID,
			RemoteAddr:    f.RemoteAddr,
			Strategy:      f.Strategy,
			Status:        f.Status,
			SpeedID:       f.SpeedID,
			MaxConn:       f.MaxConn,
			IPMaxConn:     f.IPMaxConn,
			IPSpeedID:     f.IPSpeedID,
			ProxyProtocol: f.ProxyProtocol,
		})
	}
	return out, nil
}
```

- [ ] **Step 6: Route nftables forwards away from GOST sync**

In `forwardCreate`, after loading `tunnel`, add:

```go
	nftTunnel := h.tunnelUsesNftables(tunnelID)
	if nftTunnel {
		if err := validateNftablesForwardRequest(req); err != nil {
			response.WriteJSON(w, response.ErrDefault(err.Error()))
			return
		}
	}
```

After creating `createdForward`, replace the existing unconditional `syncForwardServices` block with:

```go
	if nftTunnel {
		for _, nodeID := range entryNodes {
			if err := h.syncNftablesNode(nodeID); err != nil {
				_ = h.deleteForwardByID(forwardID)
				response.WriteJSON(w, response.ErrDefault(err.Error()))
				return
			}
		}
	} else if err := h.syncForwardServices(createdForward, "UpdateService", true); err != nil {
		_ = h.deleteForwardByID(forwardID)
		response.WriteJSON(w, response.ErrDefault(err.Error()))
		return
	}
```

Make the same pattern in `forwardUpdate`: validate when target tunnel uses nftables, and after `updatedForward` use `syncNftablesNode` for new entry nodes instead of `syncForwardServicesWithWarnings`.

- [ ] **Step 7: Validate nftables tunnel creation**

In `tunnelCreate`, after `runtimeState, err := h.prepareTunnelCreateState(...)` and before federation runtime, add:

```go
	if err := h.validateNftablesTunnelState(runtimeState); err != nil {
		response.WriteJSON(w, response.ErrDefault(err.Error()))
		return
	}
```

In `tunnelUpdate`, add the same check after preparing update runtime state.

- [ ] **Step 8: Skip GOST service sync for nftables forwards**

At the top of `syncForwardServicesWithWarnings` in `control_plane.go`, after nil checks and before building services, add:

```go
	if h.tunnelUsesNftables(forward.TunnelID) {
		entryNodes, _ := h.tunnelEntryNodeIDs(forward.TunnelID)
		for _, nodeID := range entryNodes {
			if err := h.syncNftablesNode(nodeID); err != nil {
				return nil, err
			}
		}
		return nil, nil
	}
```

- [ ] **Step 9: Run handler tests**

Run:

```bash
(cd go-backend && go test ./internal/http/handler -run 'TestNftables' -count=1)
```

Expected: PASS.

---

### Task 5: Delete, Redeploy, Test, And Clear Operations

**Files:**
- Modify: `go-backend/internal/http/handler/handler.go`
- Modify: `go-backend/internal/http/handler/mutations.go`
- Modify: `go-backend/internal/http/handler/nftables_runtime.go`
- Modify: `vite-frontend/src/api/index.ts`

- [ ] **Step 1: Add routes**

In `handler.go` route registration, add:

```go
	mux.HandleFunc("/api/v1/node/nftables/test", h.nodeNftablesTest)
	mux.HandleFunc("/api/v1/node/nftables/reconcile", h.nodeNftablesReconcile)
	mux.HandleFunc("/api/v1/node/nftables/clear", h.nodeNftablesClear)
```

- [ ] **Step 2: Implement maintenance handlers**

Append to `nftables_runtime.go`:

```go
func (h *Handler) nodeNftablesTest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		response.WriteJSON(w, response.ErrDefault("请求失败"))
		return
	}
	id := idFromBody(r, w)
	if id <= 0 {
		return
	}
	if !isNftablesMode(h.nodeForwardMode(id)) {
		response.WriteJSON(w, response.ErrDefault("该节点不是 nftables 节点"))
		return
	}
	cfg, err := h.sshConfigForNode(id)
	if err != nil {
		response.WriteJSON(w, response.ErrDefault(err.Error()))
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	if err := h.nftables.Test(ctx, cfg); err != nil {
		response.WriteJSON(w, response.ErrDefault(err.Error()))
		return
	}
	response.WriteJSON(w, response.OKEmpty())
}

func (h *Handler) nodeNftablesReconcile(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		response.WriteJSON(w, response.ErrDefault("请求失败"))
		return
	}
	id := idFromBody(r, w)
	if id <= 0 {
		return
	}
	if !isNftablesMode(h.nodeForwardMode(id)) {
		response.WriteJSON(w, response.ErrDefault("该节点不是 nftables 节点"))
		return
	}
	if err := h.syncNftablesNode(id); err != nil {
		response.WriteJSON(w, response.ErrDefault(err.Error()))
		return
	}
	response.WriteJSON(w, response.OKEmpty())
}

func (h *Handler) nodeNftablesClear(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		response.WriteJSON(w, response.ErrDefault("请求失败"))
		return
	}
	id := idFromBody(r, w)
	if id <= 0 {
		return
	}
	if !isNftablesMode(h.nodeForwardMode(id)) {
		response.WriteJSON(w, response.ErrDefault("该节点不是 nftables 节点"))
		return
	}
	cfg, err := h.sshConfigForNode(id)
	if err != nil {
		response.WriteJSON(w, response.ErrDefault(err.Error()))
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	if err := h.nftables.Clear(ctx, cfg); err != nil {
		response.WriteJSON(w, response.ErrDefault(err.Error()))
		return
	}
	response.WriteJSON(w, response.OKEmpty())
}
```

Ensure `nftables_runtime.go` imports `go-backend/internal/http/response`.

- [ ] **Step 3: Integrate delete cleanup**

In `deleteForwardByID`, before deleting DB rows, load the forward and entry nodes:

```go
	forward, fErr := h.getForwardRecord(id)
	var nftNodeIDs []int64
	if fErr == nil && forward != nil && h.tunnelUsesNftables(forward.TunnelID) {
		nftNodeIDs, _ = h.tunnelEntryNodeIDs(forward.TunnelID)
	}
```

After DB delete succeeds, add:

```go
	if len(nftNodeIDs) > 0 {
		for _, nodeID := range nftNodeIDs {
			if err := h.syncNftablesNode(nodeID); err != nil {
				return err
			}
		}
		_ = h.repo.DeleteNftRuleBindingsByForward(id)
	}
```

For `forwardForceDelete`, call `DeleteNftRuleBindingsByForward(id)` after the forced DB cleanup, but do not require SSH success.

- [ ] **Step 4: Route batch redeploy through reconcile**

In `forwardBatchRedeploy`, before `h.syncForwardServices`, add:

```go
		if h.tunnelUsesNftables(forward.TunnelID) {
			entryNodes, _ := h.tunnelEntryNodeIDs(forward.TunnelID)
			for _, nodeID := range entryNodes {
				if err := h.syncNftablesNode(nodeID); err != nil {
					failures = append(failures, batchOperationFailure{ID: id, Name: forward.Name, Error: err.Error()})
				}
			}
			continue
		}
```

In `tunnelBatchRedeploy`, when a tunnel uses nftables, reconcile entry nodes and skip `applyTunnelRuntimeUpsert`.

- [ ] **Step 5: Add frontend API methods**

In `vite-frontend/src/api/index.ts`, add:

```ts
export const testNodeNftables = (id: number) =>
  Network.post("/node/nftables/test", { id });
export const reconcileNodeNftables = (id: number) =>
  Network.post("/node/nftables/reconcile", { id });
export const clearNodeNftables = (id: number) =>
  Network.post("/node/nftables/clear", { id });
```

- [ ] **Step 6: Run backend tests**

Run:

```bash
(cd go-backend && go test ./internal/http/handler -run 'TestNftables|TestForward' -count=1)
```

Expected: PASS.

Run:

```bash
(cd go-backend && go test ./...)
```

Expected: PASS.

---

### Task 6: Frontend Node Mode And SSH Form

**Files:**
- Modify: `vite-frontend/src/api/types.ts`
- Modify: `vite-frontend/src/pages/node.tsx`

- [ ] **Step 1: Add API types**

In `vite-frontend/src/api/types.ts`, add near node types:

```ts
export type NodeForwardMode = "agent" | "nftables";

export interface NodeSSHConfigPayload {
  host: string;
  port: number;
  username: string;
  authType: "password" | "private_key";
  password?: string;
  privateKey?: string;
  passphrase?: string;
  sudoMode: "none" | "sudo";
}
```

Add to `NodeApiItem`:

```ts
  forwardMode?: NodeForwardMode;
  sshConfig?: Partial<NodeSSHConfigPayload>;
```

- [ ] **Step 2: Extend node form state**

In `vite-frontend/src/pages/node.tsx`, extend `NodeForm`:

```ts
  forwardMode: "agent" | "nftables";
  sshHost: string;
  sshPort: string;
  sshUsername: string;
  sshAuthType: "password" | "private_key";
  sshPassword: string;
  sshPrivateKey: string;
  sshPassphrase: string;
  sshSudoMode: "none" | "sudo";
```

Update the reset/default form object to include:

```ts
      forwardMode: "agent",
      sshHost: "",
      sshPort: "22",
      sshUsername: "root",
      sshAuthType: "private_key",
      sshPassword: "",
      sshPrivateKey: "",
      sshPassphrase: "",
      sshSudoMode: "none",
```

In `handleEdit`, set these fields from `node.forwardMode` and `node.sshConfig`:

```ts
      forwardMode: node.forwardMode === "nftables" ? "nftables" : "agent",
      sshHost:
        typeof node.sshConfig?.host === "string"
          ? node.sshConfig.host
          : normalizedV4 || normalizedV6 || normalizedHost,
      sshPort:
        typeof node.sshConfig?.port === "number"
          ? String(node.sshConfig.port)
          : "22",
      sshUsername:
        typeof node.sshConfig?.username === "string"
          ? node.sshConfig.username
          : "root",
      sshAuthType:
        node.sshConfig?.authType === "password" ? "password" : "private_key",
      sshPassword: "",
      sshPrivateKey: "",
      sshPassphrase: "",
      sshSudoMode: node.sshConfig?.sudoMode === "sudo" ? "sudo" : "none",
```

- [ ] **Step 3: Add validation for nftables SSH fields**

In `validateForm`, after host/port validation, add:

```ts
    if (form.forwardMode === "nftables") {
      if (!form.sshHost.trim()) {
        newErrors.sshHost = "请输入 SSH 主机";
      }
      const sshPort = Number(form.sshPort);
      if (!Number.isInteger(sshPort) || sshPort < 1 || sshPort > 65535) {
        newErrors.sshPort = "SSH 端口必须在 1-65535 之间";
      }
      if (!form.sshUsername.trim()) {
        newErrors.sshUsername = "请输入 SSH 用户名";
      }
      if (form.sshAuthType === "password" && !isEdit && !form.sshPassword.trim()) {
        newErrors.sshPassword = "请输入 SSH 密码";
      }
      if (
        form.sshAuthType === "private_key" &&
        !isEdit &&
        !form.sshPrivateKey.trim()
      ) {
        newErrors.sshPrivateKey = "请输入 SSH 私钥";
      }
    }
```

- [ ] **Step 4: Submit mode and SSH payload**

In `handleSubmit`, build `sshConfig` before `data`:

```ts
      const sshConfig =
        form.forwardMode === "nftables"
          ? {
              host: form.sshHost.trim(),
              port: Number(form.sshPort || 22),
              username: form.sshUsername.trim(),
              authType: form.sshAuthType,
              password:
                form.sshAuthType === "password"
                  ? form.sshPassword
                  : undefined,
              privateKey:
                form.sshAuthType === "private_key"
                  ? form.sshPrivateKey
                  : undefined,
              passphrase: form.sshPassphrase,
              sudoMode: form.sshSudoMode,
            }
          : undefined;
```

Add to submitted `data`:

```ts
        forwardMode: form.forwardMode,
        ...(sshConfig ? { sshConfig } : {}),
```

- [ ] **Step 5: Add form controls**

In the node modal after the node address fields, add controls using existing imported `Select`, `SelectItem`, `Input`, and `Textarea` components:

```tsx
                <Select
                  label="转发模式"
                  selectedKeys={[form.forwardMode]}
                  onSelectionChange={(keys) => {
                    const value = Array.from(keys)[0]?.toString();
                    setForm((prev) => ({
                      ...prev,
                      forwardMode: value === "nftables" ? "nftables" : "agent",
                    }));
                  }}
                >
                  <SelectItem key="agent">Agent 节点</SelectItem>
                  <SelectItem key="nftables">nftables 纯转发</SelectItem>
                </Select>

                {form.forwardMode === "nftables" && (
                  <div className="grid grid-cols-1 gap-4 rounded-2xl border border-default-200 bg-default-50/60 p-4 md:grid-cols-2">
                    <Input
                      label="SSH 主机"
                      value={form.sshHost}
                      isInvalid={!!errors.sshHost}
                      errorMessage={errors.sshHost}
                      onValueChange={(value) =>
                        setForm((prev) => ({ ...prev, sshHost: value }))
                      }
                    />
                    <Input
                      label="SSH 端口"
                      value={form.sshPort}
                      isInvalid={!!errors.sshPort}
                      errorMessage={errors.sshPort}
                      onValueChange={(value) =>
                        setForm((prev) => ({ ...prev, sshPort: value }))
                      }
                    />
                    <Input
                      label="SSH 用户名"
                      value={form.sshUsername}
                      isInvalid={!!errors.sshUsername}
                      errorMessage={errors.sshUsername}
                      onValueChange={(value) =>
                        setForm((prev) => ({ ...prev, sshUsername: value }))
                      }
                    />
                    <Select
                      label="认证方式"
                      selectedKeys={[form.sshAuthType]}
                      onSelectionChange={(keys) => {
                        const value = Array.from(keys)[0]?.toString();
                        setForm((prev) => ({
                          ...prev,
                          sshAuthType:
                            value === "password" ? "password" : "private_key",
                        }));
                      }}
                    >
                      <SelectItem key="private_key">私钥</SelectItem>
                      <SelectItem key="password">密码</SelectItem>
                    </Select>
                    {form.sshAuthType === "password" ? (
                      <Input
                        label="SSH 密码"
                        type="password"
                        value={form.sshPassword}
                        isInvalid={!!errors.sshPassword}
                        errorMessage={errors.sshPassword}
                        onValueChange={(value) =>
                          setForm((prev) => ({ ...prev, sshPassword: value }))
                        }
                      />
                    ) : (
                      <Textarea
                        className="md:col-span-2"
                        label="SSH 私钥"
                        value={form.sshPrivateKey}
                        isInvalid={!!errors.sshPrivateKey}
                        errorMessage={errors.sshPrivateKey}
                        onValueChange={(value) =>
                          setForm((prev) => ({ ...prev, sshPrivateKey: value }))
                        }
                      />
                    )}
                    <Input
                      label="私钥口令"
                      type="password"
                      value={form.sshPassphrase}
                      onValueChange={(value) =>
                        setForm((prev) => ({ ...prev, sshPassphrase: value }))
                      }
                    />
                    <Select
                      label="nft 执行方式"
                      selectedKeys={[form.sshSudoMode]}
                      onSelectionChange={(keys) => {
                        const value = Array.from(keys)[0]?.toString();
                        setForm((prev) => ({
                          ...prev,
                          sshSudoMode: value === "sudo" ? "sudo" : "none",
                        }));
                      }}
                    >
                      <SelectItem key="none">直接执行 nft</SelectItem>
                      <SelectItem key="sudo">sudo nft</SelectItem>
                    </Select>
                  </div>
                )}
```

- [ ] **Step 6: Hide agent-only operations for nftables nodes**

Where install/upgrade/rollback buttons are rendered, wrap them with:

```tsx
{node.forwardMode !== "nftables" && (
  // existing agent-only action
)}
```

Add nftables action buttons that call `testNodeNftables`, `reconcileNodeNftables`, and `clearNodeNftables` for nodes with `forwardMode === "nftables"`.

- [ ] **Step 7: Run frontend build**

Run:

```bash
(cd vite-frontend && pnpm run build)
```

Expected: PASS.

---

### Task 7: Frontend Tunnel And Forward Capability UX

**Files:**
- Modify: `vite-frontend/src/pages/tunnel/form.ts`
- Modify: `vite-frontend/src/pages/tunnel.tsx`
- Modify: `vite-frontend/src/pages/forward.tsx`

- [ ] **Step 1: Add tunnel form validation**

In `vite-frontend/src/pages/tunnel/form.ts`, extend validation by passing nodes or add a helper exported from `tunnel.tsx`. The concrete helper should be:

```ts
export const isNftablesNode = (node?: { forwardMode?: string }) =>
  node?.forwardMode === "nftables";
```

In `tunnel.tsx`, before submit, derive selected entry nodes:

```ts
    const selectedEntryNodes = form.inNodeId
      .map((item) => nodeList.find((node) => node.id === item.nodeId))
      .filter(Boolean);
    const hasNftablesEntry = selectedEntryNodes.some(
      (node) => node?.forwardMode === "nftables",
    );
    const hasAgentEntry = selectedEntryNodes.some(
      (node) => node?.forwardMode !== "nftables",
    );
    if (hasNftablesEntry && hasAgentEntry) {
      toast.error("同一隧道不能混用 agent 和 nftables 节点");
      return;
    }
    if (hasNftablesEntry && form.type !== 1) {
      toast.error("nftables 节点只支持端口转发");
      return;
    }
```

- [ ] **Step 2: Disable tunnel forwarding option when nftables entry is selected**

In the tunnel type `Select`, keep `SelectItem key="2"` but add description near the selector:

```tsx
{form.inNodeId.some((item) =>
  nodeList.some(
    (node) => node.id === item.nodeId && node.forwardMode === "nftables",
  ),
) && (
  <p className="text-xs text-warning">
    已选择 nftables 节点：仅支持端口转发，不支持出口节点和转发链。
  </p>
)}
```

In `handleTypeChange`, if selected entry contains nftables and requested type is `2`, show toast and keep type `1`.

- [ ] **Step 3: Detect nftables tunnel in forward form**

In `forward.tsx`, add helper near other form helpers:

```ts
const isNftablesTunnel = (tunnel?: TunnelApiItem | null): boolean => {
  const entries = tunnel?.inNodeId || [];
  return entries.some((entry) => {
    const node = nodes.find((item) => item.id === entry.nodeId);
    return node?.forwardMode === "nftables";
  });
};
```

If `nodes` is not in scope where the helper lives, define it inside `ForwardPage` as:

```ts
  const selectedTunnel = tunnels.find((item) => item.id === form.tunnelId) || null;
  const selectedTunnelIsNftables = Boolean(
    selectedTunnel?.inNodeId?.some((entry) =>
      nodeList.some(
        (node) => node.id === entry.nodeId && node.forwardMode === "nftables",
      ),
    ),
  );
```

- [ ] **Step 4: Reset unsupported fields when nftables tunnel is selected**

In the tunnel selection handler, after setting `tunnelId`, add:

```ts
      const nextTunnel = tunnels.find((item) => item.id === Number(selectedKey));
      const nextIsNftables = Boolean(
        nextTunnel?.inNodeId?.some((entry) =>
          nodeList.some(
            (node) => node.id === entry.nodeId && node.forwardMode === "nftables",
          ),
        ),
      );
      if (nextIsNftables) {
        return {
          ...prev,
          tunnelId: Number(selectedKey),
          speedId: null,
          ipSpeedId: null,
          maxConn: 0,
          ipMaxConn: 0,
          proxyProtocol: 0,
          strategy: "fifo",
        };
      }
```

- [ ] **Step 5: Validate single target in forward submit**

Before `createForward` or `updateForward` in `handleSubmit`, add:

```ts
      if (selectedTunnelIsNftables) {
        if (form.remoteAddr.includes(",") || form.remoteAddr.includes("\n")) {
          toast.error("nftables 纯转发第一阶段仅支持单目标");
          return;
        }
        const unsupported =
          normalizedSpeedId !== null ||
          normalizedIPSpeedId !== null ||
          Number(form.maxConn || 0) > 0 ||
          Number(form.ipMaxConn || 0) > 0 ||
          Number(form.proxyProtocol || 0) > 0;
        if (unsupported) {
          toast.error("nftables 纯转发不支持限速、连接数限制或 Proxy Protocol");
          return;
        }
      }
```

- [ ] **Step 6: Hide unsupported controls**

Wrap speed limit, per-IP speed limit, max connection, per-IP max connection, and Proxy Protocol controls with:

```tsx
{!selectedTunnelIsNftables && (
  // existing advanced control
)}
```

Add explanatory text near the remote target input:

```tsx
{selectedTunnelIsNftables && (
  <p className="text-xs text-warning">
    nftables 纯转发仅支持单目标 host:port，不支持限速、连接数限制和 Proxy Protocol。
  </p>
)}
```

- [ ] **Step 7: Run frontend build**

Run:

```bash
(cd vite-frontend && pnpm run build)
```

Expected: PASS.

---

### Task 8: Full Verification

**Files:**
- No source changes expected unless verification exposes a defect.

- [ ] **Step 1: Run backend tests**

Run:

```bash
(cd go-backend && go test ./...)
```

Expected: PASS.

- [ ] **Step 2: Run frontend build**

Run:

```bash
(cd vite-frontend && pnpm run build)
```

Expected: PASS.

- [ ] **Step 3: Manual smoke test with fake SSH target**

Start backend and frontend:

```bash
(cd go-backend && go run ./cmd/paneld)
(cd vite-frontend && pnpm run dev)
```

Manual checks:

- Create an `nftables` node with invalid SSH host and confirm “测试 SSH” returns a clear SSH error.
- Create an `agent` node and confirm install/upgrade actions still show.
- Create a tunnel with only the nftables node as entry and `type=1`; confirm save succeeds.
- Try to create `type=2` with the nftables node; confirm UI and backend reject it.
- Create a forward on the nftables tunnel with `remoteAddr=198.51.100.20:443`; confirm unsupported controls are hidden.
- Try a multi-target `remoteAddr`; confirm UI and backend reject it.

- [ ] **Step 4: Inspect git status**

Run:

```bash
git status --short
```

Expected: only intentional implementation files are modified.

---

## Plan Self-Review

- Spec coverage: node mode, SSH config, binding state, strict nftables-only capabilities, renderer, SSH runner, create/update/delete/redeploy flows, and frontend UX all map to tasks above.
- Scope control: first phase remains single-target TCP+UDP DNAT/SNAT, no traffic accounting, no GOST limiter support, no federation support.
- Type consistency: backend mode string is `forwardMode` in JSON and `forward_mode` in DB; runtime constants use `agent` and `nftables`; binding statuses are `pending`, `applied`, and `error`.
- Verification: backend repository/runtime/handler tests plus frontend build and manual smoke checks cover the MVP.
