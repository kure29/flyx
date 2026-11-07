import type { ServiceMonitorResultApiItem } from "@/api/types";

type Result = ServiceMonitorResultApiItem;

export interface MonitorHistory {
  monitorId: number;
  rangeMs?: number;
  limit?: number;
  results: Result[];
}

export interface MonitorResultsCache {
  latest: Record<number, Result>;
  chart: MonitorHistory | null;
  modal: MonitorHistory | null;
}

// Match the existing API's range-query ceiling; count queries use their limit.
const MAX_HISTORY_ROWS = 5000;
const newestFirst = (a: Result, b: Result) =>
  b.timestamp - a.timestamp || b.id - a.id;

function boundHistory(history: MonitorHistory, rows: Result[], now: number) {
  const byId = new Map<number, Result>();
  const cutoff = history.rangeMs != null ? now - history.rangeMs : -Infinity;

  for (const row of rows) {
    if (row.monitorId !== history.monitorId || row.timestamp < cutoff) continue;
    if (!byId.has(row.id)) byId.set(row.id, row);
  }

  return {
    ...history,
    results: [...byId.values()]
      .sort(newestFirst)
      .slice(
        0,
        history.rangeMs != null ? MAX_HISTORY_ROWS : (history.limit ?? 100),
      ),
  };
}

export function mergeMonitorResults(
  cache: MonitorResultsCache,
  rows: Result[],
  now: number,
  replaceLatest = false,
): MonitorResultsCache {
  const latest: Record<number, Result> = replaceLatest
    ? {}
    : { ...cache.latest };

  for (const row of rows) {
    if (row.monitorId <= 0) continue;
    const previous = latest[row.monitorId] ?? cache.latest[row.monitorId];

    latest[row.monitorId] =
      previous && newestFirst(previous, row) <= 0 ? previous : row;
  }
  const update = (history: MonitorHistory | null) => {
    if (!history) return null;
    const incoming = latest[history.monitorId];

    return boundHistory(
      history,
      incoming ? [incoming, ...history.results] : history.results,
      now,
    );
  };

  return { latest, chart: update(cache.chart), modal: update(cache.modal) };
}

export function storeMonitorHistory(
  cache: MonitorResultsCache,
  target: "chart" | "modal",
  query: Omit<MonitorHistory, "results">,
  rows: Result[],
  now: number,
): MonitorResultsCache {
  const next = mergeMonitorResults(cache, rows, now);
  const previous = cache[target];
  const sameQuery =
    previous?.monitorId === query.monitorId &&
    previous.rangeMs === query.rangeMs &&
    previous.limit === query.limit;
  const latest = next.latest[query.monitorId];

  return {
    ...next,
    [target]: boundHistory(
      { ...query, results: [] },
      [
        ...rows,
        ...(sameQuery ? previous.results : []),
        ...(latest ? [latest] : []),
      ],
      now,
    ),
  };
}
