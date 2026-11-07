import type * as React from "react";

import { createContext, useContext } from "react";
import { ChevronLeft, ChevronRight } from "lucide-react";

/**
 * How the settings page shows its groups. "split" (desktop): a nav on the
 * left, the active group on the right. "stack" (phone): the list of groups,
 * or the one that was opened, with a way back.
 */
export interface ConfigLayout {
  mode: "split" | "stack";
  active: string | null;
  select: (id: string | null) => void;
}

export const ConfigLayoutContext = createContext<ConfigLayout>({
  mode: "split",
  active: null,
  select: () => {},
});

/** One group of settings: title, its current values, then its fields. */
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
  const { mode, active, select } = useContext(ConfigLayoutContext);
  const unsaved = changed && (
    <span className="text-xs font-normal text-warning-600">未保存</span>
  );

  // Phone, nothing opened: one row per group.
  if (mode === "stack" && active === null) {
    return (
      <button
        className="flex w-full items-center gap-3 border-b border-divider px-1 py-3.5 text-left transition-colors hover:bg-default-50"
        id={`config-${id}`}
        type="button"
        onClick={() => select(id)}
      >
        <div className="min-w-0 flex-1">
          <h2 className="flex items-baseline gap-2 text-sm font-medium">
            {title}
            {unsaved}
          </h2>
          {summary && (
            <p className="mt-0.5 truncate text-xs text-default-400">
              {summary}
            </p>
          )}
        </div>
        <ChevronRight className="h-4 w-4 shrink-0 text-default-300" />
      </button>
    );
  }
  if (active !== id) return null;

  return (
    <section className="scroll-mt-4" id={`config-${id}`}>
      {mode === "stack" && (
        <button
          className="mb-3 flex items-center gap-1 text-sm text-default-500 hover:text-foreground"
          type="button"
          onClick={() => select(null)}
        >
          <ChevronLeft className="h-4 w-4" />
          返回
        </button>
      )}
      {mode === "stack" ? (
        <div className="border-b border-divider pb-3">
          <h2 className="flex items-baseline gap-2 text-base font-semibold">
            {title}
            {unsaved}
          </h2>
          {summary && (
            <p className="mt-0.5 text-xs text-default-400">{summary}</p>
          )}
        </div>
      ) : (
        // The tab names the group; only its current values go on top.
        summary && <p className="text-xs text-default-400">{summary}</p>
      )}
      <div className="space-y-4 pt-5">{children}</div>
    </section>
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
      <div className="flex items-center justify-between gap-3">
        <div className="min-w-0">
          <p className="text-sm">{label}</p>
          {help && <p className="text-xs text-default-400">{help}</p>}
        </div>
        <div className="shrink-0">{children}</div>
      </div>
    );
  }

  return (
    <div className="space-y-1.5">
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
