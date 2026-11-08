import type { PublicStatus, PublicStatusNode, PublicStatusPing } from "@/api";

import { useCallback, useEffect, useMemo, useState } from "react";
import {
  Activity,
  CircleCheck,
  LayoutGrid,
  List,
  Server,
  TriangleAlert,
  ArrowUpRight,
} from "lucide-react";

import {
  availabilityText,
  Badge,
  Bar,
  bytesText,
  capacity,
  Card,
  cardClass,
  dayClass,
  daysUntil,
  expiryText,
  fetchStatus,
  GOOD_DAY_DOWN_MIN,
  hostLine,
  latencyText,
  lossText,
  nodeHref,
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
  usePolling,
} from "./ui";

import { Spinner } from "@/shadcn-bridge/heroui/spinner";
import { siteConfig } from "@/config/site";

const REFRESH_MS = 10_000;
const VIEW_KEY = "flyx-status:view";

type View = "cards" | "list";

// ---------------------------------------------------------------- pieces

function Stat({
  label,
  icon,
  value,
  unit,
  sub,
  valueClass = "",
}: {
  label: string;
  icon: React.ReactNode;
  value: string;
  unit?: string;
  sub?: string;
  valueClass?: string;
}) {
  return (
    <Card className="flex min-w-0 flex-col gap-1 p-4 sm:p-6">
      <div className="flex items-center justify-between gap-2 text-sm font-medium">
        <span className="truncate">{label}</span>
        <span className="text-default-500">{icon}</span>
      </div>
      <div className="mt-1 flex items-baseline gap-1 truncate">
        <span
          className={`text-xl font-bold tracking-tight tabular-nums sm:text-2xl ${valueClass}`}
        >
          {value}
        </span>
        {unit && <span className="text-sm text-default-500">{unit}</span>}
      </div>
      {sub && (
        <span className="truncate text-xs tabular-nums text-default-500">
          {sub}
        </span>
      )}
    </Card>
  );
}

function Meter({
  label,
  value,
  title,
}: {
  label: string;
  value: number | undefined;
  title?: string;
}) {
  return (
    <div
      className="grid grid-cols-[2.5rem_minmax(0,1fr)_3.25rem] items-center gap-2.5 text-[13px]"
      title={title}
    >
      <span className="text-default-500">{label}</span>
      <Bar value={value ?? 0} />
      <span
        className={`text-right font-medium tabular-nums ${
          value === undefined ? "text-default-400" : usageText(value)
        }`}
      >
        {value === undefined ? "—" : `${value.toFixed(1)}%`}
      </span>
    </div>
  );
}

function Row({
  label,
  children,
  title,
}: {
  label: string;
  children: React.ReactNode;
  title?: string;
}) {
  return (
    <div
      className="flex min-w-0 items-baseline justify-between gap-3 text-[13px]"
      title={title}
    >
      <span className="shrink-0 text-default-500">{label}</span>
      <span className="truncate font-medium tabular-nums">{children}</span>
    </div>
  );
}

const Up = () => <span className="text-default-500">↑</span>;
const Down = () => <span className="text-default-500">↓</span>;

function PingLine({ ping }: { ping: PublicStatusPing }) {
  return (
    <div className="flex min-w-0 items-center gap-2 text-[13px]">
      <span className="shrink-0 text-default-500">{ping.name}</span>
      <RouteTag route={ping.route} v6={ping.routeV6} />
      <span className="ml-auto shrink-0 tabular-nums">
        <b className={`font-medium ${latencyText(ping.ms)}`}>
          {ping.ms === null ? "超时" : ping.ms}
        </b>
        {ping.ms !== null && (
          <span className="text-[11px] text-default-500"> ms</span>
        )}
        {ping.loss >= 1 && (
          <span className={`ml-1.5 text-[11px] ${lossText(ping.loss)}`}>
            丢包 {ping.loss.toFixed(1)}%
          </span>
        )}
      </span>
    </div>
  );
}

