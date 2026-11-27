import type {
  NodeGroupItem,
  SubscriptionBatchAction,
  SubscriptionItem,
} from "@/api";

import { useState } from "react";
import {
  CalendarPlus,
  CheckSquare,
  ChevronDown,
  ChevronRight,
  ChevronsDownUp,
  ChevronsUpDown,
  Copy,
  CopyPlus,
  Folder,
  GripVertical,
  Layers,
  List,
  Pin,
  PinOff,
  Plus,
  Power,
  RotateCcw,
  Tag,
  X,
} from "lucide-react";
import { useSortable } from "@dnd-kit/sortable";
import { CSS } from "@dnd-kit/utilities";

import { useI18n } from "@/i18n";
import { Button } from "@/shadcn-bridge/heroui/button";
import { Checkbox } from "@/shadcn-bridge/heroui/checkbox";
import { Chip } from "@/shadcn-bridge/heroui/chip";
import { Switch } from "@/shadcn-bridge/heroui/switch";
import { formatTraffic } from "@/utils/traffic";

export type SubViewMode = "grouped" | "flat";
export type SubStatusFilter =
  | "all"
  | "active"
  | "expiring"
  | "issue"
  | "disabled";
export type SubSortBy = "custom" | "expiry" | "traffic" | "ratio" | "name";

const DAY_MS = 86_400_000;

export const EXPIRING_SOON_DAYS = 30;
export const EXPIRING_URGENT_DAYS = 7;

export function daysUntilExpiry(ms: number): number | null {
  if (!ms) return null;

  return Math.ceil((ms - Date.now()) / DAY_MS);
}

export function isExpiringSoon(s: SubscriptionItem): boolean {
  if (!s.enabled || !s.active || !s.expireTime) return false;
  const days = daysUntilExpiry(s.expireTime);

  return days !== null && days > 0 && days <= EXPIRING_SOON_DAYS;
}

export function isSubIssue(s: SubscriptionItem): boolean {
  return s.enabled && !s.active;
}

export function usagePercent(s: SubscriptionItem): number {
  if (!s.trafficLimit) return 0;
  const used = s.uploadBytes + s.downloadBytes;

  return Math.min(100, (used / s.trafficLimit) * 100);
}

