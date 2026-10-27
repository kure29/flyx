import { memo, useState, useCallback } from "react";
import { GripVertical } from "lucide-react";

import { Button } from "@/shadcn-bridge/heroui/button";
import { Card, CardBody, CardHeader } from "@/shadcn-bridge/heroui/card";
import { Checkbox } from "@/shadcn-bridge/heroui/checkbox";
import { Chip } from "@/shadcn-bridge/heroui/chip";
import { Progress } from "@/shadcn-bridge/heroui/progress";
import { LatencyCell, probePoints } from "@/components/latency-cell";
import { ListRow } from "@/components/page-kit";
import {
  getConnectionStatusMeta,
  getRemoteSyncErrorMessage,
} from "@/pages/node/display";
import {
  formatNodeRenewalTime,
  getNodeRenewalCycleLabel,
} from "@/pages/node/renewal";
import { Node, RemoteUsageNode } from "@/pages/node/types";
import { getNodeExpiryMeta, SortableItem } from "@/pages/node/helpers";
import { NodeLiveStats } from "@/pages/node/node-live-stats";
import {
  formatSpeed,
  formatUptime,
  formatFlow,
  formatChainType,
} from "@/pages/node/format";

export interface NodeRowActions {
  toggleSelect: (id: number) => void;
  handleDismissExpiryReminder: (nodeId: number) => Promise<void>;
  onOpenMonitor: ((nodeId: number) => void) | undefined;
  openInstallSelector: (node: Node) => Promise<void>;
  openUpgradeModal: (
    target: "single" | "batch",
    nodeId?: number,
  ) => Promise<void>;
  handleRollbackNode: (node: Node) => void;
  handleEdit: (node: Node) => void;
  handleDelete: (node: Node) => void;
  probeNode: (id: number) => Promise<void>;
  setTargetNodes: (nodes: Node[]) => void;
  setTuneNode: (node: Node) => void;
  setSheetNodeId: (id: number) => void;
  setRouteNode: (node: Node) => void;
  setTrendNode: (node: Node) => void;
}
interface NodeRowProps {
  node: Node;
  card: boolean;
  usage?: RemoteUsageNode;
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
  card,
  usage,
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
    handleDismissExpiryReminder,
    onOpenMonitor,
    openInstallSelector,
    openUpgradeModal,
    handleRollbackNode,
    handleEdit,
    handleDelete,
    probeNode,
    setTargetNodes,
    setTuneNode,
    setSheetNodeId,
    setRouteNode,
    setTrendNode,
  } = actions;
  const [infoPopoverPlacement, setInfoPopoverPlacement] = useState<
    "left" | "bottom"
  >("left");

  const updateInfoPopoverPlacement = useCallback(
    (triggerElement: HTMLElement | null) => {
      if (!triggerElement) {
        return;
      }

      const rect = triggerElement.getBoundingClientRect();
      const cardElement = triggerElement.closest("[data-node-card='true']");
      const cardRect =
        cardElement instanceof HTMLElement
          ? cardElement.getBoundingClientRect()
          : null;
      const estimatedPanelWidth = 288;
      const containerPadding = 16;
      const availableLeftSpace = cardRect
        ? rect.left - cardRect.left
        : rect.left;
      const nextPlacement: "left" | "bottom" =
        availableLeftSpace >= estimatedPanelWidth + containerPadding
          ? "left"
          : "bottom";

      setInfoPopoverPlacement(nextPlacement);
    },
    [],
  );

  const renderLatency = (node: Node, variant: "card" | "list") =>
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
          note={node.latencyTargets ? "自定义" : undefined}
          probe={node.latency}
          probing={probing}
          variant={variant}
          onEditTargets={() => setTargetNodes([node])}
          onProbe={() => probeNode(node.id)}
          onShowRoute={() => setRouteNode(node)}
          onShowTrend={() => setTrendNode(node)}
        />
      )
    ) : null;

  if (card) {
    const isRemoteNode = node.isRemote === 1;
    const remoteUsage = isRemoteNode ? usage : null;
    const expiryMeta = getNodeExpiryMeta(node.expiryTime, node.renewalCycle);
    const connectionStatusMeta = getConnectionStatusMeta(node.connectionStatus);
    const hasRemark = Boolean(node.remark?.trim());
    const hasExpiryInfo = Boolean(
      node.expiryTime &&
        node.expiryTime > 0 &&
        node.renewalCycle &&
        !node.expiryReminderDismissed,
    );
    const hasInfoTrigger = hasRemark || hasExpiryInfo;
    const infoCount = Number(hasExpiryInfo) + Number(hasRemark);
    const infoPlacement = infoPopoverPlacement;

    return (
      <Card
        key={node.id}
        className={`group relative overflow-visible h-full flex flex-col ${node.expiryReminderDismissed ? "" : expiryMeta.accentClassName}`}
        data-node-card="true"
      >
        <CardHeader className="pb-3 md:pb-3">
          <div className="flex justify-between items-start w-full gap-3">
            <div className="flex items-start gap-2 flex-1 min-w-0">
              <div
                className="cursor-grab active:cursor-grabbing p-2 -ml-2 -mt-1 text-default-400 hover:text-default-600 transition-colors touch-manipulation opacity-100 sm:opacity-0 sm:group-hover:opacity-100 flex-shrink-0"
                {...listeners}
                style={{ touchAction: "none" }}
                title="拖拽排序"
              >
                <svg
                  aria-hidden="true"
                  className="w-4 h-4"
                  fill="currentColor"
                  viewBox="0 0 20 20"
                >
                  <path d="M7 2a2 2 0 1 1 .001 4.001A2 2 0 0 1 7 2zm0 6a2 2 0 1 1 .001 4.001A2 2 0 0 1 7 8zm0 6a2 2 0 1 1 .001 4.001A2 2 0 0 1 7 14zm6-8a2 2 0 1 1-.001-4.001A2 2 0 0 1 13 6zm0 2a2 2 0 1 1 .001 4.001A2 2 0 0 1 13 8zm0 6a2 2 0 1 1 .001 4.001A2 2 0 0 1 13 14z" />
                </svg>
              </div>
              {selectMode && (
                <Checkbox
                  className="mt-0.5"
                  isSelected={selected}
                  onValueChange={() => toggleSelect(node.id)}
                />
              )}
              <div className="min-w-0 flex-1 pt-0.5">
                <div className="flex items-center gap-2 min-w-0">
                  <h3 className="font-semibold text-foreground truncate text-lg leading-5">
                    {node.name}
                  </h3>
                  <div
                    className={`flex items-center gap-1.5 px-2 py-0.5 rounded-full text-xs font-medium flex-shrink-0
                                      ${connectionStatusMeta.color === "success" ? "bg-emerald-500/20 text-emerald-600" : "bg-rose-500/20 text-rose-600"}
                                    `}
                    title={connectionStatusMeta.text}
                  >
                    <span
                      className={`h-1.5 w-1.5 rounded-full ${connectionStatusMeta.color === "success" ? "bg-emerald-500" : "bg-rose-500"}`}
                    />
                    {connectionStatusMeta.text}
                  </div>
                </div>
              </div>
            </div>
            <div className="ml-2 flex-shrink-0 self-start">
              {hasInfoTrigger && (
                <div className="group/info relative">
                  <button
                    aria-label={`查看服务器信息，共 ${infoCount} 项`}
                    className="relative flex h-7 w-7 items-center justify-center rounded-full border border-divider/80 bg-background/95 text-default-500 shadow-sm transition hover:border-default-300 hover:text-foreground focus-visible:border-default-300 focus-visible:text-foreground focus-visible:outline-none"
                    type="button"
                    onFocus={(event) =>
                      updateInfoPopoverPlacement(event.currentTarget)
                    }
                    onMouseEnter={(event) =>
                      updateInfoPopoverPlacement(event.currentTarget)
                    }
                  >
                    <svg
                      aria-hidden="true"
                      className="h-3.5 w-3.5"
                      fill="none"
                      stroke="currentColor"
                      viewBox="0 0 24 24"
                    >
                      <path
                        d="M13 16h-1v-4h-1m1-4h.01M21 12a9 9 0 11-18 0 9 9 0 0118 0z"
                        strokeLinecap="round"
                        strokeLinejoin="round"
                        strokeWidth={1.8}
                      />
                    </svg>
                    {hasRemark && (
                      <span className="absolute -right-1 -top-1 flex h-2.5 w-2.5 rounded-full border border-background bg-default-300 shadow-sm dark:bg-default-500" />
                    )}
                  </button>
                  <div
                    className={`pointer-events-none invisible absolute z-[60] w-72 max-w-[min(18rem,calc(100vw-4rem))] rounded-xl border border-divider/80 bg-background/98 p-3 opacity-0 shadow-xl backdrop-blur transition-all duration-150 group-hover/info:visible group-hover/info:pointer-events-auto group-hover/info:opacity-100 group-focus-within/info:visible group-focus-within/info:pointer-events-auto group-focus-within/info:opacity-100 ${
                      infoPlacement === "bottom"
                        ? "right-0 top-[calc(100%+0.75rem)] translate-y-1 group-hover/info:translate-y-0 group-focus-within/info:translate-y-0"
                        : "right-[calc(100%+0.75rem)] top-1/2 -translate-y-1/2 translate-x-1 group-hover/info:translate-x-0 group-focus-within/info:translate-x-0"
                    }`}
                  >
                    <div className="space-y-3">
                      {hasExpiryInfo && (
                        <div className="space-y-2">
                          <div className="flex items-center justify-between">
                            <div className="text-[11px] font-medium text-default-500">
                              到期提醒
                            </div>
                            <button
                              className="text-[10px] text-default-400 hover:text-default-600 transition-colors"
                              type="button"
                              onClick={(e) => {
                                e.stopPropagation();
                                handleDismissExpiryReminder(node.id);
                              }}
                            >
                              关闭提醒
                            </button>
                          </div>
                          <div className="flex flex-wrap gap-1.5">
                            <Chip
                              className="text-[10px] h-5 px-1 flex-shrink-0"
                              color={expiryMeta.tone}
                              size="sm"
                              title={`${formatNodeRenewalTime(expiryMeta.nextDueTime)} (${getNodeRenewalCycleLabel(node.renewalCycle)})`}
                              variant="flat"
                            >
                              {expiryMeta.label}
                            </Chip>
                          </div>
                          <div className="rounded-lg border border-divider/80 bg-default-50/80 px-3 py-2 text-xs leading-5 text-default-700">
                            {formatNodeRenewalTime(expiryMeta.nextDueTime)}
                          </div>
                        </div>
                      )}

                      {hasRemark && (
                        <div className="space-y-2">
                          <div className="text-[11px] font-medium text-default-500">
                            备注
                          </div>
                          <div
                            className="max-h-32 overflow-y-auto rounded-lg border border-divider/80 bg-default-50/80 px-3 py-2 text-xs leading-5 text-default-700 break-all [scrollbar-width:thin]"
                            title={node.remark?.trim()}
                          >
                            {node.remark?.trim()}
                          </div>
                        </div>
                      )}
                    </div>
                  </div>
                </div>
              )}
            </div>
          </div>
        </CardHeader>

        <CardBody className="pt-0 pb-3 md:pt-0 md:pb-3 flex-1 flex flex-col">
          {isRemoteNode && node.syncError && (
            <div className="mb-3 px-2 py-1.5 rounded-md bg-warning-50 dark:bg-warning-100/10 text-warning-700 dark:text-warning-400 text-xs">
              {getRemoteSyncErrorMessage(node.syncError)}
            </div>
          )}
          {isRemoteNode ? (
            <div className="space-y-2 mb-4">
              <div className="flex justify-between items-center text-sm min-w-0">
                <span className="text-default-600 flex-shrink-0">IP</span>
                <div className="text-right text-xs min-w-0 flex-1 ml-2 min-h-[2.125rem]">
                  {node.serverIpV4?.trim() || node.serverIpV6?.trim() ? (
                    <div className="space-y-0.5">
                      {node.serverIpV4?.trim() && (
                        <span
                          className="font-mono truncate block"
                          title={node.serverIpV4.trim()}
                        >
                          {node.serverIpV4.trim()}
                        </span>
                      )}
                      {node.serverIpV6?.trim() && (
                        <span
                          className="font-mono truncate block"
                          title={node.serverIpV6.trim()}
                        >
                          {node.serverIpV6.trim()}
                        </span>
                      )}
                    </div>
                  ) : (
                    <span
                      className="font-mono truncate block"
                      title={node.serverIp.trim()}
                    >
                      {node.serverIp.trim()}
                    </span>
                  )}
                </div>
              </div>
            </div>
          ) : (
            <div className="mb-4 space-y-3">
              <p className="truncate font-mono text-xs text-default-500">
                {[
                  node.serverIpV4?.trim() ||
                    node.serverIpV6?.trim() ||
                    node.serverIp.trim(),
                  node.version || "未连接过",
                  node.connectionStatus === "online" && node.systemInfo
                    ? `已运行 ${formatUptime(node.systemInfo.uptime)}`
                    : "",
                ]
                  .filter(Boolean)
                  .join(" · ")}
              </p>
              {progress && progress.percent < 100 && (
                <Progress
                  showValueLabel
                  aria-label="升级进度"
                  color="warning"
                  label={progress.message}
                  size="sm"
                  value={progress.percent}
                />
              )}
              <NodeLiveStats
                info={
                  node.connectionStatus === "online"
                    ? (node.systemInfo ?? null)
                    : null
                }
                month={month}
                variant="card"
              />
            </div>
          )}

          {isRemoteNode && (
            <div className="space-y-3 mb-4">
              {remoteUsage ? (
                <>
                  <div className="text-xs rounded-md border border-default-200 dark:border-default-100/30 bg-default-50 dark:bg-default-100/20 p-2.5 space-y-2">
                    <div className="flex justify-between gap-2">
                      <span className="text-default-500">远程地址</span>
                      <span
                        className="font-mono text-right truncate"
                        title={remoteUsage.remoteUrl || node.remoteUrl || "-"}
                      >
                        {remoteUsage.remoteUrl || node.remoteUrl || "-"}
                      </span>
                    </div>
                    <div className="flex justify-between gap-2">
                      <span className="text-default-500">共享ID</span>
                      <span className="font-mono">#{remoteUsage.shareId}</span>
                    </div>
                    <div className="flex justify-between gap-2">
                      <span className="text-default-500">流量</span>
                      <span className="font-mono">
                        {formatFlow(remoteUsage.currentFlow)}
                      </span>
                    </div>
                    <div className="flex justify-between gap-2">
                      <span className="text-default-500">带宽上限</span>
                      <span className="font-mono">
                        {remoteUsage.maxBandwidth > 0
                          ? formatSpeed(remoteUsage.maxBandwidth)
                          : "不限"}
                      </span>
                    </div>
                  </div>

                  <div className="text-xs rounded-md border border-default-200 dark:border-default-100/30 bg-default-50 dark:bg-default-100/20 p-2.5">
                    <div className="flex items-center justify-between mb-2">
                      <span className="text-default-500">占用端口</span>
                      <span className="font-mono text-default-700 dark:text-default-300">
                        {remoteUsage.usedPorts.length}/
                        {Math.max(
                          remoteUsage.portRangeEnd -
                            remoteUsage.portRangeStart +
                            1,
                          0,
                        )}
                      </span>
                    </div>
                    <div className="max-h-20 overflow-y-auto rounded bg-white/10 dark:bg-black/10 p-1.5 [scrollbar-width:thin] [&::-webkit-scrollbar]:w-1">
                      {remoteUsage.usedPorts.length > 0 ? (
                        <div className="flex flex-wrap gap-1">
                          {remoteUsage.usedPorts.map((port) => (
                            <Chip
                              key={`${node.id}-port-${port}`}
                              className="font-mono shrink-0 whitespace-nowrap"
                              size="sm"
                              variant="flat"
                            >
                              {port}
                            </Chip>
                          ))}
                        </div>
                      ) : (
                        <div className="text-default-400">暂无占用端口</div>
                      )}
                    </div>
                  </div>

                  <div className="text-xs rounded-md border border-default-200 dark:border-default-100/30 bg-default-50 dark:bg-default-100/20 p-2.5">
                    <div className="flex items-center justify-between mb-2">
                      <span className="text-default-500">绑定明细</span>
                      <span className="font-mono text-default-700 dark:text-default-300">
                        {remoteUsage.activeBindingNum}
                      </span>
                    </div>
                    <div className="max-h-32 overflow-y-auto space-y-1.5 pr-1 [scrollbar-width:thin] [&::-webkit-scrollbar]:w-1">
                      {remoteUsage.bindings.length > 0 ? (
                        remoteUsage.bindings.map((binding) => (
                          <div
                            key={binding.bindingId}
                            className="rounded border border-default-200 dark:border-default-100/30 bg-white/10 dark:bg-black/10 p-2"
                          >
                            <div className="flex items-center justify-between gap-2">
                              <span
                                className="font-medium truncate"
                                title={binding.tunnelName}
                              >
                                {binding.tunnelName}
                              </span>
                              <span className="font-mono text-[11px]">
                                #{binding.tunnelId}
                              </span>
                            </div>
                            <div className="mt-1 text-[11px] text-default-500 flex items-center justify-between gap-2">
                              <span>
                                {formatChainType(
                                  binding.chainType,
                                  binding.hopInx,
                                )}
                              </span>
                              <span className="font-mono">
                                端口 {binding.allocatedPort}
                              </span>
                            </div>
                          </div>
                        ))
                      ) : (
                        <div className="text-default-400">暂无绑定明细</div>
                      )}
                    </div>
                  </div>
                </>
              ) : (
                <div className="text-xs rounded-md border border-default-200 dark:border-default-100/30 bg-default-50 dark:bg-default-100/20 p-2.5 text-default-500">
                  暂未获取到远程占用数据
                </div>
              )}
            </div>
          )}

          {node.latency && (
            <div className="mb-4 empty:hidden">
              {renderLatency(node, "card")}
            </div>
          )}

          <div className="mt-auto space-y-3">
            {/* 操作按钮 */}
            <div
              className={`grid gap-1.5 ${isRemoteNode ? "grid-cols-1" : "grid-cols-3"}`}
            >
              {!isRemoteNode && (
                <>
                  {onOpenMonitor && (
                    <Button
                      className="min-h-8"
                      color="primary"
                      size="sm"
                      variant="flat"
                      onPress={() => onOpenMonitor(node.id)}
                    >
                      监控
                    </Button>
                  )}
                  <Button
                    className="min-h-8"
                    color="success"
                    isLoading={node.copyLoading}
                    size="sm"
                    variant="flat"
                    onPress={() => openInstallSelector(node)}
                  >
                    安装
                  </Button>
                  <Button
                    className="min-h-8 bg-default-100 text-default-700"
                    size="sm"
                    variant="flat"
                    onPress={() => setTargetNodes([node])}
                  >
                    测试点
                  </Button>
                  <Button
                    className="min-h-8"
                    color="warning"
                    isDisabled={node.connectionStatus !== "online"}
                    isLoading={node.upgradeLoading}
                    size="sm"
                    variant="flat"
                    onPress={() => openUpgradeModal("single", node.id)}
                  >
                    升级
                  </Button>
                  <Button
                    className="min-h-8"
                    color="secondary"
                    isDisabled={node.connectionStatus !== "online"}
                    isLoading={node.rollbackLoading}
                    size="sm"
                    variant="flat"
                    onPress={() => handleRollbackNode(node)}
                  >
                    回退
                  </Button>
                  <Button
                    className="min-h-8"
                    color="success"
                    isDisabled={node.connectionStatus !== "online"}
                    size="sm"
                    variant="flat"
                    onPress={() => setTuneNode(node)}
                  >
                    调优
                  </Button>
                  <Button
                    className="min-h-8"
                    color="primary"
                    size="sm"
                    variant="flat"
                    onPress={() => handleEdit(node)}
                  >
                    编辑
                  </Button>
                </>
              )}
              <Button
                className="min-h-8"
                color="danger"
                size="sm"
                variant="flat"
                onPress={() => handleDelete(node)}
              >
                删除
              </Button>
            </div>
          </div>
        </CardBody>
      </Card>
    );
  }

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
        </>
      }
      footer={
        !isRemoteNode &&
        (wide ? (
          <div className="mt-2 lg:hidden" />
        ) : (
          <NodeLiveStats
            info={online ? (node.systemInfo ?? null) : null}
            month={month}
            variant="footer"
          />
        ))
      }
      meta={meta.filter(Boolean).join(" · ")}
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
          <div className="contents lg:flex lg:w-36 lg:justify-end">
            {renderLatency(node, "list")}
          </div>
          {!isDisplayFiltered && (
            <span
              className="cursor-grab touch-none p-1 text-default-300 hover:text-default-500 active:cursor-grabbing"
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
      title={node.name}
      onPress={() => setSheetNodeId(node.id)}
      onSelect={() => toggleSelect(node.id)}
    />
  );
});
