// Shared pieces of the built-in status theme: formatting, colours and the
// small shadcn/ui-style building blocks (card, badge, segmented tabs, page
// shell) both pages use.
import type { PublicStatus, PublicStatusNode } from "@/api";

import { useEffect } from "react";
import { Moon, Sun } from "lucide-react";

import { BrandLogo } from "@/components/brand-logo";
import { useTheme } from "@/shadcn-bridge/heroui/use-theme";
import { isLoggedIn } from "@/utils/auth";

// ---------------------------------------------------------------- formatting

const UNITS = ["B", "KB", "MB", "GB", "TB", "PB"];

/** Splits a byte count into a number and its unit, e.g. [3.42, "GB"]. */
export function splitBytes(bytes: number | undefined): [string, string] {
  let v = bytes && bytes > 0 ? bytes : 0;
  let u = 0;

  while (v >= 1024 && u < UNITS.length - 1) {
    v /= 1024;
    u++;
  }

  return [
    u === 0 ? String(Math.round(v)) : v.toFixed(v >= 100 ? 0 : 2),
    UNITS[u],
  ];
}

export const bytesText = (bytes: number | undefined) =>
  splitBytes(bytes).join(" ");
export const speedText = (bps: number | undefined) => `${bytesText(bps)}/s`;

/** "1.2 GB / 4 GB", or undefined when the agent does not report it. */
export const capacity = (used?: number, total?: number) =>
  total ? `${bytesText(used)} / ${bytesText(total)}` : undefined;

export const pct = (v: number | null | undefined, digits = 2) =>
  v === null || v === undefined ? "—" : `${v.toFixed(v >= 100 ? 0 : digits)}%`;

/** The regional-indicator flag a name starts with, as [flag, code, rest]. */
export function splitFlag(name: string): [string, string, string] {
  const m = name.match(/^\s*([\u{1F1E6}-\u{1F1FF}]{2})\s*/u);

  if (!m) return ["", "", name];
  const code = [...m[1]]
    .map((c) => String.fromCharCode((c.codePointAt(0) ?? 0) - 0x1f1e6 + 65))
    .join("");

  return [m[1], code, name.slice(m[0].length) || name];
}

/**
 * The node's flag as [flag, code, name]: the flag its name starts with, else
 * the region the panel reports (set by the admin or detected from its address).
 */
export function nodeFlag(node: {
  name: string;
  region?: string;
}): [string, string, string] {
  const split = splitFlag(node.name);
  const code = (node.region ?? "").toUpperCase();

  if (split[0] || !/^[A-Z]{2}$/.test(code)) return split;
  const flag = String.fromCodePoint(
    ...[...code].map((c) => 0x1f1e6 + c.charCodeAt(0) - 65),
  );

  return [flag, code, node.name];
}

export const daysUntil = (date: string) => {
  const end = new Date(`${date}T23:59:59`).getTime();

  return Math.ceil((end - Date.now()) / 86_400_000);
};

/** "23 天" / "5 小时" / "12 分钟": how long a server has been up. */
export function uptimeShort(seconds: number | undefined): string {
  if (!seconds || seconds < 0) return "";
  if (seconds >= 86_400) return `${Math.floor(seconds / 86_400)} 天`;
  if (seconds >= 3_600) return `${Math.floor(seconds / 3_600)} 小时`;

  return `${Math.max(1, Math.floor(seconds / 60))} 分钟`;
}

/** The node's page under this theme: /status/node/<position>. */
export const nodeHref = (id: number) => `${import.meta.env.BASE_URL}node/${id}`;

/** "Debian 12 · x86_64 · kvm", when the admin shows system information. */
export function hostLine(node: PublicStatusNode): string {
  const os = (node.os ?? "")
    .replace(/\s*\(.*\)\s*$/, "")
    .replace(/\s*GNU\/Linux/, "")
    .replace(/\s+LTS$/, "");
  const virt = node.virtualization === "none" ? "" : node.virtualization;

  return [os, node.arch, virt].filter(Boolean).join(" · ");
}

// ---------------------------------------------------------------- colours

// Minutes down a day may have and still count as a good day: a restart or
// a short network blip is not an outage.
export const GOOD_DAY_DOWN_MIN = 5;

