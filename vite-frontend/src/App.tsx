import {
  Route,
  Routes,
  useLocation,
  useNavigate,
  Navigate,
} from "react-router-dom";
import { lazy, Suspense, useEffect } from "react";

import IndexPage from "@/pages/index";
import AdminLayout from "@/layouts/admin";
import H5Layout from "@/layouts/h5";
import H5SimpleLayout from "@/layouts/h5-simple";
import { isLoggedIn } from "@/utils/auth";
import { siteConfig, updateSiteConfig } from "@/config/site";
import { useH5Mode } from "@/hooks/useH5Mode";
import { SESSION_UPDATED_EVENT } from "@/utils/session";
import { useThemeContext } from "@/themes/context";
import { PageLoadingState } from "@/components/page-state";
import { ErrorBoundary } from "@/components/error-boundary";

// Pages load on first visit so the login screen does not wait for all of them.
const ChangePasswordPage = lazy(() => import("@/pages/change-password"));
// The pages in the main navigation, preloaded once logged in (see
// preloadPages) so their first visit doesn't wait for the download.
const loadDashboard = () => import("@/pages/dashboard");
const loadNodeHub = () => import("@/pages/node-hub");
const loadInbound = () => import("@/pages/inbound");
const loadRelay = () => import("@/pages/relay");
const loadSubscription = () => import("@/pages/subscription");
const loadConfig = () => import("@/pages/config");

const DashboardPage = lazy(loadDashboard);
const MonitorPage = lazy(() => import("@/pages/monitor"));
const ForwardPage = lazy(() => import("@/pages/forward"));
const TunnelPage = lazy(() => import("@/pages/tunnel"));
// 节点 hosts the 内核 tab.
const NodePage = lazy(loadNodeHub);
const ProfilePage = lazy(() => import("@/pages/profile"));
const InboundPage = lazy(loadInbound);
const RelayPage = lazy(loadRelay);
const SubscriptionPage = lazy(loadSubscription);
const ConfigPage = lazy(loadConfig);
const PanelSharingPage = lazy(() => import("@/pages/panel-sharing"));
const GithubCallbackPage = lazy(() => import("@/pages/github-callback"));

let pagesPreloaded = false;

/**
 * Downloads the main pages' code once the browser is idle after login.
 * The service worker caches them too, but only once it is installed and
 * after the first visit. Skipped when the user asked to save data.
 */
function preloadPages() {
  const connection = (navigator as { connection?: { saveData?: boolean } })
    .connection;

  if (pagesPreloaded || connection?.saveData) return;
  pagesPreloaded = true;
  const run = () => {
    const pages = [
      loadDashboard,
      loadNodeHub,
      loadInbound,
      loadRelay,
      loadSubscription,
      loadConfig,
    ];

    for (const load of pages) void load().catch(() => {});
  };

  window.setTimeout(() => {
    if ("requestIdleCallback" in window) window.requestIdleCallback(run);
    else run();
  }, 2000);
}

const ProtectedRoute = ({
  children,
  useSimpleLayout = false,
  skipLayout = false,
}: {
  children: React.ReactNode;
  useSimpleLayout?: boolean;
  skipLayout?: boolean;
}) => {
  const isH5 = useH5Mode();
  const authenticated = isLoggedIn();
  const { pathname } = useLocation();

  if (!authenticated) {
    return <Navigate replace to="/" />;
  }

  // 如果跳过布局，直接返回子组件
  // Inside the layout, so the menus still work after a page fails; keyed
  // by path, so moving to another page clears the error.
  const page = (
    <ErrorBoundary key={pathname}>
      <Suspense fallback={<PageLoadingState message="正在加载..." />}>
        {children}
      </Suspense>
    </ErrorBoundary>
  );

  if (skipLayout) {
    return page;
  }

  // 根据模式和页面类型选择布局
  const Layout =
    isH5 && useSimpleLayout ? H5SimpleLayout : isH5 ? H5Layout : AdminLayout;

  return <Layout>{page}</Layout>;
};

// 登录页面路由组件 - 已登录则重定向到dashboard
const LoginRoute = () => {
  const authenticated = isLoggedIn();
  const navigate = useNavigate();

  useEffect(() => {
    if (authenticated) {
      // 使用 React Router 导航，避免无限跳转
      navigate("/dashboard", { replace: true });
    }
  }, [authenticated, navigate]);

  if (authenticated) {
    return <Navigate replace to="/dashboard" />;
  }

  return <IndexPage />;
};

