import path from "path";

import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";

// The built-in status page theme (see src/status-theme). The panel serves
// it at /status/; `pnpm run build:status-theme` writes dist-status-theme/,
// which go-backend embeds (make status-theme).
//
// `pnpm run dev:status-theme` serves it at http://localhost:3001/status/
// with the API proxied to a panel backend (FLYX_BACKEND, default :6365).
export default defineConfig({
  root: path.resolve(__dirname, "status-theme"),
  base: "/status/",
  // The panel's public/ (PWA icons, service worker assets) is not the theme's.
  publicDir: false,
  envDir: __dirname,
  plugins: [react()],
  resolve: {
    alias: {
      "@": path.resolve(__dirname, "./src"),
    },
  },
  css: {
    postcss: __dirname,
  },
  server: {
    port: 3001,
    host: "0.0.0.0",
    fs: { allow: [__dirname] },
    proxy: {
      "/api": process.env.FLYX_BACKEND || "http://127.0.0.1:6365",
    },
  },
  build: {
    outDir: path.resolve(__dirname, "dist-status-theme"),
    emptyOutDir: true,
    sourcemap: false,
  },
});
