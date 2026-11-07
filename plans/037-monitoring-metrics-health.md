# 037 - Monitoring: Node Metrics + Service Health Checks

## Context
This worktree introduces a monitoring feature set:
- Node runtime metrics streamed via WebSocket (agent -> panel -> admin clients)
- Metrics ingestion + retention in panel DB
- Service monitoring (TCP/ICMP checks only) + result storage
- Frontend monitor view (charts + monitor CRUD + run + results)
- Dedicated monitor page (`/monitor`) that works for authorized non-admin users

The initial implementation landed without a plan doc and had several correctness issues (API JSON shape mismatch, wrong time units, contract test hangs under SQLite single-connection mode, etc.). This plan documents what exists, what was fixed, and what is still incomplete/needs decisions.

## Goals
- Metrics endpoints return stable JSON fields matching frontend types.
- Contract tests cover metrics + monitor CRUD and are deterministic.
- WebSocket metric messages update node cards correctly.
- Monitoring view queries the correct time range and renders timestamps correctly.
- go-gost/x unit tests do not depend on a local config.json.

## Non-goals (for this plan)
- A full monitor scheduling system (jitter/backoff/concurrency budgets/per-monitor next-run) beyond the current simple loop.
- Building a full alerting pipeline (notifications, thresholds, paging).

## Current Status (as of this worktree)
- Backend models updated with JSON tags for monitoring structs.
- Handler endpoints for metrics + service monitors added.
- Metrics ingestion service implemented with buffering + retention pruning.
- Health checker implemented (panel-side when `nodeId == 0`; node-executed via WS when `nodeId > 0`) and background jobs wired.
- Frontend monitor view added; build passes.
- Contract tests for monitoring added.
- Monitoring endpoints are accessible by admin users and non-admin users explicitly authorized by admin (via `monitor_permission`).
- Frontend exposes monitoring via a dedicated `/monitor` page; admin can grant/revoke monitoring permission from the User permissions modal.
- Frontend includes tunnel metrics charts (backed by `/api/v1/monitor/tunnels` list + `/api/v1/monitor/tunnels/:id/metrics`).

## Known Semantics Gaps (need decisions)
- `service_monitor.intervalSec` is best-effort (checker ticks every 30s; intervals shorter than that won't run faster).
- `service_monitor_result.success` is stored as int (0/1). Frontend currently treats it as number; decide if API should expose boolean.

## Admin Authorization API
Monitoring permission management (admin-only):
- `GET /api/v1/monitor/permission/list`
- `POST /api/v1/monitor/permission/assign` body: `{ "userId": 123 }`
- `POST /api/v1/monitor/permission/remove` body: `{ "userId": 123 }`

## Checklist

### Phase 1: Correctness + Contracts
- [x] Align monitoring JSON response fields with frontend/contract expectations (add json tags or DTO mapping).
- [x] Fix frontend monitor time range query (use ms start/end; avoid `start=60`).
- [x] Fix frontend timestamp rendering (treat timestamp as UnixMilli).
- [x] Fix node realtime metric speed field compatibility (support snake_case speed fields).
- [x] Fix SQLite contract hang by ensuring tunnel-entry precheck uses tx-safe DB reads (no nested connection acquisition).
- [x] Ensure monitoring contract tests pass.

### Phase 2: Semantics Alignment (Decide + Implement)
- [x] Decide "service monitors run where":
  - Option B: node-executed when `nodeId > 0` (chosen)
- [ ] Define interval semantics:
  - Per-monitor next-run scheduling vs global scan loop
  - Backoff on failures
  - Maximum monitors + runtime cost guardrails
- [ ] Standardize API type for `success`:
  - Keep int for backward compatibility, or
  - Return boolean in API responses (DTO) while storing int in DB

### Phase 2.1: Partial Implementation (No Semantics Decision Yet)
- [x] Honor `intervalSec` best-effort in panel-side checker (min cadence still bound by global loop).

### Phase 2.2: Node-Executed Checks
- [x] Add a WebSocket command for node-executed monitor checks (`ServiceMonitorCheck`).
- [x] Panel health checker dispatches checks to the specified node when `nodeId > 0`.
- [x] Allow unrestricted targets by policy; restrict monitoring endpoints to admin + explicitly authorized users.
- [x] Remove HTTP checks; service monitoring supports only `tcp` and `icmp`.

### Phase 3: Hardening + Performance
- [x] Add query limits/guards for metrics endpoints (max range, max rows) to avoid accidental full-history pulls.
- [ ] Consider indexing review and retention configurability (env or config table).
- [ ] Review concurrency: ingestion buffer flush goroutine spawning and DB write pressure.
- [x] Add minimal UI affordances: time range selector, empty/error states, and service monitor run/results UI.
- [x] Ensure monitoring UI works for authorized non-admin users (dedicated `/monitor` page; no reliance on admin-only `/node/*`).

### Phase 4: Hygiene
- [x] Add `.entire/metadata/` to `.gitignore` (should never be committed).
- [ ] Add a short developer note in docs/README if needed (API endpoints + semantics).

## Test Plan
Backend:
```bash
cd go-backend && go test ./... -count=1
cd go-backend && go test ./tests/contract -count=1 -timeout 120s
```

Agent fork:
```bash
cd go-gost/x && go test ./... -count=1
```

Frontend:
```bash
cd vite-frontend && npm run build
```

## Notes
- Node-executed checks can be used for internal probing by design; access is restricted to administrators.