export const dayClass = (down: number | null) =>
  down === null
    ? "bg-default-200 dark:bg-default-200"
    : down <= GOOD_DAY_DOWN_MIN
      ? "bg-emerald-500"
      : down <= 30
        ? "bg-amber-500"
        : "bg-red-500";

/** Plain text while fine; colour only when something needs a look. */
export const usageText = (v: number) =>
  v >= 90
    ? "text-red-600 dark:text-red-400"
    : v >= 80
      ? "text-amber-600 dark:text-amber-400"
      : "";

export const usageBar = (v: number) =>
  v >= 90 ? "bg-red-500" : v >= 80 ? "bg-amber-500" : "bg-primary";

export const latencyText = (ms: number | null) =>
  ms === null
    ? "text-red-600 dark:text-red-400"
    : ms >= 200
      ? "text-red-600 dark:text-red-400"
      : ms >= 120
        ? "text-amber-600 dark:text-amber-400"
        : "";

export const lossText = (loss: number) =>
  loss >= 10
    ? "text-red-600 dark:text-red-400"
    : loss >= 1
      ? "text-amber-600 dark:text-amber-400"
      : "text-default-500";

export const availabilityText = (v: number | null | undefined) =>
  v === null || v === undefined
    ? "text-default-500"
    : v >= 99.5
      ? ""
      : v >= 95
        ? "text-amber-600 dark:text-amber-400"
        : "text-red-600 dark:text-red-400";

export const expiryText = (days: number) =>
  days < 7
    ? "text-red-600 dark:text-red-400"
    : days < 30
      ? "text-amber-600 dark:text-amber-400"
      : "";

// ---------------------------------------------------------------- building blocks

export const cardClass =
  "rounded-xl border border-border bg-background text-foreground shadow-xs dark:bg-content1";

export function Card({
  className = "",
  children,
}: {
  className?: string;
  children: React.ReactNode;
}) {
  return <div className={`${cardClass} ${className}`}>{children}</div>;
}

export function Badge({
  tone = "ok",
  children,
  title,
}: {
  tone?: "ok" | "bad" | "warn" | "muted";
  children: React.ReactNode;
  title?: string;
}) {
  const dot =
    tone === "ok"
      ? "bg-emerald-500"
      : tone === "bad"
        ? "bg-red-500"
        : tone === "warn"
          ? "bg-amber-500"
          : "";

  return (
    <span
      className={`inline-flex shrink-0 items-center gap-1.5 whitespace-nowrap rounded-md border px-2 py-0.5 text-xs font-medium ${
        tone === "muted"
          ? "border-transparent bg-default-100 text-default-700"
          : tone === "bad"
            ? "border-border text-red-600 dark:text-red-400"
            : "border-border"
      }`}
      title={title}
    >
      {dot && <span className={`h-1.5 w-1.5 rounded-full ${dot}`} />}
      {children}
    </span>
  );
}

/** A small rounded label for a return route line, e.g. "CN2 GIA". */
export function RouteTag({ route, v6 }: { route?: string; v6?: string }) {
  if (!route && !v6) return null;

  return (
    <span className="inline-flex min-w-0 items-center gap-1">
      {route && (
        <span className="truncate rounded bg-default-100 px-1.5 py-px text-[11px] font-medium text-default-700">
          {route}
        </span>
      )}
      {v6 && (
        <span
          className="truncate rounded border border-border px-1.5 py-px text-[11px] text-default-600"
          title="IPv6 回程"
        >
          v6 {v6}
        </span>
      )}
    </span>
  );
}

export function Segmented<T extends string>({
  value,
  options,
  onChange,
  label,
}: {
  value: T;
  options: Array<{ key: T; label: React.ReactNode }>;
  onChange: (v: T) => void;
  label: string;
}) {
  return (
    <div
      aria-label={label}
      className="inline-flex shrink-0 rounded-lg bg-default-100 p-[3px]"
      role="tablist"
    >
      {options.map((o) => (
        <button
          key={o.key}
          aria-selected={value === o.key}
          className={`inline-flex items-center gap-1.5 rounded-md px-3 py-1 text-[13px] font-medium transition-colors ${
            value === o.key
              ? "bg-background text-foreground shadow-sm dark:border dark:border-border"
              : "text-default-500 hover:text-foreground"
          }`}
          role="tab"
          type="button"
          onClick={() => onChange(o.key)}
        >
          {o.label}
        </button>
      ))}
    </div>
  );
}

