import { AnimatedPage } from "@/components/animated-page";
import { createTunnelFormDefaults } from "@/pages/tunnel/form";
import {
  ChainTunnel,
  BestExitStateItem,
  BestExitState,
  Tunnel,
  Node,
  TunnelForm,
  TunnelForwardMode,
} from "@/pages/tunnel/types";

/** Defaults, API mapping and best-exit display helpers for the tunnel page (no React state). */
export const DEFAULT_PROBE_TARGET_HOST = "www.bing.com";
export const DEFAULT_PROBE_TARGET_PORT = 443;

export const getTunnelDiagnosisTarget = (tunnel: Tunnel) => ({
  targetIp: tunnel.probeTargetHost || DEFAULT_PROBE_TARGET_HOST,
  targetPort: tunnel.probeTargetPort || DEFAULT_PROBE_TARGET_PORT,
});

export const getTunnelForwardMode = (
  inNodeId: ChainTunnel[],
  nodes: Node[],
): TunnelForwardMode =>
  inNodeId.some((item) => {
    const node = nodes.find((candidate) => candidate.id === item.nodeId);

    return node?.forwardMode === "nftables";
  })
    ? "nftables"
    : "agent";

export const createTypedTunnelFormDefaults = (): TunnelForm =>
  createTunnelFormDefaults() as TunnelForm;

export const TUNNEL_ORDER_KEY = "tunnel-order";

export const isObjectRecord = (
  value: unknown,
): value is Record<string, unknown> =>
  !!value && typeof value === "object" && !Array.isArray(value);

export const toSafeString = (value: unknown): string => {
  if (typeof value === "string") return value;
  if (typeof value === "number" && Number.isFinite(value)) return String(value);

  return "";
};

export const toSafeNumber = (value: unknown): number | undefined => {
  if (typeof value === "number" && Number.isFinite(value)) return value;
  if (typeof value !== "string" || !value.trim()) return undefined;

  const parsed = Number(value);

  return Number.isFinite(parsed) ? parsed : undefined;
};

export const normalizeBestExitStateItem = (
  value: unknown,
): BestExitStateItem | undefined => {
  if (!isObjectRecord(value)) return undefined;

  const ownerNodeId = toSafeNumber(value.ownerNodeId);

  if (ownerNodeId === undefined) return undefined;

  const exitNodeId = toSafeNumber(value.exitNodeId);
  const updatedAt = toSafeNumber(value.updatedAt);
  const reason = toSafeString(value.reason);

  return {
    ownerNodeId,
    ownerNodeName: toSafeString(value.ownerNodeName),
    ownerRole: toSafeString(value.ownerRole),
    ...(exitNodeId !== undefined ? { exitNodeId } : {}),
    exitNodeName: toSafeString(value.exitNodeName),
    ...(updatedAt !== undefined ? { updatedAt } : {}),
    ...(reason ? { reason } : {}),
  };
};

export const normalizeBestExitState = (
  value: unknown,
): BestExitState | undefined => {
  if (!isObjectRecord(value) || value.enabled !== true) return undefined;

  const updatedAt = toSafeNumber(value.updatedAt);
  const reason = toSafeString(value.reason);
  const items = Array.isArray(value.items)
    ? value.items.flatMap((item) => {
        const normalized = normalizeBestExitStateItem(item);

        return normalized ? [normalized] : [];
      })
    : [];

  return {
    enabled: true,
    summary: toSafeString(value.summary),
    status: toSafeString(value.status),
    ...(updatedAt !== undefined ? { updatedAt } : {}),
    ...(reason ? { reason } : {}),
    items,
  };
};

export const bestExitOwnerRoleText = (role?: string) => {
  if (role === "chain") return "中转";

  return "入口";
};

export const bestExitDetailTitle = (state?: BestExitState) => {
  if (!state?.items?.length) return undefined;

  return state.items
    .map(
      (item) =>
        `${bestExitOwnerRoleText(item.ownerRole)} ${item.ownerNodeName || item.ownerNodeId} -> ${item.exitNodeName || "等待探测"}`,
    )
    .join("\n");
};

export const renderBestExitState = (state?: BestExitState) => {
  if (!state?.enabled) return null;

  const isWaiting = state.status === "waiting";
  const summaryText = state.summary || "等待探测";
  const displaySummary =
    isWaiting && !summaryText.includes("等待")
      ? `等待探测 · ${summaryText}`
      : summaryText;
  const className = isWaiting
    ? "border-warning-200/70 bg-warning-50/50 text-warning-700 dark:border-warning-300/20 dark:bg-warning-900/20 dark:text-warning-300"
    : "border-success-200/60 bg-success-50/40 text-success-700 dark:border-success-300/20 dark:bg-success-900/20 dark:text-success-300";

  return (
    <span
      className={`inline-flex max-w-full items-center rounded border px-1.5 py-0.5 text-[11px] leading-4 ${className}`}
      title={bestExitDetailTitle(state)}
    >
      <span className="min-w-0 truncate">最优出口：{displaySummary}</span>
    </span>
  );
};

export const mapTunnelApiItems = (items: any[]): Tunnel[] => {
  return (items || []).map((tunnel) => {
    const { bestExitState: rawBestExitState, ...tunnelFields } = tunnel;
    const bestExitState = normalizeBestExitState(rawBestExitState);

    return {
      ...tunnelFields,
      ...(bestExitState ? { bestExitState } : {}),
      inx: tunnel.inx ?? 0,
      inNodeId: Array.isArray(tunnel.inNodeId) ? tunnel.inNodeId : [],
      outNodeId: Array.isArray(tunnel.outNodeId) ? tunnel.outNodeId : [],
      chainNodes: Array.isArray(tunnel.chainNodes) ? tunnel.chainNodes : [],
      inIp: tunnel.inIp || "",
      flow: tunnel.flow ?? 1,
      trafficRatio: tunnel.trafficRatio ?? 1,
      status: typeof tunnel.status === "number" ? tunnel.status : 0,
      createdTime: tunnel.createdTime || "",
    };
  });
};

export const PageShell = ({
  embedded,
  children,
}: {
  embedded: boolean;
  children: React.ReactNode;
}) =>
  embedded ? (
    <div>{children}</div>
  ) : (
    <AnimatedPage className="px-4 py-6 lg:px-10 lg:py-8">
      {children}
    </AnimatedPage>
  );
