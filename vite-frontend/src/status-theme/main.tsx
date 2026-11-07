// The built-in status page theme: a standalone app served by the panel at
// /status/ (see go-backend/internal/statustheme). It shares the panel's
// styles and components but not its router, API client or service worker.
import { lazy, Suspense } from "react";
import ReactDOM from "react-dom/client";

import StatusPage from "./status-page";

import "@/themes/loader";
import { initThemeSystem } from "@/themes/registry";
import { ErrorBoundary } from "@/components/error-boundary";
import "@/styles/globals.css";

// The panel's look (colour tokens, light/dark) as this browser last chose it.
initThemeSystem();

// The charts (recharts) load only on a node's page.
const NodeDetail = lazy(() => import("./node-detail"));

// Two pages, so no router: /status/ and /status/node/<id on the page>.
// The backend answers every route with index.html.
const nodeMatch = window.location.pathname
  .slice(import.meta.env.BASE_URL.length)
  .match(/^node\/(\d+)\/?$/);

ReactDOM.createRoot(document.getElementById("root")!).render(
  <ErrorBoundary fullScreen>
    {nodeMatch ? (
      <Suspense fallback={null}>
        <NodeDetail id={Number(nodeMatch[1])} />
      </Suspense>
    ) : (
      <StatusPage />
    )}
  </ErrorBoundary>,
);
