import type * as React from "react";

import { MoreHorizontal, Search, X } from "lucide-react";

import { AnimatedPage } from "@/components/animated-page";
import { Button } from "@/shadcn-bridge/heroui/button";
import { Checkbox } from "@/shadcn-bridge/heroui/checkbox";
import {
  Dropdown,
  DropdownItem,
  DropdownMenu,
  DropdownTrigger,
} from "@/shadcn-bridge/heroui/dropdown";

/*
 * Building blocks every page uses, so pages look alike:
 *   PageHeader   title, one line of help, one primary action
 *   PageTabs     the only tab style
 *   PageToolbar  search, filters, and secondary actions behind "更多"
 *   ListRow      one item per row; tapping opens its actions
 *   EmptyHint    what to do when a list is empty
 */

export interface PageAction {
  key: string;
  label: string;
  onPress: () => void;
  disabled?: boolean;
  danger?: boolean;
  icon?: React.ReactNode;
}

export function PageHeader({
  title,
  description,
  primary,
  secondary,
}: {
  title: string;
  description?: React.ReactNode;
  primary?: PageAction;
  /** A second, quieter action left of the primary one. */
  secondary?: PageAction;
}) {
  return (
    <div className="mb-6 flex items-center justify-between gap-3">
      <div className="min-w-0">
        {/* The help line stays as a tooltip: the page itself shows only
            its title. */}
        <h1
          className="text-xl font-semibold tracking-tight"
          title={typeof description === "string" ? description : undefined}
        >
          {title}
        </h1>
      </div>
      {(primary || secondary) && (
        <div className="flex shrink-0 gap-2">
          {secondary && (
            <Button
              className="gap-1.5 text-foreground dark:text-foreground"
              isDisabled={secondary.disabled}
              size="sm"
              startContent={secondary.icon}
              variant="bordered"
              onPress={secondary.onPress}
            >
              {secondary.label}
            </Button>
          )}
          {primary && (
            <Button
              color="primary"
              isDisabled={primary.disabled}
              size="sm"
              onPress={primary.onPress}
            >
              {primary.label}
            </Button>
          )}
        </div>
      )}
    </div>
  );
}

export interface PageTab<T extends string> {
  key: T;
  label: string;
  badge?: React.ReactNode;
}

export function PageTabs<T extends string>({
  value,
  tabs,
  onChange,
}: {
  value: T;
  tabs: Array<PageTab<T>>;
  onChange: (key: T) => void;
}) {
  return (
    <div
      className="mb-4 flex gap-5 overflow-x-auto border-b border-divider scrollbar-hide"
      role="tablist"
    >
      {tabs.map((t) => (
        <button
          key={t.key}
          aria-selected={value === t.key}
          className={`-mb-px flex shrink-0 items-center gap-1.5 border-b-[1.5px] py-2 text-sm transition-colors ${
            value === t.key
              ? "border-foreground font-medium text-foreground"
              : "border-transparent text-default-500 hover:text-foreground"
          }`}
          role="tab"
          type="button"
          onClick={() => onChange(t.key)}
        >
          <span className="truncate">{t.label}</span>
          {t.badge !== undefined && t.badge !== null && (
            <span className="shrink-0 text-xs font-normal tabular-nums text-default-400">
              {t.badge}
            </span>
          )}
        </button>
      ))}
    </div>
  );
}

