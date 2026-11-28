import type { InboundApiItem, InboundRelayApiItem } from "@/api/types";
import type { RowStatus } from "@/components/page-kit";

import { Link2, Share2 } from "lucide-react";

import { LatencyCell } from "@/components/latency-cell";
import { EXTERNAL_LABEL } from "@/pages/inbound/external";
import {
  KERNEL_LABELS,
  describeInbound,
  kernelStatusText,
} from "@/pages/inbound/presets";
import { TagChips } from "@/pages/inbound/tags";
import { Button } from "@/shadcn-bridge/heroui/button";
import { Checkbox } from "@/shadcn-bridge/heroui/checkbox";
import { Chip } from "@/shadcn-bridge/heroui/chip";
import { Switch } from "@/shadcn-bridge/heroui/switch";
import { t, useI18n } from "@/i18n";
import { formatTraffic } from "@/utils/traffic";

export type InboundViewMode = "cards" | "table" | "list";
export type InboundGroupMode =
  | "server"
  | "region"
  | "protocol"
  | "tag"
  | "none";
export type InboundStatusFilter = "all" | "running" | "issue" | "disabled";
export type InboundRelayFilter = "all" | "relayed" | "direct";
export type InboundSortMode =
  | "default"
  | "traffic_desc"
  | "name_asc"
  | "port_asc";

export interface InboundHealth {
  status: RowStatus;
  dotClass: string;
  stateText: string;
  kernelBad: boolean;
  unreachable: boolean;
  running: boolean;
}

export function getInboundHealth(item: InboundApiItem): InboundHealth {
  const probe = item.probe?.skipped ? undefined : item.probe;
  const kernelBad =
    item.enabled && !item.external && item.kernelStatus !== "running";
  const unreachable =
    item.enabled && !kernelBad && probe !== undefined && !probe.ok;
  const running = item.enabled && !kernelBad && !unreachable;
  const status: RowStatus = unreachable
    ? "bad"
    : running
      ? "ok"
      : item.enabled
        ? "warn"
        : "off";
  const dotClass = unreachable
    ? "bg-danger"
    : running
      ? "bg-success"
      : item.enabled
        ? "bg-warning"
        : "bg-default-300";
  const stateText = unreachable
    ? `${t("连不上：")}${probe?.error ?? ""}`
    : running
      ? probe
        ? item.external
          ? `${t("可连接")} · ${probe.latencyMs} ms`
          : t("可连接")
        : t("运行中")
      : item.enabled
        ? kernelStatusText(item.kernelStatus)
        : t("已停用");

  return { status, dotClass, stateText, kernelBad, unreachable, running };
}

function relayEntryLabel(l: InboundRelayApiItem): string {
  const full = l.name || l.tunnelName || "";
  const cut = full.lastIndexOf(" → ");

  return cut > 0 ? full.slice(0, cut) : full || l.shareHost || t("中转");
}

function relayLatencyMs(l: InboundRelayApiItem): number | undefined {
  const lat = l.latency;

  if (!lat || lat.skipped || !lat.ok) return undefined;
  if (typeof lat.latencyMs === "number") return lat.latencyMs;
  if (typeof lat.pathMs === "number") return lat.pathMs;

  return undefined;
}

export interface InboundCardActions {
  selectMode: boolean;
  selectedIds: Set<number>;
  toggling: number | null;
  probingIds: Set<number>;
  onToggleSelect: (id: number) => void;
  onOpenSheet: (id: number) => void;
  onShare: (item: InboundApiItem) => void;
  onRelay: (item: InboundApiItem) => void;
  onToggleEnabled: (item: InboundApiItem) => void;
  onProbeOne: (id: number) => void;
}

