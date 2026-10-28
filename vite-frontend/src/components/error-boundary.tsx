import type { ErrorInfo, ReactNode } from "react";

import { Component } from "react";

import { getStoredValue, setStoredValue } from "@/utils/identity-storage";
import { Button } from "@/shadcn-bridge/heroui/button";

/** A page's code file that failed to download (gone after an upgrade). */
function isChunkLoadError(error: unknown): boolean {
  const message = error instanceof Error ? error.message : String(error);

  return /dynamically imported module|Importing a module script failed|error loading dynamically imported module|ChunkLoadError|Unable to preload/i.test(
    message,
  );
}

/**
 * Reloads to pick up a new build, once a minute at most, so a file that is
 * really missing doesn't loop. Shared with the vite:preloadError handler.
 */
export function reloadOnce(): boolean {
  try {
    const last = Number(
      getStoredValue(sessionStorage, "flyx_chunk_reload") || 0,
    );

    if (Date.now() - last < 60000) return false;
    setStoredValue(sessionStorage, "flyx_chunk_reload", String(Date.now()));
  } catch {
    return false;
  }
  window.location.reload();

  return true;
}

/**
 * Shows a way out instead of a blank screen when a page throws while
 * rendering. A page whose code failed to load (usually right after an
 * upgrade) reloads by itself once. Give it a key (the path) to clear the
 * error when the user moves to another page.
 */
export class ErrorBoundary extends Component<
  { children: ReactNode; fullScreen?: boolean },
  { error: unknown }
> {
  state = { error: null as unknown };

  static getDerivedStateFromError(error: unknown) {
    return { error };
  }

  componentDidCatch(error: unknown, info: ErrorInfo) {
    // eslint-disable-next-line no-console
    console.error("page error", error, info.componentStack);
    if (isChunkLoadError(error)) reloadOnce();
  }

  render() {
    const { error } = this.state;

    if (error == null) return this.props.children;
    const chunk = isChunkLoadError(error);
    const detail = error instanceof Error ? error.message : String(error);

    return (
      <div
        className={`flex flex-col items-center justify-center gap-3 px-6 text-center ${this.props.fullScreen ? "min-h-screen" : "h-64"}`}
      >
        <p className="font-medium">
          {chunk ? "页面文件加载失败" : "页面出错了"}
        </p>
        <p className="max-w-md break-all text-xs text-default-500">
          {chunk ? "面板可能刚升级过，或者网络不稳定。重新加载即可。" : detail}
        </p>
        <Button
          color="primary"
          size="sm"
          variant="flat"
          onPress={() => window.location.reload()}
        >
          重新加载
        </Button>
      </div>
    );
  }
}