export function PageToolbar({
  search,
  onSearch,
  placeholder = "搜索",
  children,
  more,
  primary,
}: {
  search?: string;
  onSearch?: (v: string) => void;
  placeholder?: string;
  /** Filters shown next to the search box. */
  children?: React.ReactNode;
  /** Secondary actions, behind a "更多" menu. */
  more?: PageAction[];
  /**
   * The main action, for a tab inside a page whose header holds no
   * action of its own.
   */
  primary?: PageAction;
}) {
  const actions = (more ?? []).filter(Boolean);

  return (
    <div className="mb-2 flex flex-wrap items-center gap-2 sm:flex-nowrap">
      {onSearch && (
        // With a main action too, the search gets a row of its own on a
        // phone instead of shrinking to its icon.
        <label
          className={`flex h-8 min-w-0 flex-1 items-center gap-2 rounded-md border border-divider px-2.5 transition-colors focus-within:border-default-400 sm:max-w-xs ${primary ? "basis-full sm:basis-auto" : ""}`}
        >
          <Search className="h-4 w-4 shrink-0 text-default-400" />
          <input
            className="min-w-0 flex-1 bg-transparent text-sm outline-none placeholder:text-default-400"
            enterKeyHint="search"
            placeholder={placeholder}
            type="text"
            value={search ?? ""}
            onChange={(e) => onSearch(e.target.value)}
          />
          {search && (
            <button
              aria-label="清除搜索"
              className="text-default-400"
              type="button"
              onClick={() => onSearch("")}
            >
              <X className="h-4 w-4" />
            </button>
          )}
        </label>
      )}
      {children}
      <div className="ml-auto flex shrink-0 items-center gap-2">
        {actions.length > 0 && (
          <Dropdown placement="bottom-end">
            <DropdownTrigger>
              <button
                aria-label="更多操作"
                className="flex h-8 items-center gap-1 rounded-md px-2 text-sm text-default-500 transition-colors hover:bg-default-100 hover:text-foreground"
                type="button"
              >
                <MoreHorizontal className="h-4 w-4" />
                <span className="hidden sm:inline">更多</span>
              </button>
            </DropdownTrigger>
            <DropdownMenu aria-label="更多操作">
              {actions.map((a) => (
                <DropdownItem
                  key={a.key}
                  className={a.disabled ? "pointer-events-none opacity-50" : ""}
                  color={a.danger ? "danger" : "default"}
                  onPress={a.disabled ? undefined : a.onPress}
                >
                  {a.label}
                </DropdownItem>
              ))}
            </DropdownMenu>
          </Dropdown>
        )}
        {primary && (
          <Button
            className="shrink-0"
            color="primary"
            isDisabled={primary.disabled}
            size="sm"
            onPress={primary.onPress}
          >
            {primary.label}
          </Button>
        )}
      </div>
    </div>
  );
}

/** A filter button for the toolbar; highlighted while a filter is on. */
export function ToolbarButton({
  active,
  children,
  onPress,
}: {
  active?: boolean;
  children: React.ReactNode;
  onPress: () => void;
}) {
  return (
    <button
      className={`flex h-8 shrink-0 items-center gap-1 rounded-md px-2.5 text-sm transition-colors ${
        active
          ? "bg-default-100 font-medium text-foreground"
          : "text-default-500 hover:bg-default-100 hover:text-foreground"
      }`}
      type="button"
      onClick={onPress}
    >
      {children}
    </button>
  );
}

/** Bar shown while picking items for a batch action. */
export function SelectionBar({
  count,
  allSelected,
  onToggleAll,
  actions,
  onExit,
}: {
  count: number;
  allSelected: boolean;
  onToggleAll: () => void;
  actions: PageAction[];
  onExit: () => void;
}) {
  return (
    <div className="sticky top-0 z-10 mb-2 flex flex-wrap items-center gap-2 border-b border-divider bg-background py-2">
      <span className="text-sm text-default-600">已选 {count} 个</span>
      <div className="ml-auto flex flex-wrap justify-end gap-2">
        <Button size="sm" variant="flat" onPress={onToggleAll}>
          {allSelected ? "取消全选" : "全选"}
        </Button>
        {actions.map((a) => (
          <Button
            key={a.key}
            color={a.danger ? "danger" : "default"}
            isDisabled={a.disabled || count === 0}
            size="sm"
            variant="flat"
            onPress={a.onPress}
          >
            {a.label}
          </Button>
        ))}
        <Button size="sm" variant="light" onPress={onExit}>
          完成
        </Button>
      </div>
    </div>
  );
}

export type RowStatus = "ok" | "warn" | "bad" | "off";

const STATUS_DOT: Record<RowStatus, string> = {
  ok: "bg-success",
  warn: "bg-warning",
  bad: "bg-danger",
  off: "bg-default-300",
};

