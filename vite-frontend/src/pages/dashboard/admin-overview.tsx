import type { SubscriptionItem } from "@/api";
import type {
  InboundApiItem,
  InboundRelayApiItem,
  KernelApiItem,
  NodeApiItem,
} from "@/api/types";

import { useCallback, useEffect, useState, useRef } from "react";
import { useNavigate } from "react-router-dom";
import {
  ChevronRight,
  CircleAlert,
  Globe,
  Link2,
  Rss,
  Server,
  TriangleAlert,
} from "lucide-react";

import { useVisiblePolling } from "@/hooks/use-visible-polling";
import {
  getInboundList,
  getInboundRelays,
  getKernelList,
  getNodeList,
  getSubscriptions,
} from "@/api";
import { AnimatedPage } from "@/components/animated-page";
import { PageLoadingState } from "@/components/page-state";
import {
  markPageCached,
  pageCached,
  useCachedState,
} from "@/hooks/use-cached-state";
import { formatTraffic } from "@/utils/traffic";

const DAY = 86400000;
const SOON = 3 * DAY;

function daysLeft(ms: number): string {
  const d = Math.ceil((ms - Date.now()) / DAY);

  return d <= 0 ? "已到期" : `剩 ${d} 天`;
}

/** One problem; problems of the same kind are shown as one group. */
interface Issue {
  kind: IssueKind;
  level: "danger" | "warning";
  /** What the problem is about, shown as a chip, e.g. a node's name. */
  name: string;
  /** Extra detail on the chip's tooltip. */
  detail: string;
}

type IssueKind =
  | "offline"
  | "expired"
  | "expiring"
  | "kernel"
  | "inbound"
  | "probe"
  | "subflow"
  | "subexpire";

const KINDS: Record<
  IssueKind,
  { label: (n: number) => string; hint: string; path: string }
> = {
  offline: {
    label: (n) => `${n} 个服务器离线`,
    hint: "agent 没有连接面板，检查服务器上的 agent 是否在运行",
    path: "/node",
  },
  expired: {
    label: (n) => `${n} 个服务器已到期`,
    hint: "续费后在服务器里更新到期时间",
    path: "/node",
  },
  expiring: {
    label: (n) => `${n} 个服务器即将到期`,
    hint: "3 天内到期",
    path: "/node",
  },
  kernel: {
    label: (n) => `${n} 个内核出错`,
    hint: "在服务器 → 内核里查看日志",
    path: "/node",
  },
  inbound: {
    label: (n) => `${n} 个节点未运行`,
    hint: "内核已停止或等待下发，服务器上线后会自动补上",
    path: "/inbound",
  },
  probe: {
    label: (n) => `${n} 个节点连不上`,
    hint: "从面板服务器检测",
    path: "/inbound",
  },
  subflow: {
    label: (n) => `${n} 个分享订阅流量快用完`,
    hint: "用到 90% 以上；用完后自动断开",
    path: "/subscription",
  },
  subexpire: {
    label: (n) => `${n} 个分享订阅即将到期`,
    hint: "3 天内到期或已到期",
    path: "/subscription",
  },
};

const TONES = {
  blue: [
    "bg-blue-100 dark:bg-blue-500/20",
    "text-blue-600 dark:text-blue-400",
    "bg-blue-500",
  ],
  green: [
    "bg-green-100 dark:bg-green-500/20",
    "text-green-600 dark:text-green-400",
    "bg-green-500",
  ],
  purple: [
    "bg-purple-100 dark:bg-purple-500/20",
    "text-purple-600 dark:text-purple-400",
    "bg-purple-500",
  ],
  orange: [
    "bg-orange-100 dark:bg-orange-500/20",
    "text-orange-600 dark:text-orange-400",
    "bg-orange-500",
  ],
} as const;

