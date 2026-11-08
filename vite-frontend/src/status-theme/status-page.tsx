import type { PublicStatus, PublicStatusNode, PublicStatusPing } from "@/api";

import { Fragment, useCallback, useEffect, useMemo, useState } from "react";
import { LayoutGrid, List, TriangleAlert } from "lucide-react";

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
  groupOf,
  priceText,
  quotaOf,
} from "./ui";

import { Spinner } from "@/shadcn-bridge/heroui/spinner";
import { siteConfig } from "@/config/site";

const REFRESH_MS = 10_000;
const VIEW_KEY = "flyx-status:view";

type View = "cards" | "list";

// ---------------------------------------------------------------- pieces

interface StatItem {
  label: string;
  value: string;
  unit?: string;
  sub?: string;
  valueClass?: string;
}

/** The page's headline figures in one card, split by hairlines. */
function StatStrip({ items }: { items: StatItem[] }) {
  // 1px gaps over the border colour draw the hairlines: 2 × 2 on a phone,
  // one row on a desktop.
  return (
    <div
      className={`${cardClass} grid grid-cols-2 gap-px overflow-hidden bg-border dark:bg-border ${
        items.length > 2 ? "lg:grid-cols-4" : ""
      }`}
    >
      {items.map((it) => (
        <div
          key={it.label}
          className="flex min-w-0 flex-col gap-0.5 bg-background px-4 py-3 sm:px-5 dark:bg-content1"
        >
          <span className="truncate text-xs text-default-500">{it.label}</span>
          <span className="flex items-baseline gap-1 truncate">
            <span
              className={`text-xl font-semibold tracking-tight tabular-nums ${it.valueClass ?? ""}`}
            >
              {it.value}
            </span>
            {it.unit && (
              <span className="text-xs text-default-500">{it.unit}</span>
            )}
          </span>
          {it.sub && (
            <span className="truncate text-xs tabular-nums text-default-500">
              {it.sub}
            </span>
          )}
        </div>
      ))}
    </div>
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
  const quota = showTraffic ? quotaOf(node) : null;
  const price = priceText(node);

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
        {node.note && (
          <span className="truncate text-xs text-default-600" title={node.note}>
            {node.note}
          </span>
        )}
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
                {quota && (
                  <div
                    className="grid grid-cols-[2.5rem_minmax(0,1fr)_auto] items-center gap-2.5 text-[13px]"
                    title="本期已用 / 每月额度"
                  >
                    <span className="text-default-500">额度</span>
                    <Bar value={quota.pct} />
                    <span className="font-medium tabular-nums">
                      {quota.text}
                    </span>
                  </div>
                )}
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
          <span className="min-w-0 truncate">
            {days.length} 天
            {price && <span className="ml-2 text-foreground">{price}</span>}
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

/** CPU, memory and disk in one table cell: a bar and a figure each. */
function Resources({ node, live }: { node: PublicStatusNode; live: boolean }) {
  const rows: Array<[string, number | undefined, string | undefined]> = [
    ["CPU", node.cpu, live && node.cores ? `${node.cores} 核` : undefined],
    ["内存", node.mem, capacity(node.memUsed, node.memTotal)],
    ["磁盘", node.disk, capacity(node.diskUsed, node.diskTotal)],
  ];

  return (
    <div className="grid w-44 grid-cols-[2rem_minmax(0,1fr)_2.75rem] items-center gap-x-2 gap-y-1 text-xs">
      {rows.map(([label, value, title]) => {
        const v = live ? value : undefined;

        return (
          <Fragment key={label}>
            <span className="text-default-500">{label}</span>
            <span title={live ? title : undefined}>
              <Bar value={v ?? 0} />
            </span>
            <span
              className={`text-right font-medium tabular-nums ${
                v === undefined ? "text-default-400" : usageText(v)
              }`}
            >
              {v === undefined ? "—" : `${v.toFixed(1)}%`}
            </span>
          </Fragment>
        );
      })}
    </div>
  );
}

/** Servers listed under one heading (a group or a region). */
interface Section {
  key: string;
  label: string;
  flag: string;
  nodes: PublicStatusNode[];
}

function NodeTable({
  sections,
  days,
  showTraffic,
}: {
  /** One section without a heading lists the servers plainly. */
  sections: Section[];
  days: string[];
  showTraffic: boolean;
}) {
  const all = sections.flatMap((x) => x.nodes);
  const showPrice = all.some((n) => priceText(n));
  const columns = 6 + (showTraffic ? 2 : 0) + (showPrice ? 1 : 0);
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
            <th className={th}>资源</th>
            {showTraffic && <th className={th}>网速</th>}
            {showTraffic && <th className={th}>本月流量</th>}
            <th className={th}>TCP / UDP</th>
            <th className={th}>延迟</th>
            {showPrice && <th className={th}>价格</th>}
            <th className={th}>{days.length} 天可用率</th>
          </tr>
        </thead>
        {sections.map((section) => (
          <tbody key={section.key}>
            {sections.length > 1 && (
              <tr className="border-b border-border bg-default-50/60">
                <th
                  className="px-4 py-2 text-left text-xs font-medium text-default-500"
                  colSpan={columns}
                >
                  {section.flag && (
                    <span className="mr-1.5">{section.flag}</span>
                  )}
                  {section.label}
                  <span className="ml-1.5 tabular-nums opacity-60">
                    {section.nodes.length}
                  </span>
                </th>
              </tr>
            )}
            {section.nodes.map((node) => {
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
                      <span className="flex min-w-0 max-w-[13rem] flex-col">
                        <span className="truncate font-medium" title={name}>
                          {name}
                        </span>
                        {(hostLine(node) || node.note) && (
                          <span
                            className="truncate text-xs text-default-500"
                            title={[hostLine(node), node.note]
                              .filter(Boolean)
                              .join(" · ")}
                          >
                            {[hostLine(node), node.note]
                              .filter(Boolean)
                              .join(" · ")}
                          </span>
                        )}
                      </span>
                    </a>
                  </td>
                  <td className={td}>{statusBadge(node)}</td>
                  <td className={td}>
                    <Resources live={live} node={node} />
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
                      {quotaOf(node) && (
                        <div
                          className="mt-1 w-28"
                          title={`额度 ${quotaOf(node)?.text}`}
                        >
                          <Bar value={quotaOf(node)?.pct ?? 0} />
                        </div>
                      )}
                    </td>
                  )}
                  <td className={`${td} tabular-nums`}>
                    {live ? (
                      <span>
                        {node.tcp ?? 0}
                        <span className="text-default-400"> / </span>
                        {node.udp ?? 0}
                      </span>
                    ) : (
                      <span className="text-default-400">—</span>
                    )}
                  </td>
                  <td className={td}>
                    <div className="flex flex-col gap-1">
                      {(node.ping ?? []).map((p) => (
                        <div key={p.name} className="flex items-center gap-1.5">
                          <span
                            className="max-w-[4.5rem] truncate text-default-500"
                            title={p.name}
                          >
                            {p.name}
                          </span>
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
                  {showPrice && (
                    <td className={`${td} tabular-nums`}>
                      {priceText(node) || (
                        <span className="text-default-400">—</span>
                      )}
                    </td>
                  )}
                  <td className={`${td} pr-4`}>
                    <div className="flex items-center gap-2.5">
                      <div className="w-20">
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
        ))}
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

  const groupBy = data?.groupBy ?? "region";

  // The groups (the admin's, or regions) in the order the servers are
  // listed, "其他" last.
  const groups = useMemo(() => {
    const m = new Map<string, { label: string; flag: string; count: number }>();

    for (const n of data?.nodes ?? []) {
      const g = groupOf(n, groupBy);
      const r = m.get(g.key) ?? { label: g.label, flag: g.flag, count: 0 };

      r.count++;
      m.set(g.key, r);
    }

    return [...m.entries()].sort(
      (a, b) => Number(a[0] === "其他") - Number(b[0] === "其他"),
    );
  }, [data, groupBy]);

  const nodes = useMemo(() => {
    const q = query.trim().toLowerCase();

    return (data?.nodes ?? []).filter(
      (n) =>
        (!region || groupOf(n, groupBy).key === region) &&
        (!q ||
          n.name.toLowerCase().includes(q) ||
          (n.group ?? "").toLowerCase().includes(q) ||
          (n.note ?? "").toLowerCase().includes(q)),
    );
  }, [data, region, query, groupBy]);

  // "全部" with several groups lists them under headings.
  const sections: Section[] = useMemo(() => {
    if (region || groups.length < 2) {
      return [{ key: "all", label: "", flag: "", nodes }];
    }

    return groups
      .map(([key, g]) => ({
        key,
        label: g.label,
        flag: g.flag,
        nodes: nodes.filter((n) => groupOf(n, groupBy).key === key),
      }))
      .filter((x) => x.nodes.length > 0);
  }, [groups, nodes, region, groupBy]);

  const t = data?.totals;
  const offline = (data?.nodes ?? []).filter((n) => !n.online);
  const [monthV, monthU] = splitBytes(t ? t.monthUp + t.monthDown : 0);
  const [bwV, bwU] = splitBytes(t ? t.speedUp + t.speedDown : 0);

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
          <div className="flex flex-wrap items-center justify-between gap-x-4 gap-y-1">
            <h1 className="text-xl font-semibold tracking-tight">服务器状态</h1>
            <div
              className="flex items-center gap-2 text-[13px] text-default-500"
              title={`每 ${REFRESH_MS / 1000} 秒自动刷新`}
            >
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

          <StatStrip
            items={[
              {
                label: "在线服务器",
                value: String(t.online),
                unit: `/ ${t.nodes}`,
                sub:
                  t.online === t.nodes
                    ? "全部在线"
                    : `${t.nodes - t.online} 台离线`,
                valueClass:
                  t.online < t.nodes ? "text-red-600 dark:text-red-400" : "",
              },
              ...(data.showTraffic
                ? [
                    {
                      label: "实时带宽",
                      value: bwV,
                      unit: `${bwU}/s`,
                      sub: `↑ ${speedText(t.speedUp)} · ↓ ${speedText(t.speedDown)}`,
                    },
                    {
                      label: "本月流量",
                      value: monthV,
                      unit: monthU,
                      sub: `↑ ${bytesText(t.monthUp)} · ↓ ${bytesText(t.monthDown)}`,
                    },
                  ]
                : []),
              {
                label: `${data.days.length} 天可用率`,
                value:
                  t.d30 === null ? "—" : t.d30.toFixed(t.d30 >= 100 ? 0 : 2),
                unit: t.d30 === null ? "" : "%",
                sub: "所有服务器平均",
                valueClass: availabilityText(t.d30),
              },
            ]}
          />

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

          {groups.length > 1 && (
            <nav
              aria-label={groupBy === "group" ? "按分组筛选" : "按地区筛选"}
              className="-mt-1 flex flex-wrap gap-1.5"
            >
              {[
                [
                  "",
                  { label: "全部", flag: "", count: data.nodes.length },
                ] as const,
                ...groups,
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
                  {r.label}
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
              sections={sections}
              showTraffic={data.showTraffic}
            />
          ) : (
            sections.map((section) => (
              <section key={section.key} className="flex flex-col gap-2.5">
                {sections.length > 1 && (
                  <h3 className="flex items-center gap-1.5 pt-1 text-sm font-medium text-default-600">
                    {section.flag && <span>{section.flag}</span>}
                    {section.label}
                    <span className="tabular-nums text-default-400">
                      {section.nodes.length}
                    </span>
                  </h3>
                )}
                {/* As many columns as fit cards at least 18 rem wide: 4 on
                    a desktop page, so names and latency lines fit. */}
                <div className="grid grid-cols-[repeat(auto-fill,minmax(min(100%,18rem),1fr))] gap-3">
                  {section.nodes.map((node) => (
                    <NodeCard
                      key={node.id}
                      days={data.days}
                      node={node}
                      showTraffic={data.showTraffic}
                    />
                  ))}
                </div>
              </section>
            ))
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
