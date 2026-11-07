# 039 - Monitoring: Tunnel Metrics Ingestion

## Context
The `/monitor` UI includes tunnel metric charts backed by:
- `GET /api/v1/monitor/tunnels` (list)
- `GET /api/v1/monitor/tunnels/:id/metrics` (timeseries)

The backend has the `tunnel_metric` table + query endpoints, but there is no production code path that writes tunnel metrics. As a result, tunnel charts are typically empty.

## Goal
Persist tunnel traffic timeseries based on agent flow uploads (`POST /flow/upload`).

## Scope
- Write `tunnel_metric` rows from flow uploads.
- Keep write volume bounded (aggregate per minute).
- Provide contract coverage that a flow upload creates tunnel metrics.

## Non-goals
- Populate connections/errors/latency for tunnel metrics (remain 0 for now).
- A full aggregation pipeline across multiple nodes per tunnel at query time.

UI note:
- The tunnel chart only exposes the Traffic view for now; other tabs are hidden.

## Design
- Agent reports per-service traffic deltas via `/flow/upload` with items `{n,u,d}`.
- Backend derives `forward_id` from service name (`<forwardID>_<userID>_<userTunnelID>[...suffix]`).
- Map `forward_id -> tunnel_id` in batch.
- Aggregate per `(node_id, tunnel_id, minute_bucket)` and upsert into `tunnel_metric` using an UPDATE-then-INSERT fallback.

## Checklist
- [x] Add repository helper: map forward IDs to tunnel IDs.
- [x] Add repository helper: upsert per-minute tunnel metric buckets.
- [x] Extend `/flow/upload` handler to record tunnel metrics from incoming items.
- [x] Add contract test verifying flow upload produces tunnel metrics.
- [x] Run backend tests.

## Test Plan
```bash
cd go-backend && go test ./... -count=1
```
