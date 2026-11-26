import { Languages } from "lucide-react";

import { useI18n, type Locale } from "@/i18n";

interface LanguageToggleProps {
  /**
   * - `icon`: compact 28x28 button for desktop sidebar footer
   * - `pill`: floating pill button with globe/languages icon (e.g. login page)
   * - `segmented`: two-option segmented control for Settings page
   */
  variant?: "icon" | "pill" | "segmented";
  className?: string;
}

const OPTIONS: Array<{ value: Locale; label: string }> = [
  { value: "zh-CN", label: "简体中文" },
  { value: "en-US", label: "English" },
];

export function LanguageToggle({
  variant = "icon",
  className = "",
}: LanguageToggleProps) {
  const { locale, isEn, setLocale, toggleLocale } = useI18n();

  if (variant === "segmented") {
    return (
      <div
        className={`inline-flex gap-1 rounded-lg bg-default-100 p-1 ${className}`}
        role="group"
      >
        {OPTIONS.map((opt) => {
          const active = locale === opt.value;

          return (
            <button
              key={opt.value}
              aria-pressed={active}
              className={`rounded-md px-3 py-1.5 text-sm transition-all duration-150 active:scale-[0.98] ${
                active
                  ? "bg-background font-medium text-foreground shadow-xs"
                  : "text-default-500 hover:text-foreground"
              }`}
              type="button"
              onClick={() => setLocale(opt.value)}
            >
              {opt.label}
            </button>
          );
        })}
      </div>
    );
  }

  if (variant === "pill") {
    return (
      <button
        aria-label={isEn ? "切换到中文" : "Switch to English"}
        className={`inline-flex h-8 items-center gap-1.5 rounded-full border border-divider bg-content1/85 px-3 text-xs font-medium text-default-600 shadow-xs backdrop-blur-md transition-[color,background-color,transform] duration-150 hover:bg-default-100 hover:text-foreground active:scale-95 dark:bg-default-50/85 ${className}`}
        title={isEn ? "切换到中文" : "Switch to English"}
        type="button"
        onClick={toggleLocale}
      >
        <Languages className="h-3.5 w-3.5" strokeWidth={1.75} />
        <span>{isEn ? "中文" : "EN"}</span>
      </button>
    );
  }

  return (
    <button
      aria-label={isEn ? "切换到中文" : "Switch to English"}
      className={`flex h-7 w-7 flex-shrink-0 items-center justify-center rounded-md text-[11px] font-semibold tracking-tight text-default-400 transition-[color,background-color,transform] duration-150 hover:bg-default-200/60 hover:text-foreground active:scale-90 dark:hover:bg-default-100 ${className}`}
      title={isEn ? "切换到中文" : "Switch to English"}
      type="button"
      onClick={toggleLocale}
    >
      {isEn ? "中" : "EN"}
    </button>
  );
}
