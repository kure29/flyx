import type { SubTraffic, SubTrafficRange } from "@/api";

import { type ReactNode, useEffect, useRef, useState } from "react";
import { ArrowDown, ArrowUp } from "lucide-react";
import {
  Area,
  CartesianGrid,
  ComposedChart,
  Line,
  ReferenceLine,
  ResponsiveContainer,
  Tooltip,
  XAxis,
  YAxis,
} from "recharts";

import { useVisiblePolling } from "@/hooks/use-visible-polling";
import { useI18n } from "@/i18n";
import { Segmented } from "@/pages/subscription/controls";
import { formatTrafficAxis, formatTrafficCompact } from "@/utils/traffic";

const DOWN = "#2563eb";
const UP = "#ea580c";
const DOWN_TEXT = "text-blue-700 dark:text-blue-400";
const UP_TEXT = "text-orange-700 dark:text-orange-400";

const pad = (n: number) => String(n).padStart(2, "0");

/** A bucket's axis, list row and tooltip labels. */
function bucketLabel(
  range: SubTrafficRange,
  ms: number,
  locale: string,
): [string, string, string] {
  const d = new Date(ms);
  const md = `${d.getMonth() + 1}/${d.getDate()}`;
  const hm = `${pad(d.getHours())}:${pad(d.getMinutes())}`;

  if (range === "hour") return [hm, `${md} ${hm}`, `${md} ${hm}`];
  if (range === "day") return [md, md, `${d.getFullYear()}/${md}`];

  if (locale === "en-US") {
    const mon = d.toLocaleString("en-US", { month: "short" });

    return [
      d.getMonth() === 0 ? `${mon} ${d.getFullYear()}` : mon,
      `${d.getFullYear()}/${pad(d.getMonth() + 1)}`,
      `${mon} ${d.getFullYear()}`,
    ];
  }

  return [
    d.getMonth() === 0 ? `${d.getFullYear()}/1月` : `${d.getMonth() + 1}月`,
    `${d.getFullYear()}/${d.getMonth() + 1}`,
    `${d.getFullYear()} 年 ${d.getMonth() + 1} 月`,
  ];
}

export interface TrafficHistoryChartProps {
  /** Cache identity (e.g. subscription id, node id, or "all-nodes"). */
  cacheKey: string | number;
  fetchData: (
    range: SubTrafficRange,
  ) => Promise<{ code: number; msg?: string; data?: SubTraffic }>;
  defaultRange?: SubTrafficRange;
  /** Compact mode for overview Bento cards (hides breakdown table, shorter chart). */
  compact?: boolean;
  /** Whether to show the expandable per-bucket breakdown table (defaults to !compact). */
  showBreakdown?: boolean;
  /** Optional footnotes rendered below the chart/breakdown. */
  footnotes?: (ctx: { sinceDate: string; data: SubTraffic }) => ReactNode;
}

/**
 * Reusable historical traffic chart (24h / 30d / 12m) with download/upload/total
 * KPI tiles, area + dashed line trend chart, period reset marker, and optional
 * expandable breakdown table.
 */
