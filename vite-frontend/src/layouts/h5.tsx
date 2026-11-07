import { Globe, LayoutGrid, Link2, Menu, Rss, Server } from "lucide-react";
import React from "react";
import { useNavigate, useLocation } from "react-router-dom";

import { BrandLogo } from "@/components/brand-logo";
import { siteConfig } from "@/config/site";
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

  return (
    <div className="flex flex-col min-h-screen bg-mesh-gradient">
      {/* 顶部导航栏 */}
      <header className="bg-background border-b border-divider h-14 safe-top flex-shrink-0 flex items-center justify-between px-4 relative z-10">
        <div className="flex items-center gap-2">
          <BrandLogo size={20} />
          <h1 className="text-sm font-bold text-foreground">
            {siteConfig.name}
          </h1>
        </div>

        <div className="flex items-center gap-2" />
      </header>

      {/* 主内容区域 */}
      <main className="flex-1">{children}</main>

      {/* 用于给固定 Tabbar 腾出空间的占位元素 */}
      <div aria-hidden className="h-[calc(4rem+var(--safe-area-bottom))]" />

      {/* 底部Tabbar */}
      <nav className="bg-background border-t border-divider h-[calc(4rem+var(--safe-area-bottom))] flex-shrink-0 flex items-center justify-around px-2 fixed bottom-0 left-0 right-0 z-30">
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
              className={`
                flex flex-col items-center justify-center flex-1 h-full pb-[var(--safe-area-bottom)]
                transition-colors duration-200 min-h-[44px]
                ${
                  isActive
                    ? "text-foreground"
                    : "text-gray-500 dark:text-gray-400 hover:text-gray-700 dark:hover:text-gray-200"
                }
              `}
              onClick={() => navigate(item.path)}
            >
              <div className="flex-shrink-0 mb-1">{item.icon}</div>
              <span className="text-xs font-medium">{item.label}</span>
            </button>
          );
        })}
      </nav>
    </div>
  );
}
