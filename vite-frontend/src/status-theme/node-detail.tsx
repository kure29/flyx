import type {
  PublicStatus,
  PublicStatusMetricHistory,
  PublicStatusPingHistory,
  PublicStatusRange,
} from "@/api";

import { useCallback, useEffect, useId, useMemo, useState } from "react";
import {
  Area,
  ComposedChart,
  CartesianGrid,
  ResponsiveContainer,
  Tooltip,
  XAxis,
  YAxis,
} from "recharts";

import {
  Badge,
  bytesText,
  capacity,
  Card,
  daysUntil,
  expiryText,
  fetchStatus,
  getJSON,
  hostLine,
  latencyText,
  lossText,
  Notice,
  pct,
  RouteTag,
  Segmented,
  Shell,
  splitBytes,
  nodeFlag,
  speedText,
  uptimeShort,
  usageText,
} from "./ui";

import { Spinner } from "@/shadcn-bridge/heroui/spinner";
import { siteConfig } from "@/config/site";
import { formatUptime } from "@/pages/node/format";

const RANGES: Array<{ key: PublicStatusRange; label: string }> = [
  { key: "1h", label: "1 小时" },
  { key: "6h", label: "6 小时" },
  { key: "24h", label: "24 小时" },
  { key: "7d", label: "7 天" },
];

// Matches the backend's cache per range.
const REFRESH_MS: Record<PublicStatusRange, number> = {
  "1h": 15_000,
  "6h": 60_000,
  "24h": 60_000,
  "7d": 120_000,
};

// shadcn/ui's chart palette, readable on light and dark.
const COLORS = {
  cpu: "#3b82f6",
  mem: "#8b5cf6",
  swap: "#ec4899",
  disk: "#14b8a6",
  in: "#10b981",
  out: "#f59e0b",
  load: "#6366f1",
  tcp: "#3b82f6",
  udp: "#14b8a6",
  procs: "#a855f7",
};
const PING_COLORS = [
  "#3b82f6",
  "#10b981",
  "#a855f7",
  "#f59e0b",
  "#f43f5e",
  "#14b8a6",
  "#ec4899",
  "#64748b",
];

function timeLabel(t: number, range: PublicStatusRange) {
  const d = new Date(t);
  const hm = d.toLocaleTimeString([], {
    hour: "2-digit",
    minute: "2-digit",
    hour12: false,
  });

  return range === "7d" ? `${d.getMonth() + 1}/${d.getDate()} ${hm}` : hm;
}

interface Series {
  key: string;
  name: string;
  color: string;
}

function Chart({
  title,
  description,
  rows,
  series,
  range,
  format,
  axisFormat,
  domain,
  fill = true,
}: {
  title: string;
  description?: string;
  rows: Array<Record<string, number | null>>;
  series: Series[];
  range: PublicStatusRange;
  format: (v: number) => string;
  /** Shorter labels for the axis; format otherwise. */
  axisFormat?: (v: number) => string;
  domain?: [number, number];
  /** Shade under each line (off for lines that sit close together). */
  fill?: boolean;
}) {
  const id = useId().replace(/:/g, "");

  return (
    <Card className="flex min-w-0 flex-col gap-4 p-5 sm:p-6">
      <div className="flex flex-wrap items-start justify-between gap-x-4 gap-y-1">
        <div className="min-w-0">
          <h2 className="font-semibold leading-none">{title}</h2>
          {description && (
            <p className="mt-1.5 text-[13px] text-default-500">{description}</p>
          )}
        </div>
        <div className="flex flex-wrap gap-x-3 gap-y-1 text-xs text-default-500">
          {series.map((s) => (
            <span key={s.key} className="inline-flex items-center gap-1.5">
              <span
                className="h-2 w-2 rounded-[2px]"
                style={{ backgroundColor: s.color }}
              />
              {s.name}
            </span>
          ))}
        </div>
      </div>
      <div className="h-52 text-default-500 sm:h-60">
        <ResponsiveContainer
          height="100%"
          initialDimension={{ width: 1, height: 1 }}
          width="100%"
        >
          <ComposedChart
            data={rows}
            margin={{ top: 6, right: 4, bottom: 0, left: 0 }}
          >
            <defs>
              {series.map((s) => (
                <linearGradient
                  key={s.key}
                  id={`${id}-${s.key}`}
                  x1="0"
                  x2="0"
                  y1="0"
                  y2="1"
                >
                  <stop offset="5%" stopColor={s.color} stopOpacity={0.25} />
                  <stop offset="95%" stopColor={s.color} stopOpacity={0} />
                </linearGradient>
              ))}
            </defs>
            <CartesianGrid
              stroke="currentColor"
              strokeDasharray="3 3"
              strokeOpacity={0.2}
              vertical={false}
            />
            <XAxis
              axisLine={false}
              dataKey="t"
              fontSize={11}
              minTickGap={48}
              stroke="currentColor"
              tickFormatter={(t: number) => timeLabel(t, range)}
              tickLine={false}
              tickMargin={8}
            />
            <YAxis
              axisLine={false}
              domain={domain ?? [0, "auto"]}
              fontSize={11}
              stroke="currentColor"
              tickFormatter={axisFormat ?? format}
              tickLine={false}
              width={52}
            />
            <Tooltip
              contentStyle={{
                backgroundColor: "var(--background)",
                border: "1px solid var(--border)",
                borderRadius: 8,
                boxShadow: "0 4px 12px rgba(0,0,0,0.08)",
                color: "var(--foreground)",
                fontSize: 12,
              }}
              cursor={{ stroke: "currentColor", strokeOpacity: 0.3 }}
              formatter={(value: unknown, name: unknown) => [
                value === null || value === undefined
                  ? "超时"
                  : format(Number(value)),
                String(name),
              ]}
              itemStyle={{ padding: 0 }}
              labelFormatter={(t: unknown) => timeLabel(Number(t), "7d")}
              labelStyle={{ color: "var(--foreground)", marginBottom: 2 }}
            />
            {series.map((s) => (
              <Area
                key={s.key}
                activeDot={{ r: 3 }}
                connectNulls={false}
                dataKey={s.key}
                dot={false}
                fill={fill ? `url(#${id}-${s.key})` : "transparent"}
                isAnimationActive={false}
                name={s.name}
                stroke={s.color}
                strokeWidth={1.75}
                type="monotone"
              />
            ))}
          </ComposedChart>
        </ResponsiveContainer>
      </div>
    </Card>
  );
}