function renderBadges(item: InboundApiItem, kernelBad: boolean, maxTags = 3) {
  return (
    <>
      <TagChips max={maxTags} tags={item.tags} />
      {item.outboundIpStrategy && item.outboundIpStrategy !== "auto" && (
        <Chip
          className="shrink-0 whitespace-nowrap"
          color="primary"
          size="sm"
          variant="flat"
        >
          {
            {
              ipv4_only: t("IPv4 出站"),
              ipv6_only: t("IPv6 出站"),
              prefer_ipv4: t("优先 IPv4"),
              prefer_ipv6: t("优先 IPv6"),
            }[item.outboundIpStrategy]
          }
        </Chip>
      )}
      {item.outboundId > 0 && (
        <Chip
          className="shrink-0 whitespace-nowrap"
          color={item.outboundMissing ? "danger" : "warning"}
          size="sm"
          title={
            item.outboundMissing
              ? t("出站节点已删除")
              : t("经 {name} 出站", { name: item.outboundName ?? "" })
          }
          variant="flat"
        >
          {item.outboundMissing ? t("出站已删除") : t("链式")}
        </Chip>
      )}
      {item.splitRules?.length > 0 && (
        <Chip
          className="shrink-0 whitespace-nowrap"
          color={item.defaultBlock ? "danger" : "primary"}
          size="sm"
          title={
            item.defaultBlock
              ? t("只放行分流规则选中的流量，其他的丢弃")
              : undefined
          }
          variant="flat"
        >
          {item.defaultBlock ? t("仅分流放行") : t("分流")}
        </Chip>
      )}
      {Boolean(item.rateDownMbps || item.rateUpMbps) && (
        <Chip
          className="shrink-0 whitespace-nowrap"
          size="sm"
          title={t("节点总带宽限速")}
          variant="flat"
        >
          {t("限速")}
          {item.rateDownMbps ? ` ↓${item.rateDownMbps}M` : ""}
          {item.rateUpMbps ? ` ↑${item.rateUpMbps}M` : ""}
        </Chip>
      )}
      {item.relayOnly && (
        <Chip className="shrink-0 whitespace-nowrap" size="sm" variant="flat">
          {t("仅中转")}
        </Chip>
      )}
      {kernelBad && (
        <Chip color="warning" size="sm" variant="flat">
          {kernelStatusText(item.kernelStatus)}
        </Chip>
      )}
    </>
  );
}

function RelaySummaryButton({
  item,
  relays,
  onPress,
}: {
  item: InboundApiItem;
  relays: InboundRelayApiItem[];
  onPress: () => void;
}) {
  const { t } = useI18n();
  const count = relays.length || item.relayCount;

  if (count <= 0) {
    return (
      <button
        className="inline-flex min-w-0 items-center gap-1 text-xs text-default-400 transition-colors hover:text-foreground"
        type="button"
        onClick={(e) => {
          e.stopPropagation();
          onPress();
        }}
      >
        <Link2 className="h-3.5 w-3.5 shrink-0" />
        <span className="truncate">{t("直连 · 添加中转")}</span>
      </button>
    );
  }

  const first = relays[0];
  const broken = relays.some(
    (r) => !(r.forwardExists && r.forwardStatus === 1),
  );
  const firstMs = first ? relayLatencyMs(first) : undefined;
  const label = first
    ? `${relayEntryLabel(first)}${firstMs !== undefined ? ` · ${firstMs} ms` : ""}${count > 1 ? ` (+${count - 1})` : ""}`
    : t("{count} 条中转", { count });
  const tooltip = relays
    .map((r) => {
      const ms = relayLatencyMs(r);

      return `${relayEntryLabel(r)}${ms !== undefined ? ` (${ms} ms)` : ""}`;
    })
    .join("\n");

  return (
    <button
      className={`inline-flex min-w-0 items-center gap-1 rounded-md px-1.5 py-0.5 text-xs font-medium transition-colors ${
        broken
          ? "bg-warning/15 text-warning-600 hover:bg-warning/25 dark:text-warning"
          : "bg-default-100 text-foreground hover:bg-default-200"
      }`}
      title={tooltip || label}
      type="button"
      onClick={(e) => {
        e.stopPropagation();
        onPress();
      }}
    >
      <Link2 className="h-3 w-3 shrink-0" />
      <span className="truncate">{label}</span>
    </button>
  );
}

