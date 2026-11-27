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
  AlertTriangle,
  ArrowRight,
  CheckCircle2,
  ChevronRight,
  Clock,
  Globe,
  Link2,
  RefreshCw,
  Rss,
  Server,
  Settings,
} from "lucide-react";

import { useVisiblePolling } from "@/hooks/use-visible-polling";
import {
  getInboundList,
  getInboundRelays,
  getKernelList,
  getNodeList,
  getNodeMonthTraffic,
  getSubscriptions,
} from "@/api";
import { AnimatedPage } from "@/components/animated-page";
import { PageLoadingState } from "@/components/page-state";
import { probePoints } from "@/components/latency-cell";
import {
  markPageCached,
  pageCached,
  useCachedState,
} from "@/hooks/use-cached-state";
import { t as translateStatic, useI18n } from "@/i18n";
import { nodeRegionFlag } from "@/pages/node/region";
import { Button } from "@/shadcn-bridge/heroui/button";
import { formatTraffic } from "@/utils/traffic";

const DAY = 86400000;
const SOON = 3 * DAY;

function daysLeft(ms: number): string {
  const d = Math.ceil((ms - Date.now()) / DAY);

  return d <= 0
    ? translateStatic("已到期")
    : translateStatic("剩 {days} 天", { days: d });
}

function relayEntryName(r: InboundRelayApiItem, fallback: string): string {
  if (r.latency?.entry?.trim()) return r.latency.entry.trim();
  const full = r.name || r.tunnelName || fallback;
  const cut = full.lastIndexOf(" → ");

  return cut > 0 ? full.slice(0, cut) : full;
}

function relayMs(r: InboundRelayApiItem): number | undefined {
  const lat = r.latency;

  if (!lat || lat.skipped || !lat.ok) return undefined;
  if (typeof lat.latencyMs === "number") return lat.latencyMs;
  if (typeof lat.pathMs === "number") return lat.pathMs;

  return undefined;
}

function nodeLatencyMs(n: NodeApiItem): number | undefined {
  const latency = (n as { latency?: InboundApiItem["probe"] }).latency;
  const pts = probePoints(latency).filter(
    (p) => p.ok && typeof p.latencyMs === "number",
  );

  if (pts.length === 0) return undefined;
  const pinned = pts.find((p) => p.pinned);

  return (pinned ?? pts[0]).latencyMs;
}

function withFlag(n: NodeApiItem): string {
  const flag = nodeRegionFlag(n.region || n.regionDetected || "");

  if (!flag || /^\s*[\u{1F1E6}-\u{1F1FF}]/u.test(n.name)) return n.name;

  return `${flag} ${n.name}`;
}

/** One problem; problems of the same kind are shown as one group. */
interface Issue {
  kind: IssueKind;
  level: "danger" | "warning";
  name: string;
  detail: string;
}

type IssueKind =
  | "offline"
  | "expired"
  | "expiring"
  | "kernel"
  | "inbound"
  | "probe"
  | "relay"
  | "subflow"
  | "subexpire";

const KINDS: Record<
  IssueKind,
  { labelKey: string; hint: string; path: string }
> = {
  offline: {
    labelKey: "{count} 个服务器离线",
    hint: "agent 没有连接面板，检查服务器上的 agent 是否在运行",
    path: "/node",
  },
  expired: {
    labelKey: "{count} 个服务器已到期",
    hint: "续费后在服务器里更新到期时间",
    path: "/node",
  },
  expiring: {
    labelKey: "{count} 个服务器即将到期",
    hint: "3 天内到期",
    path: "/node",
  },
  kernel: {
    labelKey: "{count} 个内核出错",
    hint: "在服务器 → 内核里查看日志",
    path: "/node",
  },
  inbound: {
    labelKey: "{count} 个节点未运行",
    hint: "内核已停止或等待下发，服务器上线后会自动补上",
    path: "/inbound",
  },
  probe: {
    labelKey: "{count} 个节点连不上",
    hint: "从面板服务器检测",
    path: "/inbound",
  },
  relay: {
    labelKey: "{count} 条中转线路暂停或异常",
    hint: "转发规则已暂停或被删除，不会出现在订阅中",
    path: "/relay",
  },
  subflow: {
    labelKey: "{count} 个分享订阅流量快用完",
    hint: "用到 90% 以上；用完后自动断开",
    path: "/subscription",
  },
  subexpire: {
    labelKey: "{count} 个分享订阅即将到期",
    hint: "3 天内到期或已到期",
    path: "/subscription",
  },
};

