import { GripVertical } from "lucide-react";
import { memo } from "react";

import { LatencyCell, probePoints } from "@/components/latency-cell";
import { useI18n } from "@/i18n";
import { getConnectionStatusMeta } from "@/pages/node/display";
import { formatFlow, formatSpeed, formatUptime } from "@/pages/node/format";
import { getNodeExpiryMeta, SortableItem } from "@/pages/node/helpers";
import { type NodeRowActions } from "@/pages/node/node-row";
import { nodeRegionFlag } from "@/pages/node/region";
import { type Node, type RemoteUsageNode } from "@/pages/node/types";
import { Checkbox } from "@/shadcn-bridge/heroui/checkbox";
import { Chip } from "@/shadcn-bridge/heroui/chip";

export type NodeViewMode = "cards" | "table" | "list";
export type NodeGroupMode = "none" | "group" | "region";
export type NodeStatusFilter = "all" | "online" | "offline" | "attention";
export type NodeSortMode =
  | "manual"
  | "cpu_desc"
  | "mem_desc"
  | "traffic_desc"
  | "latency_asc"
  | "expiry_asc"
  | "name_asc";

export interface NodeBizCount {
  inbounds: number;
  tunnels: number;
}

export interface NodeViewCallbacks extends NodeRowActions {
  onOpenInbounds: (nodeId: number) => void;
  onOpenTunnels: (nodeName: string) => void;
}

function bytesShort(v: number): string {
  const units = ["B", "K", "M", "G", "T", "P"];
  let i = 0;

  while (v >= 1024 && i < units.length - 1) {
    v /= 1024;
    i++;
  }

  return `${v >= 100 || i === 0 ? Math.round(v) : v.toFixed(1)}${units[i]}`;
}

function gbText(bytes: number): string {
  const gb = bytes / 2 ** 30;

  if (gb >= 1024) return `${(gb / 1024).toFixed(gb >= 10240 ? 0 : 1)} TB`;

  return `${gb >= 100 ? Math.round(gb) : gb.toFixed(1)} GB`;
}

export function getNodeMonthBytes(month?: [number, number]): number {
  if (!month) return 0;

  return (month[0] || 0) + (month[1] || 0);
}

export function getNodeQuotaUsage(
  node: Node,
  month?: [number, number],
): { used: string; total: string; pct: number } | null {
  if (!node.trafficQuotaGb || !month) return null;
  const [down, up] = month;
  const used =
    node.trafficCalc === "max"
      ? Math.max(up, down)
      : node.trafficCalc === "up"
        ? up
        : node.trafficCalc === "down"
          ? down
          : up + down;
  const total = node.trafficQuotaGb * 2 ** 30;

  return {
    used: gbText(used),
    total: gbText(total),
    pct: (used / total) * 100,
  };
}

export function getNodePrimaryLatencyMs(node: Node): number | undefined {
  const pts = probePoints(node.latency);
  const okPts = pts.filter((p) => p.ok && typeof p.latencyMs === "number");

  if (okPts.length === 0) return undefined;
  const pinned = okPts.find((p) => p.pinned);

  return (pinned ?? okPts[0]).latencyMs;
}

export function nodeNeedsAttention(
  node: Node,
  month?: [number, number],
): boolean {
  if (node.connectionStatus !== "online") return true;
  if (node.syncError) return true;
  const expiry = getNodeExpiryMeta(node.expiryTime, node.renewalCycle);

  if (
    node.expiryTime &&
    node.expiryTime > 0 &&
    node.renewalCycle &&
    !node.expiryReminderDismissed &&
    expiry.isHighlighted
  ) {
    return true;
  }
  const quota = getNodeQuotaUsage(node, month);

  if (quota && quota.pct >= 90) return true;

  return false;
}

function withRegionFlag(node: Node): string {
  const flag = nodeRegionFlag(node.region || node.regionDetected || "");

  if (!flag || /^\s*[\u{1F1E6}-\u{1F1FF}]/u.test(node.name)) return node.name;

  return `${flag} ${node.name}`;
}

function barColor(pct: number): string {
  if (pct >= 85) return "bg-danger";
  if (pct >= 65) return "bg-warning";

  return "bg-foreground/75";
}

