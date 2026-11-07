import ReactDOM from "react-dom/client";
import { BrowserRouter, HashRouter } from "react-router-dom";
import { registerSW } from "virtual:pwa-register";
import toast from "react-hot-toast";

import App from "./App.tsx";
import { ErrorBoundary, reloadOnce } from "./components/error-boundary";
import { IS_DEMO, installDemo } from "./demo";
import { DemoBanner } from "./demo/banner";
import { Provider } from "./provider.tsx";
import "@/styles/globals.css";

// A page chunk from the previous build is gone after an upgrade: reload
// to pick up the new build. When that already happened in the last
// minute, the error reaches the page's ErrorBoundary, which offers a reload.
window.addEventListener("vite:preloadError", (event) => {
  if (reloadOnce()) event.preventDefault();
});

if (IS_DEMO) installDemo();

// Browsers only look for a new service worker on navigation (and at most
// daily), and a dashboard tab rarely navigates: after a panel upgrade the
// refresh prompt could take hours. Ask every minute and when the tab comes
// back into view, gets focus or comes back online instead.
const SW_UPDATE_INTERVAL = 60 * 1000;

const updateSW = registerSW({
  immediate: true,
  onRegisteredSW(_swUrl, registration) {
    if (!registration) return;
    let checking = false;
    const check = () => {
      if (
        checking ||
        document.visibilityState !== "visible" ||
        !navigator.onLine
      )
        return;
      checking = true;
      registration
        .update()
        .catch(() => {})
        .finally(() => {
          checking = false;
        });
    };

    setInterval(check, SW_UPDATE_INTERVAL);
    document.addEventListener("visibilitychange", check);
    window.addEventListener("focus", check);
    window.addEventListener("online", check);
    window.addEventListener("pageshow", check);
  },
  onNeedRefresh() {
    toast(
      (t) => (
        <div className="flex flex-col gap-3">
          <span className="text-sm font-medium text-foreground">
            发现新版本，是否立即刷新以应用更新？
          </span>
          <div className="flex gap-2 justify-end">
            <button
              className="px-3 py-1.5 text-xs font-medium bg-primary text-primary-foreground hover:bg-primary/90 rounded-md transition-colors"
              onClick={() => {
                updateSW(true);
                toast.dismiss(t.id);
              }}
            >
              刷新
            </button>
            <button
              className="px-3 py-1.5 text-xs font-medium bg-default-200 text-default-700 hover:bg-default-300 rounded-md transition-colors"
              onClick={() => toast.dismiss(t.id)}
            >
              稍后
            </button>
          </div>
        </div>
      ),
      { id: "sw-update", duration: Infinity, position: "bottom-right" },
    );
  },
});

// The demo is static files on GitHub Pages: hash routes need no server.
const Router = IS_DEMO ? HashRouter : BrowserRouter;

ReactDOM.createRoot(document.getElementById("root")!).render(
  <Router>
    <Provider>
      <ErrorBoundary fullScreen>
        <App />
      </ErrorBoundary>
      {IS_DEMO && <DemoBanner />}
    </Provider>
  </Router>,
);
