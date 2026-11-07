import type { ForwardApiItem } from "@/api/types";

import {
  Forward,
  ForwardGroupOrderMap,
  ForwardGroupCollapsedMap,
} from "@/pages/forward/types";

/** Grouping, ordering and display helpers for the forward page (no React state). */
export const UNKNOWN_FORWARD_USER_NAME = "未知用户";
export const UNCATEGORIZED_FORWARD_TUNNEL_NAME = "未分类";
export const FORWARD_COMPACT_MODE_CONFIG_KEY = "forward_compact_mode";
export const FORWARD_COMPACT_MODE_EVENT = "forwardCompactModeChanged";
export const FORWARD_GROUP_ORDER_CONFIG_KEY = "forward_group_order_map";
export const FORWARD_GROUP_COLLAPSED_CONFIG_KEY = "forward_group_collapsed_map";
export const FORWARD_GROUP_ORDER_LOCAL_STORAGE_PREFIX = "forward-group-order";
export const FORWARD_GROUP_COLLAPSED_LOCAL_STORAGE_PREFIX =
  "forward-group-collapsed";
export const FORWARD_TUNNEL_GROUP_SORTABLE_PREFIX = "forward-tunnel-group";
export const normalizeForwardUserName = (userName?: string): string => {
  const normalized = (userName || UNKNOWN_FORWARD_USER_NAME).trim();

  return normalized || UNKNOWN_FORWARD_USER_NAME;
};

export const compareForwardUserNameAsc = (a: string, b: string): number => {
  return a.localeCompare(b, "en", {
    sensitivity: "base",
    numeric: true,
  });
};

export const normalizeForwardTunnelName = (tunnelName?: string): string => {
  const normalized = (tunnelName || "").trim();

  return normalized || UNCATEGORIZED_FORWARD_TUNNEL_NAME;
};

export const buildForwardTunnelGroupKey = (tunnelName?: string): string => {
  const normalized = normalizeForwardTunnelName(tunnelName);

  if (normalized === UNCATEGORIZED_FORWARD_TUNNEL_NAME) {
    return "__uncategorized__";
  }

  return normalized.toLocaleLowerCase();
};

export const compareForwardTunnelNameAsc = (a: string, b: string): number => {
  return a.localeCompare(b, "en", {
    sensitivity: "base",
    numeric: true,
  });
};

export const compareForwardTunnelGroupKeyAsc = (
  a: string,
  b: string,
): number => {
  const aIsUncategorized = a === "__uncategorized__";
  const bIsUncategorized = b === "__uncategorized__";

  if (aIsUncategorized !== bIsUncategorized) {
    return aIsUncategorized ? 1 : -1;
  }

  return compareForwardTunnelNameAsc(a, b);
};

export const normalizeTunnelTrafficRatio = (value: unknown): number => {
  if (typeof value === "number" && Number.isFinite(value) && value > 0) {
    return value;
  }

  if (typeof value === "string") {
    const parsed = Number(value);

    if (Number.isFinite(parsed) && parsed > 0) {
      return parsed;
    }
  }

  return 1;
};

export const buildForwardGroupOrderLocalKey = (tokenUserId: number): string => {
  return `${FORWARD_GROUP_ORDER_LOCAL_STORAGE_PREFIX}:u:${tokenUserId}`;
};

export const buildForwardGroupCollapsedLocalKey = (
  tokenUserId: number,
): string => {
  return `${FORWARD_GROUP_COLLAPSED_LOCAL_STORAGE_PREFIX}:u:${tokenUserId}`;
};

export const parsePreferenceMap = <T>(
  raw: string | null,
): Record<string, T> | null => {
  if (!raw) {
    return null;
  }

  try {
    const parsed = JSON.parse(raw);

    if (!parsed || typeof parsed !== "object" || Array.isArray(parsed)) {
      return null;
    }

    return parsed as Record<string, T>;
  } catch {
    return null;
  }
};

export const parseGroupOrderMap = (
  raw: string | null,
): ForwardGroupOrderMap => {
  const parsed = parsePreferenceMap<unknown>(raw);

  if (!parsed) {
    return {};
  }

  const result: ForwardGroupOrderMap = {};

  Object.entries(parsed).forEach(([userId, value]) => {
    if (!Array.isArray(value)) {
      return;
    }

    const keys = value
      .map((item) => (typeof item === "string" ? item.trim() : ""))
      .filter((item) => item !== "");

    if (keys.length > 0) {
      result[userId] = Array.from(new Set(keys));
    }
  });

  return result;
};

export const parseGroupCollapsedMap = (
  raw: string | null,
): ForwardGroupCollapsedMap => {
  const parsed = parsePreferenceMap<unknown>(raw);

  if (!parsed) {
    return {};
  }

  const result: ForwardGroupCollapsedMap = {};

  Object.entries(parsed).forEach(([key, value]) => {
    if (typeof value === "boolean") {
      result[key] = value;
    }
  });

  return result;
};

export const sanitizeGroupOrderMap = (
  source: ForwardGroupOrderMap,
  availableTunnelKeysByUser: Map<number, Set<string>>,
): ForwardGroupOrderMap => {
  const sanitized: ForwardGroupOrderMap = {};

  availableTunnelKeysByUser.forEach((availableKeys, userId) => {
    if (availableKeys.size === 0) {
      return;
    }

    const orderFromSource = source[userId.toString()] || [];
    const used = new Set<string>();
    const merged: string[] = [];

    orderFromSource.forEach((key) => {
      if (!availableKeys.has(key) || used.has(key)) {
        return;
      }

      used.add(key);
      merged.push(key);
    });

    Array.from(availableKeys)
      .sort(compareForwardTunnelGroupKeyAsc)
      .forEach((key) => {
        if (!used.has(key)) {
          used.add(key);
          merged.push(key);
        }
      });

    if (merged.length > 0) {
      sanitized[userId.toString()] = merged;
    }
  });

  return sanitized;
};

