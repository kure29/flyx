# Monitoring Retention And Storage Display Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add configurable monitoring data retention and show database storage usage on the config page.

**Architecture:** Store retention in `vite_config` as `monitor_retention_days`, parse it through a focused monitoring helper, and reuse it from existing cleanup loops. Add a repository storage-summary helper, expose it via an admin-only API, and render it in the existing React config page.

**Tech Stack:** Go `net/http`, GORM, SQLite/PostgreSQL, Vite/React/TypeScript, existing shadcn bridge components.

---

## File Structure

- Create: `go-backend/internal/monitoring/retention.go` for retention constants, parsing, and validation.
- Test: `go-backend/internal/monitoring/retention_test.go`.
- Modify: `go-backend/internal/metrics/ingestion.go` and `go-backend/internal/metrics/ingestion_test.go` for config-driven cleanup.
- Modify: `go-backend/internal/http/handler/tunnel_quality_prober.go` so `tunnel_quality` uses the same retention and still prunes when probing is disabled.
- Create: `go-backend/internal/store/repo/repository_storage.go` and `go-backend/internal/store/repo/repository_storage_test.go` for database size summaries.
- Modify: `go-backend/internal/store/repo/repository.go` to keep the SQLite DB path on `Repository`.
- Create: `go-backend/internal/http/handler/storage.go` for the storage endpoint.
- Modify: `go-backend/internal/http/handler/handler.go` to register `/api/v1/system/storage` and validate `monitor_retention_days`.
- Modify: `go-backend/internal/http/middleware/auth.go` so `/api/v1/system/*` is admin-only.
- Create: `go-backend/tests/contract/storage_contract_test.go` for endpoint auth/shape coverage.
- Modify: `vite-frontend/src/api/types.ts`, `vite-frontend/src/api/index.ts`, and `vite-frontend/src/pages/config.tsx` for UI display.

Implementation should not create git commits unless the user explicitly requests them.

---

### Task 1: Add Retention Config Helper

**Files:**
- Create: `go-backend/internal/monitoring/retention.go`
- Create: `go-backend/internal/monitoring/retention_test.go`
- Modify: `go-backend/internal/http/handler/handler.go`

- [ ] **Step 1: Write the failing tests**

Create `go-backend/internal/monitoring/retention_test.go`:

```go
package monitoring

import "testing"

func TestMonitoringRetentionDaysFromConfigMap(t *testing.T) {
	tests := []struct {
		name string
		cfg  map[string]string
		want int
	}{
		{"missing uses default", nil, 7},
		{"valid custom", map[string]string{ConfigMonitorRetentionDays: "3"}, 3},
		{"trimmed custom", map[string]string{ConfigMonitorRetentionDays: " 30 "}, 30},
		{"invalid uses default", map[string]string{ConfigMonitorRetentionDays: "abc"}, 7},
		{"too small uses default", map[string]string{ConfigMonitorRetentionDays: "0"}, 7},
		{"too large uses default", map[string]string{ConfigMonitorRetentionDays: "3651"}, 7},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := MonitoringRetentionDaysFromConfigMap(tc.cfg); got != tc.want {
				t.Fatalf("expected %d, got %d", tc.want, got)
			}
		})
	}
}

func TestNormalizeMonitoringRetentionDays(t *testing.T) {
	for _, value := range []string{"1", "7", "3650", " 30 "} {
		if got, err := NormalizeMonitoringRetentionDays(value); err != nil || got == "" {
			t.Fatalf("expected %q valid, got value=%q err=%v", value, got, err)
		}
	}
	for _, value := range []string{"", "0", "-1", "3651", "abc", "1.5"} {
		if got, err := NormalizeMonitoringRetentionDays(value); err == nil {
			t.Fatalf("expected %q invalid, got value=%q", value, got)
		}
	}
}
```

- [ ] **Step 2: Run tests to verify failure**

Run: `go test ./internal/monitoring -run 'TestMonitoringRetentionDaysFromConfigMap|TestNormalizeMonitoringRetentionDays' -count=1`

Expected: FAIL with undefined `ConfigMonitorRetentionDays`, `MonitoringRetentionDaysFromConfigMap`, and `NormalizeMonitoringRetentionDays`.

- [ ] **Step 3: Implement helper**