export function TrafficHistoryChart({
  cacheKey,
  fetchData,
  defaultRange = "hour",
  compact = false,
  showBreakdown,
  footnotes,
}: TrafficHistoryChartProps) {
  const { locale, t } = useI18n();
  const [range, setRange] = useState<SubTrafficRange>(defaultRange);
  const [data, setData] = useState<{
    key: string | number;
    traffic: SubTraffic;
  } | null>(null);
  const [error, setError] = useState("");
  const [all, setAll] = useState(false);
  const loadRef = useRef<(() => Promise<void>) | null>(null);
  const fetchRef = useRef(fetchData);

  fetchRef.current = fetchData;

  const ranges: Array<[SubTrafficRange, string]> = [
    ["hour", t("小时")],
    ["day", t("日")],
    ["month", t("月")],
  ];
  const rangeHint: Record<SubTrafficRange, string> = {
    hour: t("近 24 小时"),
    day: t("近 30 天"),
    month: t("近 12 个月"),
  };

  useEffect(() => {
    let active = true;
    let loading = false;
    let hasData = false;

    const load = async () => {
      if (loading) return;
      loading = true;
      try {
        const res = await fetchRef.current(range);

        if (!active) return;
        if (res.code === 0 && res.data) {
          hasData = true;
          setData({ key: cacheKey, traffic: res.data });
          setError("");
        } else if (!hasData) {
          setError(res.msg || t("加载失败"));
        }
      } catch {
        if (active && !hasData) setError(t("加载失败"));
      } finally {
        loading = false;
      }
    };

    setError("");
    loadRef.current = load;
    void load();

    return () => {
      active = false;
      loadRef.current = null;
    };
  }, [cacheKey, range, t]);

  useVisiblePolling(async () => {
    await loadRef.current?.();
  }, 30_000);

  const shown =
    data?.key === cacheKey && data.traffic.range === range
      ? data.traffic
      : null;
  const points = (shown?.points ?? []).map((p) => {
    const [label, row, long] = bucketLabel(range, p.time, locale);

    return { ...p, label, row, long };
  });
  const empty = shown !== null && shown.up + shown.down === 0;
  const rows = [...points].reverse();
  const maxTotal = Math.max(1, ...points.map((p) => p.up + p.down));
  const sinceAt =
    shown && shown.since > 0 && points.length && shown.since >= points[0].time
      ? points.filter((p) => p.time <= shown.since).pop()
      : undefined;
  const sinceDate =
    shown && shown.since > 0
      ? new Date(shown.since).toLocaleDateString(
          locale === "en-US" ? "en-US" : "zh-CN",
        )
      : "";
  const shouldShowBreakdown = showBreakdown ?? !compact;

  return (
    <div className={compact ? "space-y-2.5" : "space-y-3"}>
      <div className="flex flex-wrap items-center justify-between gap-2">
        <Segmented
          options={ranges}
          value={range}
          onChange={(r) => {
            setRange(r);
            setAll(false);
          }}
        />
        <span className="text-xs text-default-400">
          {t("{range}的流量", { range: rangeHint[range] })}
        </span>
      </div>

      {shown?.estimated && (
        <p className="text-xs text-default-500">
          {t("部分旧记录跨越本地时间边界，已按时长估算分配。")}
        </p>
      )}

      <div className="grid grid-cols-3 gap-2">
        {(
          [
            [t("下载"), shown?.down, DOWN, DOWN_TEXT],
            [t("上传"), shown?.up, UP, UP_TEXT],
            [t("合计"), shown ? shown.up + shown.down : undefined, "", ""],
          ] as Array<[string, number | undefined, string, string]>
        ).map(([label, value, color, textColor]) => (
          <div
            key={label}
            className="rounded-xl bg-default-100/80 px-2.5 py-2 dark:bg-default-100/40"
          >
            <div className="flex items-center gap-1.5 text-xs text-default-500">
              {color && (
                <span
                  className="h-2 w-2 shrink-0 rounded-full"
                  style={{ background: color }}
                />
              )}
              <span className="truncate">{label}</span>
            </div>
            <div
              className={`mt-0.5 truncate font-mono text-sm font-semibold tabular-nums ${textColor}`}
            >
              {value === undefined ? "…" : formatTrafficCompact(value)}
            </div>
          </div>
        ))}
      </div>

      {error ? (
        <p className="py-6 text-center text-sm text-danger">{error}</p>
      ) : !shown ? (
        <p className="py-6 text-center text-sm text-default-400">
          {t("正在加载...")}
        </p>
      ) : (
        <>
          <div
            className={
              compact ? "relative h-40 w-full" : "relative h-52 w-full sm:h-64"
            }
          >
            <ResponsiveContainer height="100%" width="100%">
              <ComposedChart
                data={points}
                margin={{ top: 8, right: 8, left: 0, bottom: 0 }}
              >
                <CartesianGrid
                  stroke="currentColor"
                  strokeDasharray="3 3"
                  strokeOpacity={0.1}
                  vertical={false}
                />
                <XAxis
                  axisLine={false}
                  dataKey="label"
                  interval="preserveStartEnd"
                  minTickGap={24}
                  tick={{ fill: "currentColor", opacity: 0.55, fontSize: 11 }}
                  tickLine={false}
                />
                <YAxis
                  axisLine={false}
                  tick={{ fill: "currentColor", opacity: 0.55, fontSize: 11 }}
                  tickFormatter={(v: number) => formatTrafficAxis(v)}
                  tickLine={false}
                  width={48}
                />
                <Tooltip
                  content={({
                    active,
                    payload,
                  }: {
                    active?: boolean;
                    payload?: Array<{ payload?: (typeof points)[number] }>;
                  }) => {
                    const p = payload?.[0]?.payload;

                    if (!active || !p) return null;

                    return (
                      <div className="rounded-xl border border-divider bg-content1 px-3 py-2 text-xs text-foreground shadow-lg">
                        <p className="font-medium">{p.long}</p>
                        <p className={`mt-1 tabular-nums ${DOWN_TEXT}`}>
                          {t("下载")} {formatTrafficCompact(p.down)}
                        </p>
                        <p className={`tabular-nums ${UP_TEXT}`}>
                          {t("上传")} {formatTrafficCompact(p.up)}
                        </p>
                      </div>
                    );
                  }}
                  cursor={{ stroke: "currentColor", strokeOpacity: 0.2 }}
                />
                {sinceAt && (
                  <ReferenceLine
                    stroke="currentColor"
                    strokeDasharray="2 3"
                    strokeOpacity={0.35}
                    x={sinceAt.label}
                  />
                )}
                <Area
                  dataKey="down"
                  fill={DOWN}
                  fillOpacity={0.12}
                  isAnimationActive={false}
                  name={t("下载")}
                  stroke={DOWN}
                  strokeWidth={2}
                  type="monotone"
                />
                <Line
                  dataKey="up"
                  dot={false}
                  isAnimationActive={false}
                  name={t("上传")}
                  stroke={UP}
                  strokeDasharray="5 4"
                  strokeWidth={2}
                  type="monotone"
                />
              </ComposedChart>
            </ResponsiveContainer>
            {empty && (
              <p className="absolute inset-0 flex items-center justify-center text-center text-xs text-default-400">
                {t("{range}还没有流量", { range: rangeHint[range] })}
              </p>
            )}
          </div>

          <div className="flex flex-wrap items-center gap-x-4 gap-y-1 text-xs text-default-500">
            <span className="flex items-center gap-1.5">
              <span className="w-4 border-t-2" style={{ borderColor: DOWN }} />
              {t("下载")}
            </span>
            <span className="flex items-center gap-1.5">
              <span
                className="w-4 border-t-2 border-dashed"
                style={{ borderColor: UP }}
              />
              {t("上传")}
            </span>
            {sinceAt && sinceDate && (
              <span className="flex items-center gap-1.5">
                <span className="h-3 border-l border-dashed border-default-400" />
                {t("本周期起（{date}）", { date: sinceDate })}
              </span>
            )}
          </div>

          {shouldShowBreakdown && (
            <div className="overflow-hidden rounded-xl border border-divider">
              <div className="grid grid-cols-[4.75rem_minmax(0,1fr)_4.5rem] gap-2 bg-default-100/60 px-3 py-1.5 text-xs text-default-500">
                <span>{t("时间")}</span>
                <span>{t("下载 / 上传")}</span>
                <span className="text-right">{t("合计")}</span>
              </div>
              {(all ? rows : rows.slice(0, 7)).map((p) => (
                <div
                  key={p.time}
                  className="grid grid-cols-[4.75rem_minmax(0,1fr)_4.5rem] items-center gap-2 border-t border-divider px-3 py-2"
                >
                  <span className="truncate text-xs tabular-nums">{p.row}</span>
                  <div className="min-w-0 space-y-1">
                    <div
                      aria-hidden="true"
                      className="flex h-1.5 overflow-hidden rounded-full bg-default-100"
                    >
                      <div
                        className="h-full shrink-0"
                        style={{
                          background: DOWN,
                          width: `${(p.down / maxTotal) * 100}%`,
                        }}
                      />
                      <div
                        className="h-full shrink-0"
                        style={{
                          background: UP,
                          width: `${(p.up / maxTotal) * 100}%`,
                        }}
                      />
                    </div>
                    <div className="flex flex-wrap items-center gap-x-3 gap-y-1 text-xs font-medium tabular-nums">
                      <span
                        className={`inline-flex items-center gap-1 ${DOWN_TEXT}`}
                      >
                        <ArrowDown
                          aria-hidden="true"
                          className="h-3 w-3 shrink-0"
                        />
                        <span>{t("下载")}</span>
                        <span className="whitespace-nowrap font-mono">
                          {formatTrafficCompact(p.down)}
                        </span>
                      </span>
                      <span
                        className={`inline-flex items-center gap-1 ${UP_TEXT}`}
                      >
                        <ArrowUp
                          aria-hidden="true"
                          className="h-3 w-3 shrink-0"
                        />
                        <span>{t("上传")}</span>
                        <span className="whitespace-nowrap font-mono">
                          {formatTrafficCompact(p.up)}
                        </span>
                      </span>
                    </div>
                  </div>
                  <span className="text-right font-mono text-xs font-semibold tabular-nums">
                    {formatTrafficCompact(p.up + p.down)}
                  </span>
                </div>
              ))}
              {rows.length > 7 && (
                <button
                  className="w-full border-t border-divider py-2 text-xs text-primary transition-colors hover:bg-default-50"
                  type="button"
                  onClick={() => setAll(!all)}
                >
                  {all
                    ? t("收起")
                    : t("查看全部 {count} 条", { count: rows.length })}
                </button>
              )}
            </div>
          )}

          {footnotes?.({ sinceDate, data: shown })}
        </>
      )}
    </div>
  );
}