export function Bar({ value }: { value: number }) {
  return (
    <div className="h-1.5 overflow-hidden rounded-full bg-default-100">
      <div
        className={`h-full rounded-full ${usageBar(value)}`}
        style={{ width: `${Math.min(Math.max(value, 0), 100)}%` }}
      />
    </div>
  );
}

const buttonBase =
  "inline-flex h-8 shrink-0 items-center justify-center gap-1.5 rounded-lg border border-border bg-background text-[13px] font-medium shadow-xs transition-colors hover:bg-default-100";
const buttonClass = `${buttonBase} px-3`;

function ThemeToggle() {
  const { theme, setTheme } = useTheme();
  const dark = theme === "dark";

  return (
    <button
      aria-label={dark ? "切换到浅色" : "切换到深色"}
      className={`${buttonBase} w-8`}
      title={dark ? "浅色" : "深色"}
      type="button"
      onClick={() => setTheme(dark ? "light" : "dark")}
    >
      {dark ? <Sun size={15} /> : <Moon size={15} />}
    </button>
  );
}

/** The top bar and the page column both pages sit in. */
export function Shell({
  title,
  children,
}: {
  title: string;
  children: React.ReactNode;
}) {
  const home = import.meta.env.BASE_URL;

  return (
    <div className="min-h-screen bg-background text-foreground">
      <header className="sticky top-0 z-10 border-b border-border bg-background/90 backdrop-blur">
        <div className="mx-auto flex h-14 max-w-[1400px] items-center gap-3 px-4 sm:px-8">
          <a className="flex min-w-0 items-center gap-2" href={home}>
            <BrandLogo className="h-6 w-6 shrink-0" size={24} />
            <span className="truncate font-semibold tracking-tight">
              {title}
            </span>
          </a>
          <div className="flex-1" />
          <ThemeToggle />
          <a className={buttonClass} href={isLoggedIn() ? "/dashboard" : "/"}>
            {isLoggedIn() ? "进入面板" : "登录"}
          </a>
        </div>
      </header>
      <main className="mx-auto flex max-w-[1400px] flex-col gap-4 px-4 py-6 sm:px-8 sm:py-8">
        {children}
      </main>
    </div>
  );
}

export function Notice({
  title,
  children,
  onRetry,
}: {
  title: string;
  children?: React.ReactNode;
  /** Shows a retry button. */
  onRetry?: () => void;
}) {
  return (
    <Card className="flex flex-col items-center gap-2 px-6 py-16 text-center">
      <span className="text-base font-medium">{title}</span>
      {children && <span className="text-sm text-default-500">{children}</span>}
      {onRetry && (
        <button
          className={`${buttonClass} mt-2`}
          type="button"
          onClick={onRetry}
        >
          重试
        </button>
      )}
    </Card>
  );
}

// ---------------------------------------------------------------- API

export interface Envelope<T> {
  code: number;
  msg: string;
  data: T;
}

export async function getJSON<T>(path: string): Promise<Envelope<T>> {
  const res = await fetch(path, { headers: { Accept: "application/json" } });

  if (!res.ok) throw new Error(`HTTP ${res.status}`);

  return res.json();
}

/** The public status API (flyxApi 1): no login, the panel's usual envelope. */
export const fetchStatus = () => getJSON<PublicStatus>("/api/v1/public/status");

/**
 * Runs load now, every ms while the page is visible, and at once when the
 * page becomes visible again (a background tab skips the timer, so its data
 * would otherwise stay stale until the next tick).
 */
export function usePolling(load: () => void, ms: number) {
  useEffect(() => {
    load();
    const timer = window.setInterval(() => {
      if (document.visibilityState === "visible") load();
    }, ms);
    const onVisible = () => {
      if (document.visibilityState === "visible") load();
    };

    document.addEventListener("visibilitychange", onVisible);

    return () => {
      window.clearInterval(timer);
      document.removeEventListener("visibilitychange", onVisible);
    };
  }, [load, ms]);
}