Create `go-backend/internal/monitoring/retention.go`:

```go
package monitoring

import (
	"fmt"
	"strconv"
	"strings"
)

const (
	ConfigMonitorRetentionDays = "monitor_retention_days"
	DefaultMonitorRetentionDays = 7
	MinMonitorRetentionDays     = 1
	MaxMonitorRetentionDays     = 3650
)

func MonitoringRetentionDaysFromConfigMap(cfg map[string]string) int {
	if cfg == nil {
		return DefaultMonitorRetentionDays
	}
	days, err := parseMonitoringRetentionDays(cfg[ConfigMonitorRetentionDays])
	if err != nil {
		return DefaultMonitorRetentionDays
	}
	return days
}

func NormalizeMonitoringRetentionDays(value string) (string, error) {
	days, err := parseMonitoringRetentionDays(value)
	if err != nil {
		return "", err
	}
	return strconv.Itoa(days), nil
}

func parseMonitoringRetentionDays(value string) (int, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return 0, fmt.Errorf("监控数据保留天数不能为空")
	}
	days, err := strconv.Atoi(trimmed)
	if err != nil {
		return 0, fmt.Errorf("监控数据保留天数必须是整数")
	}
	if days < MinMonitorRetentionDays || days > MaxMonitorRetentionDays {
		return 0, fmt.Errorf("监控数据保留天数必须在 %d 到 %d 之间", MinMonitorRetentionDays, MaxMonitorRetentionDays)
	}
	return days, nil
}
```

- [ ] **Step 4: Validate config updates**

In `go-backend/internal/http/handler/handler.go`, add this case to `normalizeAndValidateConfigValue`:

```go
	case monitoring.ConfigMonitorRetentionDays:
		return monitoring.NormalizeMonitoringRetentionDays(value)
```

- [ ] **Step 5: Run tests**

Run: `go test ./internal/monitoring ./internal/http/handler -run 'TestMonitoringRetention|TestNormalize|Test' -count=1`

Expected: PASS or only unrelated pre-existing failures, which must be investigated before continuing.

---

### Task 2: Use Retention Config In Cleanup

**Files:**
- Modify: `go-backend/internal/metrics/ingestion.go`
- Modify: `go-backend/internal/metrics/ingestion_test.go`
- Modify: `go-backend/internal/http/handler/tunnel_quality_prober.go`

- [ ] **Step 1: Write failing cleanup test**

Append to `go-backend/internal/metrics/ingestion_test.go`, adding `go-backend/internal/store/model` to imports:

```go
func TestPruneMetricsUsesConfiguredRetentionDays(t *testing.T) {
	r, err := repo.Open(":memory:")
	if err != nil {
		t.Fatalf("open repo: %v", err)
	}
	defer r.Close()

	now := time.Now().UnixMilli()
	if err := r.UpsertConfig("monitor_retention_days", "2", now); err != nil {
		t.Fatalf("upsert retention config: %v", err)
	}

	oldMetric := &model.NodeMetric{NodeID: 1, Timestamp: now - int64(3*24*time.Hour/time.Millisecond), CPUUsage: 10}
	newMetric := &model.NodeMetric{NodeID: 1, Timestamp: now - int64(1*24*time.Hour/time.Millisecond), CPUUsage: 20}
	if err := r.InsertNodeMetric(oldMetric); err != nil {
		t.Fatalf("insert old metric: %v", err)
	}
	if err := r.InsertNodeMetric(newMetric); err != nil {
		t.Fatalf("insert new metric: %v", err)
	}

	svc := NewIngestionService(r)
	svc.pruneMetricsAt(time.UnixMilli(now))

	metrics, err := r.GetNodeMetrics(1, now-int64(4*24*time.Hour/time.Millisecond), now+1000)
	if err != nil {
		t.Fatalf("get node metrics: %v", err)
	}
	if len(metrics) != 1 || metrics[0].CPUUsage != 20 {
		t.Fatalf("expected only newer metric to remain, got %#v", metrics)
	}
}
```

- [ ] **Step 2: Run test to verify failure**

Run: `go test ./internal/metrics -run TestPruneMetricsUsesConfiguredRetentionDays -count=1`

Expected: FAIL with undefined `pruneMetricsAt`.

- [ ] **Step 3: Implement config-driven prune**

