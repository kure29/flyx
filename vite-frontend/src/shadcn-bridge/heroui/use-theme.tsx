/**
 * useTheme — backwards-compatible hook
 * =====================================
 * Wraps the new theme system's context to provide the same API that the
 * rest of the codebase already expects: `{ theme, setTheme }`.
 *
 * For full theme system access, use `useThemeContext` from "@/themes".
 */

import { useSyncExternalStore, useCallback } from "react";

import {
  subscribe,
  getSavedMode,
  getEffectiveMode,
  saveMode,
  reapplyActiveTheme,
  type ThemeMode,
} from "@/themes/registry";

// Monotonic counter for snapshot identity
let _rev = 0;
const _sub = (cb: () => void) =>
  subscribe(() => {
    _rev++;
    cb();
  });
const _snap = () => _rev;

export function useTheme() {
  useSyncExternalStore(_sub, _snap);

  const theme = getEffectiveMode();
  const mode = getSavedMode();

  const setTheme = useCallback((next: string) => {
    if (next !== "dark" && next !== "light" && next !== "system") return;
    saveMode(next as ThemeMode);
    reapplyActiveTheme();
  }, []);

  return { theme, mode, setTheme };
}
