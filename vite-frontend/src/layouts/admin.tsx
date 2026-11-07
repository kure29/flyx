import React, { useState, useEffect, useRef } from "react";
import { useNavigate, useLocation } from "react-router-dom";
import {
  ChevronsLeft,
  ChevronsRight,
  Globe,
  LayoutGrid,
  Link2,
  LogOut,
  Menu,
  Rss,
  Server,
  Settings,
  Share2,
  UserRound,
} from "lucide-react";

import { BrandLogo } from "@/components/brand-logo";
import { VersionFooter } from "@/components/version-footer";
import { safeLogout } from "@/utils/logout";
import { siteConfig } from "@/config/site";
import { useMobileBreakpoint } from "@/hooks/useMobileBreakpoint";

interface MenuItem {
  path: string;
  label: string;
  icon: React.ReactNode;
  // Menu heading shown above this item.
  section?: string;
}

export default function AdminLayout({
  children,
}: {
  children: React.ReactNode;
}) {
  const navigate = useNavigate();
  const location = useLocation();
  const [mobileMenuVisible, setMobileMenuVisible] = useState(false);
  const [isCollapsed, setIsCollapsed] = useState(
    () => localStorage.getItem("sidebar_collapsed") === "true",
  );
  const isMobile = useMobileBreakpoint();

  // 菜单项按 线路 / 分发 / 系统 分组；规则和隧道在 中转 里，监控在 节点 和 中转 里
  const menuItems: MenuItem[] = [
    {
      path: "/dashboard",
      label: "概览",
      icon: <LayoutGrid className="h-4 w-4" strokeWidth={1.75} />,
    },
    {
      path: "/node",
      section: "线路",
      label: "服务器",
      icon: <Server className="h-4 w-4" strokeWidth={1.75} />,
    },
    {
      path: "/inbound",
      label: "节点",
      icon: <Globe className="h-4 w-4" strokeWidth={1.75} />,
    },
    {
      path: "/relay",
      label: "中转",
      icon: <Link2 className="h-4 w-4" strokeWidth={1.75} />,
    },
    {
      path: "/subscription",
      section: "分发",
      label: "订阅",
      icon: <Rss className="h-4 w-4" strokeWidth={1.75} />,
    },
    {
      path: "/panel-sharing",
      section: "系统",
      label: "共享",
      icon: <Share2 className="h-4 w-4" strokeWidth={1.75} />,
    },
    {
      path: "/config",
      label: "设置",
      icon: <Settings className="h-4 w-4" strokeWidth={1.75} />,
    },
  ];

  useEffect(() => {
    if (!isMobile) {
      setMobileMenuVisible(false);
    }
  }, [isMobile]);

  // The layout now stays mounted across pages: start each page at the top
  // and with the mobile menu closed, as a fresh layout did.
  const mainRef = useRef<HTMLElement>(null);

  useEffect(() => {
    mainRef.current?.scrollTo(0, 0);
    setMobileMenuVisible(false);
  }, [location.pathname]);

  // 退出登录
  const handleLogout = () => {
    safeLogout();
  };

  // 切换移动端菜单
  const toggleMobileMenu = () => {
    setMobileMenuVisible(!mobileMenuVisible);
  };

  // 隐藏移动端菜单
  const hideMobileMenu = () => {
    setMobileMenuVisible(false);
  };

  // 切换折叠状态
  const toggleCollapse = () => {
    const newCollapsed = !isCollapsed;

    setIsCollapsed(newCollapsed);
    localStorage.setItem("sidebar_collapsed", newCollapsed.toString());
  };

  // 菜单点击处理
  const handleMenuClick = (path: string) => {
    navigate(path);
    if (isMobile) {
      hideMobileMenu();
    }
  };

  return (
    <div
      className={`flex bg-background ${isMobile ? "min-h-screen" : "h-screen overflow-hidden"}`}
    >
      {/* 移动端遮罩层 */}
      {isMobile && mobileMenuVisible && (
        <button
          aria-label="关闭菜单"
          className="fixed inset-0 z-40 bg-black/40"
          type="button"
          onClick={hideMobileMenu}
        />
      )}

      {/* 左侧菜单栏：贴边平铺，只用一条细线和内容区分开 */}
      <aside
        className={`
        ${isMobile ? "fixed top-0 left-0 h-screen" : "relative h-full"}
        ${isMobile && !mobileMenuVisible ? "-translate-x-full" : "translate-x-0"}
        ${isMobile ? "w-60" : isCollapsed ? "w-16" : "w-56"}
        z-50 flex flex-shrink-0 flex-col border-r border-divider bg-background
        transition-[width,transform] duration-200`}
      >
        {/* Logo 区域 */}
        <div
          className={`flex h-14 items-center overflow-hidden whitespace-nowrap ${isCollapsed ? "justify-center px-0" : "px-5"}`}
        >
          <BrandLogo className="h-6 w-6 flex-shrink-0" size={24} />
          {!isCollapsed && (
            <span className="ml-2.5 truncate text-[15px] font-semibold text-foreground">
              {siteConfig.name}
            </span>
          )}
        </div>

        {/* 菜单导航 */}
        <nav className="flex-1 overflow-y-auto overflow-x-hidden px-3 scrollbar-hide">
          <ul className="space-y-0.5">
            {menuItems.map((item) => {
              const isActive =
                location.pathname === item.path ||
                (item.path === "/relay" &&
                  ["/tunnel", "/forward"].includes(location.pathname));

              return (
                <li key={item.path}>
                  {item.section &&
                    (isCollapsed ? (
                      <div className="mx-2 my-2 border-t border-divider" />
                    ) : (
                      <div className="px-2 pb-1 pt-4 text-xs text-default-400">
                        {item.section}
                      </div>
                    ))}
                  <button
                    aria-current={isActive ? "page" : undefined}
                    className={`flex h-9 w-full items-center rounded-md text-left text-sm transition-colors ${isCollapsed ? "justify-center" : "gap-2.5 px-2"} ${
                      isActive
                        ? "bg-default-100 font-medium text-foreground"
                        : "text-default-500 hover:bg-default-50 hover:text-foreground"
                    }`}
                    title={isCollapsed ? item.label : undefined}
                    type="button"
                    onClick={() => handleMenuClick(item.path)}
                  >
                    <span className="flex h-4 w-4 flex-shrink-0 items-center justify-center">
                      {item.icon}
                    </span>
                    {!isCollapsed && (
                      <span className="truncate">{item.label}</span>
                    )}
                  </button>
                </li>
              );
            })}
          </ul>
        </nav>

        {/* 底部：账号、退出、版本和折叠 */}
        <div className="mt-auto flex flex-shrink-0 flex-col gap-1 overflow-hidden whitespace-nowrap px-3 py-3">
          {!isMobile &&
            [
              // Password, passkeys and GitHub live on the account page.
              {
                key: "account",
                label: "账户与登录",
                icon: UserRound,
                onPress: () => navigate("/profile"),
              },
              {
                key: "logout",
                label: "退出登录",
                icon: LogOut,
                onPress: handleLogout,
              },
            ].map((a) => (
              <button
                key={a.key}
                className={`flex h-8 items-center rounded-md text-sm text-default-500 transition-colors hover:bg-default-50 hover:text-foreground ${isCollapsed ? "justify-center" : "gap-2.5 px-2"}`}
                title={a.label}
                type="button"
                onClick={a.onPress}
              >
                <a.icon className="h-4 w-4 shrink-0" strokeWidth={1.75} />
                {!isCollapsed && <span>{a.label}</span>}
              </button>
            ))}
          <div
            className={`mt-2 flex items-center ${isCollapsed ? "justify-center" : "justify-between gap-2 pl-2"}`}
          >
            {!isCollapsed && (
              <VersionFooter
                containerClassName="min-w-0"
                poweredClassName="hidden"
                updateBadgeClassName="ml-1.5 text-foreground"
                version={siteConfig.version}
                versionClassName="truncate text-xs text-default-400"
              />
            )}
            {!isMobile && (
              <button
                aria-label={isCollapsed ? "展开菜单" : "收起菜单"}
                className="flex h-7 w-7 flex-shrink-0 items-center justify-center rounded-md text-default-400 transition-colors hover:bg-default-50 hover:text-foreground"
                type="button"
                onClick={toggleCollapse}
              >
                {isCollapsed ? (
                  <ChevronsRight className="h-4 w-4" />
                ) : (
                  <ChevronsLeft className="h-4 w-4" />
                )}
              </button>
            )}
          </div>
        </div>
      </aside>

      {/* 主内容区域 */}
      <div
        className={`relative flex flex-1 flex-col ${isMobile ? "min-h-0" : "h-full overflow-hidden"}`}
      >
        {isMobile && (
          <header className="flex h-12 items-center gap-2 border-b border-divider px-3">
            <button
              aria-label="打开菜单"
              className="flex h-8 w-8 items-center justify-center rounded-md text-foreground hover:bg-default-100"
              type="button"
              onClick={toggleMobileMenu}
            >
              <Menu className="h-5 w-5" strokeWidth={1.75} />
            </button>
            <BrandLogo className="h-5 w-5" size={20} />
            <span className="text-sm font-semibold">{siteConfig.name}</span>
          </header>
        )}

        <main ref={mainRef} className="flex-1 overflow-y-auto scrollbar-hide">
          <div className="mx-auto h-full w-full max-w-6xl">{children}</div>
        </main>
      </div>
    </div>
  );
}
