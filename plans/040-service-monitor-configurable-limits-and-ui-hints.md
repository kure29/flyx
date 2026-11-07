# 040 - Service Monitor Limits Config + UI Hints

## Goal
Make service monitor interval/timeout constraints configurable (instead of hard-coded clamps) and make the UI clearly communicate the effective limits.

## Current Pain
- Backend clamps `intervalSec` and `timeoutSec` with hard-coded constants.
- Checker scan cadence is also hard-coded, so users can set values that will never be honored.
- Frontend form does not explain allowed ranges or why values may change.

## Approach
- Add frontend-configurable limits stored in `vite_config` (with safe defaults matching current behavior).
- Backend always normalizes using the configured limits.
- Expose the current limits via a monitoring endpoint so the UI can render accurate hints.
- Frontend shows min/max and validates before submit.
- Admin can edit the limits on `/config`.

## Config Keys (vite_config)
- `service_monitor_checker_scan_interval_sec` (default: 30)
- `service_monitor_min_interval_sec` (default: 30; auto-raised to at least scan interval)
- `service_monitor_default_interval_sec` (default: 60)
- `service_monitor_min_timeout_sec` (default: 1)
- `service_monitor_default_timeout_sec` (default: 5)
- `service_monitor_max_timeout_sec` (default: 60)

## Checklist
Backend:
- [x] Introduce shared `ServiceMonitorLimits` config loader.
- [x] Use limits for create/update normalization.
- [x] Use limits in checker (scan interval + timeout clamp).
- [x] Add `GET /api/v1/monitor/services/limits` to return current limits.

Frontend:
- [x] Fetch limits once and render input descriptions.
- [x] Validate interval/timeout client-side and show inline errors.
- [x] Add `/config` items to edit the `vite_config` keys.

Verification:
- [x] `cd go-backend && go test ./... -count=1`
- [x] `cd vite-frontend && npm run lint && npm run build`
