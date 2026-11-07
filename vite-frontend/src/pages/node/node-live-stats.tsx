import type { NodeSystemInfo } from "@/pages/node/system-info";

function bytes(v: number): string {
  const units = ["B", "K", "M", "G", "T", "P"];
  let i = 0;

  while (v >= 1024 && i < units.length - 1) {
    v /= 1024;
    i++;
  }

  return `${v >= 100 || i === 0 ? Math.round(v) : v.toFixed(1)}${units[i]}`;
}

const capacity = (used?: number, total?: number) =>
  total ? `${bytes(used ?? 0)} / ${bytes(total)}` : "";

/**
 * A node's live resources on its list row: CPU, memory and disk, speed
 * and this month's traffic. "row" sits at the end of a wide row, "footer"
 * under the name on narrow screens.
 */
export function NodeLiveStats({
  info,
  month,
  variant,
}: {
  info: NodeSystemInfo | null;
  /** This month's [in, out] bytes. */
  month?: [number, number];
  variant: "row" | "footer";
}) {
  const up = info ? (info.netOutSpeed ?? info.uploadSpeed) : undefined;
  const down = info ? (info.netInSpeed ?? info.downloadSpeed) : undefined;
  const rate = (v?: number) => (v === undefined ? "—" : `${bytes(v)}/s`);
  const total = month ? bytes(month[0] + month[1]) : "—";
  const monthHint =
    new Date().getTimezoneOffset() % 60 !== 0
      ? "按本地时区统计；跨月的旧小时记录按时长估算"
      : "按本地时区统计";
  const split = month ? `↑${bytes(month[1])} ↓${bytes(month[0])}` : "";

  // List rows: one quiet line, and nothing for data the node has not sent.
  const usage = info
    ? (
        [
          ["CPU", info.cpuUsage, info.cpuCores ? `${info.cpuCores} 核` : ""],
          ["内存", info.memoryUsage, capacity(info.memUsed, info.memTotal)],
          ["磁盘", info.diskUsage, capacity(info.diskUsed, info.diskTotal)],
        ] as const
      ).filter(([, v]) => v !== undefined)
    : [];
  const usageLine = usage.length > 0 && (
    <span className="whitespace-nowrap">
      {usage.map(([label, v, hint], i) => (
        <span key={label} title={hint || undefined}>
          {i > 0 && " · "}
          {label}{" "}
          <span
            className={
              v! >= 85 ? "text-danger" : v! >= 60 ? "text-warning-600" : ""
            }
          >
            {Math.round(v!)}%
          </span>
        </span>
      ))}
    </span>
  );
  const speedLine = info && (up !== undefined || down !== undefined) && (
    <span className="whitespace-nowrap">
      ↑ {rate(up)} ↓ {rate(down)}
    </span>
  );

  if (variant === "footer") {
    if (!usageLine && !speedLine && !month) return null;

    return (
      <div className="flex flex-wrap gap-x-3 text-xs tabular-nums text-default-400 lg:hidden">
        {usageLine}
        {speedLine}
        {month && <span title={`${split} ${monthHint}`}>本月 {total}</span>}
      </div>
    );
  }

  return (
    <div className="hidden items-center gap-6 text-xs tabular-nums text-default-400 lg:flex">
      {usageLine}
      {speedLine}
      <span
        className="w-24 text-right text-sm text-foreground"
        title={`${split} ${monthHint}`}
      >
        {month ? total : "—"}
      </span>
    </div>
  );
}
