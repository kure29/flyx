import type { InboundProbe, InboundProbePoint } from "@/api/types";

import { ChartLine, ListChecks, Loader2, RefreshCw, Route } from "lucide-react";

import {
  Dropdown,
  DropdownItem,
  DropdownMenu,
  DropdownTrigger,
} from "@/shadcn-bridge/heroui/dropdown";
import { RouteTag } from "@/components/route-modal";

/** Latency below GOOD shows green, below FAIR orange, anything else red. */
export const LATENCY_GOOD_MS = 100;
export const LATENCY_FAIR_MS = 300;

type Tone = "success" | "warning" | "danger" | "default";

export const TONE: Record<Tone, [string, string]> = {
  // Minimal: plain text, colored only when something needs a look.
  success: ["bg-transparent text-foreground", "bg-success"],
  warning: ["bg-transparent text-warning-600", "bg-warning"],
  danger: ["bg-transparent text-danger", "bg-danger"],
  default: ["bg-transparent text-default-400", "bg-default-400"],
};

const RANK: Record<Tone, number> = {
  default: 0,
  success: 1,
  warning: 2,
  danger: 3,
};

export function pointTone(p: InboundProbePoint): Tone {
  if (!p.ok) return "danger";
  const ms = p.latencyMs ?? 0;
  const loss = p.lossPct ?? 0;

  if (ms >= LATENCY_FAIR_MS || loss >= 50) return "danger";
  if (ms >= LATENCY_GOOD_MS || loss > 0) return "warning";

  return "success";
}

/** The probe's reference points; a single-point probe becomes one point. */
export function probePoints(probe?: InboundProbe): InboundProbePoint[] {
  if (!probe || probe.skipped) return [];
  if (probe.points?.length) return probe.points;

  return [
    {
      name: probe.from || "面板服务器",
      ok: probe.ok,
      latencyMs: probe.latencyMs,
      lossPct: probe.lossPct,
      error: probe.error,
    },
  ];
}

// Unreachable sorts above any latency of the same red tone.
const rank = (p: InboundProbePoint) => (p.ok ? RANK[pointTone(p)] : 4);

/** Positive when a looks worse than b: unreachable, worse tone, slower. */
const compare = (a: InboundProbePoint, b: InboundProbePoint) =>
  rank(a) - rank(b) || (a.latencyMs ?? 0) - (b.latencyMs ?? 0);

export function worstPoint(points: InboundProbePoint[]) {
  return points.reduce<InboundProbePoint | undefined>(
    (w, p) => (!w || compare(p, w) > 0 ? p : w),
    undefined,
  );
}

function ago(ts: number) {
  const s = Math.max(0, Math.round((Date.now() - ts) / 1000));

  if (s < 60) return "刚刚";
  if (s < 3600) return `${Math.floor(s / 60)} 分钟前`;

  return `${Math.floor(s / 3600)} 小时前`;
}

function valueText(p: InboundProbePoint) {
  return p.ok ? `${p.latencyMs} ms` : p.error || "连不上";
}

function Pill({
  point,
  label,
  spinning,
}: {
  point: InboundProbePoint;
  /** Name the point; hidden on phones, where rows are narrow. */
  label?: boolean;
  spinning?: boolean;
}) {
  const [pill] = TONE[pointTone(point)];
  const loss = point.ok ? (point.lossPct ?? 0) : 0;

  return (
    <span
      className={`inline-flex shrink-0 items-center gap-1.5 whitespace-nowrap text-sm tabular-nums ${pill}`}
    >
      {spinning && <Loader2 className="h-3 w-3 animate-spin" />}
      {label && (
        <span className="hidden text-xs text-default-400 sm:inline">
          {point.name}
        </span>
      )}
      <span>{valueText(point)}</span>
      {loss > 0 && <span className="text-xs">丢包 {loss}%</span>}
    </span>
  );
}

/** A dual-stack node's IPv6 route, shown only when it differs from IPv4. */
function V6Route({ point }: { point: InboundProbePoint }) {
  if (
    !point.routeV6 ||
    point.routeV6 === "未识别" ||
    point.routeV6 === point.route
  ) {
    return null;
  }

  return (
    <span className="inline-flex items-center gap-0.5">
      <span className="text-[10px] opacity-70">v6</span>
      <RouteTag short route={point.routeV6} />
    </span>
  );
}

const stop = (e: React.SyntheticEvent) => e.stopPropagation();

/**
 * The inbound's latency to each reference point: the worst one and how
 * many others there are; pressing it opens every point's result and a
 * re-probe button.
 */
