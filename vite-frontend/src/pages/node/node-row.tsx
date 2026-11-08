import { Fragment, memo, type ReactNode } from "react";
import { GripVertical } from "lucide-react";

import { Chip } from "@/shadcn-bridge/heroui/chip";
import { LatencyCell, probePoints } from "@/components/latency-cell";
import { ListRow } from "@/components/page-kit";
import { getConnectionStatusMeta } from "@/pages/node/display";
import { Node } from "@/pages/node/types";
import { getNodeExpiryMeta, SortableItem } from "@/pages/node/helpers";
import { NodeLiveStats } from "@/pages/node/node-live-stats";
import { formatUptime } from "@/pages/node/format";
import { nodeRegionFlag } from "@/pages/node/region";

export interface NodeRowActions {
  toggleSelect: (id: number) => void;
  probeNode: (id: number) => Promise<void>;
  setTargetNodes: (nodes: Node[]) => void;
  setSheetNodeId: (id: number) => void;
  setRouteNode: (node: Node) => void;
  setTrendNode: (node: Node) => void;
}
interface NodeRowProps {
  node: Node;
  progress?: { stage: string; percent: number; message: string };
  month?: [number, number];
  selected: boolean;
  probing: boolean;
  selectMode: boolean;
  isDisplayFiltered: boolean;
  wide: boolean;
  actions: NodeRowActions;
}
export const NodeRow = memo(function NodeRow(props: NodeRowProps) {
  return (
    <SortableItem id={props.node.id}>
      {(listeners) => <NodeRowContent {...props} listeners={listeners} />}
    </SortableItem>
  );
});

const NodeRowContent = memo(function NodeRowContent({
  node,
  progress,
  month,
  selected,
  probing,
  selectMode,
  isDisplayFiltered,
  wide,
  actions,
  listeners,
}: NodeRowProps & { listeners: any }) {
  const {
    toggleSelect,
    probeNode,
    setTargetNodes,
    setSheetNodeId,
    setRouteNode,
    setTrendNode,
  } = actions;
  const renderLatency = (node: Node) =>
    node.isRemote !== 1 && node.connectionStatus === "online" ? (
      node.latencyTargets?.points.length === 0 ? (
        <button
          className="rounded-full bg-default-100 px-2 py-0.5 text-xs text-default-500 hover:text-default-700"
          title="这台服务器没选测试点，点此设置"
          type="button"
          onClick={(e) => {
            e.stopPropagation();
            setTargetNodes([node]);
          }}
        >
          不测延迟
        </button>
      ) : probePoints(node.latency).length === 0 ? null : (
        // Nothing until the first round (every 3 minutes) has a result;
        // re-probing is in the result's menu.
        <LatencyCell
          probe={node.latency}
          probing={probing}
          onEditTargets={() => setTargetNodes([node])}
          onProbe={() => probeNode(node.id)}
          onShowRoute={() => setRouteNode(node)}
          onShowTrend={() => setTrendNode(node)}
        />
      )
    ) : null;

  const isRemoteNode = node.isRemote === 1;
  const online = node.connectionStatus === "online";
  const expiryMeta = getNodeExpiryMeta(node.expiryTime, node.renewalCycle);
  const showExpiry = Boolean(
    node.expiryTime &&
      node.expiryTime > 0 &&
      node.renewalCycle &&
      !node.expiryReminderDismissed &&
      expiryMeta.isHighlighted,
  );

  const upgrading = progress && progress.percent < 100;
  const quota = quotaUsage(node, month);
  const address =
    node.serverIpV4?.trim() || node.serverIpV6?.trim() || node.serverIp.trim();
  const meta = isRemoteNode
    ? [address, node.syncError ? "同步失败" : ""]
    : [
        address,
        upgrading ? `升级中 ${progress.percent}%` : node.version || "未连接过",
        online && node.systemInfo
          ? `已运行 ${formatUptime(node.systemInfo.uptime)}`
          : "",
      ];

  // The machine right after the agent version, before the uptime, so a
  // narrow row truncates the uptime first.
  const host = isRemoteNode ? null : hostSummary(node);
  const metaParts: ReactNode[] = meta.filter(Boolean);

  if (host) {
    metaParts.splice(
      Math.min(2, metaParts.length),
      0,
      <span key="host" title={host.full}>
        {host.short}
      </span>,
    );
  }

  return (
    <ListRow
      badges={
        <>
          {isRemoteNode && (
            <Chip className="shrink-0" size="sm" variant="flat">
              远程
            </Chip>
          )}
          {showExpiry && (
            <Chip
              className="shrink-0"
              color={expiryMeta.tone}
              size="sm"
              variant="flat"
            >
              {expiryMeta.label}
            </Chip>
          )}
          {quota && (
            <Chip
              className="shrink-0"
              color={
                quota.pct >= 100
                  ? "danger"
                  : quota.pct >= 90
                    ? "warning"
                    : "default"
              }
              size="sm"
              title={`本期已用 ${quota.used} / 每月额度 ${quota.total}`}
              variant="flat"
            >
              {quota.used} / {quota.total}
            </Chip>
          )}
          {node.group?.trim() && (
            <span className="hidden shrink-0 text-xs text-default-400 sm:inline">
              {node.group.trim()}
            </span>
          )}
        </>
      }
      footer={
        !isRemoteNode && !wide ? (
          <NodeLiveStats
            info={online ? (node.systemInfo ?? null) : null}
            month={month}
            variant="footer"
          />
        ) : undefined
      }
      meta={metaParts.map((part, i) => (
        <Fragment key={i}>
          {i > 0 && " · "}
          {part}
        </Fragment>
      ))}
      right={
        <>
          {!isRemoteNode && wide && (
            <NodeLiveStats
              info={online ? (node.systemInfo ?? null) : null}
              month={month}
              variant="row"
            />
          )}
          {/* A fixed slot keeps the stats in line across rows. */}
          <div className="contents lg:flex lg:w-56 lg:justify-end">
            {renderLatency(node)}
          </div>
          {!isDisplayFiltered && (
            <span
              className="cursor-grab touch-none p-1 text-default-300 transition-opacity hover:text-default-500 active:cursor-grabbing sm:opacity-0 sm:group-hover:opacity-100"
              title="拖动排序"
              {...listeners}
            >
              <GripVertical className="h-4 w-4" />
            </span>
          )}
        </>
      }
      selectMode={selectMode}
      selected={selected}
      status={upgrading ? "warn" : online ? "ok" : "bad"}
      statusTitle={getConnectionStatusMeta(node.connectionStatus).text}
      title={withRegionFlag(node)}
      onPress={() => setSheetNodeId(node.id)}
      onSelect={() => toggleSelect(node.id)}
    />
  );
});

