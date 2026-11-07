# 041 - Monitoring Reliability, Realtime, and UX Hardening

## Context
Current monitoring support in FLVX already covers three major areas:
- Node runtime metrics from agent WebSocket telemetry, buffered into `node_metric`, exposed by `/api/v1/monitor/nodes*`, and rendered on `/monitor`.
- Tunnel metrics derived from `/flow/upload`, stored in `tunnel_metric`, exposed by `/api/v1/monitor/tunnels*`, and rendered on `/monitor`.
- Service monitoring for `tcp` and `icmp`, including CRUD, scheduled checks, manual run, history, and non-admin authorization via `monitor_permission`.

The feature set is usable, but the audit found several correctness, reliability, and UX gaps:
- The monitor page is not truly realtime and can lag DB ingestion by tens of seconds.
- Tunnel metrics are only partially implemented and are not aggregated correctly for multi-node tunnels.
- Some monitoring writes fail silently, which can hide data-loss and retention issues.
- Service monitor scheduling is functional but too naive for larger monitor sets and restart scenarios.
- The monitoring UI exposes incomplete semantics, weak freshness cues, and inconsistent permission/error affordances.
- Several monitoring endpoints and edge cases still lack direct automated coverage.

This plan collects all currently known monitoring follow-up work into one implementation document.

## Goals
- Make node monitoring data freshness explicit and reduce stale or misleading chart behavior.
- Make tunnel metrics correct for multi-node tunnels and align schema/query/UI semantics.
- Harden service monitor scheduling, persistence, and cleanup behavior.
- Improve observability so monitoring ingestion and result writes never fail silently.
- Upgrade the monitoring UI so operators can understand status, freshness, scope, and failures at a glance.
- Expand automated coverage for all monitoring APIs and the highest-risk aggregation/scheduler cases.

## Non-goals
- Add a full alerting or notification pipeline.
- Add brand-new monitor protocols beyond the current `tcp` and `icmp` scope.
- Build a large analytics dashboard outside the existing monitoring page structure.
- Introduce frontend test infrastructure for broad component/unit testing unless required by an implementation step.

## Audit Findings To Address
- Node metrics on `/monitor` are DB-polled rather than realtime-streamed.
- Node metrics are buffered for 30s, so charts can lag behind observed node state.
- Tunnel metrics are stored per `(tunnel_id, node_id, timestamp)` but queried and rendered as if they were already tunnel-level aggregates.
- Tunnel metric minute-bucket upsert uses update-then-insert without uniqueness guarantees.
- Tunnel metrics only populate `bytesIn` and `bytesOut`; `connections`, `errors`, and `avgLatencyMs` are placeholder values.
- Node/tunnel/service-monitor writes can fail silently due to ignored errors.
- Service monitor scheduler is serial and uses in-memory `lastRun`, causing restart skew and slow-monitor head-of-line blocking.
- Deleting a service monitor does not clean up related historical results.
- `expectedCode` exists on the model but is not implemented in behavior or UX.
- The monitoring page/menu is exposed before permission is known, leading to avoidable denied-entry UX.
- Service monitor UI does not clearly show latest result freshness, last check time, or whether a displayed row is stale.
- Chart labels and units are not operator-friendly for long time windows and network-heavy views.
- Monitoring API coverage is incomplete for list, permission, limits, latest-results, multi-node tunnel aggregation, and concurrency paths.

## Design

### 1. Node Monitoring Freshness and Realtime Model
- Keep the existing WebSocket node telemetry stream as the source of live state.
- Preserve DB-backed metrics queries for historical charts, but explicitly separate them from live cards/status.
- On `/monitor`, add a lightweight realtime subscription path reusing the existing admin WebSocket feed already used by the node page.
- Use realtime events for:
  - node online/offline state,
  - a small “latest value” strip or summary above charts,
  - freshness timestamp display.
- Keep charts historical and DB-backed by default, but add a visible freshness hint such as:
  - `历史图表，最近落库延迟约 0-30s`, or
  - `最近入库时间: ...`.
- Do not remove buffered ingestion immediately; first make lag transparent in UI and observable in logs/metrics.
- Optional second-step optimization: reduce flush interval or add a bounded flush-on-latest-view mode if DB pressure remains acceptable.

### 2. Tunnel Metrics Data Model and Query Semantics
- Decide and document one API contract:
  - `GET /api/v1/monitor/tunnels/:id/metrics` must return tunnel-level aggregated series for the selected time range, not raw per-node rows.
- Keep storage per `(tunnel_id, node_id, timestamp)` because it is useful for future drill-down.
- Change query behavior so the tunnel metrics endpoint aggregates rows by timestamp across all nodes for the tunnel:
  - `SUM(bytes_in)`,
  - `SUM(bytes_out)`,
  - `SUM(connections)`,
  - `SUM(errors)`,
  - `AVG` or weighted-average strategy for latency, if latency is later implemented.
