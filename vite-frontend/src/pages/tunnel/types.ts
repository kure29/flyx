import type { BatchOperationFailure } from "@/api/types";

/** Shapes used by the tunnel page. */
export interface ChainTunnel {
  nodeId: number;
  protocol?: string; // 'tls' | 'wss' | 'tcp' | 'mtls' | 'mwss' | 'mtcp' | 'kcp' - 转发链协议
  strategy?: string; // 'fifo' | 'round' | 'rand' | 'best' - 仅转发链/多出口需要
  chainType?: number; // 1: 入口, 2: 转发链, 3: 出口
  inx?: number; // 转发链序号
  connectIp?: string; // 连接IP（多IP节点指定连接地址）
}

export interface BestExitStateItem {
  ownerNodeId: number;
  ownerNodeName: string;
  ownerRole: "entry" | "chain" | string;
  exitNodeId?: number;
  exitNodeName: string;
  updatedAt?: number;
  reason?: string;
}

export interface BestExitState {
  enabled: boolean;
  summary: string;
  status: "applied" | "waiting" | string;
  updatedAt?: number;
  reason?: string;
  items: BestExitStateItem[];
}

export interface Tunnel {
  id: number;
  inx?: number;
  name: string;
  type: number; // 1: 端口转发, 2: 隧道转发
  forwardMode?: "agent" | "nftables";
  inNodeId: ChainTunnel[]; // 入口节点列表
  outNodeId?: ChainTunnel[]; // 出口节点列表
  chainNodes?: ChainTunnel[][]; // 转发链节点列表，二维数组
  inIp: string;
  outIp?: string;
  protocol?: string;
  flow: number; // 1: 单向, 2: 双向
  trafficRatio: number;
  ipPreference?: string;
  probeTargetHost?: string;
  probeTargetPort?: number;
  /** "" = agent forwarder, "realm" = realm on the entry nodes. */
  engine?: string;
  bestExitState?: BestExitState;
  status: number;
  createdTime: string;
}

export interface Node {
  id: number;
  name: string;
  status: number; // 1: 在线, 0: 离线
  forwardMode?: "agent" | "nftables";
  serverIp?: string;
  serverIpV4?: string;
  serverIpV6?: string;
  extraIPs?: string;
}

export interface TunnelForm {
  id?: number;
  name: string;
  type: number;
  forwardMode?: "agent" | "nftables";
  inNodeId: ChainTunnel[];
  outNodeId?: ChainTunnel[];
  chainNodes?: ChainTunnel[][]; // 转发链节点列表，二维数组，外层是跳数，内层是该跳的节点
  flow: number;
  trafficRatio: number;
  inIp: string; // 入口IP
  ipPreference: string;
  probeTargetHost?: string;
  probeTargetPort?: number;
  engine?: string;
  status: number;
}

export type TunnelForwardMode = NonNullable<TunnelForm["forwardMode"]>;

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

export type TunnelDeleteAction = "replace" | "delete_forwards";

export const EMPTY_BATCH_RESULT_MODAL_STATE: BatchResultModalState = {
  failures: [],
  open: false,
  summary: "",
  title: "",
};

export const DEFAULT_TUNNEL_DELETE_ACTION: TunnelDeleteAction = "replace";
