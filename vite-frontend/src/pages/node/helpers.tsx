import { useSortable } from "@dnd-kit/sortable";
import { CSS } from "@dnd-kit/utilities";

import {
  getNodeRenewalSnapshot,
  type NodeRenewalCycle,
} from "@/pages/node/renewal";
import { Node, NodeExpiryState } from "@/pages/node/types";
import { latestProbe } from "@/utils/latest-probe";

/** Expiry, realtime-merge and sortable-row helpers for the node page (no React state). */
export const EXPIRING_SOON_DAYS = 7;

export const getNodeReminderEnabled = (node: Node): boolean => {
  return !!node.expiryTime && node.expiryTime > 0 && !!node.renewalCycle;
};

export const getNodeExpiryMeta = (
  timestamp?: number,
  cycle?: NodeRenewalCycle,
) => {
  const renewal = getNodeRenewalSnapshot(timestamp, cycle, EXPIRING_SOON_DAYS);

  if (renewal.state === "unset") {
    return {
      state: "permanent" as NodeExpiryState,
      label: "未设置续费周期",
      tone: "default" as const,
      isHighlighted: false,
      sortWeight: 3,
      nextDueTime: undefined,
    };
  }

  if (renewal.state === "expired") {
    return {
      state: "expired" as NodeExpiryState,
      label: "已过期",
      tone: "danger" as const,
      isHighlighted: true,
      sortWeight: 0,
      nextDueTime: renewal.nextDueTime,
    };
  }

  if (renewal.state === "dueSoon") {
    return {
      state: "expiringSoon" as NodeExpiryState,
      label: renewal.label,
      tone: "warning" as const,
      isHighlighted: true,
      sortWeight: 1,
      nextDueTime: renewal.nextDueTime,
    };
  }

  return {
    state: "healthy" as NodeExpiryState,
    label: renewal.label,
    tone: "success" as const,
    isHighlighted: false,
    sortWeight: 2,
    nextDueTime: renewal.nextDueTime,
  };
};

export const mergeNodeRealtimeState = (
  incomingNode: Node,
  existingNode?: Node,
): Node => {
  return {
    ...incomingNode,
    latency: latestProbe(incomingNode.latency, existingNode?.latency),
    systemInfo: existingNode?.systemInfo ?? incomingNode.systemInfo ?? null,
    copyLoading: existingNode?.copyLoading ?? incomingNode.copyLoading ?? false,
    upgradeLoading:
      existingNode?.upgradeLoading ?? incomingNode.upgradeLoading ?? false,
    rollbackLoading:
      existingNode?.rollbackLoading ?? incomingNode.rollbackLoading ?? false,
  };
};

export const SortableItem = ({
  id,
  children,
}: {
  id: number;
  children: (listeners: any, attributes?: any) => any;
}) => {
  const {
    attributes,
    listeners,
    setNodeRef,
    transform,
    transition,
    isDragging,
  } = useSortable({ id });

  const style: React.CSSProperties = {
    transform: transform
      ? CSS.Transform.toString({
          ...transform,
          x: Math.round(transform.x),
          y: Math.round(transform.y),
        })
      : undefined,
    transition: isDragging ? undefined : transition || undefined,
    opacity: isDragging ? 0.5 : 1,
    willChange: isDragging ? "transform" : undefined,
  };

  return (
    <div ref={setNodeRef} className="overflow-visible h-full" style={style}>
      {children(listeners, attributes)}
    </div>
  );
};
