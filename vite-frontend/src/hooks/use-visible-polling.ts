import { useEffect, useRef } from "react";

/**
 * Pull cached results while visible; resume immediately after returning
 * online. Probe results change every few minutes (or after a reconnect), so
 * 15 seconds keeps them current without loading the panel.
 */
export function useVisiblePolling(
  refresh: () => Promise<void>,
  interval = 15_000,
  enabled = true,
) {
  const refreshRef = useRef(refresh);

  useEffect(() => {
    refreshRef.current = refresh;
  }, [refresh]);

  useEffect(() => {
    if (!enabled) return;
    let stopped = false;
    let running = false;
    let timer: ReturnType<typeof setTimeout>;
    const visible = () =>
      document.visibilityState !== "hidden" && navigator.onLine !== false;
    const schedule = () => {
      clearTimeout(timer);
      if (!stopped && visible()) timer = setTimeout(tick, interval);
    };
    const tick = async () => {
      if (stopped || running || !visible()) return;
      running = true;
      try {
        await refreshRef.current();
      } finally {
        running = false;
        schedule();
      }
    };
    const resume = () => {
      clearTimeout(timer);
      if (visible()) void tick();
    };

    schedule();
    document.addEventListener("visibilitychange", resume);
    window.addEventListener("focus", resume);
    window.addEventListener("online", resume);

    return () => {
      stopped = true;
      clearTimeout(timer);
      document.removeEventListener("visibilitychange", resume);
      window.removeEventListener("focus", resume);
      window.removeEventListener("online", resume);
    };
  }, [interval, enabled]);
}
