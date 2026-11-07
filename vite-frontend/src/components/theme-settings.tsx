/**
 * ThemeSettings — theme picker card for the Settings page
 * ========================================================
 * Shows:
 *  • Mode toggle (light / dark / system)
 *  • Grid of registered themes with preview dots
 *  • "Reset to default" option
 */

import type { ThemeMode } from "@/themes/registry";

import React from "react";
import toast from "react-hot-toast";

import { Card, CardBody } from "@/shadcn-bridge/heroui/card";
import { Button } from "@/shadcn-bridge/heroui/button";
import { useThemeContext } from "@/themes/context";

// ─── Constants ──────────────────────────────────────────────────────────────

const MODE_OPTIONS: Array<{ value: ThemeMode; label: string }> = [
  { value: "light", label: "亮色" },
  { value: "dark", label: "暗色" },
  { value: "system", label: "跟随系统" },
];

// ─── Component ──────────────────────────────────────────────────────────────

export const ThemeSettings: React.FC = () => {
  const {
    themes,
    activeThemeId,
    mode,
    effectiveMode,
    switchTheme,
    resetTheme,
    setMode,
  } = useThemeContext();

  const handleModeChange = (m: ThemeMode) => {
    setMode(m);
    const label = m === "light" ? "亮色" : m === "dark" ? "暗色" : "跟随系统";

    toast.success(`已切换为${label}模式`);
  };

  const handleThemeSelect = (id: string) => {
    switchTheme(id);
    const theme = themes.find((t) => t.id === id);

    toast.success(`已切换主题「${theme?.name ?? id}」`);
  };

  const handleReset = () => {
    resetTheme();
    toast.success("已恢复默认主题");
  };

  return (
    <Card>
      <CardBody className="p-6">
        <h2 className="text-lg font-medium text-gray-900 dark:text-white mb-5">
          主题设置
        </h2>

        {/* ── Mode toggle ────────────────────────────────────── */}
        <div className="mb-6">
          <p className="mb-2 text-sm">外观模式</p>
          <div className="inline-flex gap-1">
            {MODE_OPTIONS.map((opt) => (
              <button
                key={opt.value}
                className={`rounded-md px-3 py-1.5 text-sm transition-colors ${
                  mode === opt.value
                    ? "bg-default-100 font-medium text-foreground"
                    : "text-default-500 hover:text-foreground"
                }`}
                type="button"
                onClick={() => handleModeChange(opt.value)}
              >
                {opt.label}
              </button>
            ))}
          </div>
        </div>

        {/* ── Theme grid ─────────────────────────────────────── */}
        <div className="mb-4">
          <p className="mb-2 text-sm">
            选择主题
            <span className="ml-2 text-xs text-default-400">
              共 {themes.length} 个可用主题
            </span>
          </p>

          <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-3">
            {themes.map((theme) => {
              const isActive = activeThemeId === theme.id;
              // Pick the right token set for preview
              const previewTokens =
                effectiveMode === "dark" && theme.tokens?.dark
                  ? theme.tokens.dark
                  : theme.tokens?.light;

              const primary = previewTokens?.["--primary"] ?? "#2563eb";
              const secondary = previewTokens?.["--secondary"] ?? "#6366f1";
              const success = previewTokens?.["--success"] ?? "#16a34a";
              const danger = previewTokens?.["--danger"] ?? "#dc2626";
              const bg = previewTokens?.["--background"] ?? "#ffffff";

              return (
                <button
                  key={theme.id}
                  className={`relative overflow-hidden rounded-lg border text-left transition-colors ${
                    isActive
                      ? "border-foreground"
                      : "border-divider hover:bg-default-50"
                  }`}
                  type="button"
                  onClick={() => handleThemeSelect(theme.id)}
                >
                  {/* Colour strip preview */}
                  <div className="flex h-8">
                    <div className="flex-1" style={{ background: primary }} />
                    <div className="flex-1" style={{ background: secondary }} />
                    <div className="flex-1" style={{ background: success }} />
                    <div className="flex-1" style={{ background: danger }} />
                    <div
                      className="flex-1"
                      style={{
                        background: bg,
                        borderLeft: "1px solid rgba(0,0,0,0.06)",
                      }}
                    />
                  </div>

                  {/* Info */}
                  <div className="p-3">
                    <div className="flex items-center gap-2">
                      <p className="text-sm font-semibold text-gray-900 dark:text-white truncate">
                        {theme.name}
                      </p>
                      {isActive && (
                        <span className="shrink-0 px-1.5 py-0.5 rounded text-[10px] font-bold bg-primary/15 text-primary">
                          当前
                        </span>
                      )}
                    </div>
                    {theme.description && (
                      <p className="text-xs text-gray-500 dark:text-gray-400 mt-0.5 truncate">
                        {theme.description}
                      </p>
                    )}
                    <p className="text-[10px] text-gray-400 dark:text-gray-500 mt-1">
                      {theme.author} · v{theme.version}
                    </p>
                  </div>

                  {/* Active indicator dot */}
                  {isActive && (
                    <span className="absolute right-2 top-2 h-1.5 w-1.5 rounded-full bg-foreground" />
                  )}
                </button>
              );
            })}
          </div>
        </div>

        {/* ── Reset ──────────────────────────────────────────── */}
        {activeThemeId && activeThemeId !== "default" && (
          <div className="pt-2">
            <Button
              className="text-gray-500 dark:text-gray-400"
              size="sm"
              variant="light"
              onPress={handleReset}
            >
              ↩ 恢复默认主题
            </Button>
          </div>
        )}
      </CardBody>
    </Card>
  );
};