/** A headline figure, in the upstream metric card style. */
function Metric({
  label,
  icon: Icon,
  tone,
  value,
  total,
  foot,
  footTone,
  footRight,
  onPress,
}: {
  label: string;
  icon: typeof Server;
  tone: keyof typeof TONES;
  value: number;
  /** Shown as "value / total" with a bar, when set. */
  total?: number;
  foot: string;
  footTone?: "danger" | "warning" | "success";
  footRight?: string;
  onPress: () => void;
}) {
  const [chip, iconColor, bar] = TONES[tone];
  const pct = total ? Math.round((value / total) * 100) : 0;

  return (
    <button
      className="rounded-2xl border border-divider bg-content1 p-4 text-left shadow-sm transition-shadow hover:shadow-md lg:p-5"
      type="button"
      onClick={onPress}
    >
      <div className="flex items-center justify-between gap-2">
        <span className="truncate text-sm text-default-600">{label}</span>
        <span className={`flex-shrink-0 rounded-xl p-2 ${chip}`}>
          <Icon className={`h-4 w-4 lg:h-5 lg:w-5 ${iconColor}`} />
        </span>
      </div>
      <div className="mt-2 text-2xl font-bold lg:text-3xl">
        {value}
        {total !== undefined && (
          <span className="ml-1 text-base font-normal text-default-400">
            / {total}
          </span>
        )}
      </div>
      <div className="mt-3 h-1.5 overflow-hidden rounded-full bg-default-100">
        {total !== undefined && total > 0 && (
          <div
            className={`h-full rounded-full ${bar}`}
            style={{ width: `${pct}%` }}
          />
        )}
      </div>
      <div className="mt-1.5 flex items-center justify-between gap-2 text-xs">
        <span
          className={`truncate ${
            footTone === "danger"
              ? "text-danger"
              : footTone === "warning"
                ? "text-warning-600"
                : footTone === "success"
                  ? "text-success-600"
                  : "text-default-500"
          }`}
        >
          {foot}
        </span>
        {footRight && (
          <span className="shrink-0 text-default-400">{footRight}</span>
        )}
      </div>
    </button>
  );
}

function Panel({
  title,
  badge,
  action,
  children,
}: {
  title: string;
  badge?: React.ReactNode;
  action?: React.ReactNode;
  children: React.ReactNode;
}) {
  return (
    <section className="rounded-2xl border border-divider bg-content1 p-4 shadow-sm lg:p-5">
      <div className="mb-3 flex items-center gap-2">
        <h2 className="text-sm font-semibold lg:text-base">{title}</h2>
        {badge}
        {action && <div className="ml-auto">{action}</div>}
      </div>
      {children}
    </section>
  );
}

const PAGE = "admin-overview";