In `go-backend/internal/metrics/ingestion.go`, import `go-backend/internal/monitoring` and replace `pruneMetrics` with:

```go
func (s *IngestionService) retentionDaysFromConfig() int {
	if s == nil || s.repo == nil {
		return monitoring.DefaultMonitorRetentionDays
	}
	cfg, err := s.repo.GetConfigsByNames([]string{monitoring.ConfigMonitorRetentionDays})
	if err != nil {
		return monitoring.DefaultMonitorRetentionDays
	}
	return monitoring.MonitoringRetentionDaysFromConfigMap(cfg)
}

func (s *IngestionService) pruneMetrics() {
	s.pruneMetricsAt(time.Now())
}

func (s *IngestionService) pruneMetricsAt(now time.Time) {
	cutoff := now.Add(-time.Duration(s.retentionDaysFromConfig()) * 24 * time.Hour).UnixMilli()
	if s.repo == nil {
		return
	}
	if err := s.repo.PruneNodeMetrics(cutoff); err != nil {
		log.Printf("monitoring prune failed op=node_metric cutoff=%d err=%v", cutoff, err)
	}
	if err := s.repo.PruneTunnelMetrics(cutoff); err != nil {
		log.Printf("monitoring prune failed op=tunnel_metric cutoff=%d err=%v", cutoff, err)
	}
	if err := s.repo.PruneServiceMonitorResults(cutoff); err != nil {
		log.Printf("monitoring prune failed op=service_monitor_result cutoff=%d err=%v", cutoff, err)
	}
}
```

Remove the unused `retentionDays` field from `IngestionService` and remove `svc.retentionDays = 1` from existing tests.

- [ ] **Step 4: Update tunnel quality pruning**

In `go-backend/internal/http/handler/tunnel_quality_prober.go`, import `go-backend/internal/monitoring`, remove `tunnelQualityRetention`, remove the `if !p.isEnabled() { return }` guard from `maybePrune`, and calculate cutoff with:

```go
func (p *tunnelQualityProber) retentionDays() int {
	if p == nil || p.handler == nil || p.handler.repo == nil {
		return monitoring.DefaultMonitorRetentionDays
	}
	cfg, err := p.handler.repo.GetConfigsByNames([]string{monitoring.ConfigMonitorRetentionDays})
	if err != nil {
		return monitoring.DefaultMonitorRetentionDays
	}
	return monitoring.MonitoringRetentionDaysFromConfigMap(cfg)
}
```

Then use:

```go
	cutoff := now - int64(time.Duration(p.retentionDays())*24*time.Hour/time.Millisecond)
```

- [ ] **Step 5: Run cleanup tests**

Run: `go test ./internal/metrics ./internal/http/handler -run 'TestPruneMetrics|TestPruneMetricsUsesConfiguredRetentionDays|TunnelQuality' -count=1`

Expected: PASS.

---

### Task 3: Add Storage Summary Backend API

**Files:**
- Modify: `go-backend/internal/store/repo/repository.go`
- Create: `go-backend/internal/store/repo/repository_storage.go`
- Create: `go-backend/internal/store/repo/repository_storage_test.go`
- Create: `go-backend/internal/http/handler/storage.go`
- Modify: `go-backend/internal/http/handler/handler.go`
- Modify: `go-backend/internal/http/middleware/auth.go`
- Create: `go-backend/tests/contract/storage_contract_test.go`

- [ ] **Step 1: Write failing repository tests**

Create `go-backend/internal/store/repo/repository_storage_test.go`:

```go
package repo

import (
	"path/filepath"
	"testing"

	"go-backend/internal/store/model"
)

func TestDatabaseStorageSummarySQLiteIncludesSize(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "storage.db")
	r, err := Open(dbPath)
	if err != nil {
		t.Fatalf("open repo: %v", err)
	}
	defer r.Close()

	if err := r.InsertNodeMetric(&model.NodeMetric{NodeID: 1, Timestamp: 123, CPUUsage: 1}); err != nil {
		t.Fatalf("insert metric: %v", err)
	}
	summary, err := r.DatabaseStorageSummary()
	if err != nil {
		t.Fatalf("storage summary: %v", err)
	}
	if summary.DBType != "sqlite" || summary.DatabaseSizeBytes <= 0 || summary.DatabaseSizeText == "" {
		t.Fatalf("unexpected summary: %#v", summary)
	}
}

func TestFormatDatabaseSize(t *testing.T) {
	for _, tc := range []struct{ bytes int64; want string }{{0, "0 B"}, {512, "512 B"}, {1024, "1.0 KB"}, {1024 * 1024, "1.0 MB"}} {
		if got := formatDatabaseSize(tc.bytes); got != tc.want {
			t.Fatalf("formatDatabaseSize(%d)=%q want %q", tc.bytes, got, tc.want)
		}
	}
}
```

