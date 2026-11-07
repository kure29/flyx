# 038 - Monitoring Bug Fixes + Optimizations

## Context
Monitoring in FLVX currently spans:
- Agent -> panel WebSocket realtime system metrics (CPU/mem/disk/net/load/conns)
- Panel-side ingestion + retention pruning (`node_metric`)
- Service monitors (TCP/ICMP) with scheduled checks + stored results
- Frontend monitor page (`/monitor`) with charts + monitor CRUD/run/results

While the feature set works end-to-end, there are a few correctness footguns and a couple of obvious performance hot spots (agent-side sampling cost and frontend N+1 polling patterns).

## Goals
- Service monitor updates do not accidentally clear `nodeId` / `enabled` when fields are omitted.
- Checker cadence is explicit (intervals below the scan cadence are clamped / best-effort).
- Reduce frontend requests for service monitor status (avoid per-monitor polling).
- Reduce agent sampling overhead and DB write volume without breaking UI expectations.
- Avoid misclassifying arbitrary JSON as a metric message on the WS channel.

## Non-goals
- A full scheduler (per-monitor next-run queue, jitter/backoff, concurrency budgets).
- Alerting/notifications.
- Implementing full tunnel-metrics ingestion (connections/errors/latency) beyond current endpoints.

## Checklist

### Phase 1: Backend Correctness + Hardening
- [x] Make `/api/v1/monitor/services/update` treat `nodeId` and `enabled` as optional fields (no accidental zeroing).
- [x] Clamp `intervalSec` to a minimum that matches the checker scan cadence (and apply the same clamp in the checker).
- [x] Add `GET /api/v1/monitor/services/latest-results` returning the latest result per monitor (for frontend list rendering).
- [x] WS metric parsing: only treat messages as metrics when they look like a system-metric payload.

### Phase 2: Frontend UX + Request Reduction
- [x] Fix “立即检查” toast severity (failure should be an error toast).
- [x] Use `latest-results` endpoint to render service monitor status without N+1 polling.
- [x] Add a small hint when chart data is truncated by backend row limits.

### Phase 3: Agent Sampling Optimizations
- [x] Reduce default WS metric send interval (2s -> 5s).
- [x] Make CPU sampling non-blocking and cache heavy metrics (e.g. connection counts) to reduce per-sample cost.

## Test Plan
Backend:
```bash
cd go-backend && go test ./... -count=1
```

Agent fork:
```bash
cd go-gost/x && go test ./... -count=1
```

Frontend (best-effort in this environment):
```bash
cd vite-frontend && npm run build
```

## Rollout Notes
- Agent sampling interval change reduces metric resolution and DB growth; charts remain usable and realtime UI remains responsive.
- Existing monitors with very small `intervalSec` are best-effort; effective cadence remains bounded by the checker scan loop.
