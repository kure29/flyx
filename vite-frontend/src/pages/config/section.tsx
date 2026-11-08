import type * as React from "react";

import { createContext, useContext } from "react";
import { ChevronLeft, ChevronRight, Search, X } from "lucide-react";

/**
 * How the settings page shows its groups. "split" (desktop): tabs on top,
 * the active group below. "stack" (phone): the list of groups, or the one
 * that was opened, with a way back.
 *
 * A group stays mounted (hidden) once it has been opened, so going back to
 * it only shows it again instead of building it anew.
 */
export interface ConfigLayout {
  mode: "split" | "stack";
  active: string | null;
  /** Groups opened at least once on this visit. */
  visited: ReadonlySet<string>;
  /** The phone's slide while a group opens ("push") or the list returns ("pop"). */
  transition: "push" | "pop" | null;
  onTransitionEnd: () => void;
  back: () => void;
}

export const ConfigLayoutContext = createContext<ConfigLayout>({
  mode: "split",
  active: null,
  visited: new Set(),
  transition: null,
  onTransitionEnd: () => {},
  back: () => {},
});

const Unsaved = () => (
  <span className="rounded-full bg-warning-50 px-1.5 py-px text-[11px] font-normal text-warning-700 dark:bg-warning-950/40 dark:text-warning-300">
    未保存
  </span>
);

/** A white rounded group on the phone's grey page (iOS-style inset list). */
export const groupClass =
  "overflow-hidden rounded-2xl bg-content1 dark:bg-default-50";

/** One group of settings: its fields, under a title on the phone. */
export function ConfigSection({
  id,
  title,
  summary,
  changed,
  children,
}: {
  id: string;
  title: string;
  /** Current values, shown under the title. */
  summary?: React.ReactNode;
  /** Has edits that are not saved yet. */
  changed?: boolean;
  children: React.ReactNode;
}) {
  const { mode, active, visited, transition, onTransitionEnd, back } =
    useContext(ConfigLayoutContext);
  const shown = active === id;

  if (!shown && !visited.has(id)) return null;

  if (mode === "split") {
    return (
      <section hidden={!shown} id={`config-${id}`}>
        {/* The tab names the group; only its current values go on top. */}
        {summary && <p className="text-xs text-default-400">{summary}</p>}
        <div className="space-y-4 pt-5">{children}</div>
      </section>
    );
  }

  return (
    <section
      className={shown && transition === "push" ? "config-pane-push" : ""}
      hidden={!shown}
      id={`config-${id}`}
      onAnimationEnd={onTransitionEnd}
    >
      <button
        className="-ml-1.5 mb-2 flex h-9 items-center gap-0.5 rounded-lg pl-0.5 pr-2.5 text-[15px] text-default-500 transition-colors active:bg-default-200/70"
        type="button"
        onClick={back}
      >
        <ChevronLeft className="h-5 w-5" />
        设置
      </button>
      <div className="mb-4 px-1">
        <h2 className="flex items-center gap-2 text-2xl font-bold tracking-tight">
          {title}
          {changed && <Unsaved />}
        </h2>
        {summary && (
          <p className="mt-1 text-[13px] text-default-500">{summary}</p>
        )}
      </div>
      <div className={`${groupClass} space-y-4 p-4`}>{children}</div>
    </section>
  );
}

export interface ConfigIndexItem {
  id: string;
  title: string;
  icon: React.ReactNode;
  summary?: React.ReactNode;
  changed?: boolean;
}

/** The phone's list of groups: icon, name, current values, chevron. */
export function ConfigIndex({
  items,
  onOpen,
}: {
  items: ConfigIndexItem[];
  onOpen: (id: string) => void;
}) {
  const { transition, onTransitionEnd } = useContext(ConfigLayoutContext);

  return (
    <nav
      aria-label="设置分组"
      className={`${groupClass} ${transition === "pop" ? "config-pane-pop" : ""}`}
      onAnimationEnd={onTransitionEnd}
    >
      {items.map((item) => (
        <button
          key={item.id}
          className="group flex w-full items-center gap-3.5 pl-4 text-left transition-colors active:bg-default-100 dark:active:bg-default-100"
          type="button"
          onClick={() => onOpen(item.id)}
        >
          <span className="flex h-10 w-10 shrink-0 items-center justify-center rounded-xl bg-default-100 text-foreground dark:bg-default-200 [&_svg]:h-5 [&_svg]:w-5">
            {item.icon}
          </span>
          {/* The divider starts after the icon, as in iOS lists. */}
          <span className="flex min-w-0 flex-1 items-center gap-2 border-b border-divider py-3.5 pr-3 group-last:border-b-0">
            <span className="min-w-0 flex-1">
              <span className="flex items-center gap-2 text-[15px] font-medium">
                {item.title}
                {item.changed && <Unsaved />}
              </span>
              {item.summary && (
                <span className="mt-0.5 block truncate text-[13px] text-default-500">
                  {item.summary}
                </span>
              )}
            </span>
            <ChevronRight className="h-4 w-4 shrink-0 text-default-400" />
          </span>
        </button>
      ))}
    </nav>
  );
}

