/**
 * @module @/themes
 * Public API for the FLYX theme system.
 *
 * Usage:
 *   import { ThemeProvider, useThemeContext, registerTheme } from "@/themes";
 */

export type {
  ThemePackage,
  ThemeTokens,
  ComponentKey,
  LayoutKey,
  PageKey,
} from "./types";

export {
  registerTheme,
  unregisterTheme,
  getRegisteredThemes,
  getTheme,
  getActiveThemeId,
  getActiveTheme,
  activateTheme,
  deactivateTheme,
  reapplyActiveTheme,
  initThemeSystem,
  resolveComponent,
  resolveLayout,
  resolvePage,
  getSavedMode,
  saveMode,
  getEffectiveMode,
  subscribe,
} from "./registry";

export type { ThemeMode } from "./registry";

export {
  ThemeProvider,
  useThemeContext,
  useThemedComponent,
  useThemedLayout,
  useThemedPage,
} from "./context";