/** The name with the node's country flag in front (admin's choice, else detected). */
function withRegionFlag(node: Node): string {
  const flag = nodeRegionFlag(node.region || node.regionDetected || "");

  // Names that already start with a flag ("🇭🇰 香港") keep their own.
  if (!flag || /^\s*[\u{1F1E6}-\u{1F1FF}]/u.test(node.name)) return node.name;

  return `${flag} ${node.name}`;
}

/** "Debian 12 · kvm" for the row, every fact for its tooltip. */
function hostSummary(node: Node): { short: string; full: string } | null {
  const os = node.hostOs?.trim() ?? "";
  const virt = node.hostVirtualization?.trim() ?? "";

  if (!os && !virt) return null;
  const shortOs = os
    .replace(/\s*\(.*\)\s*$/, "")
    .replace(/\s*GNU\/Linux/, "")
    .replace(/\s+LTS$/, "");
  const short = [shortOs, virt && virt !== "none" ? virt : ""]
    .filter(Boolean)
    .join(" · ");
  const full = [
    os && `系统：${os}`,
    node.hostKernel && `内核：${node.hostKernel}`,
    node.hostArch && `架构：${node.hostArch}`,
    node.hostCpuName && `CPU：${node.hostCpuName}`,
    virt && `虚拟化：${virt === "none" ? "无（物理机）" : virt}`,
  ]
    .filter(Boolean)
    .join("\n");

  return { short: short || os, full };
}

const gbText = (bytes: number) => {
  const gb = bytes / 2 ** 30;

  if (gb >= 1024) return `${(gb / 1024).toFixed(gb >= 10240 ? 0 : 1)} TB`;

  return `${gb >= 100 ? Math.round(gb) : gb.toFixed(1)} GB`;
};

/** This period's traffic against the monthly quota, as the node counts it. */
function quotaUsage(
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
