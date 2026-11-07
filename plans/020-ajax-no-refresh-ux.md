# 020 AJAX No-refresh UX

## Objective

- Implement issue `#276` as a focused frontend UX improvement initiative, not a full data-layer rewrite.
- Keep the existing `axios + local React state + custom hooks` architecture, and extend it with polling, realtime hardening, and local state patching where it improves responsiveness.
- Deliver the work in phases so the highest-value improvements ship first: dashboard auto-refresh and node realtime resilience, then local list updates after mutations, then batch progress and search/filter polish.

## Non-goals

- Do not introduce `@tanstack/react-query`, SWR, or other new frontend data libraries for this issue.
- Do not rewrite page architecture, routing, or modal flows that already submit asynchronously without browser reloads.
- Do not require backend changes unless a batch-progress requirement cannot be met with the current API surface.
- Do not change the raw JWT auth convention used by `vite-frontend/src/api/network.ts`.

## Current State

- `vite-frontend/src/pages/node/use-node-realtime.ts` and `vite-frontend/src/pages/node.tsx` already provide websocket-driven node status, system info, and upgrade progress updates.
- `vite-frontend/src/pages/forward.tsx`, `vite-frontend/src/pages/tunnel.tsx`, `vite-frontend/src/pages/user.tsx`, and `vite-frontend/src/pages/node.tsx` already submit forms asynchronously, so the main remaining gap is consistency of post-submit local refresh behavior.
- `vite-frontend/src/pages/dashboard/use-dashboard-data.ts` currently fetches dashboard data only once on mount, so traffic charts and counters do not auto-refresh.
- Several mutation handlers still rely on page-level reload functions such as `loadData()`, `loadUsers()`, or `loadNodes()` instead of patching only the changed records.
- Batch progress UI exists for node upgrade but not for other batch actions such as forward and tunnel operations.

## Design Principles

- Prefer local state patching after successful mutations when the changed record set is known.
- Prefer targeted refetches over full-page refetches when the server is the source of truth for a small dependent dataset.
- Use polling only where realtime transport does not already exist.
- Pause or reduce background refresh work when the page is hidden to avoid unnecessary traffic.
- Keep UI feedback explicit: loading states, toast feedback, and visible progress for long-running batch actions.

## Checklist

- [x] Refactor dashboard data loading into reusable refresh callbacks in `vite-frontend/src/pages/dashboard/use-dashboard-data.ts`.
- [x] Add dashboard traffic polling with visibility-aware pause/resume and safe notification deduplication.
- [x] Harden node realtime reconnection behavior in `vite-frontend/src/pages/node/use-node-realtime.ts` and define a fallback refresh path if websocket recovery fails.
- [x] Add shared local-list patch helpers for replace/remove/upsert patterns used by page-level mutation handlers.
- [x] Convert forward create/edit/delete/service-toggle flows in `vite-frontend/src/pages/forward.tsx` from whole-page refetches to local or targeted updates where safe.
- [x] Convert tunnel create/edit/delete flows in `vite-frontend/src/pages/tunnel.tsx` from whole-page refetches to local or targeted updates where safe.
- [x] Convert user create/edit/delete and user-tunnel permission mutation flows in `vite-frontend/src/pages/user.tsx` to local or targeted updates where safe.
- [x] Extend batch action UX to show visible progress or staged feedback for forward and tunnel batch operations.
- [x] Normalize search/filter behavior and document where client-side instant filtering is appropriate versus where server-side pagination must remain authoritative.
- [ ] Run focused frontend verification and record the result in this plan after implementation.

## Implementation Plan

### Phase 1 - Dashboard auto-refresh and node realtime resilience

#### 1. Dashboard traffic/statistics auto-refresh

- Extract `loadPackageData()` and `loadAnnouncement()` in `vite-frontend/src/pages/dashboard/use-dashboard-data.ts` into stable callbacks so the hook can refresh data without re-running the whole mount sequence.
- Add a 5-second polling loop for package, flow, and chart data returned by `getUserPackageInfo()`.
- Keep announcement loading low-frequency or first-load only unless the API contract clearly expects live updates.
- Pause polling when `document.visibilityState !== "visible"`, then trigger an immediate refresh when the tab becomes visible again.
- Preserve current loading UX for first load, but use a silent refresh path for polling so the page does not flicker.

#### 2. Dashboard notification safety

- Audit `checkExpirationNotifications()` in `vite-frontend/src/pages/dashboard/use-dashboard-data.ts` so polling does not repeatedly emit expiration warnings.
- Continue using notification deduplication, but base it on stable expiration identifiers rather than every poll cycle.
- Ensure refreshes that only change traffic counters do not retrigger expiry toasts.

#### 3. Node realtime hardening

- Review `vite-frontend/src/pages/node/use-node-realtime.ts` reconnect logic, which currently stops after a fixed retry budget.
- Replace the hard stop with controlled backoff reconnect behavior, or explicitly trigger a degraded polling fallback once retry exhaustion is reached.
- If a fallback list refresh is introduced, merge incoming node metadata with existing `systemInfo`, `connectionStatus`, and upgrade-progress state so live metrics are not wiped during recovery.
- Keep the existing offline debounce behavior in `vite-frontend/src/pages/node/use-node-offline-timers.ts`.

