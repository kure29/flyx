# Monitor Nezha Redesign

## Summary
Redesign the monitor overview to resemble Nezha dashboard. Phase 1 introduced ServerCard grid. Phase 2 hides node metrics from the overview and adds a detail view with charts and service monitor latency sparklines.

## Objectives
- [x] Analyze `monitor-view.tsx` layout boundaries.
- [x] Create `ServerCard` utilizing `Progress` component.
- [x] Render metrics (CPU, RAM, Disk, System Load, Connections, Network speeds).
- [x] Implement robust real-time updates and seamless state linkage via existing hooks.
- [x] Adjust layout positioning for impact at top-of-page.
- [x] Hide node metrics from overview — click node card to enter detail view.
- [x] Detail view: back button + node header + realtime KPI cards.
- [x] Detail view: node metrics chart (CPU/Memory/Disk/Network/Load/Connections).
- [x] Detail view: tunnel traffic chart.
- [x] Detail view: service monitors rendered as latency sparkline charts (Nezha-style).
- [x] Service monitor cards include status dot, type chip, target, interval, and dropdown actions.

## Technical Details
- Phase 1: Injected a `ServerCard` inline component with progress bars and real-time metrics.
- Phase 2: Added `detailNodeId` state for drill-down navigation. Grid view shows only summary bar + clickable server cards. Detail view shows KPI summary cards, node metrics chart (reusing existing recharts setup), tunnel traffic chart, and service monitors as a responsive grid of cards each containing a latency-over-time sparkline chart. Reduced file from 2108 to 1799 lines by consolidating the old overview card into the summary bar.

## Status: Complete