export function LatencyCell({
  probe,
  probing,
  onProbe,
  onShowRoute,
  onShowTrend,
  onEditTargets,
  title = "到各测试点的延迟",
  footer,
}: {
  probe?: InboundProbe;
  probing?: boolean;
  onProbe: () => void;
  /** Opens the node's return routes; omitted where there is no node. */
  onShowRoute?: () => void;
  /** Opens the node's latency over time. */
  onShowTrend?: () => void;
  /** Opens the node's reference point choice. */
  onEditTargets?: () => void;
  /** Heading of the details. */
  title?: string;
  /** Shown under the points in the details, e.g. a relay line's legs. */
  footer?: React.ReactNode;
}) {
  const points = probePoints(probe);

  if (points.length === 0) {
    return (
      <button
        className={`inline-flex items-center gap-1 rounded-full px-2 py-0.5 text-xs font-medium ${TONE.default[0]}`}
        disabled={probing}
        type="button"
        onClick={(e) => {
          e.stopPropagation();
          onProbe();
        }}
      >
        {probing ? (
          <Loader2 className="h-3 w-3 animate-spin" />
        ) : (
          <RefreshCw className="h-3 w-3" />
        )}
        检测
      </button>
    );
  }

  const worst = worstPoint(points)!;
  const others = points.filter((p) => p !== worst);

  return (
    <Dropdown placement="bottom-end">
      <DropdownTrigger>
        <button
          className="inline-flex max-w-full items-center justify-end gap-1 rounded-lg transition-opacity hover:opacity-80"
          type="button"
          onClick={stop}
          onKeyDown={stop}
        >
          <Pill label={points.length > 1} point={worst} spinning={probing} />
          {/* The other points are in the menu; only flag that they
                exist. */}
          {others.length > 0 && (
            <span className="text-xs text-default-300">+{others.length}</span>
          )}
        </button>
      </DropdownTrigger>
      <DropdownMenu
        aria-label="延迟详情"
        className="w-80 max-w-[calc(100vw-32px)]"
      >
        <div role="presentation" onClick={stop} onPointerDown={stop}>
          <div className="flex items-center justify-between px-2 pb-1.5 pt-1 text-xs text-default-500">
            <span>{title}</span>
            {probe && <span>{ago(probe.checkedAt)}</span>}
          </div>
          <div className="space-y-1 px-2 pb-2">
            {points.map((p) => {
              const [, dot] = TONE[pointTone(p)];

              return (
                <div
                  key={p.name}
                  className="flex items-center justify-between gap-2 text-sm"
                >
                  <span className="flex min-w-0 items-center gap-2">
                    <span className={`h-2 w-2 shrink-0 rounded-full ${dot}`} />
                    <span className="shrink-0">{p.name}</span>
                    <span className="flex min-w-0 flex-wrap items-center gap-1">
                      <RouteTag short route={p.route} />
                      <V6Route point={p} />
                    </span>
                  </span>
                  <span className="shrink-0 font-mono text-xs">
                    <span className={TONE[pointTone(p)][0].split(" ")[1]}>
                      {valueText(p)}
                    </span>
                    {p.ok && (
                      <span className="ml-2 text-default-400">
                        丢包 {p.lossPct ?? 0}%
                      </span>
                    )}
                  </span>
                </div>
              );
            })}
          </div>
          {footer}
          <div className="grid auto-cols-fr grid-flow-col border-t border-divider">
            <DropdownItem
              className="justify-center whitespace-nowrap px-1 text-xs text-primary"
              startContent={
                probing ? (
                  <Loader2 className="h-3.5 w-3.5 animate-spin" />
                ) : (
                  <RefreshCw className="h-3.5 w-3.5" />
                )
              }
              onPress={onProbe}
            >
              {probing ? "检测中" : "重测"}
            </DropdownItem>
            {onShowTrend && (
              <DropdownItem
                closeOnSelect
                className="justify-center whitespace-nowrap px-1 text-xs text-primary"
                startContent={<ChartLine className="h-3.5 w-3.5" />}
                onPress={() => setTimeout(onShowTrend, 0)}
              >
                趋势
              </DropdownItem>
            )}
            {onShowRoute && (
              <DropdownItem
                closeOnSelect
                className="justify-center whitespace-nowrap px-1 text-xs text-primary"
                startContent={<Route className="h-3.5 w-3.5" />}
                onPress={() => setTimeout(onShowRoute, 0)}
              >
                回程
              </DropdownItem>
            )}
            {onEditTargets && (
              <DropdownItem
                closeOnSelect
                className="justify-center whitespace-nowrap px-1 text-xs text-primary"
                startContent={<ListChecks className="h-3.5 w-3.5" />}
                onPress={() => setTimeout(onEditTargets, 0)}
              >
                测试点
              </DropdownItem>
            )}
          </div>
        </div>
      </DropdownMenu>
    </Dropdown>
  );
}