function Days({ node, days }: { node: PublicStatusNode; days: string[] }) {
  return (
    <div aria-label="每日可用率" className="flex gap-[2px]" role="img">
      {days.map((day, i) => {
        const value = node.availability.days[i] ?? null;
        const down = node.availability.down[i] ?? null;

        return (
          <span
            key={day}
            className={`h-3.5 flex-1 rounded-[2px] ${dayClass(down)}`}
            title={
              value === null || down === null
                ? `${day}：无数据`
                : `${day}：可用 ${pct(value)}${down > 0 ? `，离线约 ${down} 分钟` : ""}`
            }
          />
        );
      })}
    </div>
  );
}

const Sep = () => <div className="h-px bg-border" />;

function statusBadge(node: PublicStatusNode) {
  if (!node.online) return <Badge tone="bad">离线</Badge>;
  const up = uptimeShort(node.uptime);

  return <Badge title={up ? `已运行 ${up}` : undefined}>{up || "在线"}</Badge>;
}

function NodeCard({
  node,
  days,
  showTraffic,
}: {
  node: PublicStatusNode;
  days: string[];
  showTraffic: boolean;
}) {
  const [flag, , name] = nodeFlag(node);
  const live = node.online && node.cpu !== undefined;
  const remaining = node.expiry ? daysUntil(node.expiry) : null;
  const host = hostLine(node);

  return (
    <a
      className={`${cardClass} group flex min-w-0 flex-col gap-3 p-4 transition-colors hover:border-default-400 ${
        node.online ? "" : "opacity-75"
      }`}
      href={nodeHref(node.id)}
    >
      <div className="flex min-w-0 flex-col gap-0.5">
        <div className="flex min-w-0 items-center gap-2">
          {flag && <span className="text-lg leading-none">{flag}</span>}
          <h3 className="truncate text-[15px] font-semibold">{name}</h3>
          <span className="flex-1" />
          {statusBadge(node)}
        </div>
        <span
          className="truncate text-xs text-default-500"
          title={node.cpuName || undefined}
        >
          {host || (node.online ? " " : "离线中，暂无实时数据")}
        </span>
      </div>

      <div className="flex flex-col gap-1.5">
        <Meter
          label="CPU"
          title={live && node.cores ? `${node.cores} 核` : undefined}
          value={live ? node.cpu : undefined}
        />
        <Meter
          label="内存"
          title={live ? capacity(node.memUsed, node.memTotal) : undefined}
          value={live ? node.mem : undefined}
        />
        {live && node.swapTotal ? (
          <Meter
            label="Swap"
            title={capacity(node.swapUsed, node.swapTotal)}
            value={node.swap}
          />
        ) : null}
        <Meter
          label="磁盘"
          title={live ? capacity(node.diskUsed, node.diskTotal) : undefined}
          value={live ? node.disk : undefined}
        />
      </div>

      {live && (
        <>
          <Sep />
          <div className="flex flex-col gap-1">
            {showTraffic && (
              <>
                <Row label="网速">
                  <Up /> {speedText(node.speedUp)} <Down />{" "}
                  {speedText(node.speedDown)}
                </Row>
                <Row label="本月">
                  <Up /> {bytesText(node.monthUp)} <Down />{" "}
                  {bytesText(node.monthDown)}
                </Row>
              </>
            )}
            <Row label="连接">
              <span className="text-default-500">TCP</span> {node.tcp ?? 0}{" "}
              <span className="text-default-500">UDP</span> {node.udp ?? 0}
              {node.procs ? (
                <>
                  {" "}
                  <span className="text-default-500">进程</span> {node.procs}
                </>
              ) : null}
            </Row>
            <Row label="负载">
              {node.load ? node.load.map((l) => l.toFixed(2)).join(" ") : "—"}
            </Row>
          </div>
        </>
      )}

      {node.ping && node.ping.length > 0 && (
        <>
          <Sep />
          <div className="flex flex-col gap-1.5">
            {node.ping.map((p) => (
              <PingLine key={p.name} ping={p} />
            ))}
          </div>
        </>
      )}

      <div className="mt-auto flex flex-col gap-1.5 pt-1">
        <Days days={days} node={node} />
        <div className="flex items-center justify-between text-xs text-default-500">
          <span>
            {days.length} 天
            {remaining !== null && (
              <span className={`ml-2 ${expiryText(remaining)}`}>
                {remaining < 0 ? "已过期" : `${remaining} 天后到期`}
              </span>
            )}
          </span>
          <span
            className={`tabular-nums ${availabilityText(node.availability.d30)}`}
          >
            {pct(node.availability.d30)}
          </span>
        </div>
      </div>
    </a>
  );
}

