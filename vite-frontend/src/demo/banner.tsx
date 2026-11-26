import { useState } from "react";
import { Languages, Shield, UserRound, X } from "lucide-react";

import { getDemoRole, switchDemoRole, type DemoRole } from "@/demo";
import { useI18n } from "@/i18n";

const DOCS = "https://kure29.github.io/flyx/guide/install";
const REPO = "https://github.com/kure29/flyx";

/** The demo's floating note: role switch, language toggle, and install link. */
export function DemoBanner() {
  const [open, setOpen] = useState(true);
  const { isEn, toggleLocale, t } = useI18n();
  const role: DemoRole = getDemoRole();

  if (!open) {
    return (
      <button
        className="fixed bottom-[calc(5rem+var(--safe-area-bottom))] right-3 z-[60] flex items-center gap-1.5 rounded-full bg-primary px-3.5 py-1.5 text-xs font-medium text-primary-foreground shadow-lg transition-transform active:scale-95 lg:bottom-4"
        type="button"
        onClick={() => setOpen(true)}
      >
        <span>{t("演示站")}</span>
      </button>
    );
  }

  return (
    <div className="fixed bottom-[calc(5rem+var(--safe-area-bottom))] right-3 z-[60] w-[min(21rem,calc(100vw-24px))] rounded-2xl border border-divider bg-content1/95 p-3.5 text-xs shadow-xl backdrop-blur-md lg:bottom-4">
      <div className="mb-1.5 flex items-center justify-between gap-2">
        <span className="text-sm font-semibold">{t("FLYX 在线演示")}</span>
        <div className="flex items-center gap-1">
          <button
            aria-label={isEn ? "切换到中文" : "Switch to English"}
            className="inline-flex h-6 items-center gap-1 rounded-md px-1.5 text-[11px] font-medium text-default-500 transition-colors hover:bg-default-100 hover:text-foreground"
            type="button"
            onClick={toggleLocale}
          >
            <Languages className="h-3.5 w-3.5" />
            <span>{isEn ? "中文" : "EN"}</span>
          </button>
          <button
            aria-label={t("收起")}
            className="rounded-md p-1 text-default-400 transition-colors hover:bg-default-100 hover:text-foreground"
            type="button"
            onClick={() => setOpen(false)}
          >
            <X className="h-3.5 w-3.5" />
          </button>
        </div>
      </div>
      <p className="leading-relaxed text-default-500">
        {t("数据都是示例，可以随便点；修改不会保存，刷新后还原。")}
      </p>

      {/* Role switcher: experience both admin panel and user portal */}
      <div className="mt-2.5 grid grid-cols-2 gap-1 rounded-lg bg-default-100 p-0.5">
        <button
          className={`flex items-center justify-center gap-1.5 rounded-md py-1.5 font-medium transition-all ${
            role === "admin"
              ? "bg-background text-foreground shadow-xs"
              : "text-default-500 hover:text-foreground"
          }`}
          type="button"
          onClick={() => role !== "admin" && switchDemoRole("admin")}
        >
          <Shield className="h-3.5 w-3.5" />
          <span>{t("管理员")}</span>
        </button>
        <button
          className={`flex items-center justify-center gap-1.5 rounded-md py-1.5 font-medium transition-all ${
            role === "user"
              ? "bg-background text-foreground shadow-xs"
              : "text-default-500 hover:text-foreground"
          }`}
          type="button"
          onClick={() => role !== "user" && switchDemoRole("user")}
        >
          <UserRound className="h-3.5 w-3.5" />
          <span>{t("普通用户")}</span>
        </button>
      </div>

      <div className="mt-2.5 flex gap-2">
        <a
          className="flex flex-1 items-center justify-center rounded-lg bg-primary px-2.5 py-1.5 font-medium text-primary-foreground transition-opacity hover:opacity-90"
          href={DOCS}
          rel="noreferrer"
          target="_blank"
        >
          {t("安装 FLYX")}
        </a>
        <a
          className="flex items-center justify-center rounded-lg border border-divider px-3 py-1.5 font-medium text-default-600 transition-colors hover:bg-default-100 hover:text-foreground"
          href={REPO}
          rel="noreferrer"
          target="_blank"
        >
          GitHub
        </a>
      </div>
    </div>
  );
}