export function InboundCard({
  item,
  relays,
  showServer,
  actions,
}: {
  item: InboundApiItem;
  relays: InboundRelayApiItem[];
  showServer: boolean;
  actions: InboundCardActions;
}) {
  const { t } = useI18n();
  const health = getInboundHealth(item);
  const probe = item.probe?.skipped ? undefined : item.probe;
  const selected = actions.selectMode && actions.selectedIds.has(item.id);
  const where = item.external
    ? item.sourceId
      ? item.sourceName || EXTERNAL_LABEL
      : item.listen || EXTERNAL_LABEL
    : showServer
      ? `${KERNEL_LABELS[item.kernelType] ?? item.kernelType} · ${item.nodeName}`
      : (KERNEL_LABELS[item.kernelType] ?? item.kernelType);
  const totalBytes = item.uploadBytes + item.downloadBytes;

  const activate = () =>
    actions.selectMode
      ? actions.onToggleSelect(item.id)
      : actions.onOpenSheet(item.id);

  return (
    <div
      aria-pressed={actions.selectMode ? selected : undefined}
      className={`phone-card phone-card-fill group flex cursor-pointer flex-col justify-between gap-2.5 rounded-xl border p-3.5 text-left transition-colors ${
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
      <div className="space-y-2">
        <div className="flex items-start justify-between gap-2">
          <div className="flex min-w-0 items-center gap-2">
            {actions.selectMode && (
              <div
                className="flex shrink-0"
                role="presentation"
                onClick={(e) => e.stopPropagation()}
              >
                <Checkbox
                  aria-label={t("选择 {name}", { name: item.name })}
                  isSelected={selected}
                  onValueChange={() => actions.onToggleSelect(item.id)}
                />
              </div>
            )}
            <span
              className={`h-2 w-2 shrink-0 rounded-full ${health.dotClass}`}
              title={health.stateText}
            />
            <span className="truncate text-sm font-medium text-foreground">
              {item.name}
            </span>
          </div>
          <span
            className="shrink-0 text-xs font-medium tabular-nums text-default-600"
            title={`↑ ${formatTraffic(item.uploadBytes)} · ↓ ${formatTraffic(item.downloadBytes)}`}
          >
            {formatTraffic(totalBytes)}
          </span>
        </div>

        <div className="flex items-center justify-between gap-2 text-xs text-default-400">
          <span className="truncate font-mono">
            {describeInbound(item)} · :{item.port}
            {where ? ` · ${where}` : ""}
          </span>
          {item.external && item.enabled && item.probe?.skipped !== true && (
            <div
              className="shrink-0"
              role="presentation"
              onClick={(e) => e.stopPropagation()}
            >
              <LatencyCell
                probe={probe}
                probing={actions.probingIds.has(item.id)}
                onProbe={() => actions.onProbeOne(item.id)}
              />
            </div>
          )}
        </div>

        <div className="flex flex-wrap items-center gap-1 empty:hidden">
          {renderBadges(item, health.kernelBad)}
        </div>
      </div>

      <div
        className="flex items-center justify-between gap-2 border-t border-divider/70 pt-2"
        role="presentation"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="min-w-0 flex-1">
          <RelaySummaryButton
            item={item}
            relays={relays}
            onPress={() => actions.onRelay(item)}
          />
        </div>
        {!actions.selectMode && (
          <div className="flex shrink-0 items-center gap-1.5">
            <Button
              aria-label={t("分享")}
              className="h-7 min-w-0 px-2 text-xs text-default-500 hover:text-foreground"
              size="sm"
              variant="light"
              onPress={() => actions.onShare(item)}
            >
              <Share2 className="h-3.5 w-3.5" />
              <span className="hidden sm:inline">{t("分享")}</span>
            </Button>
            <Switch
              isDisabled={actions.toggling === item.id}
              isSelected={item.enabled}
              size="sm"
              onValueChange={() => actions.onToggleEnabled(item)}
            />
          </div>
        )}
      </div>
    </div>
  );
}

export function InboundTable({
  items,
  relaysByInbound,
  actions,
}: {
  items: InboundApiItem[];
  relaysByInbound: Map<number, InboundRelayApiItem[]>;
  actions: InboundCardActions;
}) {
  const { t } = useI18n();

  return (
    <div className="phone-card phone-card-fill desktop-scrollbar overflow-x-auto rounded-xl border border-divider bg-content1">
      <table className="w-full min-w-[700px] text-left text-xs">
        <thead className="border-b border-divider bg-default-50/70 text-default-500">
          <tr>
            {actions.selectMode && <th className="w-9 px-3 py-2.5" />}
            <th className="px-3 py-2.5 font-medium">{t("节点")}</th>
            <th className="px-3 py-2.5 font-medium">{t("服务器 / 来源")}</th>
            <th className="px-3 py-2.5 font-medium">{t("协议 / 端口")}</th>
            <th className="px-3 py-2.5 font-medium">{t("中转")}</th>
            <th className="px-3 py-2.5 text-right font-medium">{t("流量")}</th>
            {!actions.selectMode && (
              <th className="px-3 py-2.5 text-right font-medium">
                {t("操作")}
              </th>
            )}
          </tr>
        </thead>
        <tbody className="divide-y divide-divider">
          {items.map((item) => {
            const health = getInboundHealth(item);
            const selected =
              actions.selectMode && actions.selectedIds.has(item.id);
            const relays = relaysByInbound.get(item.id) ?? [];
            const sourceLabel = item.external
              ? item.sourceId
                ? item.sourceName || EXTERNAL_LABEL
                : EXTERNAL_LABEL
              : item.nodeName;
            const activate = () =>
              actions.selectMode
                ? actions.onToggleSelect(item.id)
                : actions.onOpenSheet(item.id);

            return (
              <tr
                key={item.id}
                className={`cursor-pointer transition-colors ${
                  selected ? "bg-default-100" : "hover:bg-default-50"
                }`}
                onClick={activate}
              >
                {actions.selectMode && (
                  <td
                    className="px-3 py-2.5"
                    onClick={(e) => e.stopPropagation()}
                  >
                    <Checkbox
                      aria-label={t("选择 {name}", { name: item.name })}
                      isSelected={selected}
                      onValueChange={() => actions.onToggleSelect(item.id)}
                    />
                  </td>
                )}
                <td className="max-w-[16rem] px-3 py-2.5">
                  <div className="flex items-center gap-2">
                    <span
                      className={`h-2 w-2 shrink-0 rounded-full ${health.dotClass}`}
                      title={health.stateText}
                    />
                    <span className="truncate font-medium text-foreground">
                      {item.name}
                    </span>
                  </div>
                  <div className="mt-1 flex flex-wrap gap-1 pl-4 empty:hidden">
                    {renderBadges(item, health.kernelBad, 2)}
                  </div>
                </td>
                <td className="max-w-[10rem] truncate px-3 py-2.5 text-default-500">
                  {sourceLabel}
                </td>
                <td className="whitespace-nowrap px-3 py-2.5 font-mono text-default-500">
                  {describeInbound(item)} · :{item.port}
                  {!item.external && (
                    <span className="ml-1.5 text-default-400">
                      {KERNEL_LABELS[item.kernelType] ?? item.kernelType}
                    </span>
                  )}
                </td>
                <td
                  className="max-w-[13rem] px-3 py-2.5"
                  onClick={(e) => e.stopPropagation()}
                >
                  <RelaySummaryButton
                    item={item}
                    relays={relays}
                    onPress={() => actions.onRelay(item)}
                  />
                </td>
                <td
                  className="whitespace-nowrap px-3 py-2.5 text-right tabular-nums text-foreground"
                  title={`↑ ${formatTraffic(item.uploadBytes)} · ↓ ${formatTraffic(item.downloadBytes)}`}
                >
                  {formatTraffic(item.uploadBytes + item.downloadBytes)}
                </td>
                {!actions.selectMode && (
                  <td
                    className="whitespace-nowrap px-3 py-2.5 text-right"
                    onClick={(e) => e.stopPropagation()}
                  >
                    <div className="inline-flex items-center gap-1.5">
                      <Button
                        className="h-7 min-w-0 px-2 text-xs text-default-500 hover:text-foreground"
                        size="sm"
                        variant="light"
                        onPress={() => actions.onShare(item)}
                      >
                        {t("分享")}
                      </Button>
                      <Switch
                        isDisabled={actions.toggling === item.id}
                        isSelected={item.enabled}
                        size="sm"
                        onValueChange={() => actions.onToggleEnabled(item)}
                      />
                    </div>
                  </td>
                )}
              </tr>
            );
          })}
        </tbody>
      </table>
    </div>
  );
}