function Pct({ v }: { v: number | undefined }) {
  if (v === undefined) return <span className="text-default-400">—</span>;

  return (
    <div className="flex w-20 flex-col gap-1.5">
      <span className={`font-medium tabular-nums ${usageText(v)}`}>
        {v.toFixed(1)}%
      </span>
      <Bar value={v} />
    </div>
  );
}

function NodeTable({
  nodes,
  days,
  showTraffic,
}: {
  nodes: PublicStatusNode[];
  days: string[];
  showTraffic: boolean;
}) {
  const th =
    "h-10 whitespace-nowrap px-3 text-left text-[13px] font-medium text-default-500";
  const td = "whitespace-nowrap px-3 py-3 align-middle";

  return (
    <Card className="overflow-x-auto">
      <table className="w-full border-collapse text-[13px]">
        <thead>
          <tr className="border-b border-border">
            <th className={`${th} pl-4`}>服务器</th>
            <th className={th}>状态</th>
            <th className={th}>CPU</th>
            <th className={th}>内存</th>
            <th className={th}>磁盘</th>
            {showTraffic && <th className={th}>网速</th>}
            {showTraffic && <th className={th}>本月流量</th>}
            <th className={th}>连接</th>
            <th className={th}>延迟</th>
            <th className={th}>{days.length} 天可用率</th>
          </tr>
        </thead>
        <tbody>
          {nodes.map((node) => {
            const [flag, , name] = nodeFlag(node);
            const live = node.online && node.cpu !== undefined;

            return (
              // The name's link stretches over the whole row (::after), so a
              // row opens like a link: keyboard, middle click, new tab.
              <tr
                key={node.id}
                className="relative border-b border-border last:border-0 hover:bg-default-50"
              >
                <td className={`${td} pl-4`}>
                  <a
                    className="flex items-center gap-2.5 rounded-sm outline-none after:absolute after:inset-0 after:content-[''] focus-visible:after:ring-2 focus-visible:after:ring-inset focus-visible:after:ring-primary"
                    href={nodeHref(node.id)}
                  >
                    {flag && <span className="text-lg">{flag}</span>}
                    <span className="flex flex-col">
                      <span className="font-medium">{name}</span>
                      {hostLine(node) && (
                        <span className="text-xs text-default-500">
                          {hostLine(node)}
                        </span>
                      )}
                    </span>
                  </a>
                </td>
                <td className={td}>{statusBadge(node)}</td>
                <td className={td}>
                  <Pct v={live ? node.cpu : undefined} />
                </td>
                <td className={td}>
                  <Pct v={live ? node.mem : undefined} />
                </td>
                <td className={td}>
                  <Pct v={live ? node.disk : undefined} />
                </td>
                {showTraffic && (
                  <td className={`${td} tabular-nums`}>
                    {live ? (
                      <>
                        <div>
                          <Up /> {speedText(node.speedUp)}
                        </div>
                        <div>
                          <Down /> {speedText(node.speedDown)}
                        </div>
                      </>
                    ) : (
                      <span className="text-default-400">—</span>
                    )}
                  </td>
                )}
                {showTraffic && (
                  <td className={`${td} tabular-nums`}>
                    <div>
                      <Up /> {bytesText(node.monthUp)}
                    </div>
                    <div>
                      <Down /> {bytesText(node.monthDown)}
                    </div>
                  </td>
                )}
                <td className={`${td} tabular-nums`}>
                  {live ? (
                    <>
                      <div>
                        <span className="text-default-500">TCP</span>{" "}
                        {node.tcp ?? 0}
                      </div>
                      <div>
                        <span className="text-default-500">UDP</span>{" "}
                        {node.udp ?? 0}
                      </div>
                    </>
                  ) : (
                    <span className="text-default-400">—</span>
                  )}
                </td>
                <td className={td}>
                  <div className="flex flex-col gap-1">
                    {(node.ping ?? []).map((p) => (
                      <div key={p.name} className="flex items-center gap-1.5">
                        <span className="text-default-500">{p.name}</span>
                        <b
                          className={`font-medium tabular-nums ${latencyText(p.ms)}`}
                        >
                          {p.ms === null ? "超时" : p.ms}
                        </b>
                        <RouteTag route={p.route} v6={p.routeV6} />
                      </div>
                    ))}
                    {!node.ping?.length && (
                      <span className="text-default-400">—</span>
                    )}
                  </div>
                </td>
                <td className={`${td} pr-4`}>
                  <div className="flex items-center gap-2.5">
                    <div className="w-24">
                      <Days days={days} node={node} />
                    </div>
                    <span
                      className={`text-xs tabular-nums ${availabilityText(node.availability.d30)}`}
                    >
                      {pct(node.availability.d30)}
                    </span>
                  </div>
                </td>
              </tr>
            );
          })}
        </tbody>
      </table>
    </Card>
  );
}