function App() {
  const location = useLocation();
  const navigate = useNavigate();
  const { effectiveMode } = useThemeContext();

  useEffect(() => {
    if (isLoggedIn()) preloadPages();
  }, [location.pathname]);

  // 全局登录状态监听，当检测到未登录且不在首页时，跳转到首页
  useEffect(() => {
    const handleSessionUpdate = () => {
      if (!isLoggedIn() && location.pathname !== "/") {
        navigate("/", { replace: true });
      }
    };

    window.addEventListener(SESSION_UPDATED_EVENT, handleSessionUpdate);

    return () => {
      window.removeEventListener(SESSION_UPDATED_EVENT, handleSessionUpdate);
    };
  }, [location.pathname, navigate]);

  // 处理自定义背景图片
  useEffect(() => {
    const updateBg = () => {
      const customBg =
        (effectiveMode === "dark"
          ? siteConfig.app_bg_image_dark
          : siteConfig.app_bg_image_light) || siteConfig.app_bg_image;

      const root = document.documentElement;
      const isImage =
        customBg.startsWith("http") ||
        customBg.startsWith("data:") ||
        customBg.startsWith("/") ||
        customBg.startsWith("blob:");
      // "" and "theme" are the default plain background, as is "wallpaper"
      // (the built-in picture, since removed) where a panel still has it
      // saved; anything else is an image URL or a colour.
      const mode =
        !customBg || customBg === "theme" || customBg === "wallpaper"
          ? "plain"
          : "custom";

      root.classList.toggle("has-custom-bg", mode === "custom");
      root.classList.remove("has-theme-bg");
      if (mode === "custom") {
        root.style.setProperty(
          "--custom-bg-image",
          isImage ? `url(${customBg})` : "none",
        );
        root.style.setProperty(
          "--custom-bg-color",
          isImage ? "transparent" : customBg,
        );
      } else {
        root.style.removeProperty("--custom-bg-image");
        root.style.removeProperty("--custom-bg-color");
      }
    };

    updateBg();
    window.addEventListener("site-config-updated", updateBg);

    return () => {
      window.removeEventListener("site-config-updated", updateBg);
    };
  }, [effectiveMode]);

  // 立即设置页面标题（使用已从缓存读取的配置）
  useEffect(() => {
    document.title = siteConfig.name;

    void updateSiteConfig();

    const handleConfigUpdate = () => {
      void updateSiteConfig();
    };

    window.addEventListener("configUpdated", handleConfigUpdate);

    return () => {
      window.removeEventListener("configUpdated", handleConfigUpdate);
    };
  }, []);

  // No key and no exit animation: the layout stays mounted across pages
  // and the next page mounts (and starts loading) right away, instead of
  // after the old one has animated out.
  return (
    <Routes location={location}>
      <Route element={<LoginRoute />} path="/" />
      <Route
        element={
          <Suspense fallback={<PageLoadingState message="正在加载..." />}>
            <GithubCallbackPage />
          </Suspense>
        }
        path="/github-callback"
      />
      <Route
        element={
          <ProtectedRoute skipLayout={true}>
            <ChangePasswordPage />
          </ProtectedRoute>
        }
        path="/change-password"
      />
      <Route
        element={
          <ProtectedRoute>
            <DashboardPage />
          </ProtectedRoute>
        }
        path="/dashboard"
      />
      <Route
        element={
          <ProtectedRoute>
            <MonitorPage />
          </ProtectedRoute>
        }
        path="/monitor"
      />
      <Route
        element={
          <ProtectedRoute>
            <ForwardPage />
          </ProtectedRoute>
        }
        path="/forward"
      />
      <Route
        element={
          <ProtectedRoute>
            <TunnelPage />
          </ProtectedRoute>
        }
        path="/tunnel"
      />
      <Route
        element={
          <ProtectedRoute>
            <NodePage />
          </ProtectedRoute>
        }
        path="/node"
      />
      <Route
        element={
          <ProtectedRoute>
            <ProfilePage />
          </ProtectedRoute>
        }
        path="/profile"
      />
      <Route
        element={
          <ProtectedRoute>
            <InboundPage />
          </ProtectedRoute>
        }
        path="/inbound"
      />
      <Route
        element={
          <ProtectedRoute>
            <RelayPage />
          </ProtectedRoute>
        }
        path="/relay"
      />
      <Route
        element={
          <ProtectedRoute>
            <SubscriptionPage />
          </ProtectedRoute>
        }
        path="/subscription"
      />
      <Route
        element={
          <ProtectedRoute>
            <ConfigPage />
          </ProtectedRoute>
        }
        path="/config"
      />
      <Route
        element={
          <ProtectedRoute useSimpleLayout={true}>
            <PanelSharingPage />
          </ProtectedRoute>
        }
        path="/panel-sharing"
      />
      {/* Unknown addresses, and pages that are gone (用户, 分组, 限速, 我的订阅). */}
      <Route
        element={<Navigate replace to={isLoggedIn() ? "/dashboard" : "/"} />}
        path="*"
      />
    </Routes>
  );
}

export default App;
