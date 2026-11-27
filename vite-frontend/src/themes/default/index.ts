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
      "--background": "#111214",
      "--foreground": "#d8dbe2",
      "--border": "#2a2d33",
      "--input": "#32363e",
      "--ring": "#525866",
      "--content1": "#181a1e",
      "--divider": "#25282e",

      "--default-50": "#1d2025",
      "--default-100": "#25282f",
      "--default-200": "#30343d",
      "--default-300": "#464c59",
      "--default-400": "#757c8a",
      "--default-500": "#9198a6",
      "--default-600": "#adb3bf",
      "--default-700": "#c4c9d4",
      "--default-800": "#d1d5de",
      "--default-900": "#dfe2e8",

      "--primary": "#b8bfcc",
      "--secondary": "#9ea5b3",
      "--danger": "#e5534b",
      "--success": "#3fb950",
      "--warning": "#d29922",
      "--primary-foreground": "#111214",
      "--secondary-foreground": "#111214",
      "--primary-50": "#24272e",
      "--primary-100": "#2d313a",
      "--primary-200": "#3a3f4a",
      "--primary-300": "#b8bfcc",
      "--primary-400": "#c4cad6",
      "--primary-500": "#b8bfcc",
      "--primary-600": "#a5adbc",
      "--primary-700": "#9199a8",
      "--primary-800": "#464c59",
      "--primary-900": "#2a2d33",
      "--secondary-50": "#24272e",
      "--secondary-100": "#2d313a",
      "--secondary-200": "#3a3f4a",
      "--secondary-300": "#9ea5b3",
      "--secondary-400": "#adb4c1",
      "--secondary-500": "#9ea5b3",
      "--secondary-600": "#8c93a2",
      "--secondary-700": "#7a8190",
      "--secondary-800": "#464c59",
      "--secondary-900": "#2a2d33",
      "--danger-50": "#2c181a",
      "--danger-100": "#3a1d20",
      "--danger-200": "#542529",
      "--success-50": "#14261c",
      "--success-100": "#1a3525",
      "--success-200": "#234932",
      "--warning-50": "#2b2214",
      "--warning-100": "#3a2d17",
      "--warning-200": "#523e1c",
    },
  },

  // No component/layout/page overrides — uses all defaults.
};

export default defaultTheme;
