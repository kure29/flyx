/**
 * ThemeProvider — app-level wrapper
 * =================================
 * Loads all registered themes and wraps children with the theme context.
 * Import the loader to ensure all built-in themes are registered before
 * the provider mounts.
 */

import React from "react";

// Side-effect: registers all built-in themes
import "@/themes/loader";

import { ThemeProvider as ThemeContextProvider } from "@/themes/context";
import { initThemeSystem } from "@/themes/registry";

// Restore saved theme SYNCHRONOUSLY at module load time — before first render.
// This ensures the CSS tokens are injected before any component renders,
// preventing a flash of the default theme.
initThemeSystem();

interface ThemeProviderProps {
  children: React.ReactNode;
}

export const ThemeProvider: React.FC<ThemeProviderProps> = ({ children }) => {
  return <ThemeContextProvider>{children}</ThemeContextProvider>;
};