/** The admin's overview: health of nodes → landings → relays → subscriptions. */
export function AdminOverview() {
  const navigate = useNavigate();
  // A revisit shows the last data at once and reloads in the background.
  const [loading, setLoading] = useState(() => !pageCached(PAGE));
  const [nodes, setNodes] = useCachedState<NodeApiItem[]>(PAGE, "nodes", []);
  const [kernels, setKernels] = useCachedState<KernelApiItem[]>(
    PAGE,
    "kernels",
    [],
  );
  const [inbounds, setInbounds] = useCachedState<InboundApiItem[]>(
    PAGE,
    "inbounds",
    [],
  );
  const [relays, setRelays] = useCachedState<InboundRelayApiItem[]>(
    PAGE,
    "relays",
    [],
  );
  const [subs, setSubs] = useCachedState<SubscriptionItem[]>(PAGE, "subs", []);

  const loadPending = useRef(0);

  const load = useCallback(async (options?: { silent?: boolean }) => {
    const silent = options?.silent ?? false;

    if (silent && loadPending.current > 0) return;
    loadPending.current += 1;
    try {
      const [n, k, i, r, s] = await Promise.all([
        getNodeList().catch(() => null),
        getKernelList().catch(() => null),
        getInboundList().catch(() => null),
        getInboundRelays(0).catch(() => null),
        getSubscriptions().catch(() => null),
      ]);

      if (n?.code === 0) setNodes(n.data ?? []);
      if (k?.code === 0) setKernels(k.data ?? []);
      if (i?.code === 0) setInbounds(i.data ?? []);
      if (r?.code === 0) setRelays(r.data ?? []);
      if (s?.code === 0) setSubs(s.data?.items ?? []);
      markPageCached(PAGE);
    } finally {
      loadPending.current -= 1;
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    void load();
  }, [load]);
  useVisiblePolling(
    useCallback(() => load({ silent: true }), [load]),
    30_000,
  );

  if (loading) return <PageLoadingState message="正在加载..." />;

  const now = Date.now();
  const online = nodes.filter((n) => n.status === 1).length;
  const kernelDown = (i: InboundApiItem) =>
    i.enabled && !i.external && i.kernelStatus !== "running";
  // A stopped kernel is reported as such, not as a failed probe.
  const probeDown = (i: InboundApiItem) =>
    i.enabled &&
    !kernelDown(i) &&
    Boolean(i.probe && !i.probe.skipped && !i.probe.ok);
  const badInbound = (i: InboundApiItem) => kernelDown(i) || probeDown(i);
  const enabledInbounds = inbounds.filter((i) => i.enabled);
  const runningInbounds = enabledInbounds.filter((i) => !badInbound(i));
  const relaysUp = relays.filter(
    (r) => r.forwardExists && r.forwardStatus === 1,
  );
  const shares = subs.filter((s) => s.kind === "share");
  const activeSubs = subs.filter((s) => s.active);

  const issues: Issue[] = [];

  for (const n of nodes) {
    if (n.status !== 1) {
      issues.push({
        kind: "offline",
        level: "danger",
        name: n.name,
        detail: "agent 没有连接面板",
      });
    }
    if (
      n.expiryTime &&
      n.expiryTime - now <= SOON &&
      !n.expiryReminderDismissed
    ) {
      const expired = n.expiryTime <= now;

      issues.push({
        kind: expired ? "expired" : "expiring",
        level: expired ? "danger" : "warning",
        name: n.name,
        detail: daysLeft(n.expiryTime),
      });
    }
  }
  for (const k of kernels) {
    if (k.status === "error") {
      issues.push({
        kind: "kernel",
        level: "danger",
        name: `${k.nodeName} · ${k.kernelType}`,
        detail: k.lastError || "查看内核日志",
      });
    }
  }
  for (const i of inbounds) {
    // Kernel errors are listed per kernel above.
    if (kernelDown(i) && i.kernelStatus !== "error") {
      issues.push({
        kind: "inbound",
        level: "warning",
        name: i.name,
        detail: `${i.nodeName} · ${i.kernelStatus}`,
      });
    }
    if (probeDown(i)) {
      issues.push({
        kind: "probe",
        level: "warning",
        name: i.name,
        detail: i.probe?.error || "连不上",
      });
    }
  }
  for (const s of shares) {
    const used = s.uploadBytes + s.downloadBytes;

    if (s.enabled && s.trafficLimit && used >= s.trafficLimit * 0.9) {
      issues.push({
        kind: "subflow",
        level: s.overQuota ? "danger" : "warning",
        name: s.name,
        detail: `${formatTraffic(used)} / ${formatTraffic(s.trafficLimit)}`,
      });
    }
    if (s.enabled && s.expireTime && s.expireTime - now <= SOON) {
      issues.push({
        kind: "subexpire",
        level: s.expireTime <= now ? "danger" : "warning",
        name: s.name,
        detail: daysLeft(s.expireTime),
      });
    }
  }
  const groups = (Object.keys(KINDS) as IssueKind[])
    .map((kind) => {
      const items = issues.filter((i) => i.kind === kind);

      return {
        kind,
        items,
        level: items.some((i) => i.level === "danger") ? "danger" : "warning",
      };
    })
    .filter((g) => g.items.length > 0)
    .sort((a, b) => (a.level === b.level ? 0 : a.level === "danger" ? -1 : 1));

  const up = inbounds.reduce((t, i) => t + i.uploadBytes, 0);
  const down = inbounds.reduce((t, i) => t + i.downloadBytes, 0);
  const top = [...inbounds]
    .filter((i) => !i.external && i.uploadBytes + i.downloadBytes > 0)
    .sort(
      (a, b) =>
        b.uploadBytes + b.downloadBytes - (a.uploadBytes + a.downloadBytes),
    )
    .slice(0, 4);
  const topMax = top[0] ? top[0].uploadBytes + top[0].downloadBytes : 1;

  const expiring = [
    ...nodes
      .filter(
        (n) =>
          n.expiryTime &&
          n.expiryTime - now <= 30 * DAY &&
          !n.expiryReminderDismissed,
      )
      .map((n) => ({
        key: `n${n.id}`,
        kind: "服务器",
        name: n.name,
        at: n.expiryTime ?? 0,
        path: "/node",
      })),
    ...shares
      .filter(
        (s) => s.enabled && s.expireTime && s.expireTime - now <= 30 * DAY,
      )
      .map((s) => ({
        key: `s${s.id}`,
        kind: "订阅",
        name: s.name,
        at: s.expireTime,
        path: "/subscription",
      })),
  ]
    .sort((a, b) => a.at - b.at)
    .slice(0, 5);

  const shownNodes = [...nodes].sort((a, b) => a.status - b.status).slice(0, 6);

  return (
    <AnimatedPage className="space-y-4 px-3 py-6 lg:px-6">
      <div>
        <h1 className="text-lg font-semibold">概览</h1>
        <p className="text-xs text-default-500">
          {new Date().toLocaleDateString("zh-CN", {
            month: "long",
            day: "numeric",
            weekday: "long",
          })}
          {" · 每 30 秒自动刷新"}
        </p>
      </div>

      <div className="grid grid-cols-2 gap-3 lg:grid-cols-4 lg:gap-4">
        <Metric
          foot={
            nodes.length === 0
              ? "还没有服务器"
              : online === nodes.length
                ? "全部在线"
                : `${nodes.length - online} 个离线`
          }
          footRight={
            nodes.length
              ? `${Math.round((online / nodes.length) * 100)}%`
              : undefined
          }
          footTone={
            nodes.length > online
              ? "danger"
              : nodes.length
                ? "success"
                : undefined
          }
          icon={Server}
          label="服务器在线"
          tone="blue"
          total={nodes.length}
          value={online}
          onPress={() => navigate("/node")}
        />
        <Metric
          foot={
            enabledInbounds.length === 0
              ? inbounds.length === 0
                ? "还没有节点"
                : "全部停用"
              : runningInbounds.length === enabledInbounds.length
                ? "全部正常"
                : `${enabledInbounds.length - runningInbounds.length} 个异常`
          }
          footRight={
            inbounds.length > enabledInbounds.length
              ? `${inbounds.length - enabledInbounds.length} 个停用`
              : undefined
          }
          footTone={
            runningInbounds.length < enabledInbounds.length
              ? "warning"
              : enabledInbounds.length
                ? "success"
                : undefined
          }
          icon={Globe}
          label="节点运行"
          tone="green"
          total={enabledInbounds.length}
          value={runningInbounds.length}
          onPress={() => navigate("/inbound")}
        />
        <Metric
          foot={
            relays.length === 0
              ? "还没有中转"
              : relaysUp.length === relays.length
                ? "全部正常"
                : `${relays.length - relaysUp.length} 条未启用`
          }
          footTone={
            relays.length > relaysUp.length
              ? "warning"
              : relays.length
                ? "success"
                : undefined
          }
          icon={Link2}
          label="中转线路"
          tone="purple"
          total={relays.length || undefined}
          value={relaysUp.length}
          onPress={() => navigate("/relay")}
        />
        <Metric
          foot={
            subs.length === 0
              ? "还没有订阅"
              : activeSubs.length === subs.length
                ? "全部有效"
                : `${subs.length - activeSubs.length} 个失效`
          }
          footRight={shares.length ? `分享 ${shares.length} 个` : undefined}
          footTone={
            activeSubs.length < subs.length
              ? "warning"
              : subs.length
                ? "success"
                : undefined
          }
          icon={Rss}
          label="有效订阅"
          tone="orange"
          total={subs.length}
          value={activeSubs.length}
          onPress={() => navigate("/subscription")}
        />
      </div>

      <div className="grid grid-cols-1 gap-4 lg:grid-cols-[minmax(0,1.55fr)_minmax(0,1fr)]">
        <div className="space-y-4">
          <Panel
            badge={
              issues.length > 0 && (
                <span className="rounded-full bg-danger-50 px-2 text-xs font-semibold text-danger dark:bg-danger-500/20">
                  {issues.length}
                </span>
              )
            }
            title="需要处理"
          >
            {groups.length === 0 ? (
              <p className="py-3 text-center text-sm text-success-600">
                一切正常
              </p>
            ) : (
              <div className="space-y-2.5">
                {groups.map((g) => {
                  const danger = g.level === "danger";
                  const meta = KINDS[g.kind];
                  const Icon = danger ? CircleAlert : TriangleAlert;
                  const hint =
                    g.kind === "probe" && g.items[0].detail
                      ? `${meta.hint}：${g.items[0].detail}`
                      : meta.hint;

                  return (
                    <button
                      key={g.kind}
                      className={`flex w-full items-start gap-3 rounded-xl border p-3 text-left transition-colors ${
                        danger
                          ? "border-danger-100 bg-danger-50/70 hover:bg-danger-50 dark:border-danger-500/20 dark:bg-danger-500/10"
                          : "border-warning-100 bg-warning-50/70 hover:bg-warning-50 dark:border-warning-500/20 dark:bg-warning-500/10"
                      }`}
                      type="button"
                      onClick={() => navigate(meta.path)}
                    >
                      <span
                        className={`flex h-8 w-8 shrink-0 items-center justify-center rounded-lg text-white ${danger ? "bg-danger" : "bg-warning"}`}
                      >
                        <Icon className="h-4 w-4" />
                      </span>
                      <span className="min-w-0 flex-1">
                        <span className="block text-sm font-semibold">
                          {meta.label(g.items.length)}
                        </span>
                        <span className="block truncate text-xs text-default-500">
                          {hint}
                        </span>
                        <span className="mt-1.5 flex flex-wrap gap-1.5">
                          {g.items.slice(0, 6).map((it) => (
                            <span
                              key={it.name}
                              className="max-w-[12rem] truncate rounded-full border border-divider bg-content1 px-2 py-0.5 text-xs text-default-700"
                              title={it.detail}
                            >
                              {it.name}
                            </span>
                          ))}
                          {g.items.length > 6 && (
                            <span className="rounded-full border border-divider bg-content1 px-2 py-0.5 text-xs text-default-500">
                              +{g.items.length - 6}
                            </span>
                          )}
                        </span>
                      </span>
                      <span className="hidden shrink-0 items-center self-center text-xs text-default-500 sm:flex">
                        去处理
                        <ChevronRight className="h-3.5 w-3.5" />
                      </span>
                    </button>
                  );
                })}
              </div>
            )}
          </Panel>

          {nodes.length > 0 && (
            <Panel
              action={
                <button
                  className="flex items-center text-xs text-primary"
                  type="button"
                  onClick={() => navigate("/node")}
                >
                  查看全部
                  <ChevronRight className="h-3.5 w-3.5" />
                </button>
              }
              title="服务器状态"
            >
              <div className="grid grid-cols-1 gap-2 sm:grid-cols-2 xl:grid-cols-3">
                {shownNodes.map((n) => {
                  const up = n.status === 1;
                  const ip = String(n.serverIpV4 || n.serverIp || "");

                  return (
                    <button
                      key={n.id}
                      className="flex items-center gap-2.5 rounded-xl border border-divider px-3 py-2 text-left transition-colors hover:bg-default-50"
                      type="button"
                      onClick={() => navigate("/node")}
                    >
                      <span
                        className={`h-2 w-2 shrink-0 rounded-full ${up ? "bg-success" : "bg-danger"}`}
                      />
                      <span className="min-w-0">
                        <span className="block truncate text-sm font-medium">
                          {n.name}
                        </span>
                        <span className="block truncate font-mono text-xs text-default-500">
                          {up ? "在线" : "离线"}
                          {ip ? ` · ${ip}` : ""}
                        </span>
                      </span>
                    </button>
                  );
                })}
              </div>
            </Panel>
          )}
        </div>

        <div className="space-y-4">
          <Panel
            action={<span className="text-xs text-default-400">累计</span>}
            title="节点流量"
          >
            <div className="mb-3 grid grid-cols-2 gap-2">
              {(
                [
                  ["↑ 上传", up],
                  ["↓ 下载", down],
                ] as Array<[string, number]>
              ).map(([label, bytes]) => (
                <div
                  key={label}
                  className="rounded-xl bg-default-100/70 px-3 py-2"
                >
                  <div className="text-xs text-default-500">{label}</div>
                  <div className="font-mono text-base font-semibold">
                    {formatTraffic(bytes)}
                  </div>
                </div>
              ))}
            </div>
            {top.length === 0 ? (
              <p className="py-1 text-center text-xs text-default-500">
                还没有流量数据（导入的节点不统计流量）
              </p>
            ) : (
              <div className="space-y-2.5">
                {top.map((i) => {
                  const total = i.uploadBytes + i.downloadBytes;

                  return (
                    <div key={i.id}>
                      <div className="mb-1 flex items-center justify-between gap-2 text-xs">
                        <span className="truncate">{i.name}</span>
                        <span className="shrink-0 font-mono text-default-500">
                          {formatTraffic(total)}
                        </span>
                      </div>
                      <div className="h-1.5 overflow-hidden rounded-full bg-default-100">
                        <div
                          className="h-full rounded-full bg-primary"
                          style={{
                            width: `${Math.max(4, (total / topMax) * 100)}%`,
                          }}
                        />
                      </div>
                    </div>
                  );
                })}
              </div>
            )}
          </Panel>

          {expiring.length > 0 && (
            <Panel title="即将到期">
              <div className="divide-y divide-divider">
                {expiring.map((e) => {
                  const days = Math.ceil((e.at - now) / DAY);

                  return (
                    <button
                      key={e.key}
                      className="flex w-full items-center gap-2.5 py-2 text-left text-sm"
                      type="button"
                      onClick={() => navigate(e.path)}
                    >
                      <span
                        className={`shrink-0 rounded-full px-2 py-0.5 text-xs ${e.kind === "服务器" ? "bg-warning-50 text-warning-700 dark:bg-warning-500/20" : "bg-orange-50 text-orange-700 dark:bg-orange-500/20"}`}
                      >
                        {e.kind}
                      </span>
                      <span className="min-w-0 flex-1 truncate">{e.name}</span>
                      <span
                        className={`shrink-0 text-xs ${days <= 3 ? "text-warning-600" : "text-default-500"}`}
                      >
                        {daysLeft(e.at)}
                      </span>
                    </button>
                  );
                })}
              </div>
            </Panel>
          )}
        </div>
      </div>
    </AnimatedPage>
  );
}