### Phase 2 - Local mutation updates and partial refreshes

#### 4. Shared list-patching helpers

- Add small reusable helpers for common state operations such as:
  - replace one item by `id`
  - remove one or many items by `id`
  - upsert a created or updated item into an ordered list
  - preserve derived UI-only fields during server payload merges
- Keep these helpers local to the frontend codebase and avoid introducing a generic state-management abstraction.

#### 5. Forward page partial refresh conversion

- Target `vite-frontend/src/pages/forward.tsx` mutation handlers first because the page already contains some optimistic/local patterns.
- Preserve the current local behavior for service toggles, but review rollback handling so final UI state matches backend truth after success or failure.
- Change create/edit/delete flows to patch `forwards` state directly when the response payload is sufficient.
- Use targeted refetches only when an operation changes dependent datasets that are not reliably derivable from the local page state.
- Re-check grouped ordering, collapsed-state persistence, and selected-row state after local mutations.

#### 6. Tunnel page partial refresh conversion

- Update `vite-frontend/src/pages/tunnel.tsx` so create/edit/delete mutate `tunnels` state directly instead of always calling `loadData()`.
- Keep node reference data refresh separate from tunnel list refresh so a tunnel mutation does not force a full page data reload.
- Preserve existing drag-sort behavior and ensure local patching keeps `inx` and stored order consistent.

#### 7. User page partial refresh conversion

- Update `vite-frontend/src/pages/user.tsx` so create/edit/delete patch the `users` list when the current page can be updated safely.
- Update user-tunnel permission flows to patch `userTunnels` directly after assign, edit, remove, and flow-reset operations.
- Respect server-side pagination semantics for the user list; if the server response does not provide enough data for a safe local patch, use a targeted page refetch rather than a full multi-dataset refresh.
- Keep current modal and toast behavior unchanged unless the local update path exposes stale-state issues.

### Phase 3 - Batch progress UX and search/filter polish

#### 8. Batch progress UX

- Use the node upgrade progress model in `vite-frontend/src/pages/node.tsx` as the UI reference for long-running operations.
- Review `vite-frontend/src/pages/forward/batch-actions.ts` and tunnel batch handlers to determine whether current APIs expose enough intermediate state for real progress.
- If only final summary APIs are available, implement staged client-side progress feedback such as `processing X/Y`, current action label, success count, and failure count.
- If the UX requirement cannot be met without backend support, document the missing backend contract and split the work into frontend and backend follow-ups.

#### 9. Search and filter responsiveness

- Preserve instant client-side filtering on pages that already hold the authoritative dataset locally, including node, tunnel, and forward pages.
- Audit the user page separately because it depends on server-side pagination and keyword search.
- If user-page instant filtering is desired, choose one of two explicit strategies:
  - keep server-side pagination authoritative and add debounce for keyword-triggered requests, or
  - load a larger local dataset only if product requirements accept the cost.
- Do not silently mix partial client filtering with incomplete paginated datasets.

## Risks and Mitigations

- Repeated dashboard polling may spam expiry toasts.
  - Mitigation: deduplicate notifications based on expiration identity and only emit on meaningful state changes.
- Node recovery refreshes may wipe websocket-derived metrics.
  - Mitigation: merge fetched node metadata into existing live state instead of replacing the whole record blindly.
- Local mutation patching may desynchronize grouped, sorted, or selected views.
  - Mitigation: patch canonical source arrays first, then recompute derived memoized groupings from state.
- Batch APIs may not expose progress details.
  - Mitigation: implement client-side staged progress where possible and document backend gaps where not.
- User-page local updates may conflict with pagination semantics.
  - Mitigation: prefer targeted page refetch over unsafe optimistic filtering or cross-page list mutation.

## Verification Plan

- Dashboard:
  - Open `dashboard` and confirm traffic counters and chart data refresh at least once every 5 seconds without manual reload.
  - Confirm hidden-tab pause and visible-tab immediate refresh behavior.
  - Confirm expiry toasts do not repeat on every polling cycle.
- Nodes:
  - Confirm websocket-driven online/offline transitions still work.
  - Simulate websocket interruption and verify reconnect or fallback refresh behavior.
  - Confirm recovery does not clear existing live metrics unexpectedly.
- Forwards, tunnels, users:
  - Create, edit, delete, enable, disable, and reset flows without browser reload.
  - Confirm the affected rows update immediately and other unrelated rows stay stable.
  - Confirm selection state, ordering, and modal close behavior remain correct after local patching.
- Batch actions:
  - Confirm visible progress or staged status feedback exists during long-running operations.
  - Confirm success and failure summaries remain accurate after completion.
- Build:
  - Run `cd vite-frontend && npm run build`.

## Rollout Notes

- Ship Phase 1 first because it matches the issue approval priority and provides the clearest user-visible gain.
- Keep each phase in reviewable commits so regressions in local list patching can be isolated quickly.
- If backend support becomes necessary for real batch progress, land the frontend scaffolding separately and track the backend dependency explicitly.

## Test Record

- Command: `cd vite-frontend && npm install`
- Result: passed.
- Command: `cd vite-frontend && npm run build`
- Result: passed.