- Return a single point per timestamp to the frontend.
- If future node drill-down is needed, add a separate endpoint rather than mixing per-node rows into the current chart API.

### 3. Tunnel Metric Upsert Safety
- Replace the current update-then-insert fallback with a uniqueness-backed upsert strategy.
- Add a unique index on `(tunnel_id, node_id, timestamp)`.
- Implement DB-safe upsert behavior compatible with SQLite and PostgreSQL via GORM clauses or equivalent dialect-safe SQL.
- Preserve additive semantics for traffic counters inside the bucket.
- Add concurrency coverage proving that parallel uploads for the same bucket do not create duplicate rows.

### 4. Tunnel Metric Scope Clarification
- Short term: make the UI and API explicitly traffic-only where the backend only has traffic truth.
- Remove or hide unsupported tunnel metric modes from the current UX until real data exists.
- Do not expose zero-filled placeholders as if they were valid telemetry.
- Keep schema fields if future support is planned, but label them as unimplemented in code comments and avoid rendering them as live features.

### 5. Service Monitor Scheduler Hardening
- Replace the current fully serial best-effort loop with bounded concurrency:
  - retain a global scan loop or next-run calculation,
  - collect monitors due for execution,
  - execute them with a configurable worker limit,
  - avoid one slow node/target delaying all others.
- Move scheduling semantics from pure in-memory `lastRun` toward persisted or history-derived next-run safety:
  - on restart, do not fire an uncontrolled burst for all monitors if they just ran;
  - use latest persisted result timestamp or a persisted scheduler state to calculate due-ness.
- Keep interval clamping behavior aligned with configured limits.
- Continue supporting local panel execution for `tcp` and node execution for `tcp`/`icmp`.

### 6. Service Monitor Data Lifecycle
- Define monitor deletion semantics explicitly:
  - either cascade-delete historical `service_monitor_result` rows when a monitor is deleted, or
  - retain them intentionally and exclude orphan rows from latest/list endpoints.
- Preferred approach: delete associated results with the monitor so the UI/API model stays simple.
- Either implement `expectedCode` fully or remove it from the model/API surface for now.
- Because service monitoring currently supports only `tcp` and `icmp`, and no HTTP checks are implemented, `expectedCode` should likely be removed from the data model/API until a real HTTP monitor exists.

### 7. Observability and Failure Handling
- Stop swallowing monitoring persistence errors.
- For all node/tunnel/service-monitor writes:
  - log structured errors with entity identifiers and operation names,
  - increment internal counters if an existing metrics/logging primitive exists,
  - keep request/loop behavior resilient, but make failure visible.
- Apply this to:
  - node metric batch flush,
  - tunnel metric bucket writes,
  - scheduled service monitor result writes,
  - manual service monitor result writes,
  - pruning failures.
- Avoid user-facing hard failures for background ingestion, but surface operational diagnostics in logs.

### 8. Monitoring UI Semantics and Navigation
- Keep `/monitor` accessible only to authenticated users, but improve pre-entry affordances:
  - hide or disable the navigation item for users without monitor permission when role/permission data is known,
  - or show a locked state with explanation instead of allowing a full denied page transition.
- Preserve the backend permission check as the source of truth.
- On the page itself, upgrade semantics:
  - distinguish `enabled/disabled` from `healthy/unhealthy` in the service monitor table,
  - show `last checked at`,
  - show `latest result` separately from monitor switch state,
  - show whether the latest displayed result is stale.
- Add an at-a-glance monitoring summary near the top:
  - online/offline node counts,
  - monitors healthy/unhealthy/disabled counts,
  - latest data freshness text.

### 9. Monitoring UI Readability Improvements
- Improve chart axis labeling for long ranges:
  - use date + time formatting for 24h windows,
  - keep shorter labels for short ranges.
- Format bytes and rates into human-readable units (`KB/s`, `MB/s`, `GB`) instead of raw integers.
- Expose clear empty states and fetch-error states instead of silent failures.
- Make tunnel charts explicitly say `流量趋势` if only traffic is supported.
- Show `statusCode` in the results modal only if the corresponding monitor type ever uses it; otherwise omit it.

### 10. API and Test Coverage Expansion
- Add contract coverage for:
  - `GET /api/v1/monitor/nodes`,
  - `GET /api/v1/monitor/tunnels`,
  - `GET /api/v1/monitor/services/latest-results`,
  - `GET /api/v1/monitor/services/limits`,
  - monitor permission list/assign/remove endpoints.
