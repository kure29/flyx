import type { NodeSystemInfo } from "@/pages/node/system-info";

import { useI18n } from "@/i18n";
import { formatTrafficShort } from "@/utils/traffic";

const capacity = (used?: number, total?: number) =>
  total
    ? `${formatTrafficShort(used ?? 0)} / ${formatTrafficShort(total)}`
    : "";

/**
 * A node's live resources on its list row: CPU, memory and disk, speed
 * and current billing period traffic. "row" sits at the end of a wide row,
 * "footer" under the name on narrow screens.
 */
export function NodeLiveStats({
  info,
  month,
  resetDay,
  variant,
}: {
  info: NodeSystemInfo | null;
  /** Current period's [in, out] bytes. */
  month?: [number, number];
  /** Custom traffic reset day (1-31; 0 or 1 = calendar month). */
  resetDay?: number;
  variant: "row" | "footer";
}) {
  const { t } = useI18n();
  const up = info ? (info.netOutSpeed ?? info.uploadSpeed) : undefined;
  const down = info ? (info.netInSpeed ?? info.downloadSpeed) : undefined;
  const rate = (v?: number) =>
    v === undefined ? "—" : `${formatTrafficShort(v)}/s`;
  const total = month ? formatTrafficShort(month[0] + month[1]) : "—";
  const hasCustomReset = Boolean(resetDay && resetDay > 1);
  const periodLabel = hasCustomReset ? t("本周期") : t("本月");
  const periodHint = hasCustomReset
    ? t("按面板时区统计（每月 {day} 日重置）", { day: resetDay! })
    : t("按面板时区统计");
  const split = month
    ? `↑${formatTrafficShort(month[1])} ↓${formatTrafficShort(month[0])}`
    : "";

  // List rows: one quiet line, and nothing for data the node has not sent.
  const usage = info
    ? (
        [
          [
            "CPU",
            info.cpuUsage,
            info.cpuCores ? t("{n} 核", { n: info.cpuCores }) : "",
          ],
          [t("内存"), info.memoryUsage, capacity(info.memUsed, info.memTotal)],
          [t("磁盘"), info.diskUsage, capacity(info.diskUsed, info.diskTotal)],
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
        {month && (
          <span title={`${split} · ${periodHint}`}>
            {periodLabel} {total}
          </span>
        )}
      </div>
    );
  }

  return (
    <div className="hidden items-center gap-6 text-xs tabular-nums text-default-400 lg:flex">
      {usageLine}
      {speedLine}
      <span
        className="w-24 text-right text-sm text-foreground"
        title={`${split} · ${periodHint}`}
      >
        {month ? total : "—"}
      </span>
    </div>
  );
}
