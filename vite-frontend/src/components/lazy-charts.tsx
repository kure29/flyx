import type { ComponentProps, ComponentType, ReactNode } from "react";

import { Suspense, lazy, useState } from "react";

import { PageLoadingState } from "@/components/page-state";

/**
 * Chart components, loaded only when one is shown. They pull in the chart
 * library (about 100 KB gzipped), which most visits to the pages that use
 * them (in a tab, a modal or a detail view) never need.
 */

function lazyNamed<C extends ComponentType<any>>(
  load: () => Promise<C>,
  fallback: ReactNode,
) {
  const Lazy = lazy(async () => ({ default: await load() }));

  return function LazyChart(props: ComponentProps<C>) {
    return (
      <Suspense fallback={fallback}>
        <Lazy {...props} />
      </Suspense>
    );
  };
}

const loading = <PageLoadingState message="正在加载..." />;

const LazyLatencyTrendModal = lazyNamed(
  () =>
    import("@/components/latency-trend-modal").then((m) => m.LatencyTrendModal),
  null,
);

/**
 * The latency trend modal is always rendered and opens when nodeId is set:
 * its code loads on the first open and then stays, so closing animates.
 */
export function LatencyTrendModal(
  props: ComponentProps<typeof LazyLatencyTrendModal>,
) {
  const [opened, setOpened] = useState(false);

  if (props.nodeId != null && !opened) setOpened(true);
  if (!opened) return null;

  return <LazyLatencyTrendModal {...props} />;
}

export const MonitorView = lazyNamed(
  () => import("@/pages/node/monitor-view").then((m) => m.MonitorView),
  loading,
);

export const TunnelMonitorView = lazyNamed(
  () =>
    import("@/pages/node/tunnel-monitor-view").then((m) => m.TunnelMonitorView),
  loading,
);

export const SubTrafficStats = lazyNamed(
  () =>
    import("@/pages/subscription/traffic-stats").then((m) => m.SubTrafficStats),
  loading,
);