function renderLatency(
  node: Node,
  probing: boolean,
  actions: NodeViewCallbacks,
) {
  if (node.isRemote === 1 || node.connectionStatus !== "online") return null;
  if (node.latencyTargets?.points.length === 0) {
    return (
      <button
        className="rounded-full bg-default-100 px-2 py-0.5 text-xs text-default-500 hover:text-default-700"
        title="这台服务器没选测试点，点此设置"
        type="button"
        onClick={(e) => {
          e.stopPropagation();
          actions.setTargetNodes([node]);
        }}
      >
        不测延迟
      </button>
    );
  }
  if (probePoints(node.latency).length === 0) return null;

  return (
    <LatencyCell
      probe={node.latency}
      probing={probing}
      onEditTargets={() => actions.setTargetNodes([node])}
      onProbe={() => actions.probeNode(node.id)}
      onShowRoute={() => actions.setRouteNode(node)}
      onShowTrend={() => actions.setTrendNode(node)}
    />
  );
}

interface NodeCardProps {
  node: Node;
  progress?: { stage: string; percent: number; message: string };
  month?: [number, number];
  biz?: NodeBizCount;
  remoteUsage?: RemoteUsageNode;
  selected: boolean;
  probing: boolean;
  selectMode: boolean;
  sortable: boolean;
  actions: NodeViewCallbacks;
}

