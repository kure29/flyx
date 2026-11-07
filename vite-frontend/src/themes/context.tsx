/**
 * Theme Context + Provider (React integration)
 * =============================================
 * Wraps the registry in a React context so that the entire component tree
 * re-renders when the active theme changes.
 */

import type { ThemePackage, ComponentKey, LayoutKey, PageKey } from "./types";

import React, {
  createContext,
  useContext,
  useEffect,
  useSyncExternalStore,
  useCallback,
  useMemo,
} from "react";

import {
  subscribe,
  getActiveTheme,
  getActiveThemeId,
  getRegisteredThemes,
  activateTheme,
  deactivateTheme,
  reapplyActiveTheme,
  resolveComponent,
  resolveLayout,
  resolvePage,
  getSavedMode,
  saveMode,
  getEffectiveMode,
  registerTheme,
  unregisterTheme,
  type ThemeMode,
} from "./registry";

import { removeStoredValue } from "@/utils/identity-storage";

// ─── context value ───────────────────────────────────────────────────────────

interface ThemeContextValue {
  /** Currently active theme package (null = default). */
  activeTheme: ThemePackage | null;
  activeThemeId: string | null;
  /** All registered themes. */
  themes: ThemePackage[];
  /** Current mode preference. */
  mode: ThemeMode;
  /** Resolved effective mode (never "system"). */
  effectiveMode: "light" | "dark";

  /** Switch the active theme. */
  switchTheme: (id: string) => void;
  /** Reset to no custom theme (use defaults). */
  resetTheme: () => void;
  /** Change mode preference. */
  setMode: (mode: ThemeMode) => void;
  /** Register a new theme at runtime. */
  register: (pkg: ThemePackage) => void;
  /** Unregister a theme by id. */
  unregister: (id: string) => void;

  /** Resolve a component (returns themed override or fallback). */
  component: <P = any>(
    key: ComponentKey,
    fallback: React.ComponentType<P>,
  ) => React.ComponentType<P>;
  /** Resolve a layout. */
  layout: (
    key: LayoutKey,
    fallback: React.ComponentType<{ children: React.ReactNode }>,
  ) => React.ComponentType<{ children: React.ReactNode }>;
  /** Resolve a page. */
  page: <P = any>(
    key: PageKey,
    fallback: React.ComponentType<P>,
  ) => React.ComponentType<P>;
}

const ThemeContext = createContext<ThemeContextValue | null>(null);

// ─── snapshot for useSyncExternalStore ────────────────────────────────────────

// We use a monotonic counter to create new snapshot references when the
// registry notifies.
let snapshotCounter = 0;

function getSnapshot() {
  return snapshotCounter;
}
const originalSubscribe = (onStoreChange: () => void) => {
  const unsub = subscribe(() => {
    snapshotCounter++;
    onStoreChange();
  });

  return unsub;
};

// ─── provider ────────────────────────────────────────────────────────────────

interface ThemeProviderProps {
  children: React.ReactNode;
}

export const ThemeProvider: React.FC<ThemeProviderProps> = ({ children }) => {
  // Re-render whenever register changes
  useSyncExternalStore(originalSubscribe, getSnapshot);

  // Listen for system mode changes
  useEffect(() => {
    const mq = window.matchMedia("(prefers-color-scheme: dark)");
    const handler = () => reapplyActiveTheme();

    mq.addEventListener("change", handler);

    return () => mq.removeEventListener("change", handler);
  }, []);

  const switchTheme = useCallback((id: string) => activateTheme(id), []);
  const resetTheme = useCallback(() => {
    deactivateTheme();
    reapplyActiveTheme();
    removeStoredValue(localStorage, "flyx:active-theme");
  }, []);
  const setMode = useCallback((m: ThemeMode) => {
    saveMode(m);
    reapplyActiveTheme();
  }, []);
  const register = useCallback((pkg: ThemePackage) => registerTheme(pkg), []);
  const unregister = useCallback((id: string) => unregisterTheme(id), []);

  const value = useMemo<ThemeContextValue>(
    () => ({
      activeTheme: getActiveTheme(),
      activeThemeId: getActiveThemeId(),
      themes: getRegisteredThemes(),
      mode: getSavedMode(),
      effectiveMode: getEffectiveMode(),
      switchTheme,
      resetTheme,
      setMode,
      register,
      unregister,
      component: resolveComponent,
      layout: resolveLayout,
      page: resolvePage,
    }),
    [snapshotCounter, switchTheme, resetTheme, setMode, register, unregister],
  );

  return (
    <ThemeContext.Provider value={value}>{children}</ThemeContext.Provider>
  );
};

// ─── hooks ───────────────────────────────────────────────────────────────────

/** Access the full theme context. */
export function useThemeContext(): ThemeContextValue {
  const ctx = useContext(ThemeContext);

  if (!ctx)
    throw new Error("useThemeContext must be used within <ThemeProvider>");

  return ctx;
}

/**
 * Convenience: resolve a single themed component.
 *
 * ```tsx
 * import { useThemedComponent } from "@/themes/context";
 * import { Button as DefaultButton } from "@/shadcn-bridge/heroui/button";
 *
 * function MyPage() {
 *   const Button = useThemedComponent("Button", DefaultButton);
 *   return <Button color="primary">Click</Button>;
 * }
 * ```
 */
export function useThemedComponent<P = any>(
  key: ComponentKey,
  fallback: React.ComponentType<P>,
): React.ComponentType<P> {
  const ctx = useThemeContext();

  return ctx.component(key, fallback);
}

/** Convenience: resolve a themed layout. */
export function useThemedLayout(
  key: LayoutKey,
  fallback: React.ComponentType<{ children: React.ReactNode }>,
): React.ComponentType<{ children: React.ReactNode }> {
  const ctx = useThemeContext();

  return ctx.layout(key, fallback);
}

/** Convenience: resolve a themed page. */
export function useThemedPage<P = any>(
  key: PageKey,
  fallback: React.ComponentType<P>,
): React.ComponentType<P> {
  const ctx = useThemeContext();

  return ctx.page(key, fallback);
}
