# Monitoring Retention And Storage Display Design

## Goal

Add an administrator-facing configuration for monitoring data retention and display the current database storage usage in the configuration page.

## Scope

- Add a single config key: `monitor_retention_days`.
- Default retention is `7` days.
- Apply the retention window uniformly to:
  - `node_metric`
  - `tunnel_metric`
  - `service_monitor_result`
  - `tunnel_quality`
- Show database usage on the config page as a read-only operational value.

## Non-Goals

- No per-table retention settings.
- No manual purge button.
- No database vacuum/compaction action.
- No frontend test framework changes.

## Backend Design

### Retention Config

- Store `monitor_retention_days` in `vite_config`, consistent with existing site settings.
- Accept integer values from `1` through `3650`.
- Missing or invalid stored values fall back to `7` days.
- `normalizeAndValidateConfigValue` rejects invalid user-submitted values so bad config does not get saved through the API.

### Cleanup Flow

- `metrics.IngestionService.pruneMetrics()` reads `monitor_retention_days` from the repository each hourly cleanup cycle.
- The computed cutoff is used for `node_metric`, `tunnel_metric`, and `service_monitor_result`.
- `tunnel_quality` uses the same retention config.
- `tunnel_quality` cleanup must run even when real-time tunnel quality probing is disabled; disabling probing should stop new probe writes, not stop cleanup.

### Database Storage API

- Add an admin-only API endpoint for storage summary, for example `/api/v1/system/storage`.
- Response fields:
  - `dbType`: `sqlite` or `postgres`
  - `databaseSizeBytes`: raw byte count
  - `databaseSizeText`: human-readable formatted size
- SQLite implementation reports the DB file size and includes `-wal` and `-shm` sidecar files when present.
- PostgreSQL implementation uses `pg_database_size(current_database())`.
- If size cannot be determined, return an API error rather than a misleading zero.

## Frontend Design

- Add `monitor_retention_days` to the config page.
- Label: `监控数据保留天数`.
- Description: `统一清理节点指标、隧道流量、服务监控结果和隧道质量历史；默认 7 天。`
- Use a regular numeric input through the existing config rendering path.
- Fetch database storage summary when the config page loads.
- Display a read-only card/row named `数据库占用` with `databaseSizeText`.
- If fetching fails, show `获取失败` and keep config editing usable.

## Error Handling

- Invalid retention values return a validation error on save.
- Cleanup logs individual prune failures and continues with other tables, matching existing monitoring cleanup behavior.
- Storage summary failures are non-blocking in the frontend.

## Testing

- Backend unit tests for retention config parsing and validation.
- Backend tests proving custom retention is used by monitoring cleanup.
- Backend API/repository test for SQLite storage size returning a non-negative byte count and formatted text.
- Run `go test ./...` in `go-backend`.
- Run `pnpm run build` in `vite-frontend`.
