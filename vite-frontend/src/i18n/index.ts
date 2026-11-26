import { useCallback, useSyncExternalStore } from "react";

import { EN_DICT } from "./en";

export type Locale = "zh-CN" | "en-US";

export const LOCALE_STORAGE_KEY = "flyx_locale";

type Listener = () => void;

const listeners = new Set<Listener>();

function normalizeLocale(raw: string | null | undefined): Locale {
  if (raw === "en-US" || raw === "en") return "en-US";

  return "zh-CN";
}

function readStoredLocale(): Locale {
  if (typeof window === "undefined") return "zh-CN";
  try {
    return normalizeLocale(window.localStorage.getItem(LOCALE_STORAGE_KEY));
  } catch {
    return "zh-CN";
  }
}

let currentLocale: Locale = readStoredLocale();

function syncDocumentLang(locale: Locale) {
  if (typeof document !== "undefined") {
    document.documentElement.lang = locale;
  }
}

syncDocumentLang(currentLocale);

if (typeof window !== "undefined") {
  window.addEventListener("storage", (e) => {
    if (e.key === LOCALE_STORAGE_KEY) {
      const next = normalizeLocale(e.newValue);

      if (next !== currentLocale) {
        currentLocale = next;
        syncDocumentLang(next);
        listeners.forEach((l) => l());
      }
    }
  });
}

function subscribe(listener: Listener): () => void {
  listeners.add(listener);

  return () => {
    listeners.delete(listener);
  };
}

export function getLocale(): Locale {
  return currentLocale;
}

export function setLocale(locale: Locale): void {
  const next = normalizeLocale(locale);

  if (next === currentLocale) return;
  currentLocale = next;
  syncDocumentLang(next);
  try {
    window.localStorage.setItem(LOCALE_STORAGE_KEY, next);
  } catch {
    // ignore storage errors (private mode, etc.)
  }
  listeners.forEach((l) => l());
}

export function toggleLocale(): Locale {
  const next: Locale = currentLocale === "zh-CN" ? "en-US" : "zh-CN";

  setLocale(next);

  return next;
}

function interpolate(
  template: string,
  vars?: Record<string, string | number>,
): string {
  if (!vars) return template;

  return template.replace(/\{(\w+)\}/g, (_, k: string) =>
    k in vars ? String(vars[k]) : `{${k}}`,
  );
}

/**
 * Translate a Chinese UI string into the active locale.
 * Supports `{var}` placeholder substitution.
 */
export function t(
  zh: string,
  vars?: Record<string, string | number>,
  locale: Locale = currentLocale,
): string {
  const raw = locale === "en-US" ? (EN_DICT[zh] ?? zh) : zh;

  return interpolate(raw, vars);
}

export function useI18n() {
  const locale = useSyncExternalStore<Locale>(
    subscribe,
    getLocale,
    () => "zh-CN",
  );
  const translate = useCallback(
    (zh: string, vars?: Record<string, string | number>) => t(zh, vars, locale),
    [locale],
  );

  return {
    locale,
    isEn: locale === "en-US",
    setLocale,
    toggleLocale,
    t: translate,
  };
}
