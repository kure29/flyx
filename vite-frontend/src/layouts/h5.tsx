import { Globe, LayoutGrid, Link2, Menu, Rss, Server } from "lucide-react";
import React from "react";
import { useNavigate, useLocation } from "react-router-dom";

import { BrandLogo } from "@/components/brand-logo";
import { siteConfig } from "@/config/site";
import { useRouteTransition } from "@/hooks/useRouteTransition";
import { useScrollTopOnPathChange } from "@/hooks/useScrollTopOnPathChange";

interface TabItem {
  path: string;
  label: string;
  icon: React.ReactNode;
}

// The bar follows the landing → relay → subscription flow; settings and
// sharing are under 更多.
const tabItems: TabItem[] = [
  {
    path: "/dashboard",
    label: "概览",
    icon: <LayoutGrid className="h-5 w-5" strokeWidth={1.75} />,
  },
  {
    path: "/node",
    label: "服务器",
    icon: <Server className="h-5 w-5" strokeWidth={1.75} />,
  },
  {
    path: "/inbound",
    label: "节点",
    icon: <Globe className="h-5 w-5" strokeWidth={1.75} />,
  },
  {
    path: "/relay",
    label: "中转",
    icon: <Link2 className="h-5 w-5" strokeWidth={1.75} />,
  },
  {
    path: "/subscription",
    label: "订阅",
    icon: <Rss className="h-5 w-5" strokeWidth={1.75} />,
  },
  {
    path: "/profile",
    label: "更多",
    icon: <Menu className="h-5 w-5" strokeWidth={1.75} />,
  },
];

export default function H5Layout({ children }: { children: React.ReactNode }) {
  const navigate = useNavigate();
  const location = useLocation();

  useScrollTopOnPathChange();
  const transition = useRouteTransition();

  return (
    <div className="h5-shell flex flex-col min-h-screen bg-mesh-gradient">
      {/* 顶部导航栏：与灰色页面同色，像 iOS 的大标题页 */}
      <header className="h-14 safe-top flex-shrink-0 flex items-center justify-between px-4 relative z-10">
        <div className="flex items-center gap-2">
          <BrandLogo size={20} />
          <h1 className="text-sm font-bold text-foreground">
            {siteConfig.name}
          </h1>
        </div>

        <div className="flex items-center gap-2" />
      </header>

      {/* 主内容区域 */}
      {/* overflow-x: clip keeps a page sliding in from the side from adding
          a horizontal scrollbar, without breaking sticky bars inside. */}
      <main className="flex-1 overflow-x-clip">
        <div key={location.pathname} className={transition}>
          {children}
        </div>
      </main>

      {/* 给悬浮 Tabbar 腾出空间 */}
      <div aria-hidden className="h-[calc(5rem+var(--safe-area-bottom))]" />

      {/* 底部悬浮 Tabbar：圆角胶囊，选中项带底色。纯色不加模糊，滚动更流畅 */}
      <nav className="fixed inset-x-3 bottom-[calc(0.5rem+var(--safe-area-bottom))] z-30 flex h-[3.75rem] items-center gap-0.5 rounded-[1.75rem] border border-divider bg-content1 p-1 shadow-[0_6px_24px_rgba(0,0,0,0.08)] dark:bg-default-50 dark:shadow-none">
        {tabItems.map((item) => {
          const isActive =
            location.pathname === item.path ||
            (item.path === "/relay" &&
              ["/tunnel", "/forward"].includes(location.pathname)) ||
            (item.path === "/profile" &&
              ["/config", "/panel-sharing"].includes(location.pathname));

          return (
            <button
              key={item.path}
              className={`flex h-full min-w-0 flex-1 flex-col items-center justify-center rounded-3xl transition-colors duration-200 active:scale-95 ${
                isActive
                  ? "bg-default-100 text-foreground dark:bg-default-200"
                  : "text-default-500 hover:text-foreground"
              }`}
              onClick={() => navigate(item.path)}
            >
              <div className="mb-0.5 flex-shrink-0">{item.icon}</div>
              <span className="text-[11px] font-medium">{item.label}</span>
            </button>
          );
        })}
      </nav>
    </div>
  );
}