/** The search box over the settings. */
export function ConfigSearchBox({
  value,
  onChange,
}: {
  value: string;
  onChange: (v: string) => void;
}) {
  return (
    <div className="relative">
      <Search className="pointer-events-none absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-default-400" />
      <input
        aria-label="搜索设置"
        className="h-10 w-full rounded-xl border border-divider bg-content1 pl-9 pr-9 text-[15px] outline-none placeholder:text-default-400 focus:border-default-400 dark:bg-default-50 sm:h-9 sm:text-sm [&::-webkit-search-cancel-button]:appearance-none"
        enterKeyHint="search"
        placeholder="搜索设置"
        type="search"
        value={value}
        onChange={(e) => onChange(e.target.value)}
      />
      {value && (
        <button
          aria-label="清除搜索"
          className="absolute right-2 top-1/2 flex h-6 w-6 -translate-y-1/2 items-center justify-center rounded-full text-default-400 active:bg-default-200"
          type="button"
          onClick={() => onChange("")}
        >
          <X className="h-4 w-4" />
        </button>
      )}
    </div>
  );
}

export interface ConfigSearchHit {
  section: string;
  sectionTitle: string;
  icon: React.ReactNode;
  label: string;
  help?: string;
}

/** What the search found: one row per setting, tapping opens it. */
export function ConfigSearchResults({
  hits,
  onPick,
}: {
  hits: ConfigSearchHit[];
  onPick: (hit: ConfigSearchHit) => void;
}) {
  if (hits.length === 0) {
    return (
      <p className="py-10 text-center text-sm text-default-500">
        没有找到相关设置
      </p>
    );
  }

  return (
    <div className={groupClass}>
      {hits.map((hit) => (
        <button
          key={`${hit.section}:${hit.label}`}
          className="group flex w-full items-center gap-3.5 pl-4 text-left transition-colors active:bg-default-100"
          type="button"
          onClick={() => onPick(hit)}
        >
          <span className="flex h-8 w-8 shrink-0 items-center justify-center rounded-lg bg-default-100 dark:bg-default-200 [&_svg]:h-4 [&_svg]:w-4">
            {hit.icon}
          </span>
          <span className="flex min-w-0 flex-1 items-center gap-2 border-b border-divider py-3 pr-3 group-last:border-b-0">
            <span className="min-w-0 flex-1">
              <span className="block truncate text-[15px] sm:text-sm">
                {hit.label}
              </span>
              <span className="mt-0.5 block truncate text-xs text-default-500">
                {hit.sectionTitle}
                {hit.help ? ` · ${hit.help}` : ""}
              </span>
            </span>
            <ChevronRight className="h-4 w-4 shrink-0 text-default-400" />
          </span>
        </button>
      ))}
    </div>
  );
}

/** Label above the control with a line of help; `inline` puts the control on the right (switches). */
export function Field({
  label,
  help,
  inline,
  children,
}: {
  label: React.ReactNode;
  help?: React.ReactNode;
  inline?: boolean;
  children: React.ReactNode;
}) {
  if (inline) {
    return (
      <div data-field className="flex items-center justify-between gap-3">
        <div className="min-w-0">
          <p className="text-sm">{label}</p>
          {help && <p className="text-xs text-default-400">{help}</p>}
        </div>
        <div className="shrink-0">{children}</div>
      </div>
    );
  }

  return (
    <div data-field className="space-y-1.5">
      <p className="text-sm">{label}</p>
      {children}
      {help && <p className="text-xs text-default-400">{help}</p>}
    </div>
  );
}

/** Small heading inside a section, for a group of fields. */
export function SubHeading({ children }: { children: React.ReactNode }) {
  return (
    <p className="border-t border-divider pt-3 text-xs font-semibold text-default-500 first:border-t-0 first:pt-0">
      {children}
    </p>
  );
}