const NodeCardBody = memo(function NodeCardBody({
  node,
  progress,
  month,
  biz,
  remoteUsage,
  selected,
  probing,
  selectMode,
  sortable,
  actions,
  listeners,
}: NodeCardProps & { listeners?: any }) {
  const { t } = useI18n();
  const isRemote = node.isRemote === 1;
  const online = node.connectionStatus === "online";
  const upgrading = Boolean(progress && progress.percent < 100);
  const expiryMeta = getNodeExpiryMeta(node.expiryTime, node.renewalCycle);
  const showExpiry = Boolean(
    node.expiryTime &&
      node.expiryTime > 0 &&
      node.renewalCycle &&
      !node.expiryReminderDismissed &&
      expiryMeta.isHighlighted,
  );
  const quota = getNodeQuotaUsage(node, month);
  const info = online ? (node.systemInfo ?? null) : null;
  const up = info ? (info.netOutSpeed ?? info.uploadSpeed) : undefined;
  const down = info ? (info.netInSpeed ?? info.downloadSpeed) : undefined;
  const address =
    node.serverIpV4?.trim() || node.serverIpV6?.trim() || node.serverIp.trim();
  const dotCls = upgrading
    ? "bg-warning ring-[3px] ring-warning/20"
    : online
      ? "bg-success ring-[3px] ring-success/20"
      : "bg-danger ring-[3px] ring-danger/20";

  const activate = () =>
    selectMode
      ? actions.toggleSelect(node.id)
      : actions.setSheetNodeId(node.id);

  const metrics: Array<{ label: string; value?: number }> = [
    { label: "CPU", value: info?.cpuUsage },
    { label: t("内存"), value: info?.memoryUsage },
    { label: t("磁盘"), value: info?.diskUsage },
  ];

  const monthTotalBytes = getNodeMonthBytes(month);
  const inboundCount = biz?.inbounds ?? 0;
  const tunnelCount = biz?.tunnels ?? 0;

  return (
    <div
      aria-pressed={selectMode ? selected : undefined}
      className={`phone-card phone-card-fill group flex h-full cursor-pointer flex-col justify-between gap-3 rounded-xl border p-3.5 text-left transition-colors ${
        selected
          ? "border-foreground bg-default-100"
          : "border-divider bg-content1 hover:border-default-400"
      }`}
      role="button"
      tabIndex={0}
      onClick={activate}
      onKeyDown={(e) => {
        if (e.target !== e.currentTarget) return;
        if (e.key === "Enter" || e.key === " ") {
          e.preventDefault();
          activate();
        }
      }}
    >
      {/* Header */}
      <div className="space-y-1">
        <div className="flex items-start justify-between gap-2">
          <div className="flex min-w-0 items-center gap-2">
            {selectMode && (
              <div
                className="flex shrink-0"
                role="presentation"
                onClick={(e) => e.stopPropagation()}
              >
                <Checkbox
                  isSelected={selected}
                  onValueChange={() => actions.toggleSelect(node.id)}
                />
              </div>
            )}
            <span
              className={`h-2 w-2 shrink-0 rounded-full ${dotCls}`}
              title={getConnectionStatusMeta(node.connectionStatus).text}
            />
            <span className="truncate text-sm font-medium text-foreground">
              {withRegionFlag(node)}
            </span>
          </div>
          <div
            className="flex shrink-0 items-center gap-1"
            role="presentation"
            onClick={(e) => e.stopPropagation()}
          >
            {node.group?.trim() && (
              <span className="max-w-[6.5rem] truncate rounded-md bg-default-100 px-1.5 py-0.5 text-[11px] text-default-600">
                {node.group.trim()}
              </span>
            )}
            {isRemote && (
              <Chip size="sm" variant="flat">
                {t("远程")}
              </Chip>
            )}
            {sortable && listeners && (
              <span
                className="cursor-grab touch-none p-0.5 text-default-300 transition-opacity hover:text-default-500 active:cursor-grabbing sm:opacity-0 sm:group-hover:opacity-100"
                title={t("拖动排序")}
                {...listeners}
              >
                <GripVertical className="h-3.5 w-3.5" />
              </span>
            )}
          </div>
        </div>
        <div className="truncate font-mono text-xs text-default-400">
          {address}
          {!isRemote &&
            ` · ${upgrading ? `${t("升级中")} ${progress?.percent}%` : node.version || t("未连接过")}`}
          {!isRemote && online && info?.uptime
            ? ` · ${formatUptime(info.uptime)}`
            : ""}
          {isRemote && node.syncError ? ` · ${t("同步失败")}` : ""}
        </div>
      </div>

      {/* Resource bars or remote usage */}
      {isRemote ? (
        <div className="rounded-lg bg-default-50 px-2.5 py-2 text-xs text-default-500 dark:bg-default-100/40">
          {remoteUsage ? (
            <div className="flex flex-wrap items-center justify-between gap-2 tabular-nums">
              <span>
                {t("流量")} {formatFlow(remoteUsage.currentFlow)}
              </span>
              <span>
                {t("绑定")} {remoteUsage.activeBindingNum}
              </span>
            </div>
          ) : (
            <span className="truncate">{node.remoteUrl || "-"}</span>
          )}
        </div>
      ) : (
        <div className="space-y-2.5">
          <div className="grid grid-cols-3 gap-2 text-xs tabular-nums">
            {metrics.map((m) => {
              const pct =
                m.value !== undefined
                  ? Math.max(0, Math.min(100, Math.round(m.value)))
                  : undefined;

              return (
                <div key={m.label} className="space-y-1">
                  <div className="flex items-center justify-between text-[11px]">
                    <span className="text-default-400">{m.label}</span>
                    <span
                      className={`font-medium ${
                        pct !== undefined && pct >= 85
                          ? "text-danger"
                          : pct !== undefined && pct >= 65
                            ? "text-warning-600 dark:text-warning"
                            : "text-foreground"
                      }`}
                    >
                      {pct !== undefined ? `${pct}%` : "—"}
                    </span>
                  </div>
                  <div className="h-1.5 overflow-hidden rounded-full bg-default-100">
                    <div
                      className={`h-full rounded-full transition-all ${
                        pct !== undefined ? barColor(pct) : "bg-transparent"
                      }`}
                      style={{ width: `${pct ?? 0}%` }}
                    />
                  </div>
                </div>
              );
            })}
          </div>

          <div className="space-y-1 text-xs tabular-nums">
            <div className="flex items-center justify-between gap-2 text-[11px]">
              <span className="truncate text-default-500">
                {quota
                  ? `${t("配额")} ${quota.used} / ${quota.total}`
                  : month
                    ? `${t("本月")} ${bytesShort(monthTotalBytes)}`
                    : `${t("本月")} —`}
              </span>
              <span className="shrink-0 text-default-400">
                {info && (up !== undefined || down !== undefined)
                  ? `↑ ${up !== undefined ? `${bytesShort(up)}/s` : "—"} ↓ ${down !== undefined ? `${bytesShort(down)}/s` : "—"}`
                  : online
                    ? ""
                    : t("离线")}
              </span>
            </div>
            {quota && (
              <div className="h-1.5 overflow-hidden rounded-full bg-default-100">
                <div
                  className={`h-full rounded-full ${barColor(quota.pct)}`}
                  style={{ width: `${Math.min(100, quota.pct)}%` }}
                />
              </div>
            )}
          </div>
        </div>
      )}

      {/* Footer: cross-entity counts + expiry + latency */}
      <div
        className="flex flex-wrap items-center justify-between gap-2 border-t border-divider/70 pt-2 text-xs"
        role="presentation"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="flex flex-wrap items-center gap-1.5">
          {!isRemote && inboundCount > 0 && (
            <button
              className="rounded-md bg-default-100 px-1.5 py-0.5 text-[11px] font-medium text-default-700 transition-colors hover:bg-default-200 hover:text-foreground"
              title={t("查看此服务器上的入站节点")}
              type="button"
              onClick={() => actions.onOpenInbounds(node.id)}
            >
              {t("{count} 入站", { count: inboundCount })}
            </button>
          )}
          {tunnelCount > 0 && (
            <button
              className="rounded-md bg-default-100 px-1.5 py-0.5 text-[11px] font-medium text-default-700 transition-colors hover:bg-default-200 hover:text-foreground"
              title={t("查看涉及此服务器的隧道")}
              type="button"
              onClick={() => actions.onOpenTunnels(node.name)}
            >
              {t("{count} 隧道", { count: tunnelCount })}
            </button>
          )}
          {showExpiry && (
            <Chip color={expiryMeta.tone} size="sm" variant="flat">
              {expiryMeta.label}
            </Chip>
          )}
          {!isRemote &&
            inboundCount === 0 &&
            tunnelCount === 0 &&
            !showExpiry && (
              <span className="text-[11px] text-default-400">
                {node.expiryTime && node.renewalCycle
                  ? expiryMeta.label
                  : t("暂无业务")}
              </span>
            )}
        </div>
        <div className="ml-auto shrink-0">
          {renderLatency(node, probing, actions)}
        </div>
      </div>
    </div>
  );
});