function MetricCell({
  icon,
  label,
  value,
  total,
  foot,
  footTone,
  footRight,
  onPress,
}: {
  icon: React.ReactNode;
  label: string;
  value: number;
  total?: number;
  foot: string;
  footTone?: "danger" | "warning" | "success";
  footRight?: string;
  onPress: () => void;
}) {
  const dotTone =
    footTone === "danger"
      ? "bg-danger"
      : footTone === "warning"
        ? "bg-warning"
        : footTone === "success"
          ? "bg-success"
          : "bg-default-300";

  return (
    <button
      className="group flex flex-col justify-between bg-content1 px-4 py-3.5 text-left transition-colors duration-150 hover:bg-default-50/80 active:bg-default-100/60"
      type="button"
      onClick={onPress}
    >
      <div className="flex items-center justify-between gap-2">
        <span className="flex min-w-0 items-center gap-1.5 text-xs font-medium text-default-500">
          <span className="text-default-400 transition-colors group-hover:text-foreground">
            {icon}
          </span>
          <span className="truncate">{label}</span>
        </span>
        <ChevronRight className="h-3.5 w-3.5 shrink-0 text-default-300 transition-transform duration-150 group-hover:translate-x-0.5 group-hover:text-foreground" />
      </div>

      <div className="mt-2 flex items-baseline gap-1">
        <span className="text-2xl font-semibold tabular-nums tracking-tight text-foreground">
          {value}
        </span>
        {total !== undefined && (
          <span className="text-sm font-normal tabular-nums text-default-400">
            / {total}
          </span>
        )}
      </div>

      <div className="mt-2 flex items-center justify-between gap-2 text-xs">
        <span
          className={`flex min-w-0 items-center gap-1.5 truncate ${
            footTone === "danger"
              ? "text-danger"
              : footTone === "warning"
                ? "text-warning-600 dark:text-warning"
                : "text-default-400"
          }`}
        >
          <span className={`h-1.5 w-1.5 shrink-0 rounded-full ${dotTone}`} />
          <span className="truncate">{foot}</span>
        </span>
        {footRight && (
          <span className="shrink-0 tabular-nums text-default-400">
            {footRight}
          </span>
        )}
      </div>
    </button>
  );
}

function BentoCard({
  title,
  badge,
  action,
  children,
  className = "",
}: {
  title: string;
  badge?: React.ReactNode;
  action?: React.ReactNode;
  children: React.ReactNode;
  className?: string;
}) {
  return (
    <section
      className={`phone-card phone-card-fill overflow-hidden rounded-xl border border-divider/80 bg-content1 shadow-[0_1px_2px_rgba(15,23,42,0.03)] ${className}`}
    >
      <div className="flex items-center gap-2 border-b border-divider/70 px-4 py-3">
        <h2 className="text-sm font-semibold text-foreground">{title}</h2>
        {badge}
        {action && <div className="ml-auto">{action}</div>}
      </div>
      <div className="p-4">{children}</div>
    </section>
  );
}

function MoreLink({ onPress }: { onPress: () => void }) {
  const { t } = useI18n();

  return (
    <button
      className="flex items-center gap-0.5 text-xs text-default-500 transition-colors hover:text-foreground"
      type="button"
      onClick={onPress}
    >
      {t("全部")}
      <ChevronRight className="h-3.5 w-3.5" />
    </button>
  );
}

const PAGE = "admin-overview";

