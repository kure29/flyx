import { type NodeRenewalCycle } from "@/pages/node/renewal";
import { type NodeSystemInfo } from "@/pages/node/system-info";
import { type InboundProbe } from "@/api/types";
import { type NodeNotifyEvents } from "@/utils/notification-events";

/** Shapes used by the node page. */
export interface Node {
  id: number;
  inx?: number;
  name: string;
  /** Latency from the node to each reference point. */
  latency?: InboundProbe;
  /** The node's own reference points; null follows the defaults. */
  latencyTargets?: { points: string[]; pinned: string[] } | null;
  /** Missing event overrides follow the global notification settings. */
  notifyEvents?: NodeNotifyEvents;
  remark?: string;
  expiryTime?: number;
  renewalCycle?: NodeRenewalCycle;
  /** 管理员选的地区（ISO 代码），空为自动识别。 */
  region?: string;
  /** 面板按地址识别出的地区。 */
  regionDetected?: string;
  /** The admin's group ("" = none) and the note shown on the status page. */
  group?: string;
  publicNote?: string;
  /** Price per renewal cycle in currency (ISO 4217, "" = CNY); 0 = none. */
  price?: number;
  currency?: string;
  /** Monthly traffic quota (GB, 0 = none), how it is counted, reset day. */
  trafficQuotaGb?: number;
  trafficCalc?: TrafficCalc;
  trafficResetDay?: number;
  /** Shown on the public status page. */
  statusVisible?: boolean;
  expiryReminderDismissed?: number;
  ip: string;
  serverIp: string;
  serverIpV4?: string;
  serverIpV6?: string;
  port: string;
  tcpListenAddr?: string;
  udpListenAddr?: string;
  extraIPs?: string;
  version?: string;
  /** What the agent reports about the machine (agents 3.9.0 on). */
  hostOs?: string;
  hostKernel?: string;
  hostArch?: string;
  hostCpuName?: string;
  hostVirtualization?: string;
  http?: number; // 0 关 1 开
  tls?: number; // 0 关 1 开
  socks?: number; // 0 关 1 开
  status: number;
  isRemote?: number;
  forwardMode?: "agent" | "nftables";
  sshConfig?: {
    host?: string;
    port?: number;
    username?: string;
    authType?: "password" | "private_key";
    password?: string;
    privateKey?: string;
    passphrase?: string;
    sudoMode?: "none" | "sudo" | "sudo_su";
  } | null;
  remoteUrl?: string;
  syncError?: string;
  connectionStatus: "online" | "offline";
  systemInfo?: NodeSystemInfo | null;
  copyLoading?: boolean;
  upgradeLoading?: boolean;
  rollbackLoading?: boolean;
}

/** How traffic counts against the quota: up + down, the larger, one side. */
export type TrafficCalc = "" | "max" | "up" | "down";

export interface NodeForm {
  id: number | null;
  name: string;
  remark: string;
  group: string;
  publicNote: string;
  statusVisible: boolean;
  /** Typed values; "" is none. */
  price: string;
  currency: string;
  trafficQuotaGb: string;
  trafficCalc: TrafficCalc;
  trafficResetDay: string;
  /** 流量校正: what the provider says was used this period (GB); "" = keep. */
  correctUpGb: string;
  correctDownGb: string;
  notifyEvents: NodeNotifyEvents;
  expiryTime: number;
  renewalCycle: NodeRenewalCycle;
  /** 管理员选的地区（ISO 代码），空为自动识别。 */
  region: string;
  /** 面板按地址识别出的地区，只读。 */
  regionDetected: string;
  serverHost: string;
  serverIpV4: string;
  serverIpV6: string;
  port: string;
  tcpListenAddr: string;
  udpListenAddr: string;
  interfaceName: string;
  extraIPs: string;
  forwardMode: "agent" | "nftables";
  sshHost: string;
  sshPort: string;
  sshUsername: string;
  sshAuthType: "password" | "private_key";
  sshPassword: string;
  sshPrivateKey: string;
  sshPassphrase: string;
  sshSudoMode: "none" | "sudo" | "sudo_su";
  http: number; // 0 关 1 开
  tls: number; // 0 关 1 开
  socks: number; // 0 关 1 开
}

export type NodeTab = "local" | "remote";

export interface RemoteUsageBinding {
  bindingId: number;
  tunnelId: number;
  tunnelName: string;
  chainType: number;
  hopInx: number;
  allocatedPort: number;
  resourceKey: string;
  remoteBindingId: string;
  updatedTime: number;
}

export interface RemoteUsageNode {
  nodeId: number;
  nodeName: string;
  remoteUrl: string;
  shareId: number;
  portRangeStart: number;
  portRangeEnd: number;
  maxBandwidth: number;
  currentFlow: number;
  usedPorts: number[];
  bindings: RemoteUsageBinding[];
  activeBindingNum: number;
  syncError?: string;
}

export type NodeExpiryState =
  | "permanent"
  | "healthy"
  | "expiringSoon"
  | "expired";

export type NodeFilterMode = "all" | "expiringSoon" | "expired" | "withExpiry";
