import type { BatchOperationFailure } from "@/api/types";

/** Shapes used by the forward page. */
export interface Forward {
  id: number;
  name: string;
  forwardMode?: "agent" | "nftables";
  tunnelId: number;
  tunnelName: string;
  tunnelTrafficRatio?: number;
  inIp: string;
  inPort: number;
  remoteAddr: string;
  interfaceName?: string;
  strategy: string;
  status: number;
  inFlow: number;
  outFlow: number;
  serviceRunning: boolean;
  federationShareFlow?: number;
  maxConn?: number;
  createdTime: string;
  userName?: string;
  userId?: number;
  inx?: number;
  speedId?: number | null;
  ipMaxConn?: number;
  ipSpeedId?: number | null;
  ipSpeedLimitName?: string;
  proxyProtocol?: number;
  proxyProtocolReceive?: number;
  proxyProtocolSend?: number;
}

export interface Tunnel {
  id: number;
  name: string;
  forwardMode?: "agent" | "nftables";
  type?: number;
  inIp?: string;
  inNodeId?: Array<{ nodeId: number }>;
  inNodePortSta?: number;
  inNodePortEnd?: number;
  portRangeMin?: number;
  portRangeMax?: number;
}

export interface Node {
  id: number;
  name?: string;
  serverIp?: string;
  serverIpV4?: string;
  serverIpV6?: string;
  extraIPs?: string;
}

export interface ForwardForm {
  id?: number;
  userId?: number;
  name: string;
  tunnelId: number | null;
  inPort: number | null;
  inIp: string;
  remoteAddr: string;
  interfaceName?: string;
  strategy: string;
  speedId: number | null;
  ipMaxConn?: number;
  ipSpeedId: number | null;
  maxConn?: number;
  proxyProtocol?: number;
  proxyProtocolReceive?: number;
  proxyProtocolSend?: number;
}

export interface ForwardUserGroup {
  userId: number;
  userName: string;
  tunnels: ForwardTunnelGroup[];
}

export interface ForwardTunnelGroup {
  tunnelKey: string;
  tunnelName: string;
  tunnelTrafficRatio?: number;
  items: Forward[];
}

export interface BatchProgressState {
  active: boolean;
  label: string;
  percent: number;
}

export interface BatchResultModalState {
  failures: BatchOperationFailure[];
  open: boolean;
  summary: string;
  title: string;
}

export const EMPTY_BATCH_RESULT_MODAL_STATE: BatchResultModalState = {
  failures: [],
  open: false,
  summary: "",
  title: "",
};

export type ForwardGroupOrderMap = Record<string, string[]>;
export type ForwardGroupCollapsedMap = Record<string, boolean>;

export type ImportFormat = "flyx" | "ny";