/** The admin's overview: health of nodes → landings → relays → subscriptions. */
export function AdminOverview() {
  const navigate = useNavigate();
  const { locale, isEn, t } = useI18n();
  const [loading, setLoading] = useState(() => !pageCached(PAGE));
  const [refreshing, setRefreshing] = useState(false);
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
  const [monthTraffic, setMonthTraffic] = useCachedState<
    Record<string, [number, number]>
  >(PAGE, "monthTraffic", {});

  const loadPending = useRef(0);

  const load = useCallback(async (options?: { silent?: boolean }) => {
    const silent = options?.silent ?? false;

    if (silent && loadPending.current > 0) return;
    loadPending.current += 1;
    if (!silent) setRefreshing(true);
    try {
      const [n, k, i, r, s, m] = await Promise.all([
        getNodeList().catch(() => null),
        getKernelList().catch(() => null),
        getInboundList().catch(() => null),
        getInboundRelays(0).catch(() => null),
        getSubscriptions().catch(() => null),
        getNodeMonthTraffic().catch(() => null),
      ]);

      if (n?.code === 0) setNodes(n.data ?? []);
      if (k?.code === 0) setKernels(k.data ?? []);
      if (i?.code === 0) setInbounds(i.data ?? []);
      if (r?.code === 0) setRelays(r.data ?? []);
      if (s?.code === 0) setSubs(s.data?.items ?? []);
      if (m?.code === 0 && m.data) setMonthTraffic(m.data);
      markPageCached(PAGE);
    } finally {
      loadPending.current -= 1;
      setLoading(false);
      if (!silent) setRefreshing(false);
    }
  }, []);

  useEffect(() => {
    void load({ silent: pageCached(PAGE) });
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
  const relaysBroken = relays.filter(
    (r) => !(r.forwardExists && r.forwardStatus === 1),
  );
  const shares = subs.filter((s) => s.kind === "share");
  const activeSubs = subs.filter((s) => s.active);
  const inboundById = new Map(inbounds.map((i) => [i.id, i] as const));
  const inboundCountByNode = new Map<number, number>();

  for (const item of inbounds) {
    if (!item.external && item.nodeId) {
      inboundCountByNode.set(
        item.nodeId,
        (inboundCountByNode.get(item.nodeId) ?? 0) + 1,
      );
    }
  }

  const issues: Issue[] = [];

  for (const n of nodes) {
    if (n.status !== 1) {
      issues.push({
        kind: "offline",
        level: "danger",
        name: n.name,
        detail: t("agent 没有连接面板"),
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
        detail: k.lastError || t("查看内核日志"),
      });
    }
  }
  for (const i of inbounds) {
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
        detail: i.probe?.error || t("连不上"),
      });
    }
  }
  for (const r of relaysBroken) {
    issues.push({
      kind: "relay",
      level: "warning",
      name: r.name || r.tunnelName || `#${r.id}`,
      detail: r.forwardExists ? t("转发已暂停") : t("关联转发已删除"),
    });
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

  const up = inbounds.reduce((acc, i) => acc + i.uploadBytes, 0);
  const down = inbounds.reduce((acc, i) => acc + i.downloadBytes, 0);
  const serverMonthBytes = Object.values(monthTraffic).reduce(
    (acc, pair) => acc + (pair?.[0] ?? 0) + (pair?.[1] ?? 0),
    0,
  );
  const top = [...inbounds]
    .filter((i) => !i.external && i.uploadBytes + i.downloadBytes > 0)
    .sort(
      (a, b) =>
        b.uploadBytes + b.downloadBytes - (a.uploadBytes + a.downloadBytes),
    )
    .slice(0, 6);
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
        kind: t("服务器"),
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
        kind: t("订阅"),
        name: s.name,
        at: s.expireTime,
        path: "/subscription",
      })),
  ]
    .sort((a, b) => a.at - b.at)
    .slice(0, 6);

  const shownNodes = [...nodes].sort((a, b) => a.status - b.status).slice(0, 8);
  const shownRelays = [...relays]
    .sort((a, b) => {
      const aOk = a.forwardExists && a.forwardStatus === 1 ? 1 : 0;
      const bOk = b.forwardExists && b.forwardStatus === 1 ? 1 : 0;

      return aOk - bOk;
    })
    .slice(0, 6);
  const coveredLandingCount = new Set(relays.map((r) => r.inboundId)).size;

  return (
    <AnimatedPage className="space-y-5 px-4 py-6 lg:px-10 lg:py-8">
      {/* Header */}
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div>
          <h1 className="page-title text-xl font-semibold tracking-tight">
            {t("概览")}
          </h1>
          <p className="mt-0.5 text-xs text-default-400">
            {new Date().toLocaleDateString(locale, {
              month: "long",
              day: "numeric",
              weekday: "long",
            })}
            {` · ${t("每 30 秒自动刷新")}`}
          </p>
        </div>
        <div className="flex flex-wrap items-center gap-1.5">
          <Button
            className="h-8 gap-1.5 text-xs"
            isLoading={refreshing}
            size="sm"
            variant="flat"
            onPress={() => void load()}
          >
            {!refreshing && <RefreshCw className="h-3.5 w-3.5" />}
            {t("刷新")}
          </Button>
          <Button
            className="h-8 gap-1.5 text-xs"
            size="sm"
            variant="bordered"
            onPress={() => navigate("/config")}
          >
            <Settings className="h-3.5 w-3.5" />
            {t("设置")}
          </Button>
        </div>
      </div>

      {/* Health Status Banner */}
      {groups.length === 0 ? (
        <div className="phone-card phone-card-fill flex flex-wrap items-center justify-between gap-3 rounded-xl border border-divider/80 bg-content1 px-4 py-2.5 shadow-[0_1px_2px_rgba(15,23,42,0.03)]">
          <div className="flex min-w-0 items-center gap-2.5">
            <CheckCircle2 className="h-4 w-4 shrink-0 text-success" />
            <span className="text-sm font-medium text-foreground">
              {t("一切正常")}
            </span>
            <span className="hidden text-xs text-default-400 sm:inline">
              {t("所有在线服务器、运行节点、中转线路和订阅状态均健康")}
            </span>
          </div>
          <div className="flex flex-wrap items-center gap-1 text-xs">
            <button
              className="rounded-md px-2.5 py-1 text-default-600 transition-colors hover:bg-default-100 hover:text-foreground"
              type="button"
              onClick={() => navigate("/node")}
            >
              {t("服务器")}
            </button>
            <button
              className="rounded-md px-2.5 py-1 text-default-600 transition-colors hover:bg-default-100 hover:text-foreground"
              type="button"
              onClick={() => navigate("/inbound")}
            >
              {t("节点")}
            </button>
            <button
              className="rounded-md px-2.5 py-1 text-default-600 transition-colors hover:bg-default-100 hover:text-foreground"
              type="button"
              onClick={() => navigate("/relay")}
            >
              {t("中转")}
            </button>
            <button
              className="rounded-md px-2.5 py-1 text-default-600 transition-colors hover:bg-default-100 hover:text-foreground"
              type="button"
              onClick={() => navigate("/subscription")}
            >
              {t("订阅")}
            </button>
          </div>
        </div>
      ) : (
        <section className="phone-card phone-card-fill overflow-hidden rounded-xl border border-warning-200 bg-content1 shadow-[0_1px_2px_rgba(15,23,42,0.03)] dark:border-warning-900/50">
          <div className="flex items-center gap-2 border-b border-divider/80 bg-warning-50/70 px-4 py-2.5 dark:bg-warning-950/25">
            <AlertTriangle className="h-4 w-4 shrink-0 text-warning-600 dark:text-warning" />
            <h2 className="text-sm font-semibold text-foreground">
              {t("需要处理")}
            </h2>
            <span className="rounded-full bg-warning/15 px-2 py-0.5 text-xs font-medium tabular-nums text-warning-700 dark:text-warning">
              {issues.length}
            </span>
          </div>
          <div className="divide-y divide-divider">
            {groups.map((g) => {
              const danger = g.level === "danger";
              const meta = KINDS[g.kind];
              const translatedHint = t(meta.hint);
              const hint =
                g.kind === "probe" && g.items[0].detail
                  ? `${translatedHint}${isEn ? ": " : "："}${g.items[0].detail}`
                  : translatedHint;
              const names = g.items.slice(0, 4).map((it) => it.name);

              return (
                <button
                  key={g.kind}
                  className="group flex w-full items-center gap-3 px-4 py-3 text-left transition-colors hover:bg-default-50"
                  title={hint}
                  type="button"
                  onClick={() => navigate(meta.path)}
                >
                  <span
                    className={`h-2 w-2 shrink-0 rounded-full ${danger ? "bg-danger ring-[3px] ring-danger/20" : "bg-warning ring-[3px] ring-warning/20"}`}
                  />
                  <span className="min-w-0 flex-1">
                    <span className="block text-sm font-medium text-foreground">
                      {t(meta.labelKey, { count: g.items.length })}
                    </span>
                    <span className="block truncate text-xs text-default-400">
                      {names.join(isEn ? ", " : "、")}
                      {g.items.length > names.length &&
                        ` ${t("等 {count} 个", { count: g.items.length })}`}
                    </span>
                  </span>
                  <ChevronRight className="h-4 w-4 shrink-0 text-default-300 transition-transform duration-150 group-hover:translate-x-0.5 group-hover:text-foreground" />
                </button>
              );
            })}
          </div>
        </section>
      )}

      {/* Unified Horizontal Metric Bar */}
      <div className="phone-card phone-card-fill grid grid-cols-2 gap-px overflow-hidden rounded-xl border border-divider/80 bg-divider/70 shadow-[0_1px_2px_rgba(15,23,42,0.03)] lg:grid-cols-4">
        <MetricCell
          foot={
            nodes.length === 0
              ? t("还没有服务器")
              : online === nodes.length
                ? t("全部在线")
                : t("{count} 个离线", { count: nodes.length - online })
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
          icon={<Server className="h-3.5 w-3.5" />}
          label={t("服务器在线")}
          total={nodes.length}
          value={online}
          onPress={() => navigate("/node")}
        />
        <MetricCell
          foot={
            enabledInbounds.length === 0
              ? inbounds.length === 0
                ? t("还没有节点")
                : t("全部停用")
              : runningInbounds.length === enabledInbounds.length
                ? t("全部正常")
                : t("{count} 个异常", {
                    count: enabledInbounds.length - runningInbounds.length,
                  })
          }
          footRight={
            inbounds.length > enabledInbounds.length
              ? t("{count} 个停用", {
                  count: inbounds.length - enabledInbounds.length,
                })
              : undefined
          }
          footTone={
            runningInbounds.length < enabledInbounds.length
              ? "warning"
              : enabledInbounds.length
                ? "success"
                : undefined
          }
          icon={<Globe className="h-3.5 w-3.5" />}
          label={t("节点运行")}
          total={enabledInbounds.length}
          value={runningInbounds.length}
          onPress={() => navigate("/inbound")}
        />
        <MetricCell
          foot={
            relays.length === 0
              ? t("还没有中转")
              : relaysUp.length === relays.length
                ? t("覆盖 {count} 个落地节点", { count: coveredLandingCount })
                : t("{count} 条未启用", {
                    count: relays.length - relaysUp.length,
                  })
          }
          footTone={
            relays.length > relaysUp.length
              ? "warning"
              : relays.length
                ? "success"
                : undefined
          }
          icon={<Link2 className="h-3.5 w-3.5" />}
          label={t("中转线路")}
          total={relays.length || undefined}
          value={relaysUp.length}
          onPress={() => navigate("/relay")}
        />
        <MetricCell
          foot={
            subs.length === 0
              ? t("还没有订阅")
              : activeSubs.length === subs.length
                ? t("全部有效")
                : t("{count} 个失效", {
                    count: subs.length - activeSubs.length,
                  })
          }
          footRight={
            shares.length
              ? t("分享 {count} 个", { count: shares.length })
              : undefined
          }
          footTone={
            activeSubs.length < subs.length
              ? "warning"
              : subs.length
                ? "success"
                : undefined
          }
          icon={<Rss className="h-3.5 w-3.5" />}
          label={t("有效订阅")}
          total={subs.length}
          value={activeSubs.length}
          onPress={() => navigate("/subscription")}
        />
      </div>

      {/* Main 2-Column Bento Workspace */}
      <div className="grid grid-cols-1 gap-4 lg:grid-cols-12 lg:items-start">
        {/* Left Main Column */}
        <div className="space-y-4 lg:col-span-7">
          {/* Servers Mini Grid */}
          <BentoCard
            action={<MoreLink onPress={() => navigate("/node")} />}
            badge={
              <span className="text-xs tabular-nums text-default-400">
                {online}/{nodes.length}
              </span>
            }
            title={t("服务器")}
          >
            {nodes.length === 0 ? (
              <p className="py-4 text-center text-xs text-default-400">
                {t("还没有服务器")}
              </p>
            ) : (
              <div className="grid grid-cols-1 gap-2.5 sm:grid-cols-2">
                {shownNodes.map((n) => {
                  const upStatus = n.status === 1;
                  const ip = String(n.serverIpV4 || n.serverIp || "");
                  const pair = monthTraffic[String(n.id)];
                  const monthBytes = pair ? (pair[0] || 0) + (pair[1] || 0) : 0;
                  const inCount = inboundCountByNode.get(n.id) ?? 0;
                  const ms = upStatus ? nodeLatencyMs(n) : undefined;

                  return (
                    <div
                      key={n.id}
                      className="group flex cursor-pointer flex-col justify-between gap-2 rounded-lg border border-divider/80 bg-default-50/50 p-3 text-left transition-colors hover:border-default-400 hover:bg-default-50 dark:bg-default-100/20"
                      role="button"
                      tabIndex={0}
                      onClick={() => navigate("/node")}
                      onKeyDown={(e) => {
                        if (e.target !== e.currentTarget) return;
                        if (e.key === "Enter" || e.key === " ") {
                          e.preventDefault();
                          navigate("/node");
                        }
                      }}
                    >
                      <div className="flex items-start justify-between gap-2">
                        <div className="flex min-w-0 items-center gap-2">
                          <span
                            className={`h-2 w-2 shrink-0 rounded-full ${
                              upStatus
                                ? "bg-success ring-[3px] ring-success/20"
                                : "bg-danger ring-[3px] ring-danger/20"
                            }`}
                          />
                          <span className="truncate text-sm font-medium text-foreground">
                            {withFlag(n)}
                          </span>
                        </div>
                        <span
                          className={`shrink-0 font-mono text-xs tabular-nums ${
                            !upStatus
                              ? "text-danger"
                              : ms !== undefined
                                ? ms < 80
                                  ? "text-success"
                                  : ms < 180
                                    ? "text-warning-600 dark:text-warning"
                                    : "text-danger"
                                : "text-default-400"
                          }`}
                        >
                          {!upStatus
                            ? t("离线")
                            : ms !== undefined
                              ? `${ms} ms`
                              : t("在线")}
                        </span>
                      </div>

                      <div className="flex items-center justify-between gap-2 text-xs text-default-400">
                        <span className="truncate font-mono">{ip || "-"}</span>
                        <div className="flex shrink-0 items-center gap-1.5 tabular-nums">
                          {monthBytes > 0 && (
                            <span>{formatTraffic(monthBytes)}</span>
                          )}
                          {inCount > 0 && (
                            <button
                              className="rounded bg-content1 px-1.5 py-0.5 text-[11px] text-default-600 transition-colors hover:bg-default-200 hover:text-foreground"
                              type="button"
                              onClick={(e) => {
                                e.stopPropagation();
                                navigate("/inbound", {
                                  state: { nodeId: n.id },
                                });
                              }}
                            >
                              {t("{count} 入站", { count: inCount })}
                            </button>
                          )}
                        </div>
                      </div>
                    </div>
                  );
                })}
              </div>
            )}
          </BentoCard>

          {/* Relay Lines Snapshot */}
          <BentoCard
            action={<MoreLink onPress={() => navigate("/relay")} />}
            badge={
              relays.length > 0 && (
                <span className="text-xs tabular-nums text-default-400">
                  {relaysUp.length}/{relays.length}
                </span>
              )
            }
            title={t("中转链路")}
          >
            {shownRelays.length === 0 ? (
              <p className="py-4 text-center text-xs text-default-400">
                {t("还没有中转线路")}
              </p>
            ) : (
              <div className="divide-y divide-divider/80">
                {shownRelays.map((r) => {
                  const ok = r.forwardExists && r.forwardStatus === 1;
                  const landing = inboundById.get(r.inboundId);
                  const ms = ok ? relayMs(r) : undefined;

                  return (
                    <button
                      key={r.id}
                      className="flex w-full items-center gap-2.5 py-2.5 text-left text-xs transition-colors first:pt-0 last:pb-0 hover:text-foreground"
                      type="button"
                      onClick={() => navigate("/relay")}
                    >
                      <span
                        className={`h-1.5 w-1.5 shrink-0 rounded-full ${
                          ok ? "bg-success" : "bg-warning"
                        }`}
                      />
                      <span className="min-w-0 max-w-[35%] truncate font-medium text-foreground">
                        {relayEntryName(r, t("中转"))}
                      </span>
                      <ArrowRight className="h-3 w-3 shrink-0 text-default-400" />
                      <span className="min-w-0 flex-1 truncate text-default-600">
                        {landing?.name || r.name || `#${r.inboundId}`}
                      </span>
                      <span
                        className={`shrink-0 font-mono tabular-nums ${
                          !ok
                            ? "text-warning-600 dark:text-warning"
                            : ms !== undefined
                              ? ms < 80
                                ? "text-success"
                                : ms < 180
                                  ? "text-warning-600 dark:text-warning"
                                  : "text-danger"
                              : "text-default-400"
                        }`}
                      >
                        {!ok
                          ? t("已暂停")
                          : ms !== undefined
                            ? `${ms} ms`
                            : "—"}
                      </span>
                    </button>
                  );
                })}
              </div>
            )}
          </BentoCard>
        </div>

        {/* Right Side Column */}
        <div className="space-y-4 lg:col-span-5">
          {/* Traffic Summary + Top Inbounds */}
          <BentoCard
            action={<MoreLink onPress={() => navigate("/inbound")} />}
            title={t("流量统计")}
          >
            <div className="mb-3.5 grid grid-cols-2 gap-2.5">
              <div className="rounded-lg bg-default-50 px-3 py-2 dark:bg-default-100/30">
                <div className="text-[11px] text-default-400">
                  {t("服务器本月总流量")}
                </div>
                <div className="mt-0.5 text-base font-semibold tabular-nums text-foreground">
                  {formatTraffic(serverMonthBytes)}
                </div>
              </div>
              <div className="rounded-lg bg-default-50 px-3 py-2 dark:bg-default-100/30">
                <div className="text-[11px] text-default-400">
                  {t("节点累计流量")}
                </div>
                <div className="mt-0.5 text-base font-semibold tabular-nums text-foreground">
                  {formatTraffic(up + down)}
                </div>
                <div className="truncate text-[11px] tabular-nums text-default-400">
                  ↑ {formatTraffic(up)} · ↓ {formatTraffic(down)}
                </div>
              </div>
            </div>

            {top.length === 0 ? (
              <p className="py-3 text-center text-xs text-default-400">
                {t("还没有流量数据（导入的节点不统计流量）")}
              </p>
            ) : (
              <div className="space-y-3">
                {top.map((i) => {
                  const total = i.uploadBytes + i.downloadBytes;

                  return (
                    <button
                      key={i.id}
                      className="block w-full text-left transition-opacity hover:opacity-80"
                      type="button"
                      onClick={() =>
                        navigate("/inbound", { state: { nodeId: i.nodeId } })
                      }
                    >
                      <div className="flex items-center justify-between gap-2 text-xs">
                        <span className="min-w-0 truncate font-medium text-foreground">
                          {i.name}
                          <span className="ml-1.5 font-normal text-default-400">
                            {i.nodeName}
                          </span>
                        </span>
                        <span className="shrink-0 tabular-nums text-default-500">
                          {formatTraffic(total)}
                        </span>
                      </div>
                      <div className="mt-1.5 h-1.5 overflow-hidden rounded-full bg-default-100">
                        <div
                          className="h-full rounded-full bg-foreground/80"
                          style={{
                            width: `${Math.max(4, (total / topMax) * 100)}%`,
                          }}
                        />
                      </div>
                    </button>
                  );
                })}
              </div>
            )}
          </BentoCard>

          {/* Expiring Soon */}
          {expiring.length > 0 && (
            <BentoCard
              badge={
                <span className="inline-flex items-center gap-1 text-xs tabular-nums text-default-400">
                  <Clock className="h-3 w-3" />
                  {expiring.length}
                </span>
              }
              title={t("即将到期")}
            >
              <div className="divide-y divide-divider/80">
                {expiring.map((e) => {
                  const days = Math.ceil((e.at - now) / DAY);

                  return (
                    <button
                      key={e.key}
                      className="flex w-full items-center gap-2.5 py-2.5 text-left text-xs transition-colors first:pt-0 last:pb-0 hover:text-foreground"
                      type="button"
                      onClick={() => navigate(e.path)}
                    >
                      <span className="min-w-0 flex-1 truncate font-medium text-foreground">
                        {e.name}
                      </span>
                      <span className="shrink-0 rounded bg-default-100 px-1.5 py-0.5 text-[11px] text-default-500">
                        {e.kind}
                      </span>
                      <span
                        className={`w-16 shrink-0 text-right tabular-nums ${
                          days <= 0
                            ? "font-medium text-danger"
                            : days <= 3
                              ? "font-medium text-warning-600 dark:text-warning"
                              : "text-default-500"
                        }`}
                      >
                        {daysLeft(e.at)}
                      </span>
                    </button>
                  );
                })}
              </div>
            </BentoCard>
          )}
        </div>
      </div>
    </AnimatedPage>
  );
}