function toShortDate(ms: number): string {
  const d = new Date(ms);
  const pad = (n: number) => String(n).padStart(2, "0");

  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}`;
}

export function SubUsageBar({
  s,
  track = "bg-default-100",
}: {
  s: SubscriptionItem;
  track?: string;
}) {
  const pct = usagePercent(s);
  const tone =
    pct >= 100
      ? "bg-rose-500"
      : pct >= 85
        ? "bg-amber-500"
        : "bg-foreground/80";

  return (
    <div className={`h-[3px] overflow-hidden rounded-full ${track}`}>
      <div
        className={`h-full rounded-full transition-[width] duration-300 ${tone}`}
        style={{ width: `${Math.max(2, pct)}%` }}
      />
    </div>
  );
}

/** 4-column hairline overview strip matching Overview and Kernels tabs. */
export function SubSummaryStrip({
  subs,
  filteredSubs,
  isFiltered,
  statusFilter,
  onSelectStatus,
}: {
  subs: SubscriptionItem[];
  filteredSubs: SubscriptionItem[];
  isFiltered: boolean;
  statusFilter: SubStatusFilter;
  onSelectStatus: (next: SubStatusFilter) => void;
}) {
  const { t } = useI18n();
  const activeCount = subs.filter((s) => s.active).length;
  const disabledCount = subs.filter((s) => !s.enabled).length;
  const issueCount = subs.filter(isSubIssue).length;
  const expiringCount = subs.filter(isExpiringSoon).length;
  const urgentCount = subs.filter((s) => {
    const d = daysUntilExpiry(s.expireTime);

    return (
      s.enabled && s.active && d !== null && d > 0 && d <= EXPIRING_URGENT_DAYS
    );
  }).length;
  const trafficSource = isFiltered ? filteredSubs : subs;
  const totalTraffic = trafficSource.reduce(
    (sum, s) => sum + s.uploadBytes + s.downloadBytes,
    0,
  );
  const limitedCount = trafficSource.filter((s) => s.trafficLimit > 0).length;

  return (
    <div className="mb-4 grid grid-cols-2 gap-px overflow-hidden rounded-xl border border-divider bg-divider lg:grid-cols-4">
      <button
        className={`bg-background px-4 py-3 text-left transition-colors hover:bg-default-50/80 dark:hover:bg-default-100/20 ${
          statusFilter === "active"
            ? "ring-1 ring-inset ring-foreground/20"
            : ""
        }`}
        type="button"
        onClick={() =>
          onSelectStatus(statusFilter === "active" ? "all" : "active")
        }
      >
        <div className="text-xs text-default-400">{t("活跃订阅")}</div>
        <div className="mt-1 flex items-baseline gap-1 tabular-nums">
          <span className="text-xl font-semibold tracking-tight text-foreground">
            {activeCount}
          </span>
          <span className="text-xs text-default-400">/ {subs.length}</span>
        </div>
        <div className="mt-1 text-[11px] text-emerald-600 dark:text-emerald-400">
          {disabledCount > 0
            ? t("{count} 个已停用", { count: disabledCount })
            : t("全部已启用")}
        </div>
      </button>

      <button
        className={`bg-background px-4 py-3 text-left transition-colors hover:bg-default-50/80 dark:hover:bg-default-100/20 ${
          statusFilter === "expiring"
            ? "ring-1 ring-inset ring-amber-500/40"
            : ""
        }`}
        type="button"
        onClick={() =>
          onSelectStatus(statusFilter === "expiring" ? "all" : "expiring")
        }
      >
        <div className="text-xs text-default-400">{t("即将到期")}</div>
        <div className="mt-1 flex items-baseline gap-1.5 tabular-nums">
          <span
            className={`text-xl font-semibold tracking-tight ${
              urgentCount > 0
                ? "text-rose-600 dark:text-rose-400"
                : expiringCount > 0
                  ? "text-amber-600 dark:text-amber-400"
                  : "text-foreground"
            }`}
          >
            {expiringCount}
          </span>
          <span className="text-xs text-default-400">{t("30 天内")}</span>
        </div>
        <div className="mt-1 text-[11px] text-default-400">
          {urgentCount > 0
            ? t("其中 {count} 个 7 天内到期", { count: urgentCount })
            : expiringCount > 0
              ? t("点击筛出待续费订阅")
              : t("暂无临期订阅")}
        </div>
      </button>

      <button
        className={`bg-background px-4 py-3 text-left transition-colors hover:bg-default-50/80 dark:hover:bg-default-100/20 ${
          statusFilter === "issue" ? "ring-1 ring-inset ring-rose-500/40" : ""
        }`}
        type="button"
        onClick={() =>
          onSelectStatus(statusFilter === "issue" ? "all" : "issue")
        }
      >
        <div className="text-xs text-default-400">{t("超额或过期")}</div>
        <div className="mt-1 flex items-baseline gap-1.5 tabular-nums">
          <span
            className={`text-xl font-semibold tracking-tight ${
              issueCount > 0
                ? "text-rose-600 dark:text-rose-400"
                : "text-foreground"
            }`}
          >
            {issueCount}
          </span>
          {disabledCount > 0 && (
            <span className="text-xs text-default-400">
              · {t("{count} 停用", { count: disabledCount })}
            </span>
          )}
        </div>
        <div className="mt-1 text-[11px] text-default-400">
          {issueCount > 0 ? t("已自动断开，点击查看") : t("状态全部正常")}
        </div>
      </button>

      <div className="bg-background px-4 py-3">
        <div className="text-xs text-default-400">
          {isFiltered ? t("当前筛选流量") : t("累计流量")}
        </div>
        <div className="mt-1 truncate text-xl font-semibold tracking-tight tabular-nums text-foreground">
          {formatTraffic(totalTraffic)}
        </div>
        <div className="mt-1 text-[11px] text-default-400">
          {t("{count} 个设了配额", { count: limitedCount })}
        </div>
      </div>
    </div>
  );
}

/** Compact filter, view mode, sort, tag & batch controls bar. */
export function SubFilterBar({
  subs,
  groups,
  allTags,
  viewMode,
  onViewMode,
  allCollapsed,
  onToggleCollapseAll,
  statusFilter,
  onStatusFilter,
  groupFilter,
  onGroupFilter,
  tagFilter,
  onTagFilter,
  sortBy,
  onSortBy,
  batchMode,
  onToggleBatchMode,
}: {
  subs: SubscriptionItem[];
  groups: NodeGroupItem[];
  allTags: Array<[string, number]>;
  viewMode: SubViewMode;
  onViewMode: (m: SubViewMode) => void;
  allCollapsed: boolean;
  onToggleCollapseAll: () => void;
  statusFilter: SubStatusFilter;
  onStatusFilter: (f: SubStatusFilter) => void;
  groupFilter: number;
  onGroupFilter: (id: number) => void;
  tagFilter: string;
  onTagFilter: (tag: string) => void;
  sortBy: SubSortBy;
  onSortBy: (s: SubSortBy) => void;
  batchMode: boolean;
  onToggleBatchMode: () => void;
}) {
  const { t } = useI18n();
  const pinnedCount = subs.filter((s) => s.pinned).length;
  const expiringCount = subs.filter(isExpiringSoon).length;
  const issueCount = subs.filter(isSubIssue).length;
  const disabledCount = subs.filter((s) => !s.enabled).length;

  const statusPills: Array<{
    key: SubStatusFilter;
    label: string;
    count?: number;
    tone?: "amber" | "rose";
  }> = [
    { key: "all", label: t("全部") },
    { key: "active", label: t("正常") },
    ...(expiringCount > 0 || statusFilter === "expiring"
      ? [
          {
            key: "expiring" as const,
            label: t("即将到期"),
            count: expiringCount,
            tone: "amber" as const,
          },
        ]
      : []),
    ...(issueCount > 0 || statusFilter === "issue"
      ? [
          {
            key: "issue" as const,
            label: t("异常"),
            count: issueCount,
            tone: "rose" as const,
          },
        ]
      : []),
    ...(disabledCount > 0 || statusFilter === "disabled"
      ? [
          {
            key: "disabled" as const,
            label: t("已停用"),
            count: disabledCount,
          },
        ]
      : []),
  ];

  return (
    <div className="mb-3 space-y-2">
      <div className="flex flex-wrap items-center gap-2">
        {/* View mode toggle: grouped vs flat */}
        <div
          aria-label={t("视图模式")}
          className="inline-flex rounded-lg border border-divider bg-default-50/70 p-0.5 dark:bg-default-100/30"
          role="group"
        >
          <button
            aria-pressed={viewMode === "grouped"}
            className={`inline-flex h-6 items-center gap-1 rounded-md px-2 text-xs font-medium transition-colors ${
              viewMode === "grouped"
                ? "bg-background text-foreground shadow-xs"
                : "text-default-500 hover:text-foreground"
            }`}
            type="button"
            onClick={() => onViewMode("grouped")}
          >
            <Layers className="h-3.5 w-3.5" />
            <span>{t("分组")}</span>
          </button>
          <button
            aria-pressed={viewMode === "flat"}
            className={`inline-flex h-6 items-center gap-1 rounded-md px-2 text-xs font-medium transition-colors ${
              viewMode === "flat"
                ? "bg-background text-foreground shadow-xs"
                : "text-default-500 hover:text-foreground"
            }`}
            type="button"
            onClick={() => onViewMode("flat")}
          >
            <List className="h-3.5 w-3.5" />
            <span>{t("平铺")}</span>
          </button>
        </div>

        {viewMode === "grouped" && (
          <button
            className="inline-flex h-7 items-center gap-1 rounded-md border border-divider px-2 text-xs text-default-500 transition-colors hover:bg-default-100 hover:text-foreground"
            title={allCollapsed ? t("全部展开") : t("全部折叠")}
            type="button"
            onClick={onToggleCollapseAll}
          >
            {allCollapsed ? (
              <ChevronsUpDown className="h-3.5 w-3.5" />
            ) : (
              <ChevronsDownUp className="h-3.5 w-3.5" />
            )}
            <span className="hidden sm:inline">
              {allCollapsed ? t("展开") : t("折叠")}
            </span>
          </button>
        )}

        {/* Status pills */}
        <div className="flex flex-wrap items-center gap-1">
          {statusPills.map((p) => {
            const active = statusFilter === p.key;

            return (
              <button
                key={p.key}
                className={`h-7 rounded-md px-2.5 text-xs font-medium transition-colors ${
                  active
                    ? p.tone === "amber"
                      ? "bg-amber-500 text-white dark:bg-amber-500/80 dark:text-[#111214]"
                      : p.tone === "rose"
                        ? "bg-rose-500 text-white dark:bg-rose-500/80 dark:text-[#111214]"
                        : "bg-foreground text-background dark:bg-default-200 dark:text-foreground"
                    : p.tone === "amber"
                      ? "text-amber-600 hover:bg-amber-500/10 dark:text-amber-400"
                      : p.tone === "rose"
                        ? "text-rose-600 hover:bg-rose-500/10 dark:text-rose-400"
                        : "text-default-500 hover:bg-default-100 hover:text-foreground"
                }`}
                type="button"
                onClick={() => onStatusFilter(p.key)}
              >
                {p.label}
                {p.count !== undefined && (
                  <span className="ml-1 tabular-nums opacity-75">
                    {p.count}
                  </span>
                )}
              </button>
            );
          })}
        </div>

        {/* Right controls: node group select, sort select, batch toggle */}
        <div className="ml-auto flex flex-wrap items-center gap-1.5">
          {groups.length > 0 && (
            <select
              aria-label={t("按节点组筛选")}
              className="h-7 max-w-[11rem] rounded-md border border-divider bg-background px-2 text-xs text-foreground outline-none focus:border-ring"
              value={groupFilter}
              onChange={(e) => onGroupFilter(Number(e.target.value))}
            >
              <option value={0}>{t("全部节点组")}</option>
              {groups.map((g) => (
                <option key={g.id} value={g.id}>
                  {g.name} ({g.subscriptions.length})
                </option>
              ))}
            </select>
          )}

          <select
            aria-label={t("排序方式")}
            className="h-7 rounded-md border border-divider bg-background px-2 text-xs text-foreground outline-none focus:border-ring"
            value={sortBy}
            onChange={(e) => onSortBy(e.target.value as SubSortBy)}
          >
            <option value="custom">{t("自定义顺序")}</option>
            <option value="expiry">{t("到期最近")}</option>
            <option value="traffic">{t("已用流量")}</option>
            <option value="ratio">{t("流量占比")}</option>
            <option value="name">{t("名称排序")}</option>
          </select>

          <button
            aria-pressed={batchMode}
            className={`inline-flex h-7 items-center gap-1 rounded-md border px-2 text-xs font-medium transition-colors ${
              batchMode
                ? "border-foreground bg-foreground text-background dark:border-default-300 dark:bg-default-200 dark:text-foreground"
                : "border-divider text-default-600 hover:bg-default-100 hover:text-foreground"
            }`}
            type="button"
            onClick={onToggleBatchMode}
          >
            <CheckSquare className="h-3.5 w-3.5" />
            <span>{t("批量")}</span>
          </button>
        </div>
      </div>

      {/* Tag & Pinned quick filter strip (visible when any tags or pins exist) */}
      {(allTags.length > 0 || pinnedCount > 0 || tagFilter !== "") && (
        <div className="flex flex-wrap items-center gap-1.5">
          <span className="inline-flex items-center gap-1 text-[11px] text-default-400">
            <Tag className="h-3 w-3" />
            {t("标签")}
          </span>
          <button
            className={`h-6 rounded-full px-2.5 text-[11px] font-medium transition-colors ${
              tagFilter === ""
                ? "bg-foreground text-background dark:bg-default-200 dark:text-foreground"
                : "bg-default-100 text-default-600 hover:bg-default-200/70 dark:bg-default-100/50"
            }`}
            type="button"
            onClick={() => onTagFilter("")}
          >
            {t("全部")}
          </button>
          {pinnedCount > 0 && (
            <button
              className={`inline-flex h-6 items-center gap-1 rounded-full px-2.5 text-[11px] font-medium transition-colors ${
                tagFilter === "__pinned__"
                  ? "bg-amber-500 text-white dark:bg-amber-500/80 dark:text-[#111214]"
                  : "bg-amber-500/10 text-amber-700 hover:bg-amber-500/20 dark:text-amber-400"
              }`}
              type="button"
              onClick={() =>
                onTagFilter(tagFilter === "__pinned__" ? "" : "__pinned__")
              }
            >
              <Pin className="h-3 w-3" />
              <span>{t("已置顶")}</span>
              <span className="tabular-nums opacity-80">{pinnedCount}</span>
            </button>
          )}
          {allTags.map(([tag, count]) => {
            const active = tagFilter === tag;

            return (
              <button
                key={tag}
                className={`h-6 rounded-full px-2.5 text-[11px] font-medium transition-colors ${
                  active
                    ? "bg-foreground text-background dark:bg-default-200 dark:text-foreground"
                    : "bg-default-100 text-default-600 hover:bg-default-200/70 dark:bg-default-100/50"
                }`}
                type="button"
                onClick={() => onTagFilter(active ? "" : tag)}
              >
                #{tag}
                <span className="ml-1 tabular-nums opacity-65">{count}</span>
              </button>
            );
          })}
        </div>
      )}
    </div>
  );
}

/** Batch action bar displayed when batch mode is active. */
export function SubBatchBar({
  visibleIds,
  selectedIds,
  busy,
  onSelectAll,
  onClearSelection,
  onBatch,
  onOpenGroupModal,
  onOpenTagModal,
  onExit,
}: {
  visibleIds: number[];
  selectedIds: Set<number>;
  busy: boolean;
  onSelectAll: () => void;
  onClearSelection: () => void;
  onBatch: (action: SubscriptionBatchAction, days?: number) => Promise<void>;
  onOpenGroupModal: () => void;
  onOpenTagModal: () => void;
  onExit: () => void;
}) {
  const { t } = useI18n();
  const count = selectedIds.size;
  const allSelected =
    visibleIds.length > 0 && visibleIds.every((id) => selectedIds.has(id));

  return (
    <div className="mb-3 flex flex-wrap items-center gap-2 rounded-xl border border-divider bg-default-50/90 px-3.5 py-2.5 dark:bg-default-100/30">
      <div className="flex items-center gap-2">
        <Checkbox
          isSelected={allSelected}
          size="sm"
          onValueChange={(checked) =>
            checked ? onSelectAll() : onClearSelection()
          }
        >
          <span className="text-xs font-medium text-foreground">
            {t("已选 {count} 项", { count })}
          </span>
        </Checkbox>
      </div>

      <div className="flex flex-wrap items-center gap-1.5 sm:ml-2">
        <Button
          className="h-7 gap-1 px-2.5 text-xs"
          isDisabled={count === 0 || busy}
          size="sm"
          startContent={<Power className="h-3.5 w-3.5" />}
          variant="flat"
          onPress={() => void onBatch("enable")}
        >
          {t("启用")}
        </Button>
        <Button
          className="h-7 px-2.5 text-xs"
          isDisabled={count === 0 || busy}
          size="sm"
          variant="flat"
          onPress={() => void onBatch("disable")}
        >
          {t("停用")}
        </Button>
        <Button
          className="h-7 gap-1 px-2.5 text-xs"
          isDisabled={count === 0 || busy}
          size="sm"
          startContent={<CalendarPlus className="h-3.5 w-3.5" />}
          variant="flat"
          onPress={() => void onBatch("extend-days", 30)}
        >
          {t("+30 天")}
        </Button>
        <Button
          className="h-7 px-2.5 text-xs"
          isDisabled={count === 0 || busy}
          size="sm"
          variant="flat"
          onPress={() => void onBatch("extend-days", 90)}
        >
          {t("+90 天")}
        </Button>
        <Button
          className="h-7 px-2.5 text-xs"
          isDisabled={count === 0 || busy}
          size="sm"
          variant="flat"
          onPress={() => void onBatch("extend-days", 365)}
        >
          {t("+1 年")}
        </Button>
        <Button
          className="h-7 gap-1 px-2.5 text-xs"
          isDisabled={count === 0 || busy}
          size="sm"
          startContent={<RotateCcw className="h-3.5 w-3.5" />}
          variant="flat"
          onPress={() => void onBatch("reset-traffic")}
        >
          {t("重置流量")}
        </Button>
        <Button
          className="h-7 gap-1 px-2.5 text-xs"
          isDisabled={count === 0 || busy}
          size="sm"
          startContent={<Folder className="h-3.5 w-3.5" />}
          variant="flat"
          onPress={onOpenGroupModal}
        >
          {t("设分组")}
        </Button>
        <Button
          className="h-7 gap-1 px-2.5 text-xs"
          isDisabled={count === 0 || busy}
          size="sm"
          startContent={<Tag className="h-3.5 w-3.5" />}
          variant="flat"
          onPress={onOpenTagModal}
        >
          {t("加标签")}
        </Button>
        <Button
          className="h-7 gap-1 px-2.5 text-xs"
          isDisabled={count === 0 || busy}
          size="sm"
          startContent={<Pin className="h-3.5 w-3.5" />}
          variant="flat"
          onPress={() => void onBatch("pin")}
        >
          {t("置顶")}
        </Button>
      </div>

      <Button
        className="ml-auto h-7 px-2.5 text-xs"
        size="sm"
        variant="light"
        onPress={onExit}
      >
        {t("完成")}
      </Button>
    </div>
  );
}

/** Single subscription row with drag handle / batch checkbox, tags, expiry badge & quick actions. */
export function SubRowItem({
  sub,
  sourceText,
  showGroup,
  sortable,
  batchMode,
  selected,
  toggling,
  pinning,
  onToggleSelect,
  onOpenDetail,
  onTogglePin,
  onClone,
  onCopyLink,
  onToggleEnabled,
  onTagClick,
}: {
  sub: SubscriptionItem;
  sourceText: string;
  showGroup?: boolean;
  sortable: boolean;
  batchMode: boolean;
  selected: boolean;
  toggling: boolean;
  pinning: boolean;
  onToggleSelect: () => void;
  onOpenDetail: () => void;
  onTogglePin: () => void;
  onClone: () => void;
  onCopyLink: () => void;
  onToggleEnabled: () => void;
  onTagClick: (tag: string) => void;
}) {
  const { t } = useI18n();
  const {
    attributes,
    listeners,
    isDragging,
    setActivatorNodeRef,
    setNodeRef,
    transform,
    transition,
  } = useSortable({ id: sub.id, disabled: !sortable });

  const days = daysUntilExpiry(sub.expireTime);
  const showExpiryPill =
    sub.enabled &&
    sub.active &&
    days !== null &&
    days > 0 &&
    days <= EXPIRING_SOON_DAYS;
  const urgentExpiry = days !== null && days <= EXPIRING_URGENT_DAYS;
  const tags = sub.tags ?? [];

  const expiryText = !sub.expireTime
    ? ""
    : days !== null && days <= 0
      ? t("{date} 已过期", { date: toShortDate(sub.expireTime) })
      : t("{date} 到期 · 剩 {days} 天", {
          date: toShortDate(sub.expireTime),
          days: days ?? 0,
        });

  return (
    <div
      ref={setNodeRef}
      className={`group flex items-center gap-2.5 bg-background px-3 py-3 transition-colors hover:bg-default-50/80 dark:hover:bg-default-100/25 ${
        isDragging
          ? "relative z-20 rounded-lg shadow-md ring-1 ring-divider"
          : ""
      } ${selected ? "bg-default-100/60 dark:bg-default-100/25" : ""}`}
      style={{ transform: CSS.Transform.toString(transform), transition }}
    >
      {batchMode ? (
        <Checkbox
          aria-label={sub.name}
          isSelected={selected}
          size="sm"
          onValueChange={onToggleSelect}
        />
      ) : (
        sortable && (
          <button
            {...attributes}
            {...listeners}
            ref={setActivatorNodeRef}
            aria-label={t("拖拽调整 {name} 的顺序", { name: sub.name })}
            className="flex h-7 w-6 shrink-0 touch-none cursor-grab items-center justify-center rounded text-default-300 transition-colors hover:bg-default-100 hover:text-default-500 active:cursor-grabbing"
            type="button"
          >
            <GripVertical className="h-4 w-4" />
          </button>
        )
      )}

      {/* Main clickable info area */}
      <button
        className="flex min-w-0 flex-1 items-center gap-2.5 text-left focus:outline-none"
        type="button"
        onClick={batchMode ? onToggleSelect : onOpenDetail}
      >
        <span
          className={`h-2 w-2 shrink-0 rounded-full ${
            sub.active
              ? "bg-emerald-500"
              : sub.enabled
                ? "bg-rose-500"
                : "bg-default-300"
          }`}
        />
        <div className="min-w-0 flex-1">
          <div className="flex flex-wrap items-center gap-1.5">
            <span className="truncate text-sm font-medium text-foreground">
              {sub.name}
            </span>
            {sub.pinned && (
              <Pin
                aria-label={t("已置顶")}
                className="h-3 w-3 shrink-0 text-amber-500"
              />
            )}
            {showGroup && sub.group && (
              <span className="inline-flex shrink-0 items-center gap-1 rounded-md border border-divider/80 bg-default-50 px-1.5 py-0.5 text-[10px] font-medium text-default-600 dark:bg-default-100/40 dark:text-default-400">
                <Folder className="h-2.5 w-2.5 text-default-400" />
                {sub.group}
              </span>
            )}
            {!sub.enabled ? (
              <Chip className="h-5 text-[11px]" size="sm" variant="flat">
                {t("已停用")}
              </Chip>
            ) : (
              !sub.active && (
                <Chip
                  className="h-5 text-[11px]"
                  color="danger"
                  size="sm"
                  variant="flat"
                >
                  {sub.overQuota ? t("流量用完") : t("已过期")}
                </Chip>
              )
            )}
            {showExpiryPill && (
              <span
                className={`inline-flex shrink-0 items-center rounded-full px-1.5 py-0.5 text-[10px] font-medium tabular-nums ${
                  urgentExpiry
                    ? "bg-rose-500/12 text-rose-600 dark:text-rose-400"
                    : "bg-amber-500/12 text-amber-700 dark:text-amber-400"
                }`}
              >
                {t("剩 {days} 天", { days: days ?? 0 })}
              </span>
            )}
            {tags.slice(0, 3).map((tag) => (
              <span
                key={tag}
                className="inline-flex shrink-0 cursor-pointer items-center rounded-md bg-default-100 px-1.5 py-0.5 text-[10px] font-medium text-default-600 transition-colors hover:bg-default-200 dark:bg-default-100/60 dark:text-default-400"
                role="presentation"
                onClick={(e) => {
                  e.stopPropagation();
                  onTagClick(tag);
                }}
              >
                #{tag}
              </span>
            ))}
            {tags.length > 3 && (
              <span className="text-[10px] text-default-400">
                +{tags.length - 3}
              </span>
            )}
          </div>

          <div className="mt-0.5 truncate text-xs text-default-400">
            {[
              t("{count} 个节点", { count: sub.nodeCount }),
              sourceText,
              expiryText,
              sub.userName ? `@${sub.userName}` : "",
              sub.remark,
            ]
              .filter(Boolean)
              .join(" · ")}
          </div>

          {sub.trafficLimit > 0 && (
            <div className="mt-1.5 max-w-xs">
              <SubUsageBar s={sub} />
            </div>
          )}
        </div>
      </button>

      {/* Traffic column */}
      <div className="hidden w-36 shrink-0 text-right text-sm tabular-nums sm:block">
        <span className="text-foreground">
          {formatTraffic(sub.uploadBytes + sub.downloadBytes)}
        </span>
        {sub.trafficLimit > 0 && (
          <span className="text-xs text-default-400">
            {" "}
            / {formatTraffic(sub.trafficLimit)}
          </span>
        )}
      </div>

      {/* Quick actions */}
      {!batchMode && (
        <div className="flex shrink-0 items-center gap-1">
          <button
            aria-label={sub.pinned ? t("取消置顶") : t("置顶")}
            className={`hidden h-7 w-7 items-center justify-center rounded-md transition-colors sm:flex ${
              sub.pinned
                ? "text-amber-500 hover:bg-amber-500/10"
                : "text-default-400 opacity-0 hover:bg-default-200/70 hover:text-foreground group-hover:opacity-100 dark:hover:bg-default-100"
            }`}
            disabled={pinning}
            title={sub.pinned ? t("取消置顶") : t("置顶")}
            type="button"
            onClick={onTogglePin}
          >
            {sub.pinned ? (
              <PinOff className="h-3.5 w-3.5" />
            ) : (
              <Pin className="h-3.5 w-3.5" />
            )}
          </button>
          <button
            aria-label={t("克隆新建")}
            className="hidden h-7 w-7 items-center justify-center rounded-md text-default-400 opacity-0 transition-colors hover:bg-default-200/70 hover:text-foreground group-hover:opacity-100 sm:flex dark:hover:bg-default-100"
            title={t("以此配置克隆新建")}
            type="button"
            onClick={onClone}
          >
            <CopyPlus className="h-3.5 w-3.5" />
          </button>
          <button
            aria-label={t("复制 {name} 的订阅链接", { name: sub.name })}
            className="flex h-7 w-7 items-center justify-center rounded-md text-default-400 transition-colors hover:bg-default-200/70 hover:text-foreground dark:hover:bg-default-100"
            title={t("复制订阅链接")}
            type="button"
            onClick={onCopyLink}
          >
            <Copy className="h-3.5 w-3.5" />
          </button>
          <span
            className="ml-0.5 flex"
            title={
              sub.enabled
                ? t("已启用，点击停用订阅")
                : t("已停用，点击启用订阅")
            }
          >
            <Switch
              aria-label={`${sub.enabled ? t("停用") : t("启用")} ${sub.name}`}
              isDisabled={toggling}
              isSelected={sub.enabled}
              size="sm"
              onValueChange={onToggleEnabled}
            />
          </span>
        </div>
      )}
    </div>
  );
}

/** Collapsible section card used in grouped view. */
export function SubGroupSection({
  title,
  pinned,
  items,
  collapsed,
  onToggleCollapse,
  onCreateInGroup,
  renderRow,
}: {
  title: string;
  pinned?: boolean;
  items: SubscriptionItem[];
  collapsed: boolean;
  onToggleCollapse: () => void;
  onCreateInGroup?: () => void;
  renderRow: (s: SubscriptionItem) => React.ReactNode;
}) {
  const { t } = useI18n();
  const activeCount = items.filter((s) => s.active).length;
  const expiringCount = items.filter(isExpiringSoon).length;
  const totalTraffic = items.reduce(
    (sum, s) => sum + s.uploadBytes + s.downloadBytes,
    0,
  );

  return (
    <section className="overflow-hidden rounded-xl border border-divider bg-background">
      <div className="flex items-center justify-between gap-2 bg-default-50/70 px-3.5 py-2.5 dark:bg-default-50/20">
        <button
          aria-expanded={!collapsed}
          className="flex min-w-0 flex-1 items-center gap-2 text-left"
          type="button"
          onClick={onToggleCollapse}
        >
          {collapsed ? (
            <ChevronRight className="h-4 w-4 shrink-0 text-default-400" />
          ) : (
            <ChevronDown className="h-4 w-4 shrink-0 text-default-400" />
          )}
          {pinned && <Pin className="h-3.5 w-3.5 shrink-0 text-amber-500" />}
          <span className="truncate text-sm font-semibold text-foreground">
            {title}
          </span>
          <span className="shrink-0 rounded-full bg-default-100 px-2 py-0.5 text-[11px] font-medium tabular-nums text-default-500 dark:bg-default-100/60">
            {items.length}
          </span>
          <span className="hidden shrink-0 text-xs tabular-nums text-default-400 sm:inline">
            ·{" "}
            {t("{active}/{total} 活跃", {
              active: activeCount,
              total: items.length,
            })}
          </span>
          {totalTraffic > 0 && (
            <span className="hidden shrink-0 text-xs tabular-nums text-default-400 md:inline">
              · {formatTraffic(totalTraffic)}
            </span>
          )}
          {expiringCount > 0 && (
            <span className="shrink-0 rounded-full bg-amber-500/12 px-2 py-0.5 text-[10px] font-medium text-amber-700 dark:text-amber-400">
              {t("{count} 临期", { count: expiringCount })}
            </span>
          )}
        </button>

        {onCreateInGroup && (
          <Button
            className="h-7 min-w-0 shrink-0 gap-1 px-2 text-xs text-default-600 hover:text-foreground"
            size="sm"
            startContent={<Plus className="h-3.5 w-3.5" />}
            variant="light"
            onPress={onCreateInGroup}
          >
            {t("新建")}
          </Button>
        )}
      </div>

      {!collapsed && (
        <div className="divide-y divide-divider/60 border-t border-divider/70">
          {items.map((s) => renderRow(s))}
        </div>
      )}
    </section>
  );
}

/** Tag chip input with existing-tag suggestions for Subscription Editor & Batch Tag modal. */
export function TagEditorInput({
  value,
  suggestions,
  onChange,
  placeholder,
}: {
  value: string[];
  suggestions: string[];
  onChange: (next: string[]) => void;
  placeholder?: string;
}) {
  const { t } = useI18n();
  const [input, setInput] = useState("");

  const addTags = (raw: string) => {
    const parts = raw
      .replace(/，/g, ",")
      .split(",")
      .map((s) => s.trim())
      .filter(Boolean);

    if (parts.length === 0) return;
    const next = [...value];

    for (const p of parts) {
      if (!next.includes(p) && next.length < 10) {
        next.push(p);
      }
    }
    onChange(next);
    setInput("");
  };

  const removeTag = (tag: string) => {
    onChange(value.filter((x) => x !== tag));
  };

  const unusedSuggestions = suggestions.filter((s) => !value.includes(s));

  return (
    <div className="space-y-2">
      <div className="flex flex-wrap items-center gap-1.5 rounded-lg border border-divider bg-background px-2.5 py-2 focus-within:border-ring focus-within:ring-2 focus-within:ring-ring/20">
        {value.map((tag) => (
          <span
            key={tag}
            className="inline-flex items-center gap-1 rounded-md bg-default-100 px-2 py-0.5 text-xs font-medium text-foreground dark:bg-default-100/70"
          >
            <span>#{tag}</span>
            <button
              aria-label={`${t("移除")} ${tag}`}
              className="text-default-400 hover:text-foreground"
              type="button"
              onClick={() => removeTag(tag)}
            >
              <X className="h-3 w-3" />
            </button>
          </span>
        ))}
        <input
          className="min-w-[8rem] flex-1 bg-transparent text-sm outline-none placeholder:text-default-400"
          placeholder={
            placeholder ?? t("输入标签后按回车（如：拼车、自用、月付）")
          }
          type="text"
          value={input}
          onBlur={() => {
            if (input.trim()) addTags(input);
          }}
          onChange={(e) => {
            const v = e.target.value;

            if (v.includes(",") || v.includes("，")) {
              addTags(v);
            } else {
              setInput(v);
            }
          }}
          onKeyDown={(e) => {
            if (e.key === "Enter") {
              e.preventDefault();
              addTags(input);
            } else if (e.key === "Backspace" && !input && value.length > 0) {
              removeTag(value[value.length - 1]);
            }
          }}
        />
      </div>

      {unusedSuggestions.length > 0 && (
        <div className="flex flex-wrap items-center gap-1.5">
          <span className="text-[11px] text-default-400">{t("常用标签")}</span>
          {unusedSuggestions.slice(0, 12).map((tag) => (
            <button
              key={tag}
              className="rounded-full border border-divider px-2 py-0.5 text-[11px] text-default-600 transition-colors hover:border-foreground/40 hover:text-foreground"
              type="button"
              onClick={() => addTags(tag)}
            >
              + {tag}
            </button>
          ))}
        </div>
      )}
    </div>
  );
}
