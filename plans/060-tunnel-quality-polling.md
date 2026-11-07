# 060 Nezha-style Monitoring (1s test, 30s report)

## Objective
Update all monitoring subsystems to test every 1 second and report (write to DB) every 30 seconds, matching Nezha-style monitoring behavior.

## Changes

### 1. Tunnel Quality Prober (`go-backend/internal/http/handler/tunnel_quality_prober.go`)
- [x] Change `tunnelQualityProbeInterval` from 10s to 1s
- [x] Add `tunnelQualityReportInterval = 30s` for DB write throttling
- [x] Update `storeResult` to cache in-memory every tick, write to DB only every 30s per tunnel
- [x] Add atomic `probing` flag to prevent overlapping `probeAll()` goroutine pile-up
- [x] Increase `maxWorkers` from 4 to 20

### 2. Service Monitor Checker (`go-backend/internal/health/checker.go`)
- [x] Add `serviceMonitorReportInterval = 30s` for DB write throttling
- [x] Add `latestResults` in-memory map and `lastDBWrite` map per monitor
- [x] Add `GetLatestCached()` method for real-time API reads
- [x] Add atomic `checking` flag to prevent overlapping `runChecks()` goroutine pile-up
- [x] Modify worker goroutines to always update in-memory cache, only write to DB every 30s

### 3. Service Monitor Limits (`go-backend/internal/monitoring/limits.go`)
- [x] Change `CheckerScanIntervalSec` default from 30 to 1
- [x] Change `WorkerLimit` default from 5 to 20
- [x] Change `MinIntervalSec` default from 30 to 1
- [x] Change `DefaultIntervalSec` default from 60 to 1

### 4. Monitoring API Handler (`go-backend/internal/http/handler/monitoring.go`)
- [x] Update `monitorServiceLatestResultsHandler` to prefer in-memory cached results from `healthCheck.GetLatestCached()`

### 5. Agent WebSocket Reporter (`go-gost/x/socket/websocket_reporter.go`)
- [x] Change `pingInterval` (metric reporting) from 5s to 1s

### 6. Frontend - Tunnel Monitor (`vite-frontend/src/pages/node/tunnel-monitor-view.tsx`)
- [x] Change `QUALITY_POLL_INTERVAL` from 10s to 1s 
- [x] Update detail view text: "自动探测中（每秒测试，30秒上报）"
- [x] Update list view text: "每秒探测 · 更新于 ..."

### 7. Frontend - Service Monitor (`vite-frontend/src/pages/node/monitor-view.tsx`)
- [x] Change `DEFAULT_SERVICE_MONITOR_LIMITS` defaults to match backend (1s intervals)
- [x] Change service monitor + latest results polling from 30s to 1s
- [x] Update info bar text: "每秒测试，30秒上报"