export const NodeCard = memo(function NodeCard(props: NodeCardProps) {
  if (!props.sortable) {
    return <NodeCardBody {...props} />;
  }

  return (
    <SortableItem id={props.node.id}>
      {(listeners) => <NodeCardBody {...props} listeners={listeners} />}
    </SortableItem>
  );
});

export function NodeTable({
  nodes,
  monthTraffic,
  bizCounts,
  upgradeProgress,
  probingIds,
  selectMode,
  selectedIds,
  actions,
}: {
  nodes: Node[];
  monthTraffic: Record<string, [number, number]>;
  bizCounts: Map<number, NodeBizCount>;
  upgradeProgress: Record<
    number,
    { stage: string; percent: number; message: string }
  >;
  probingIds: Set<number>;
  selectMode: boolean;
  selectedIds: Set<number>;
  actions: NodeViewCallbacks;
}) {
  const { t } = useI18n();

  return (
    <div className="phone-card phone-card-fill overflow-x-auto rounded-xl border border-divider bg-content1">
      <table className="w-full min-w-[780px] text-left text-xs">
        <thead className="border-b border-divider bg-default-50/70 text-default-500">
          <tr>
            {selectMode && <th className="w-9 px-3 py-2.5" />}
            <th className="px-3 py-2.5 font-medium">{t("服务器")}</th>
            <th className="px-3 py-2.5 font-medium">{t("分组")}</th>
            <th className="px-3 py-2.5 font-medium">
              {t("CPU / 内存 / 磁盘")}
            </th>
            <th className="px-3 py-2.5 font-medium">{t("实时速率")}</th>
            <th className="px-3 py-2.5 font-medium">{t("本月流量")}</th>
            <th className="px-3 py-2.5 font-medium">{t("业务")}</th>
            <th className="px-3 py-2.5 text-right font-medium">{t("延迟")}</th>
          </tr>
        </thead>
        <tbody className="divide-y divide-divider tabular-nums">
          {nodes.map((node) => {
            const isRemote = node.isRemote === 1;
            const online = node.connectionStatus === "online";
            const info = online ? (node.systemInfo ?? null) : null;
            const progress = upgradeProgress[node.id];
            const upgrading = Boolean(progress && progress.percent < 100);
            const month = monthTraffic[String(node.id)];
            const quota = getNodeQuotaUsage(node, month);
            const biz = bizCounts.get(node.id);
            const selected = selectMode && selectedIds.has(node.id);
            const address =
              node.serverIpV4?.trim() ||
              node.serverIpV6?.trim() ||
              node.serverIp.trim();
            const up = info
              ? (info.netOutSpeed ?? info.uploadSpeed)
              : undefined;
            const down = info
              ? (info.netInSpeed ?? info.downloadSpeed)
              : undefined;
            const dotCls = upgrading
              ? "bg-warning"
              : online
                ? "bg-success"
                : "bg-danger";
            const expiryMeta = getNodeExpiryMeta(
              node.expiryTime,
              node.renewalCycle,
            );
            const showExpiry = Boolean(
              node.expiryTime &&
                node.expiryTime > 0 &&
                node.renewalCycle &&
                !node.expiryReminderDismissed &&
                expiryMeta.isHighlighted,
            );

            return (
              <tr
                key={node.id}
                className={`cursor-pointer transition-colors ${
                  selected ? "bg-default-100" : "hover:bg-default-50"
                }`}
                onClick={() =>
                  selectMode
                    ? actions.toggleSelect(node.id)
                    : actions.setSheetNodeId(node.id)
                }
              >
                {selectMode && (
                  <td
                    className="px-3 py-2.5"
                    onClick={(e) => e.stopPropagation()}
                  >
                    <Checkbox
                      isSelected={selected}
                      onValueChange={() => actions.toggleSelect(node.id)}
                    />
                  </td>
                )}
                <td className="max-w-[15rem] px-3 py-2.5">
                  <div className="flex items-center gap-2">
                    <span
                      className={`h-2 w-2 shrink-0 rounded-full ${dotCls}`}
                    />
                    <span className="truncate font-medium text-foreground">
                      {withRegionFlag(node)}
                    </span>
                    {showExpiry && (
                      <Chip color={expiryMeta.tone} size="sm" variant="flat">
                        {expiryMeta.label}
                      </Chip>
                    )}
                  </div>
                  <div className="truncate pl-4 font-mono text-[11px] text-default-400">
                    {address}
                    {!isRemote &&
                      ` · ${upgrading ? `${progress?.percent}%` : node.version || t("未连接过")}`}
                  </div>
                </td>
                <td className="whitespace-nowrap px-3 py-2.5 text-default-500">
                  {node.group?.trim() || "—"}
                </td>
                <td className="whitespace-nowrap px-3 py-2.5 text-default-600">
                  {!isRemote && info
                    ? `${info.cpuUsage !== undefined ? `${Math.round(info.cpuUsage)}%` : "—"} · ${info.memoryUsage !== undefined ? `${Math.round(info.memoryUsage)}%` : "—"} · ${info.diskUsage !== undefined ? `${Math.round(info.diskUsage)}%` : "—"}`
                    : "—"}
                </td>
                <td className="whitespace-nowrap px-3 py-2.5 text-default-500">
                  {!isRemote && info && (up !== undefined || down !== undefined)
                    ? `↑${up !== undefined ? formatSpeed(up) : "—"} ↓${down !== undefined ? formatSpeed(down) : "—"}`
                    : "—"}
                </td>
                <td className="whitespace-nowrap px-3 py-2.5 text-foreground">
                  {quota
                    ? `${quota.used} / ${quota.total}`
                    : month
                      ? bytesShort(getNodeMonthBytes(month))
                      : "—"}
                </td>
                <td
                  className="whitespace-nowrap px-3 py-2.5"
                  onClick={(e) => e.stopPropagation()}
                >
                  <div className="inline-flex items-center gap-1.5">
                    {!isRemote && (biz?.inbounds ?? 0) > 0 && (
                      <button
                        className="rounded bg-default-100 px-1.5 py-0.5 text-[11px] text-default-700 hover:bg-default-200"
                        type="button"
                        onClick={() => actions.onOpenInbounds(node.id)}
                      >
                        {t("{count} 入站", { count: biz?.inbounds ?? 0 })}
                      </button>
                    )}
                    {(biz?.tunnels ?? 0) > 0 && (
                      <button
                        className="rounded bg-default-100 px-1.5 py-0.5 text-[11px] text-default-700 hover:bg-default-200"
                        type="button"
                        onClick={() => actions.onOpenTunnels(node.name)}
                      >
                        {t("{count} 隧道", { count: biz?.tunnels ?? 0 })}
                      </button>
                    )}
                    {(biz?.inbounds ?? 0) === 0 &&
                      (biz?.tunnels ?? 0) === 0 && (
                        <span className="text-default-400">—</span>
                      )}
                  </div>
                </td>
                <td
                  className="whitespace-nowrap px-3 py-2.5 text-right"
                  onClick={(e) => e.stopPropagation()}
                >
                  <div className="inline-flex justify-end">
                    {renderLatency(node, probingIds.has(node.id), actions) ?? (
                      <span className="text-default-400">
                        {online ? "—" : t("离线")}
                      </span>
                    )}
                  </div>
                </td>
              </tr>
            );
          })}
        </tbody>
      </table>
    </div>
  );
}
