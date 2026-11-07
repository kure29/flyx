/**
 * Default Theme
 * =============
 * This is the built-in "stock" FLYX theme.  It doesn't override any
 * components — it only declares the CSS tokens that match the colours
 * already defined in `globals.css`.  This serves as the **reference
 * implementation** that theme authors can copy and modify.
 *
 * When this theme is active, the frontend looks identical to an
 * unmodified FLYX install.
 */

import type { ThemePackage } from "../types";

const defaultTheme: ThemePackage = {
  id: "default",
  name: "默认主题",
  author: "FLYX Team",
  version: "1.0.0",
  description: "FLYX 内置默认极简黑白主题",

  tokens: {
    light: {
      "--background": "#ffffff",
      "--foreground": "#0a0a0a",
      "--border": "#e5e5e5",
      "--input": "#e5e5e5",
      "--ring": "#a3a3a3",
      "--content1": "#ffffff",
      "--divider": "#ededed",

      "--default-50": "#fafafa",
      "--default-100": "#f5f5f5",
      "--default-200": "#e5e5e5",
      "--default-300": "#d4d4d4",
      "--default-400": "#a3a3a3",
      "--default-500": "#737373",
      "--default-600": "#525252",
      "--default-700": "#404040",
      "--default-800": "#262626",
      "--default-900": "#171717",

      "--primary": "#0a0a0a",
      "--primary-foreground": "#ffffff",
      "--primary-50": "#fafafa",
      "--primary-100": "#f5f5f5",
      "--primary-200": "#e5e5e5",
      "--primary-300": "#d4d4d4",
      "--primary-400": "#737373",
      "--primary-500": "#262626",
      "--primary-600": "#0a0a0a",
      "--primary-700": "#0a0a0a",
      "--primary-800": "#0a0a0a",
      "--primary-900": "#0a0a0a",

      "--secondary": "#404040",
      "--secondary-foreground": "#ffffff",
      "--secondary-50": "#fafafa",
      "--secondary-100": "#f5f5f5",
      "--secondary-200": "#e5e5e5",
      "--secondary-300": "#d4d4d4",
      "--secondary-400": "#737373",
      "--secondary-500": "#262626",
      "--secondary-600": "#0a0a0a",
      "--secondary-700": "#0a0a0a",
      "--secondary-800": "#0a0a0a",
      "--secondary-900": "#0a0a0a",

      "--danger": "#dc2626",
      "--danger-50": "#fef2f2",
      "--danger-100": "#fee2e2",
      "--danger-200": "#fecaca",
      "--danger-300": "#fca5a5",
      "--danger-400": "#f87171",
      "--danger-500": "#ef4444",
      "--danger-600": "#dc2626",
      "--danger-700": "#b91c1c",
      "--danger-800": "#991b1b",
      "--danger-900": "#7f1d1d",

      "--success": "#16a34a",
      "--success-50": "#f0fdf4",
      "--success-100": "#dcfce7",
      "--success-200": "#bbf7d0",
      "--success-300": "#86efac",
      "--success-400": "#4ade80",
      "--success-500": "#22c55e",
      "--success-600": "#16a34a",
      "--success-700": "#15803d",
      "--success-800": "#166534",
      "--success-900": "#14532d",

      "--warning": "#d97706",
      "--warning-50": "#fffbeb",
      "--warning-100": "#fef3c7",
      "--warning-200": "#fde68a",
      "--warning-300": "#fcd34d",
      "--warning-400": "#fbbf24",
      "--warning-500": "#f59e0b",
      "--warning-600": "#d97706",
      "--warning-700": "#b45309",
      "--warning-800": "#92400e",
      "--warning-900": "#78350f",
    },

    dark: {
      "--background": "#0a0a0a",
      "--foreground": "#fafafa",
      "--border": "#262626",
      "--input": "#333333",
      "--ring": "#525252",
      "--content1": "#111111",
      "--divider": "#1f1f1f",

      "--default-50": "#171717",
      "--default-100": "#1f1f1f",
      "--default-200": "#262626",
      "--default-300": "#404040",
      "--default-400": "#525252",
      "--default-500": "#737373",
      "--default-600": "#a3a3a3",
      "--default-700": "#d4d4d4",
      "--default-800": "#e5e5e5",
      "--default-900": "#f5f5f5",

      "--primary": "#fafafa",
      "--secondary": "#d4d4d4",
      "--danger": "#ef4444",
      "--success": "#22c55e",
      "--warning": "#f59e0b",
      "--primary-foreground": "#0a0a0a",
      "--secondary-foreground": "#0a0a0a",
      "--primary-50": "#fafafa",
      "--primary-100": "#f5f5f5",
      "--primary-200": "#e5e5e5",
      "--primary-300": "#f5f5f5",
      "--primary-400": "#fafafa",
      "--primary-500": "#fafafa",
      "--primary-600": "#e5e5e5",
      "--primary-700": "#d4d4d4",
      "--primary-800": "#404040",
      "--primary-900": "#262626",
      "--secondary-50": "#fafafa",
      "--secondary-100": "#f5f5f5",
      "--secondary-200": "#e5e5e5",
      "--secondary-300": "#f5f5f5",
      "--secondary-400": "#fafafa",
      "--secondary-500": "#fafafa",
      "--secondary-600": "#e5e5e5",
      "--secondary-700": "#d4d4d4",
      "--secondary-800": "#404040",
      "--secondary-900": "#262626",
    },
  },

  // No component/layout/page overrides — uses all defaults.
};

export default defaultTheme;