- [ ] **Step 2: Run test to verify failure**

Run: `go test ./internal/store/repo -run 'TestDatabaseStorageSummarySQLiteIncludesSize|TestFormatDatabaseSize' -count=1`

Expected: FAIL with undefined `DatabaseStorageSummary` and `formatDatabaseSize`.

- [ ] **Step 3: Implement repository helper**

Modify `Repository` in `repository.go`:

```go
type Repository struct {
	db     *gorm.DB
	dbPath string
}
```

Return `&Repository{db: db, dbPath: path}` from `Open` and `&Repository{db: db}` from `OpenPostgres`.

Create `go-backend/internal/store/repo/repository_storage.go`:

```go
package repo

import (
	"errors"
	"fmt"
	"os"
)

type DatabaseStorageSummary struct {
	DBType            string `json:"dbType"`
	DatabaseSizeBytes int64  `json:"databaseSizeBytes"`
	DatabaseSizeText  string `json:"databaseSizeText"`
}

func (r *Repository) DatabaseStorageSummary() (DatabaseStorageSummary, error) {
	if r == nil || r.db == nil {
		return DatabaseStorageSummary{}, errors.New("repository not initialized")
	}
	switch r.db.Dialector.Name() {
	case "sqlite":
		size, err := sqliteDatabaseFileSize(r.dbPath)
		if err != nil { return DatabaseStorageSummary{}, err }
		return DatabaseStorageSummary{"sqlite", size, formatDatabaseSize(size)}, nil
	case "postgres":
		var size int64
		if err := r.db.Raw("SELECT pg_database_size(current_database())").Scan(&size).Error; err != nil { return DatabaseStorageSummary{}, err }
		return DatabaseStorageSummary{"postgres", size, formatDatabaseSize(size)}, nil
	default:
		return DatabaseStorageSummary{}, fmt.Errorf("unsupported database dialect %q", r.db.Dialector.Name())
	}
}

func sqliteDatabaseFileSize(path string) (int64, error) {
	if path == "" || path == ":memory:" { return 0, nil }
	var total int64
	for _, candidate := range []string{path, path + "-wal", path + "-shm"} {
		info, err := os.Stat(candidate)
		if err != nil {
			if os.IsNotExist(err) { continue }
			return 0, err
		}
		if !info.IsDir() { total += info.Size() }
	}
	return total, nil
}

func formatDatabaseSize(bytes int64) string {
	if bytes < 1024 { return fmt.Sprintf("%d B", bytes) }
	units := []string{"KB", "MB", "GB", "TB"}
	value := float64(bytes) / 1024
	for _, unit := range units {
		if value < 1024 || unit == "TB" { return fmt.Sprintf("%.1f %s", value, unit) }
		value /= 1024
	}
	return fmt.Sprintf("%d B", bytes)
}
```

- [ ] **Step 4: Add API handler and route**

Create `go-backend/internal/http/handler/storage.go`:

```go
package handler

import (
	"net/http"

	"go-backend/internal/http/response"
)

func (h *Handler) storageSummary(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		response.WriteJSON(w, response.ErrDefault("请求失败"))
		return
	}
	if h == nil || h.repo == nil {
		response.WriteJSON(w, response.Err(-2, "repository not initialized"))
		return
	}
	summary, err := h.repo.DatabaseStorageSummary()
	if err != nil {
		response.WriteJSON(w, response.Err(-2, err.Error()))
		return
	}
	response.WriteJSON(w, response.OK(summary))
}
```

Register in `Handler.Register`: `mux.HandleFunc("/api/v1/system/storage", h.storageSummary)`.

In `requiresAdmin`, add:

```go
	if strings.HasPrefix(path, "/api/v1/system/") {
		return true
	}
```