- Add backend tests for:
  - multi-node tunnel aggregation returning one point per timestamp,
  - tunnel upsert concurrency safety,
  - service monitor restart/due scheduling semantics,
  - service monitor delete cleanup behavior,
  - background write failure logging where practical.
- Keep existing build/test targets green for backend, agent, and frontend.

## Checklist

### Phase 1: Correctness Fixes
- [x] Aggregate `GET /api/v1/monitor/tunnels/:id/metrics` by timestamp across all node rows for the selected tunnel.
- [x] Add a unique index for tunnel metric minute buckets and replace the race-prone update-then-insert flow with safe upsert logic.
- [x] Stop exposing unsupported tunnel metric dimensions (`connections`, `errors`, `latency`) as active UI features while the backend still stores placeholders.
- [x] Define and implement service monitor deletion cleanup so history does not leave orphaned result rows.
- [x] Remove or fully implement `expectedCode`; do not keep dead monitoring fields in the live API/model contract.

### Phase 2: Reliability and Scheduling
- [x] Replace serial service monitor execution with bounded-concurrency execution for due monitors.
- [x] Persist or derive service monitor next-run behavior so process restarts do not trigger uncontrolled immediate reruns.
- [x] Ensure monitor scheduler semantics remain aligned with configured min interval and checker scan cadence.
- [x] Add structured logging for all monitoring persistence failures and prune failures.
- [x] Audit all ignored monitoring write errors and convert them into visible operational diagnostics.

### Phase 3: Realtime and Freshness UX
- [x] Reuse the existing admin WebSocket stream on `/monitor` for live node status and latest-value freshness indicators.
- [x] Add visible chart freshness metadata so users know historical charts are DB-backed and may lag ingestion.
- [x] Decide whether to reduce node metric flush interval after instrumentation confirms acceptable DB impact (decision: keep 30s default for now; revisit after observing DB write rate and UI staleness in production).
- [x] Add a monitoring summary strip showing online nodes, unhealthy monitors, and latest data time.

### Phase 4: Monitoring Page UX Cleanup
- [x] Separate service monitor switch state (`启用/禁用`) from probe health (`成功/失败`).
- [x] Add `last checked at` to the service monitor list and results modal context.
- [x] Mark stale results clearly when the latest result is older than the configured interval budget.
- [x] Format traffic and speed values in human-readable units instead of raw bytes.
- [x] Improve chart time labels for 24h windows to include date context.
- [x] Replace silent frontend fetch failures with explicit inline error or toast handling.
- [x] Rename or relabel tunnel chart UI to make its current scope unambiguous.

### Phase 5: Permission and Navigation UX
- [x] Avoid showing a fully interactive monitor nav entry to users who lack monitoring permission once permission state is known.
- [x] Preserve backend authorization as the final gate and keep denied responses intact.
- [x] Improve denied-state copy so users understand whether they need admin grant vs role change.

### Phase 6: Automated Coverage
- [x] Add contract tests for monitor node list, tunnel list, latest service monitor results, limits, and permission endpoints.
- [x] Add contract or repository tests for multi-node tunnel aggregation correctness.
- [x] Add concurrency tests for tunnel metric upsert safety.
- [x] Add scheduler tests covering restart behavior, due monitor selection, and slow-monitor isolation.
- [x] Keep existing monitoring contract tests passing after all changes.

## Implementation Notes
- Prefer backward-compatible API changes where possible, but favor correctness over preserving misleading tunnel metric semantics.
- Do not introduce a fake realtime chart if the data source remains DB-backed; label it honestly.
- If permission visibility requires an extra frontend capability call, keep it lightweight and cacheable.
- If schema/index changes are introduced, they must remain compatible with both SQLite and PostgreSQL.

## Final Verification Targets
- [x] `GET /api/v1/monitor/tunnels/:id/metrics` returns one aggregated point per timestamp even when multiple nodes report the same tunnel bucket.
- [x] Parallel `/flow/upload` calls for the same tunnel/node/minute do not create duplicate bucket rows.
- [x] `/monitor` clearly distinguishes live state from historical persisted charts and surfaces data freshness to the operator.
- [x] Service monitor list shows enabled state, latest health result, latest check time, and stale-state semantics correctly.
- [x] Deleting a service monitor no longer leaves dangling historical data in list-facing APIs.
- [x] Monitoring ingestion/result write failures are visible in logs and no longer fail silently.
- [x] Non-admin users without monitoring permission do not get a confusing monitor-entry experience, while granted users continue to access monitoring successfully.
- [x] Backend monitoring contract tests pass.
- [x] New repository/scheduler tests pass.
- [x] `cd go-backend && go test ./... -count=1` passes.
- [x] `cd go-gost/x && go test ./socket/... -count=1` passes.
- [x] `cd vite-frontend && npm run build` passes.