export const sanitizeGroupCollapsedMap = (
  source: ForwardGroupCollapsedMap,
  availableCollapseKeys: Set<string>,
): ForwardGroupCollapsedMap => {
  const sanitized: ForwardGroupCollapsedMap = {};

  availableCollapseKeys.forEach((key) => {
    if (source[key] === true) {
      sanitized[key] = true;
    }
  });

  return sanitized;
};

export const buildTunnelGroupCollapseKey = (
  userId: number,
  tunnelKey: string,
): string => {
  return `${userId}:${tunnelKey}`;
};

export const buildTunnelGroupSortableId = (
  userId: number,
  tunnelKey: string,
): string => {
  return `${FORWARD_TUNNEL_GROUP_SORTABLE_PREFIX}:${userId}:${tunnelKey}`;
};

export const parseTunnelGroupSortableId = (
  value: unknown,
): { userId: number; tunnelKey: string } | null => {
  if (typeof value !== "string") {
    return null;
  }

  if (!value.startsWith(`${FORWARD_TUNNEL_GROUP_SORTABLE_PREFIX}:`)) {
    return null;
  }

  const parts = value.split(":");

  if (parts.length < 3) {
    return null;
  }

  const userId = Number(parts[1]);
  const tunnelKey = parts.slice(2).join(":").trim();

  if (!Number.isFinite(userId) || tunnelKey === "") {
    return null;
  }

  return { userId, tunnelKey };
};

export const buildAvailableGroupData = (
  forwards: Forward[],
): {
  availableTunnelKeysByUser: Map<number, Set<string>>;
  availableCollapseKeys: Set<string>;
} => {
  const availableTunnelKeysByUser = new Map<number, Set<string>>();
  const availableCollapseKeys = new Set<string>();

  forwards.forEach((forward) => {
    const userId = forward.userId ?? 0;
    const tunnelKey = buildForwardTunnelGroupKey(forward.tunnelName);

    let set = availableTunnelKeysByUser.get(userId);

    if (!set) {
      set = new Set<string>();
      availableTunnelKeysByUser.set(userId, set);
    }

    set.add(tunnelKey);
    availableCollapseKeys.add(buildTunnelGroupCollapseKey(userId, tunnelKey));
  });

  return { availableTunnelKeysByUser, availableCollapseKeys };
};

export const isSameStringArray = (a: string[], b: string[]): boolean => {
  if (a.length !== b.length) {
    return false;
  }

  for (let i = 0; i < a.length; i += 1) {
    if (a[i] !== b[i]) {
      return false;
    }
  }

  return true;
};

export const isSameGroupOrderMap = (
  a: ForwardGroupOrderMap,
  b: ForwardGroupOrderMap,
): boolean => {
  const aKeys = Object.keys(a).sort(compareForwardTunnelNameAsc);
  const bKeys = Object.keys(b).sort(compareForwardTunnelNameAsc);

  if (!isSameStringArray(aKeys, bKeys)) {
    return false;
  }

  for (const key of aKeys) {
    if (!isSameStringArray(a[key] || [], b[key] || [])) {
      return false;
    }
  }

  return true;
};

export const isSameGroupCollapsedMap = (
  a: ForwardGroupCollapsedMap,
  b: ForwardGroupCollapsedMap,
): boolean => {
  const aKeys = Object.keys(a).sort(compareForwardTunnelNameAsc);
  const bKeys = Object.keys(b).sort(compareForwardTunnelNameAsc);

  if (!isSameStringArray(aKeys, bKeys)) {
    return false;
  }

  for (const key of aKeys) {
    if (a[key] !== b[key]) {
      return false;
    }
  }

  return true;
};

export const normalizeForwardItems = (items: Forward[]): Forward[] => {
  return items.map((forward) => ({
    ...forward,
    serviceRunning: forward.status === 1,
  }));
};

export const mapForwardApiItems = (items: ForwardApiItem[]): Forward[] => {
  return (items || []).map((forward) => ({
    id: forward.id,
    name: forward.name,
    tunnelId: forward.tunnelId ?? 0,
    tunnelName: forward.tunnelName || "",
    tunnelTrafficRatio: normalizeTunnelTrafficRatio(forward.tunnelTrafficRatio),
    inIp: forward.inIp || "",
    inPort: forward.inPort ?? 0,
    remoteAddr: forward.remoteAddr || "",
    strategy: typeof forward.strategy === "string" ? forward.strategy : "fifo",
    status: typeof forward.status === "number" ? forward.status : 0,
    inFlow: forward.inFlow ?? 0,
    outFlow: forward.outFlow ?? 0,
    createdTime:
      typeof forward.createdTime === "string" ? forward.createdTime : "",
    userName:
      typeof forward.userName === "string" ? forward.userName : undefined,
    userId: typeof forward.userId === "number" ? forward.userId : undefined,
    inx: typeof forward.inx === "number" ? forward.inx : undefined,
    proxyProtocol:
      typeof forward.proxyProtocol === "number"
        ? forward.proxyProtocol
        : undefined,
    proxyProtocolReceive:
      typeof forward.proxyProtocolReceive === "number"
        ? forward.proxyProtocolReceive
        : 0,
    proxyProtocolSend:
      typeof forward.proxyProtocolSend === "number"
        ? forward.proxyProtocolSend
        : typeof forward.proxyProtocol === "number"
          ? forward.proxyProtocol
          : 0,
    serviceRunning: forward.status === 1,
  }));
};
