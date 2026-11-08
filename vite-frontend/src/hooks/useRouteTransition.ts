import { useEffect, useRef } from "react";
import { useLocation } from "react-router-dom";

/*
 * The phone's page transitions: a page one level down (网站配置, 面板共享
 * under 更多) slides in from the right, going back slides the previous page
 * in from the left, and switching between the tab bar's pages fades.
 *
 * A browser back (iOS swipe back, the back button) gets no animation: the
 * browser already animates it, and a second slide on top looks broken. An
 * in-app back button that goes through history calls markInAppBack() first
 * so its back still slides.
 */

/** Pages below a tab-bar page; every other page is a top-level one. */
const DEPTH: Record<string, number> = {
  "/config": 2,
  "/panel-sharing": 2,
};

const depthOf = (path: string) => DEPTH[path] ?? 1;

// The path last shown, shared by the phone's two layouts (switching between
// them remounts the layout).
let shownPath: string | null = null;
let shownIndex: number | null = null;
let inAppBack = false;

/**
 * Where the current entry sits in the session history: React Router keeps
 * the index in history.state. Going back is the index going down.
 */
export function historyIndex(): number | null {
  const idx = (window.history.state as { idx?: unknown } | null)?.idx;

  return typeof idx === "number" ? idx : null;
}

/** Whether moving from history index `from` to `to` is a browser back. */
export function isBrowserBack(from: number | null, to: number | null) {
  return from !== null && to !== null && to < from && !inAppBack;
}

/** Call right before navigate(-1) from an in-app back button. */
export function markInAppBack() {
  inAppBack = true;
}

/**
 * For a back within one page (the settings groups): whether the last back
 * came from an in-app button, clearing the mark.
 */
export function consumeInAppBack(): boolean {
  const v = inAppBack;

  inAppBack = false;

  return v;
}

export type RouteTransition = "" | "route-push" | "route-pop" | "route-fade";

/** The class for the page container: which way the page that just opened came in. */
export function useRouteTransition(): RouteTransition {
  const { pathname } = useLocation();
  const current = useRef<{ path: string; kind: RouteTransition } | null>(null);

  if (!current.current || current.current.path !== pathname) {
    const prev = shownPath;
    let kind: RouteTransition = "";

    if (
      prev !== null &&
      prev !== pathname &&
      !isBrowserBack(shownIndex, historyIndex())
    ) {
      const from = depthOf(prev);
      const to = depthOf(pathname);

      kind = to > from ? "route-push" : to < from ? "route-pop" : "route-fade";
    }
    current.current = { path: pathname, kind };
  }

  useEffect(() => {
    shownPath = pathname;
    shownIndex = historyIndex();
    inAppBack = false;
  }, [pathname]);

  return current.current.kind;
}