function Fact({
  label,
  children,
  className = "",
  title,
}: {
  label: string;
  children: React.ReactNode;
  className?: string;
  title?: string;
}) {
  return (
    <div className="flex min-w-0 flex-col gap-0.5 border-b border-r border-border px-4 py-3 sm:px-5 sm:py-4">
      <span className="text-xs text-default-500">{label}</span>
      <span
        className={`truncate text-[15px] font-semibold tabular-nums ${className}`}
        title={title}
      >
        {children}
      </span>
    </div>
  );
}

const pctFmt = (v: number) => `${v.toFixed(v >= 10 ? 0 : 1)}%`;
const speedFmt = (v: number) => `${bytesText(v)}/s`;
/** "3.2M/s": one letter for the unit, for the axis. */
const speedAxisFmt = (v: number) => {
  const [n, unit] = splitBytes(v);

  return `${Number(n) >= 100 ? Math.round(Number(n)) : Number(n).toFixed(1).replace(/\.0$/, "")}${unit === "B" ? "" : unit[0]}/s`;
};
const countFmt = (v: number) => String(Math.round(v));
const loadFmt = (v: number) => v.toFixed(2);
const msFmt = (v: number) => `${Math.round(v)} ms`;

/** One server's history: /status/node/<its id on the page>. */
export default function NodeDetail({ id }: { id: number }) {
  const [range, setRange] = useState<PublicStatusRange>("1h");
  const [status, setStatus] = useState<PublicStatus | null>(null);
  const [metrics, setMetrics] = useState<PublicStatusMetricHistory | null>(
    null,
  );
  const [pings, setPings] = useState<PublicStatusPingHistory | null>(null);
  const [error, setError] = useState("");
  const [loading, setLoading] = useState(true);

  const load = useCallback(async () => {
    try {
      const q = `node=${id}&range=${range}`;
      const [s, m, p] = await Promise.all([
        fetchStatus(),
        getJSON<PublicStatusMetricHistory>(
          `/api/v1/public/status/history?${q}`,
        ),
        getJSON<PublicStatusPingHistory>(
          `/api/v1/public/status/ping-history?${q}`,
        ),
      ]);

      if (s.code !== 0) {
        setError(s.msg || "状态页暂不可用");

        return;
      }
      if (m.code !== 0) {
        setError(m.msg || "读取历史失败");

        return;
      }
      setStatus(s.data);
      setMetrics(m.data);
      setPings(p.code === 0 ? p.data : null);
      setError("");
    } catch {
      setError("无法连接到面板");
    } finally {
      setLoading(false);
    }
  }, [id, range]);

  useEffect(() => {
    setLoading(true);
    void load();
    const timer = window.setInterval(() => {
      if (document.visibilityState === "visible") void load();
    }, REFRESH_MS[range]);

    return () => window.clearInterval(timer);
  }, [load, range]);

  const node = status?.nodes.find((n) => n.id === id);
  const [flag, , name] = nodeFlag(node ?? { name: "" });
  const title = status?.title || siteConfig.name;

  useEffect(() => {
    document.title = `${name || "服务器"} · ${title}`;
  }, [name, title]);

  const rows = useMemo(
    () => (metrics?.points ?? []).map((p) => ({ ...p })),
    [metrics],
  );
  const hasTraffic = rows.some((p) => p.netIn !== undefined);
  const hasSwap = rows.some((p) => p.swap > 0);
  const hasProcs = rows.some((p) => p.procs > 0);

  const pingRows = useMemo(() => {
    const byT = new Map<number, Record<string, number | null>>();

    for (const s of pings?.points ?? []) {
      for (const sample of s.samples) {
        const row = byT.get(sample.t) ?? { t: sample.t };

        row[s.name] = sample.ms;
        byT.set(sample.t, row);
      }
    }

    return [...byT.values()].sort((a, b) => Number(a.t) - Number(b.t));
  }, [pings]);

  const home = import.meta.env.BASE_URL;
  const live = node?.online && node.cpu !== undefined;
  const remaining = node?.expiry ? daysUntil(node.expiry) : null;
  const facts: Array<{
    label: string;
    value: React.ReactNode;
    className?: string;
    title?: string;
  }> = [];

  if (node && live) {
    facts.push(
      {
        label: "CPU",
        value: `${pct(node.cpu, 1)}${node.cores ? ` · ${node.cores} 核` : ""}`,
        className: usageText(node.cpu ?? 0),
      },
      {
        label: "内存",
        value: capacity(node.memUsed, node.memTotal) ?? pct(node.mem, 1),
        className: usageText(node.mem ?? 0),
        title: pct(node.mem, 1),
      },
    );
    if (node.swapTotal) {
      facts.push({
        label: "Swap",
        value: capacity(node.swapUsed, node.swapTotal),
        className: usageText(node.swap ?? 0),
        title: pct(node.swap, 1),
      });
    }
    facts.push(
      {
        label: "磁盘",
        value: capacity(node.diskUsed, node.diskTotal) ?? pct(node.disk, 1),
        className: usageText(node.disk ?? 0),
        title: pct(node.disk, 1),
      },
      {
        label: "负载",
        value: node.load?.map((l) => l.toFixed(2)).join(" ") ?? "—",
      },
      {
        label: "连接",
        value: `TCP ${node.tcp ?? 0} · UDP ${node.udp ?? 0}`,
      },
    );
    if (node.procs) facts.push({ label: "进程", value: node.procs });
    if (node.speedUp !== undefined) {
      facts.push({
        label: "网速",
        value: `↑ ${speedText(node.speedUp)} ↓ ${speedText(node.speedDown)}`,
      });
    }
    if (node.monthUp !== undefined) {
      facts.push({
        label: "本月流量",
        value: `↑ ${bytesText(node.monthUp)} ↓ ${bytesText(node.monthDown)}`,
      });
    }
    if (node.uptime) {
      facts.push({ label: "已运行", value: formatUptime(node.uptime) });
    }
  }
  if (node?.expiry && remaining !== null) {
    facts.push({
      label: "到期",
      value: node.expiry,
      className: expiryText(remaining),
      title: remaining < 0 ? "已过期" : `${remaining} 天后`,
    });
  }

  return (
    <Shell title={title}>
      {loading && !metrics ? (
        <div className="flex min-h-[40vh] items-center justify-center">
          <Spinner label="加载中..." size="lg" />
        </div>
      ) : error && !metrics ? (
        <Notice title={error}>
          <a className="hover:underline" href={home}>
            返回状态页
          </a>
        </Notice>
      ) : (
        <>
          <nav className="text-[13px] text-default-500">
            <a className="hover:text-foreground" href={home}>
              服务器
            </a>
            <span className="mx-1.5">›</span>
            <span className="text-foreground">{name}</span>
          </nav>

          <div className="flex flex-wrap items-end justify-between gap-x-4 gap-y-3">
            <div className="flex min-w-0 flex-col gap-1">
              <div className="flex min-w-0 items-center gap-2.5">
                {flag && <span className="text-2xl leading-none">{flag}</span>}
                <h1 className="truncate text-2xl font-bold tracking-tight sm:text-3xl">
                  {name}
                </h1>
                {node &&
                  (node.online ? (
                    <Badge>
                      在线
                      {uptimeShort(node.uptime)
                        ? ` ${uptimeShort(node.uptime)}`
                        : ""}
                    </Badge>
                  ) : (
                    <Badge tone="bad">离线</Badge>
                  ))}
              </div>
              {node && hostLine(node) && (
                <p className="text-sm text-default-500">
                  {hostLine(node)}
                  {node.cpuName ? ` · ${node.cpuName}` : ""}
                </p>
              )}
            </div>
            <Segmented
              label="时间范围"
              options={RANGES}
              value={range}
              onChange={setRange}
            />
          </div>

          {facts.length > 0 && (
            <Card className="overflow-hidden">
              <div className="-mb-px -mr-px grid grid-cols-[repeat(auto-fill,minmax(11rem,1fr))] sm:grid-cols-[repeat(auto-fill,minmax(13rem,1fr))]">
                {facts.map((f) => (
                  <Fact
                    key={f.label}
                    className={f.className}
                    label={f.label}
                    title={f.title}
                  >
                    {f.value}
                  </Fact>
                ))}
              </div>
            </Card>
          )}

          {node?.ping && node.ping.length > 0 && (
            <Card className="p-5 sm:p-6">
              <h2 className="font-semibold leading-none">延迟与回程</h2>
              <p className="mt-1.5 text-[13px] text-default-500">
                延迟为最近一次测试，丢包为近 1 小时平均，回程每天检测一次
              </p>
              <div className="mt-4 grid grid-cols-1 gap-3 sm:grid-cols-2 lg:grid-cols-3">
                {node.ping.map((p) => (
                  <div
                    key={p.name}
                    className="flex min-w-0 items-center gap-3 rounded-lg border border-border px-4 py-3"
                  >
                    <div className="flex min-w-0 flex-col gap-1">
                      <span className="truncate text-sm font-medium">
                        {p.name}
                      </span>
                      {p.route || p.routeV6 ? (
                        <RouteTag route={p.route} v6={p.routeV6} />
                      ) : (
                        <span className="text-xs text-default-400">
                          回程未检测
                        </span>
                      )}
                    </div>
                    <div className="ml-auto flex shrink-0 flex-col items-end">
                      <span
                        className={`text-lg font-semibold tabular-nums ${latencyText(p.ms)}`}
                      >
                        {p.ms === null ? "超时" : `${p.ms} ms`}
                      </span>
                      <span
                        className={`text-xs tabular-nums ${lossText(p.loss)}`}
                      >
                        丢包 {p.loss.toFixed(1)}%
                      </span>
                    </div>
                  </div>
                ))}
              </div>
            </Card>
          )}

          {rows.length === 0 ? (
            <Notice title="这段时间还没有记录" />
          ) : (
            <div className="grid grid-cols-1 gap-4 lg:grid-cols-2">
              <Chart
                description={`每点为 ${metrics?.bucketSec ?? 0} 秒内的平均值`}
                domain={[0, 100]}
                format={pctFmt}
                range={range}
                rows={rows}
                series={[
                  { key: "cpu", name: "CPU", color: COLORS.cpu },
                  { key: "mem", name: "内存", color: COLORS.mem },
                  ...(hasSwap
                    ? [{ key: "swap", name: "Swap", color: COLORS.swap }]
                    : []),
                  { key: "disk", name: "磁盘", color: COLORS.disk },
                ]}
                title="资源使用率"
              />
              {hasTraffic && (
                <Chart
                  axisFormat={speedAxisFmt}
                  description="上行为服务器发出，下行为收到"
                  format={speedFmt}
                  range={range}
                  rows={rows}
                  series={[
                    { key: "netOut", name: "上行", color: COLORS.out },
                    { key: "netIn", name: "下行", color: COLORS.in },
                  ]}
                  title="网速"
                />
              )}
              <Chart
                format={loadFmt}
                range={range}
                rows={rows}
                series={[
                  { key: "load", name: "1 分钟负载", color: COLORS.load },
                ]}
                title="负载"
              />
              <Chart
                fill={false}
                format={countFmt}
                range={range}
                rows={rows}
                series={[
                  { key: "tcp", name: "TCP", color: COLORS.tcp },
                  { key: "udp", name: "UDP", color: COLORS.udp },
                  ...(hasProcs
                    ? [{ key: "procs", name: "进程", color: COLORS.procs }]
                    : []),
                ]}
                title="连接数与进程"
              />
              {pingRows.length > 0 && (
                <div className="lg:col-span-2">
                  <Chart
                    axisFormat={countFmt}
                    description="单位 ms，断开处为超时"
                    fill={false}
                    format={msFmt}
                    range={range}
                    rows={pingRows}
                    series={(pings?.points ?? []).map((p, i) => ({
                      key: p.name,
                      name: p.name,
                      color: PING_COLORS[i % PING_COLORS.length],
                    }))}
                    title="延迟"
                  />
                </div>
              )}
            </div>
          )}

          <footer className="text-xs text-default-500">
            {status?.timezone ? `按 ${status.timezone} 显示` : ""}
            {error ? ` · ${error}，显示的是上次的数据` : ""}
          </footer>
        </>
      )}
    </Shell>
  );
}
