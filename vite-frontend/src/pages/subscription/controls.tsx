import type { ReactNode } from "react";

import { useState } from "react";
import { ChevronDown } from "lucide-react";

import { Switch } from "@/shadcn-bridge/heroui/switch";

/** Small controls shared by the subscription editors. */

/**
 * A choice between a few options: bordered buttons, the picked one
 * outlined in the primary colour, so it reads on a white page too.
 */
export function Segmented<T extends string>({
  value,
  options,
  onChange,
  columns,
}: {
  value: T;
  options: Array<[T, string]>;
  onChange: (v: T) => void;
  columns?: number;
}) {
  return (
    <div
      className="grid gap-2"
      role="radiogroup"
      style={{
        gridTemplateColumns: `repeat(${columns ?? options.length}, minmax(0, 1fr))`,
      }}
    >
      {options.map(([k, label]) => (
        <button
          key={k}
          aria-checked={value === k}
          className={`rounded-xl border px-2 py-2 text-sm transition-colors ${
            value === k
              ? "border-primary bg-primary-50 font-medium text-primary dark:bg-primary-900/30"
              : "border-default-200 bg-content1 text-default-600 hover:border-default-400 dark:border-default-100"
          }`}
          role="radio"
          type="button"
          onClick={() => onChange(k)}
        >
          {label}
        </button>
      ))}
    </div>
  );
}

/**
 * The editor's page tabs: a rounded segmented bar whose tabs share the width
 * equally. It never scrolls, so on a phone it can't be dragged around and
 * every page stays one tap away; keep labels short (two characters).
 */
export function EditorTabs<T extends string>({
  value,
  tabs,
  onChange,
}: {
  value: T;
  tabs: Array<{ key: T; label: string; badge?: string | number }>;
  onChange: (v: T) => void;
}) {
  return (
    <div
      className="grid gap-1 rounded-full bg-default-100/90 p-1 backdrop-blur-md"
      role="tablist"
      style={{ gridTemplateColumns: `repeat(${tabs.length}, minmax(0, 1fr))` }}
    >
      {tabs.map((t) => (
        <button
          key={t.key}
          aria-selected={value === t.key}
          className={`relative whitespace-nowrap rounded-full px-1 py-1.5 text-sm transition-colors ${
            value === t.key
              ? "bg-content1 font-medium text-primary shadow-sm"
              : "text-default-500 hover:text-foreground"
          }`}
          role="tab"
          type="button"
          onClick={() => onChange(t.key)}
        >
          {t.label}
          {t.badge !== undefined && t.badge !== "" && (
            <span
              className={`absolute right-0.5 top-0.5 rounded-full px-1 text-[10px] leading-4 ${
                value === t.key
                  ? "bg-primary text-primary-foreground"
                  : "bg-default-300 text-default-700"
              }`}
            >
              {t.badge}
            </span>
          )}
        </button>
      ))}
    </div>
  );
}

/**
 * A text field followed by three equal quick-pick buttons. The buttons take
 * the same fixed width in every row, so stacked rows line up.
 */
export function QuickPickRow({
  children,
  picks,
}: {
  children: ReactNode;
  picks: Array<{ label: string; onPick: () => void }>;
}) {
  return (
    <div className="flex gap-2">
      <div className="relative min-w-0 flex-1">{children}</div>
      <div className="grid w-40 shrink-0 grid-cols-3 gap-1.5">
        {picks.map((p) => (
          <button
            key={p.label}
            className="h-10 rounded-lg bg-default-100 text-sm text-default-700 transition-colors hover:bg-default-200"
            type="button"
            onClick={p.onPick}
          >
            {p.label}
          </button>
        ))}
      </div>
    </div>
  );
}

export function ToggleRow({
  id,
  label,
  hint,
  value,
  disabled,
  onChange,
}: {
  id: string;
  label: string;
  hint?: string;
  value: boolean;
  disabled?: boolean;
  onChange: (v: boolean) => void;
}) {
  return (
    <div className="flex items-start gap-3">
      <Switch
        aria-labelledby={`${id}-label`}
        id={id}
        isDisabled={disabled}
        isSelected={value}
        size="sm"
        onValueChange={onChange}
      />
      <label
        className="flex cursor-pointer flex-col"
        htmlFor={id}
        id={`${id}-label`}
      >
        <span className="text-sm">{label}</span>
        {hint && <span className="text-xs text-default-500">{hint}</span>}
      </label>
    </div>
  );
}

export function ChipPicker({
  options,
  selected,
  onChange,
  emptyText,
}: {
  options: Array<[string, string]>;
  selected: string[];
  onChange: (v: string[]) => void;
  emptyText: string;
}) {
  return (
    <div className="flex flex-wrap gap-1.5">
      <button
        className={`rounded-full border px-2.5 py-1 text-xs ${
          selected.length === 0
            ? "border-primary bg-primary-50 text-primary dark:bg-primary-900/30"
            : "border-divider text-default-500"
        }`}
        type="button"
        onClick={() => onChange([])}
      >
        {emptyText}
      </button>
      {options.map(([value, label]) => {
        const on = selected.includes(value);

        return (
          <button
            key={value}
            className={`rounded-full border px-2.5 py-1 text-xs ${
              on
                ? "border-primary bg-primary-50 text-primary dark:bg-primary-900/30"
                : "border-divider text-default-600"
            }`}
            type="button"
            onClick={() =>
              onChange(
                on ? selected.filter((x) => x !== value) : [...selected, value],
              )
            }
          >
            {label}
          </button>
        );
      })}
    </div>
  );
}

export function Section({
  title,
  hint,
  summary,
  collapsible,
  defaultOpen = true,
  children,
}: {
  title: string;
  hint?: string;
  /** Shown next to the title (while collapsed, for a collapsible one). */
  summary?: string;
  collapsible?: boolean;
  defaultOpen?: boolean;
  children: ReactNode;
}) {
  const [open, setOpen] = useState(defaultOpen);
  const shown = !collapsible || open;

  return (
    <section className="space-y-3 border-t border-divider pt-4 first:border-t-0 first:pt-0">
      {collapsible ? (
        <button
          aria-expanded={open}
          className="flex w-full items-start gap-2 text-left"
          type="button"
          onClick={() => setOpen((v) => !v)}
        >
          <div className="min-w-0 flex-1">
            <h3 className="text-sm font-semibold">
              {title}
              {!open && summary && (
                <span className="ml-2 text-xs font-normal text-primary">
                  {summary}
                </span>
              )}
            </h3>
            {open && hint && <p className="text-xs text-default-500">{hint}</p>}
          </div>
          <ChevronDown
            className={`mt-0.5 h-4 w-4 shrink-0 text-default-400 transition-transform ${open ? "rotate-180" : ""}`}
          />
        </button>
      ) : (
        <div>
          <h3 className="text-sm font-semibold">
            {title}
            {summary && (
              <span className="ml-2 text-xs font-normal text-primary">
                {summary}
              </span>
            )}
          </h3>
          {hint && <p className="text-xs text-default-500">{hint}</p>}
        </div>
      )}
      {shown && children}
    </section>
  );
}