// ---------------------------------------------------------------- page

function storedView(): View {
  try {
    return localStorage.getItem(VIEW_KEY) === "list" ? "list" : "cards";
  } catch {
    return "cards";
  }
}

/** The public status page: served without a login while the admin has it on. */
export default function StatusPage() {
  const [data, setData] = useState<PublicStatus | null>(null);
  const [error, setError] = useState("");
  // The panel answered that the page is off, as opposed to failing to answer.
  const [disabled, setDisabled] = useState(false);
  const [loading, setLoading] = useState(true);
  const [region, setRegion] = useState("");
  const [query, setQuery] = useState("");
  const [view, setView] = useState<View>(storedView);

  const load = useCallback(async () => {
    try {
      const res = await fetchStatus();

      if (res.code === 0) {
        setData(res.data);
        setError("");
        setDisabled(false);
      } else if (res.code === 404) {
        setData(null);
        setError(res.msg || "状态页未开启");
        setDisabled(true);
      } else {
        // A passing failure: keep what is on screen.
        setError(res.msg || "状态数据暂时不可用");
        setDisabled(false);
      }
    } catch {
      setError("无法连接到面板");
      setDisabled(false);
    } finally {
      setLoading(false);
    }
  }, []);

  usePolling(load, REFRESH_MS);

  const retry = () => {
    setLoading(true);
    void load();
  };

  const title = data?.title || siteConfig.name;

  useEffect(() => {
    document.title = `${title} · 服务器状态`;
  }, [title]);

  const changeView = (v: View) => {
    setView(v);
    try {
      localStorage.setItem(VIEW_KEY, v);
    } catch {
      // Private mode: the choice lasts this visit only.
    }
  };

  const regions = useMemo(() => {
    const m = new Map<string, { flag: string; count: number }>();

    for (const n of data?.nodes ?? []) {
      const [flag, code] = nodeFlag(n);
      const key = code || "其他";
      const r = m.get(key) ?? { flag, count: 0 };

      r.count++;
      m.set(key, r);
    }

    return [...m.entries()].sort((a, b) => b[1].count - a[1].count);
  }, [data]);

  const nodes = useMemo(() => {
    const q = query.trim().toLowerCase();

    return (data?.nodes ?? []).filter(
      (n) =>
        (!region || (nodeFlag(n)[1] || "其他") === region) &&
        (!q || n.name.toLowerCase().includes(q)),
    );
  }, [data, region, query]);

  const t = data?.totals;
  const offline = (data?.nodes ?? []).filter((n) => !n.online);
  const [monthV, monthU] = splitBytes(t ? t.monthUp + t.monthDown : 0);
  const [bwV, bwU] = splitBytes(t ? t.speedUp + t.speedDown : 0);
  const statCols = data?.showTraffic ? "lg:grid-cols-4" : "lg:grid-cols-2";

  return (
    <Shell title={title}>
      {loading && !data ? (
        <div className="flex min-h-[40vh] items-center justify-center">
          <Spinner label="加载中..." size="lg" />
        </div>
      ) : !data || !t ? (
        disabled ? (
          <Notice title={error}>管理员可在「网站配置 → 状态页」中开启</Notice>
        ) : (
          <Notice title={error || "状态数据暂时不可用"} onRetry={retry}>
            请稍后再试
          </Notice>
        )
      ) : (
        <>
          <div className="flex flex-wrap items-end justify-between gap-x-4 gap-y-2">
            <div className="min-w-0">
              <h1 className="text-2xl font-bold tracking-tight sm:text-3xl">
                服务器状态
              </h1>
              <p className="mt-1 text-sm text-default-500">
                {t.nodes} 台服务器的实时状态，每 {REFRESH_MS / 1000} 秒自动刷新
              </p>
            </div>
            <div className="flex items-center gap-2 text-[13px] text-default-500">
              {error ? (
                <Badge title={error} tone="warn">
                  未刷新
                </Badge>
              ) : (
                <Badge>实时</Badge>
              )}
              <span className="tabular-nums">
                更新于{" "}
                {new Date(data.generatedAt).toLocaleTimeString("zh-CN", {
                  hour12: false,
                })}
              </span>
            </div>
          </div>

          {offline.length > 0 && (
            <div className={`${cardClass} flex gap-3 px-4 py-3.5`} role="alert">
              <TriangleAlert
                className="mt-0.5 shrink-0 text-amber-500"
                size={16}
              />
              <div className="min-w-0 text-sm">
                <div className="font-medium">
                  {offline.length === 1
                    ? `${nodeFlag(offline[0])[2]} 已离线`
                    : `${offline.length} 台服务器离线`}
                </div>
                <div className="mt-0.5 text-[13px] text-default-500">
                  {offline.length > 1 &&
                    `${offline.map((n) => nodeFlag(n)[2]).join("、")}。`}
                  其余 {t.online} 台服务器运行正常。
                </div>
              </div>
            </div>
          )}

          <div className={`grid grid-cols-2 gap-3 sm:gap-4 ${statCols}`}>
            <Stat
              icon={<Server size={16} />}
              label="在线服务器"
              sub={
                t.online === t.nodes
                  ? "全部在线"
                  : `${t.nodes - t.online} 台离线`
              }
              unit={`/ ${t.nodes}`}
              value={String(t.online)}
            />
            {data.showTraffic && (
              <Stat
                icon={<Activity size={16} />}
                label="实时带宽"
                sub={`↑ ${speedText(t.speedUp)} · ↓ ${speedText(t.speedDown)}`}
                unit={`${bwU}/s`}
                value={bwV}
              />
            )}
            {data.showTraffic && (
              <Stat
                icon={<ArrowUpRight size={16} />}
                label="本月流量"
                sub={`↑ ${bytesText(t.monthUp)} · ↓ ${bytesText(t.monthDown)}`}
                unit={monthU}
                value={monthV}
              />
            )}
            <Stat
              icon={<CircleCheck size={16} />}
              label={`${data.days.length} 天可用率`}
              sub="所有服务器平均"
              unit={t.d30 === null ? "" : "%"}
              value={t.d30 === null ? "—" : t.d30.toFixed(t.d30 >= 100 ? 0 : 2)}
              valueClass={availabilityText(t.d30)}
            />
          </div>

          <div className="mt-2 flex flex-wrap items-center gap-2">
            <h2 className="text-base font-semibold">服务器</h2>
            <Badge tone="muted">{nodes.length}</Badge>
            <span className="flex-1" />
            <input
              aria-label="筛选服务器"
              className="order-last h-8 w-full rounded-lg border border-border bg-transparent px-3 text-[13px] outline-none placeholder:text-default-400 focus:border-default-400 sm:order-none sm:w-56"
              placeholder="筛选服务器…"
              type="search"
              value={query}
              onChange={(e) => setQuery(e.target.value)}
            />
            <Segmented
              label="视图"
              options={[
                {
                  key: "cards",
                  label: (
                    <>
                      <LayoutGrid size={14} />
                      卡片
                    </>
                  ),
                },
                {
                  key: "list",
                  label: (
                    <>
                      <List size={14} />
                      列表
                    </>
                  ),
                },
              ]}
              value={view}
              onChange={changeView}
            />
          </div>

          {regions.length > 1 && (
            <nav
              aria-label="按地区筛选"
              className="-mt-1 flex flex-wrap gap-1.5"
            >
              {[
                ["", { flag: "", count: data.nodes.length }] as const,
                ...regions,
              ].map(([key, r]) => (
                <button
                  key={key || "all"}
                  aria-pressed={region === key}
                  className={`inline-flex h-7 items-center gap-1.5 rounded-md border px-2.5 text-xs font-medium transition-colors ${
                    region === key
                      ? "border-primary bg-primary text-primary-foreground"
                      : "border-dashed border-border hover:bg-default-100"
                  }`}
                  type="button"
                  onClick={() => setRegion(key)}
                >
                  {r.flag && (
                    <span className="text-sm leading-none">{r.flag}</span>
                  )}
                  {key || "全部"}
                  <span className="tabular-nums opacity-60">{r.count}</span>
                </button>
              ))}
            </nav>
          )}

          {nodes.length === 0 ? (
            <Notice title={query ? "没有匹配的服务器" : "暂无服务器"} />
          ) : view === "list" ? (
            <NodeTable
              days={data.days}
              nodes={nodes}
              showTraffic={data.showTraffic}
            />
          ) : (
            <div className="grid grid-cols-1 gap-3 sm:grid-cols-2 lg:grid-cols-3 xl:grid-cols-4 2xl:grid-cols-5">
              {nodes.map((node) => (
                <NodeCard
                  key={node.id}
                  days={data.days}
                  node={node}
                  showTraffic={data.showTraffic}
                />
              ))}
            </div>
          )}

          <footer className="flex flex-wrap items-center justify-between gap-2 pt-1 text-xs text-default-500">
            <span>
              延迟为最近一次测试，回程每天检测一次 · 点击服务器查看历史 · 按{" "}
              {data.timezone} 统计
            </span>
            <span className="inline-flex flex-wrap items-center gap-x-3 gap-y-1">
              <span className="inline-flex items-center gap-1">
                <i className="inline-block h-2 w-2 rounded-[2px] bg-emerald-500" />
                离线 ≤ {GOOD_DAY_DOWN_MIN} 分钟
              </span>
              <span className="inline-flex items-center gap-1">
                <i className="inline-block h-2 w-2 rounded-[2px] bg-amber-500" />
                {GOOD_DAY_DOWN_MIN}–30 分钟
              </span>
              <span className="inline-flex items-center gap-1">
                <i className="inline-block h-2 w-2 rounded-[2px] bg-red-500" />
                超过 30 分钟
              </span>
            </span>
          </footer>
        </>
      )}
    </Shell>
  );
}