/** One item of a list. Tapping it opens its actions. */
export function ListRow({
  status,
  statusTitle,
  title,
  badges,
  meta,
  footer,
  right,
  selectMode,
  selected,
  onSelect,
  onPress,
}: {
  status?: RowStatus;
  statusTitle?: string;
  title: React.ReactNode;
  badges?: React.ReactNode;
  meta?: React.ReactNode;
  /** Below the meta line, e.g. a usage bar. */
  footer?: React.ReactNode;
  /** Shown at the end of the row; clicks on it do not open the row. */
  right?: React.ReactNode;
  selectMode?: boolean;
  selected?: boolean;
  onSelect?: () => void;
  onPress: () => void;
}) {
  const activate = selectMode ? onSelect : onPress;

  return (
    <div
      aria-pressed={selectMode ? selected : undefined}
      className={`group flex w-full cursor-pointer items-center gap-3 border-b border-divider px-2 py-3 text-left transition-colors ${
        selected ? "bg-default-100" : "hover:bg-default-50"
      }`}
      role="button"
      tabIndex={0}
      onClick={activate}
      onKeyDown={(e) => {
        if (e.target !== e.currentTarget) return;
        if (e.key === "Enter" || e.key === " ") {
          e.preventDefault();
          activate?.();
        }
      }}
    >
      {selectMode && (
        <div
          className="flex shrink-0"
          role="presentation"
          onClick={(e) => e.stopPropagation()}
        >
          <Checkbox isSelected={selected} onValueChange={() => onSelect?.()} />
        </div>
      )}
      {status && (
        <span
          className={`h-1.5 w-1.5 shrink-0 rounded-full ${STATUS_DOT[status]}`}
          title={statusTitle}
        />
      )}
      <div className="min-w-0 flex-1">
        <div className="flex items-center gap-1.5">
          <span className="truncate">{title}</span>
          {badges}
        </div>
        {meta && (
          <div className="truncate text-xs text-default-400">{meta}</div>
        )}
        {footer && <div className="mt-1.5 empty:hidden">{footer}</div>}
      </div>
      {right && !selectMode && (
        <div
          className="flex shrink-0 items-center gap-2"
          role="presentation"
          onClick={(e) => e.stopPropagation()}
        >
          {right}
        </div>
      )}
    </div>
  );
}

export function ListGroup({
  title,
  count,
  children,
}: {
  title?: React.ReactNode;
  count?: number;
  children: React.ReactNode;
}) {
  return (
    <section>
      {title && (
        <h2 className="flex items-baseline gap-2 border-b border-divider px-2 pb-1.5 pt-5 text-xs font-medium text-default-500">
          {title}
          {count !== undefined && (
            <span className="text-xs font-normal text-default-400">
              {count}
            </span>
          )}
        </h2>
      )}
      <div className="flex flex-col">{children}</div>
    </section>
  );
}

export function EmptyHint({
  text,
  action,
}: {
  text: React.ReactNode;
  action?: PageAction;
}) {
  return (
    <div className="flex flex-col items-center gap-3 px-4 py-12 text-center text-sm text-default-400">
      <p>{text}</p>
      {action && (
        <Button color="primary" size="sm" onPress={action.onPress}>
          {action.label}
        </Button>
      )}
    </div>
  );
}

const pad = (n: number) => String(n).padStart(2, "0");

/** 2030-01-01; with time: 2030-01-01 08:00. Empty for no date. */
export function formatDate(ms?: number | string | null, withTime = false) {
  if (ms === undefined || ms === null || ms === "" || ms === 0) return "";
  const d = new Date(typeof ms === "string" && /^\d+$/.test(ms) ? +ms : ms);

  if (Number.isNaN(d.getTime())) return "";
  const date = `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}`;

  return withTime
    ? `${date} ${pad(d.getHours())}:${pad(d.getMinutes())}`
    : date;
}

/**
 * A page that can also sit inside another page's tab (the 用户 page hosts
 * 分组 and 限速): embedded, it drops its own padding and header.
 */
export function PageShell({
  embedded,
  children,
}: {
  embedded?: boolean;
  children: React.ReactNode;
}) {
  return embedded ? (
    <div>{children}</div>
  ) : (
    <AnimatedPage className="px-4 py-6 lg:px-10 lg:py-8">
      {children}
    </AnimatedPage>
  );
}