- [ ] **Step 5: Write contract test for auth and shape**

Create `go-backend/tests/contract/storage_contract_test.go` with a test that sends GET `/api/v1/system/storage` as non-admin and expects `403`, then as admin and expects `code == 0`, `dbType`, numeric `databaseSizeBytes`, and `databaseSizeText`.

- [ ] **Step 6: Run storage tests**

Run: `go test ./internal/store/repo ./tests/contract -run 'TestDatabaseStorageSummarySQLiteIncludesSize|TestFormatDatabaseSize|TestStorageSummaryRequiresAdminAndReturnsSize' -count=1`

Expected: PASS.

---

### Task 4: Add Frontend Config UI

**Files:**
- Modify: `vite-frontend/src/api/types.ts`
- Modify: `vite-frontend/src/api/index.ts`
- Modify: `vite-frontend/src/pages/config.tsx`

- [ ] **Step 1: Add API type and function**

In `types.ts` add:

```ts
export interface StorageSummaryApiData {
  dbType: string;
  databaseSizeBytes: number;
  databaseSizeText: string;
}
```

In `index.ts`, import `StorageSummaryApiData` and add:

```ts
export const getStorageSummary = () =>
  Network.get<StorageSummaryApiData>("/system/storage");
```

- [ ] **Step 2: Add retention config item**

In `config.tsx`, add to `CONFIG_ITEMS` near monitoring:

```ts
  {
    key: "monitor_retention_days",
    label: "监控数据保留天数",
    placeholder: "7",
    description:
      "统一清理节点指标、隧道流量、服务监控结果和隧道质量历史；默认 7 天。",
    type: "input",
  },
```

Add `"monitor_retention_days"` to `getInitialConfigs()` keys.

- [ ] **Step 3: Fetch and display database size**

In `config.tsx`, add state:

```ts
const [storageSummary, setStorageSummary] = useState<string>("加载中...");
```

Add a load effect:

```ts
useEffect(() => {
  let mounted = true;
  getStorageSummary()
    .then((response) => {
      if (!mounted) return;
      if (response.code === 0 && response.data?.databaseSizeText) {
        setStorageSummary(response.data.databaseSizeText);
      } else {
        setStorageSummary("获取失败");
      }
    })
    .catch(() => {
      if (mounted) setStorageSummary("获取失败");
    });
  return () => {
    mounted = false;
  };
}, []);
```

Render inside the basic settings card before the save button:

```tsx
<Divider className="my-2" />
<div className="space-y-1">
  <p className="text-sm font-medium text-gray-700 dark:text-gray-300">
    数据库占用
  </p>
  <p className="text-xs text-gray-500 dark:text-gray-400">
    当前后端数据库文件/实例占用空间，仅用于容量参考。
  </p>
  <div className="rounded-lg border border-divider bg-default-50/60 dark:bg-default-100/10 px-4 py-3 text-sm font-semibold text-default-800 dark:text-default-200">
    {storageSummary}
  </div>
</div>
```

- [ ] **Step 4: Build frontend**

Run: `pnpm run build` from `vite-frontend`.

Expected: TypeScript and Vite build pass.

---

### Task 5: Final Verification

**Files:**
- All files changed by previous tasks.

- [ ] **Step 1: Run backend tests**

Run: `go test ./...` from `go-backend`.

Expected: PASS.

- [ ] **Step 2: Run frontend build**

Run: `pnpm run build` from `vite-frontend`.

Expected: PASS.

- [ ] **Step 3: Review diff**

Run: `git diff --stat` and `git diff -- docs/superpowers/specs/2026-04-28-monitoring-retention-storage-design.md docs/superpowers/plans/2026-04-28-monitoring-retention-storage.md go-backend vite-frontend`.

Expected: Diff is limited to retention config, storage summary, tests, and config UI.

---

## Self-Review

- Spec coverage: retention config, uniform cleanup, storage summary API, frontend display, validation, and verification are covered.
- Placeholder scan: no TBD/TODO placeholders; the one contract-test step describes exact assertions even though the surrounding helper functions already exist in contract tests.
- Type consistency: backend JSON fields match frontend `StorageSummaryApiData` exactly: `dbType`, `databaseSizeBytes`, `databaseSizeText`.
