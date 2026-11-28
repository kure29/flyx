import type * as React from "react";

import { useLayoutEffect, useRef, useState } from "react";
import { ChevronDown, MoreHorizontal, Search, X } from "lucide-react";

import { AnimatedPage } from "@/components/animated-page";
import { useI18n } from "@/i18n";
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
        <h1 className="page-title text-xl font-semibold tracking-tight">
          {title}
        </h1>
        {description && (
          <p
            className="mt-1 hidden truncate text-xs text-default-400 sm:block"
            title={typeof description === "string" ? description : undefined}
          >
            {description}
          </p>
        )}
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
              className="gap-1.5"
              color="primary"
              isDisabled={primary.disabled}
              size="sm"
              startContent={primary.icon}
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
  const listRef = useRef<HTMLDivElement>(null);
  // The phone's sliding pill: where the selected tab is. "ready" places it
  // without a slide (first paint), "moving" slides it to a new tab.
  const [pill, setPill] = useState<{ x: number; w: number } | null>(null);
  const [moving, setMoving] = useState(false);
  const lastValue = useRef(value);

  useLayoutEffect(() => {
    const list = listRef.current;

    if (!list) return;
    const place = () => {
      const tab = list.querySelector<HTMLElement>('[aria-selected="true"]');

      if (tab) setPill({ x: tab.offsetLeft, w: tab.offsetWidth });
    };

    place();
    const ro = new ResizeObserver(place);

    ro.observe(list);
    list
      .querySelectorAll<HTMLElement>('[role="tab"]')
      .forEach((t) => ro.observe(t));

    return () => ro.disconnect();
  }, [value, tabs.length]);

  // After a switch: slide the pill, fade in what is under the tabs.
  useLayoutEffect(() => {
    if (lastValue.current === value) return;
    lastValue.current = value;
    setMoving(true);
    // A mark on the parent fades whatever sits after the tabs, including a
    // loading state replaced by the content while the mark is on.
    const parent = listRef.current?.parentElement;

    if (!parent) return;
    parent.classList.remove("tabs-switched");
    void parent.offsetWidth;
    parent.classList.add("tabs-switched");
    const timer = window.setTimeout(
      () => parent.classList.remove("tabs-switched"),
      400,
    );

    return () => window.clearTimeout(timer);
  }, [value]);

  return (
    <div
      ref={listRef}
      className="page-tabs mb-4 flex gap-5 overflow-x-auto border-b border-divider scrollbar-hide"
      data-indicator={pill ? (moving ? "moving" : "ready") : undefined}
      role="tablist"
    >
      {pill && (
        <span
          aria-hidden
          className="page-tabs-indicator"
          style={{ width: pill.w, transform: `translateX(${pill.x}px)` }}
        />
      )}
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
  placeholder,
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
  const { t } = useI18n();
  const actions = (more ?? []).filter(Boolean);
  const resolvedPlaceholder = placeholder ?? t("搜索");

  return (
    <div className="mb-2 flex flex-wrap items-center gap-2 sm:flex-nowrap">
      {onSearch && (
        // With a main action too, the search gets a row of its own on a
        // phone instead of shrinking to its icon.
        <label
          className={`page-search flex h-8 min-w-0 flex-1 items-center gap-2 rounded-md border border-divider px-2.5 transition-[border-color,box-shadow] duration-150 focus-within:border-ring focus-within:ring-2 focus-within:ring-ring/20 sm:max-w-xs ${primary ? "basis-full sm:basis-auto" : ""}`}
        >
          <Search className="h-4 w-4 shrink-0 text-default-400" />
          <input
            className="min-w-0 flex-1 bg-transparent text-sm outline-none placeholder:text-default-400"
            enterKeyHint="search"
            placeholder={resolvedPlaceholder}
            type="text"
            value={search ?? ""}
            onChange={(e) => onSearch(e.target.value)}
          />
          {search && (
            <button
              aria-label={t("清除搜索")}
              className="text-default-400 transition-colors hover:text-foreground"
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
                aria-label={t("更多操作")}
                className="flex h-8 items-center gap-1 rounded-md px-2 text-sm text-default-500 transition-[color,background-color,transform] duration-150 hover:bg-default-100 hover:text-foreground active:scale-[0.97]"
                type="button"
              >
                <MoreHorizontal className="h-4 w-4" />
                <span className="hidden sm:inline">{t("更多")}</span>
              </button>
            </DropdownTrigger>
            <DropdownMenu aria-label={t("更多操作")}>
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
            className="shrink-0 gap-1.5"
            color="primary"
            isDisabled={primary.disabled}
            size="sm"
            startContent={primary.icon}
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
      className={`flex h-8 shrink-0 items-center gap-1 rounded-md px-2.5 text-sm transition-[color,background-color,transform] duration-150 active:scale-[0.97] ${
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
  const { t } = useI18n();

  return (
    <div className="phone-sticky-bar sticky top-0 z-10 mb-2 flex flex-wrap items-center gap-2 border-b border-divider bg-background py-2">
      <span className="text-sm text-default-600">
        {t("已选 {count} 个", { count })}
      </span>
      <div className="ml-auto flex flex-wrap justify-end gap-2">
        <Button size="sm" variant="flat" onPress={onToggleAll}>
          {allSelected ? t("取消全选") : t("全选")}
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
          {t("完成")}
        </Button>
      </div>
    </div>
  );
}

export type RowStatus = "ok" | "warn" | "bad" | "off";

const STATUS_DOT: Record<RowStatus, string> = {
  ok: "bg-success ring-[3px] ring-success/20",
  warn: "bg-warning ring-[3px] ring-warning/20",
  bad: "bg-danger ring-[3px] ring-danger/20",
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
      className={`list-row group flex w-full cursor-pointer items-center gap-3 border-b border-divider px-2.5 py-3 text-left transition-colors last:border-b-0 ${
        selected
          ? "bg-default-100"
          : "hover:bg-default-100/60 dark:hover:bg-default-100/30"
      }`}
      data-selected={selected ? "true" : undefined}
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
          className={`h-2 w-2 shrink-0 rounded-full ${STATUS_DOT[status]}`}
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
        <h2 className="list-group-title flex items-baseline gap-2 border-b border-divider px-2 pb-1.5 pt-5 text-xs font-medium text-default-500">
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
    <div className="phone-card phone-card-fill flex flex-col items-center gap-3 rounded-xl border border-dashed border-divider bg-default-50/40 px-6 py-12 text-center text-sm text-default-400 dark:bg-default-50/20">
      <p className="max-w-md leading-relaxed">{text}</p>
      {action && (
        <Button
          className="gap-1.5"
          color="primary"
          size="sm"
          startContent={action.icon}
          onPress={action.onPress}
        >
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

export interface ViewModeOption<T extends string> {
  key: T;
  label: string;
  icon?: React.ReactNode;
}

/** Compact segmented view switcher (e.g. workbench / cards / table / list). */
export function ViewModeSwitch<T extends string>({
  value,
  options,
  onChange,
  ariaLabel,
}: {
  value: T;
  options: Array<ViewModeOption<T>>;
  onChange: (next: T) => void;
  ariaLabel?: string;
}) {
  return (
    <div
      aria-label={ariaLabel}
      className="inline-flex h-8 shrink-0 items-center rounded-md border border-divider bg-default-50/70 p-0.5 dark:bg-default-100/40"
      role="group"
    >
      {options.map((opt) => {
        const active = value === opt.key;

        return (
          <button
            key={opt.key}
            aria-pressed={active}
            className={`inline-flex h-7 items-center gap-1 rounded-[5px] px-2 text-xs transition-colors ${
              active
                ? "bg-background font-medium text-foreground shadow-sm dark:bg-default-200"
                : "text-default-500 hover:text-foreground"
            }`}
            title={opt.label}
            type="button"
            onClick={() => onChange(opt.key)}
          >
            {opt.icon}
            <span className={opt.icon ? "hidden sm:inline" : ""}>
              {opt.label}
            </span>
          </button>
        );
      })}
    </div>
  );
}

/** Native compact select styled to match PageToolbar controls. */
export function ToolbarSelect({
  value,
  options,
  onChange,
  ariaLabel,
  className = "max-w-[9.5rem]",
}: {
  value: string;
  options: Array<{ value: string; label: string }>;
  onChange: (value: string) => void;
  ariaLabel: string;
  className?: string;
}) {
  return (
    <div className={`relative shrink-0 ${className}`}>
      <select
        aria-label={ariaLabel}
        className="h-8 w-full appearance-none truncate rounded-md border border-transparent bg-transparent pl-2.5 pr-7 text-xs text-default-600 outline-none transition-colors hover:bg-default-100 hover:text-foreground focus:border-divider"
        value={value}
        onChange={(e) => onChange(e.target.value)}
      >
        {options.map((opt) => (
          <option key={opt.value} value={opt.value}>
            {opt.label}
          </option>
        ))}
      </select>
      <ChevronDown className="pointer-events-none absolute right-2 top-1/2 h-3.5 w-3.5 -translate-y-1/2 text-default-400" />
    </div>
  );
}

export interface FilterPillItem<T extends string> {
  key: T;
  label: string;
  count?: number;
  tone?: "default" | "success" | "warning" | "danger";
}

const PILL_DOT: Record<NonNullable<FilterPillItem<string>["tone"]>, string> = {
  default: "bg-default-400",
  success: "bg-success",
  warning: "bg-warning",
  danger: "bg-danger",
};

/** Horizontal filter chip strip; wraps cleanly on desktop and scrolls on phones. */
export function FilterPills<T extends string>({
  value,
  items,
  onChange,
  ariaLabel,
  className = "mb-3",
}: {
  value: T;
  items: Array<FilterPillItem<T>>;
  onChange: (key: T) => void;
  ariaLabel?: string;
  className?: string;
}) {
  if (items.length === 0) return null;

  return (
    <nav
      aria-label={ariaLabel}
      className={`flex items-center gap-1.5 overflow-x-auto pb-0.5 scrollbar-hide sm:flex-wrap ${className}`}
    >
      {items.map((item) => {
        const active = value === item.key;

        return (
          <button
            key={item.key || "__all__"}
            aria-pressed={active}
            className={`inline-flex h-7 shrink-0 items-center gap-1.5 rounded-md border px-2.5 text-xs font-medium transition-colors ${
              active
                ? "border-foreground bg-foreground text-background dark:border-default-300 dark:bg-default-200 dark:text-foreground"
                : "border-divider bg-content1/70 text-default-600 hover:bg-default-100 hover:text-foreground"
            }`}
            type="button"
            onClick={() => onChange(item.key)}
          >
            {item.tone && item.tone !== "default" && (
              <span
                className={`h-1.5 w-1.5 shrink-0 rounded-full ${
                  active
                    ? item.tone === "success"
                      ? "bg-background dark:bg-success"
                      : item.tone === "warning"
                        ? "bg-background dark:bg-warning"
                        : "bg-background dark:bg-danger"
                    : PILL_DOT[item.tone]
                }`}
              />
            )}
            <span className="truncate">{item.label}</span>
            {item.count !== undefined && (
              <span className="tabular-nums opacity-65">{item.count}</span>
            )}
          </button>
        );
      })}
    </nav>
  );
}

export interface SummaryStatItem {
  key: string;
  label: string;
  value: React.ReactNode;
  sub?: React.ReactNode;
  tone?: "default" | "success" | "warning" | "danger";
  active?: boolean;
  onPress?: () => void;
}

const STAT_TONE: Record<NonNullable<SummaryStatItem["tone"]>, string> = {
  default: "text-foreground",
  success: "text-success",
  warning: "text-warning-600 dark:text-warning",
  danger: "text-danger",
};

/** Compact KPI summary strip above lists/grids; items can act as quick filters. */
export function SummaryStrip({
  items,
  className = "mb-3",
}: {
  items: SummaryStatItem[];
  className?: string;
}) {
  return (
    <div
      className={`grid grid-cols-2 gap-2 sm:gap-2.5 lg:grid-cols-4 ${className}`}
    >
      {items.map((item) => {
        const interactive = Boolean(item.onPress);
        const cls = `phone-card phone-card-fill rounded-xl border px-3.5 py-2.5 text-left transition-colors ${
          item.active
            ? "border-foreground bg-default-100/80 dark:border-default-300 dark:bg-default-100/50"
            : "border-divider bg-content1"
        } ${
          interactive
            ? "cursor-pointer hover:border-default-400"
            : "cursor-default"
        }`;

        const inner = (
          <>
            <div className="truncate text-xs text-default-500">
              {item.label}
            </div>
            <div
              className={`mt-0.5 truncate text-base font-semibold tabular-nums ${
                STAT_TONE[item.tone ?? "default"]
              }`}
            >
              {item.value}
            </div>
            {item.sub && (
              <div className="mt-0.5 truncate text-[11px] tabular-nums text-default-400">
                {item.sub}
              </div>
            )}
          </>
        );

        if (interactive) {
          return (
            <button
              key={item.key}
              aria-pressed={item.active}
              className={cls}
              type="button"
              onClick={item.onPress}
            >
              {inner}
            </button>
          );
        }

        return (
          <div key={item.key} className={cls}>
            {inner}
          </div>
        );
      })}
    </div>
  );
}

export interface WorkbenchNavItem {
  key: string;
  label: string;
  count: number;
  meta?: string;
  status?: RowStatus;
}

/**
 * Responsive master-detail workbench: sticky group sidebar on lg+ screens,
 * horizontal scrollable pill bar on smaller screens.
 */
export function WorkbenchLayout({
  navTitle,
  navSummary,
  items,
  value,
  onChange,
  allLabel,
  allCount,
  children,
}: {
  navTitle: string;
  navSummary?: string;
  items: WorkbenchNavItem[];
  value: string;
  onChange: (key: string) => void;
  allLabel: string;
  allCount: number;
  children: React.ReactNode;
}) {
  return (
    <div className="flex flex-col gap-3 lg:flex-row lg:items-start lg:gap-4">
      {/* Mobile / tablet horizontal rail */}
      <nav
        aria-label={navTitle}
        className="flex items-center gap-1.5 overflow-x-auto pb-0.5 scrollbar-hide lg:hidden"
      >
        <button
          aria-pressed={value === ""}
          className={`inline-flex h-7 shrink-0 items-center gap-1.5 rounded-md border px-2.5 text-xs font-medium transition-colors ${
            value === ""
              ? "border-foreground bg-foreground text-background dark:border-default-300 dark:bg-default-200 dark:text-foreground"
              : "border-divider bg-content1 text-default-600"
          }`}
          type="button"
          onClick={() => onChange("")}
        >
          <span>{allLabel}</span>
          <span className="tabular-nums opacity-70">{allCount}</span>
        </button>
        {items.map((item) => {
          const active = value === item.key;

          return (
            <button
              key={item.key}
              aria-pressed={active}
              className={`inline-flex h-7 shrink-0 items-center gap-1.5 rounded-md border px-2.5 text-xs font-medium transition-colors ${
                active
                  ? "border-foreground bg-foreground text-background dark:border-default-300 dark:bg-default-200 dark:text-foreground"
                  : "border-divider bg-content1 text-default-600"
              }`}
              type="button"
              onClick={() => onChange(item.key)}
            >
              {item.status && (
                <span
                  className={`h-1.5 w-1.5 shrink-0 rounded-full ${
                    active
                      ? item.status === "bad"
                        ? "bg-background dark:bg-danger"
                        : item.status === "warn"
                          ? "bg-background dark:bg-warning"
                          : item.status === "ok"
                            ? "bg-background dark:bg-success"
                            : "bg-background dark:bg-default-500"
                      : STATUS_DOT[item.status]
                  }`}
                />
              )}
              <span className="max-w-[10rem] truncate">{item.label}</span>
              <span className="tabular-nums opacity-70">{item.count}</span>
            </button>
          );
        })}
      </nav>

      {/* Desktop sticky sidebar */}
      <aside
        aria-label={navTitle}
        className="hidden w-56 shrink-0 rounded-xl border border-divider bg-content1 p-2 lg:sticky lg:top-4 lg:block xl:w-60"
      >
        <div className="mb-1 flex items-center justify-between px-2 py-1 text-xs font-medium text-default-500">
          <span className="truncate">{navTitle}</span>
          {navSummary && (
            <span className="shrink-0 tabular-nums text-default-400">
              {navSummary}
            </span>
          )}
        </div>
        <div className="desktop-scrollbar max-h-[calc(100dvh-14rem)] space-y-0.5 overflow-y-auto pr-0.5">
          <button
            aria-pressed={value === ""}
            className={`flex w-full items-center justify-between gap-2 rounded-lg px-2.5 py-2 text-left text-xs transition-colors ${
              value === ""
                ? "bg-foreground font-medium text-background dark:bg-default-200 dark:text-foreground"
                : "text-default-700 hover:bg-default-100"
            }`}
            type="button"
            onClick={() => onChange("")}
          >
            <span className="truncate">{allLabel}</span>
            <span className="shrink-0 tabular-nums opacity-75">{allCount}</span>
          </button>
          {items.map((item) => {
            const active = value === item.key;

            return (
              <button
                key={item.key}
                aria-pressed={active}
                className={`flex w-full items-center justify-between gap-2 rounded-lg px-2.5 py-2 text-left text-xs transition-colors ${
                  active
                    ? "bg-foreground font-medium text-background dark:bg-default-200 dark:text-foreground"
                    : "text-default-700 hover:bg-default-100"
                }`}
                title={item.label}
                type="button"
                onClick={() => onChange(item.key)}
              >
                <span className="flex min-w-0 items-center gap-1.5">
                  {item.status && (
                    <span
                      className={`h-1.5 w-1.5 shrink-0 rounded-full ${
                        active
                          ? item.status === "bad"
                            ? "bg-background dark:bg-danger"
                            : item.status === "warn"
                              ? "bg-background dark:bg-warning"
                              : item.status === "ok"
                                ? "bg-background dark:bg-success"
                                : "bg-background dark:bg-default-500"
                          : STATUS_DOT[item.status]
                      }`}
                    />
                  )}
                  <span className="truncate">{item.label}</span>
                </span>
                <span className="shrink-0 tabular-nums opacity-75">
                  {item.meta ? `${item.count} · ${item.meta}` : item.count}
                </span>
              </button>
            );
          })}
        </div>
      </aside>

      <div className="min-w-0 flex-1">{children}</div>
    </div>
  );
}
