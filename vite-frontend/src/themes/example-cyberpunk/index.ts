/**
 * Example Theme: Cyberpunk
 * ========================
 * A neon-cyberpunk dark theme that demonstrates ALL override capabilities:
 *   ✅ CSS tokens (full dark palette)
 *   ✅ Raw CSS (neon glow effects, custom animations, font-face)
 *   ✅ Component override (custom Button with glow)
 *   ✅ Lifecycle hooks
 *
 * Theme authors: copy this entire folder and modify it to create your own
 * theme.  See README.md for the full guide.
 */

import type { ThemePackage } from "../types";

import { CyberpunkButton } from "./components/button";

const cyberpunkTheme: ThemePackage = {
  id: "cyberpunk",
  name: "赛博朋克",
  author: "FLYX Community",
  version: "1.0.0",
  description: "霓虹灯风格的赛博朋克暗色主题",

  // ── CSS Tokens ────────────────────────────────────────────────────────────
  tokens: {
    light: {
      // This theme is dark-only, so the light tokens just fall through to dark
      "--background": "#0a0a1a",
      "--foreground": "#e0e0ff",
      "--border": "#2a2a4a",
      "--input": "#1a1a3a",
      "--ring": "#ff00ff",
      "--content1": "#12122a",
      "--divider": "#2a2a4a",

      "--primary": "#ff00ff",
      "--primary-foreground": "#ffffff",
      "--secondary": "#00ffff",
      "--secondary-foreground": "#000000",
      "--danger": "#ff3366",
      "--success": "#00ff88",
      "--warning": "#ffaa00",
    },
    dark: {
      "--background": "#0a0a1a",
      "--foreground": "#e0e0ff",
      "--border": "#2a2a4a",
      "--input": "#1a1a3a",
      "--ring": "#ff00ff",
      "--content1": "#12122a",
      "--divider": "#2a2a4a",

      "--default-50": "#0d0d20",
      "--default-100": "#14142e",
      "--default-200": "#1e1e3c",
      "--default-300": "#2a2a4a",
      "--default-400": "#4a4a6a",
      "--default-500": "#7a7a9a",
      "--default-600": "#9a9aba",
      "--default-700": "#babada",
      "--default-800": "#dadaf0",
      "--default-900": "#f0f0ff",

      "--primary": "#ff00ff",
      "--primary-foreground": "#ffffff",
      "--primary-50": "#1a001a",
      "--primary-100": "#330033",
      "--primary-200": "#660066",
      "--primary-300": "#990099",
      "--primary-400": "#cc00cc",
      "--primary-500": "#ff00ff",
      "--primary-600": "#ff33ff",
      "--primary-700": "#ff66ff",
      "--primary-800": "#ff99ff",
      "--primary-900": "#ffccff",

      "--secondary": "#00ffff",
      "--secondary-foreground": "#000000",

      "--danger": "#ff3366",
      "--success": "#00ff88",
      "--warning": "#ffaa00",
    },
  },

  // ── Raw CSS (glow effects, animations, fonts) ─────────────────────────────
  css: `
    /* Cyberpunk neon glow on primary buttons */
    [data-flyx-theme="cyberpunk"] .bg-primary,
    .bg-primary {
      box-shadow: 0 0 12px rgba(255, 0, 255, 0.4),
                  0 0 24px rgba(255, 0, 255, 0.15);
    }

    /* Neon border glow on cards */
    [data-flyx-theme="cyberpunk"] [class*="border"] {
      border-color: rgba(255, 0, 255, 0.15);
    }

    /* Scanline overlay animation */
    @keyframes flyx-scanline {
      0% { transform: translateY(-100%); }
      100% { transform: translateY(100vh); }
    }

    /* Custom scrollbar */
    ::-webkit-scrollbar { width: 6px; }
    ::-webkit-scrollbar-track { background: #0a0a1a; }
    ::-webkit-scrollbar-thumb {
      background: linear-gradient(180deg, #ff00ff, #00ffff);
      border-radius: 3px;
    }
  `,

  // ── Component Overrides ───────────────────────────────────────────────────
  components: {
    Button: CyberpunkButton,
  },

  // ── Lifecycle ─────────────────────────────────────────────────────────────
  onActivate: () => {
    // Force dark mode for this theme
    document.documentElement.classList.add("dark");
    document.documentElement.style.colorScheme = "dark";
    // Mark the body for theme-specific CSS selectors
    document.body.setAttribute("data-flyx-theme", "cyberpunk");
  },
  onDeactivate: () => {
    document.body.removeAttribute("data-flyx-theme");
  },
};

export default cyberpunkTheme;
