import type { NodeNotifyEvents } from "@/utils/notification-events";

export interface NodeApiItem {
  id: number;
  name: string;
  status: number;
  forwardMode?: "agent" | "nftables";
  sshConfig?: NodeSshConfigApiItem | null;
  inx?: number;
  notifyEvents?: NodeNotifyEvents;
  remark?: string;
  expiryTime?: number;
  renewalCycle?: "month" | "quarter" | "year" | "";
  expiryReminderDismissed?: number;
  syncError?: string;
  [key: string]: unknown;
}

export interface UserApiItem {
  id: number;
  user: string;
  name?: string;
  status: number;
  flow: number;
  flowMiB?: number;
  num: number;
  expTime?: number;
  flowResetTime?: number;
  maxConn?: number;
  inFlow?: number;
  outFlow?: number;
  dailyQuotaGB?: number;
  monthlyQuotaGB?: number;
  dailyUsedBytes?: number;
  monthlyUsedBytes?: number;
  disabledByQuota?: number;
  quotaDisabledAt?: number;
  [key: string]: unknown;
}

export interface UserListQuery {
  current?: number;
  size?: number;
  keyword?: string;
  [key: string]: unknown;
}

export interface TunnelApiItem {
  id: number;
  name: string;
  type: number;
  status: number;
  forwardMode?: "agent" | "nftables";
  flow?: number;
  trafficRatio?: number;
  inIp?: string;
  ipPreference?: string;
  probeTargetHost?: string;
  probeTargetPort?: number;
  engine?: string;
  inNodeId?: TunnelChainNodePayload[];
  outNodeId?: TunnelChainNodePayload[];
  chainNodes?: TunnelChainNodePayload[][];
  entryNodeId: number;
  exitNodeId: number;
  inx?: number;
  [key: string]: unknown;
}

export interface ForwardApiItem {
  id: number;
  name: string;
  status: number;
  forwardMode?: "agent" | "nftables";
  tunnelName?: string;
  tunnelTrafficRatio?: number;
  inIp?: string;
  inPort?: number;
  remoteAddr?: string;
  inFlow?: number;
  outFlow?: number;
  userId?: number;
  tunnelId?: number;
  speedId?: number | null;
  ipMaxConn?: number;
  ipSpeedId?: number | null;
  ipSpeedLimitName?: string;
  maxConn?: number;
  proxyProtocol?: number;
  proxyProtocolReceive?: number;
  proxyProtocolSend?: number;
  inx?: number;
  [key: string]: unknown;
}

export interface UserTunnelApiItem {
  id: number;
  name: string;
  tunnelId?: number;
  tunnelName?: string;
  inNodePortSta?: number;
  inNodePortEnd?: number;
  speedId?: number | null;
  [key: string]: unknown;
}

export interface UserTunnelPermissionApiItem {
  id: number;
  userId: number;
  tunnelId: number;
  tunnelName: string;
  status: number;
  flow: number;
  flowMiB?: number;
  num: number;
  expTime: number;
  flowResetTime: number;
  speedId?: number | null;
  speedLimitName?: string;
  inFlow: number;
  outFlow: number;
  tunnelFlow?: number;
  [key: string]: unknown;
}

export interface StatisticsFlowApiItem {
  id: number;
  userId: number;
  flow: number;
  totalFlow: number;
  time: string;
  [key: string]: unknown;
}

export interface SpeedLimitApiItem {
  id: number;
  name: string;
  speed: number;
  status: number;
  createdTime: string;
  updatedTime: string;
  uploadSpeed?: number;
  downloadSpeed?: number;
  [key: string]: unknown;
}

export interface TunnelGroupApiItem {
  id: number;
  name: string;
  status: number;
  tunnelIds: number[];
  tunnelNames: string[];
  createdTime: number;
  [key: string]: unknown;
}

export interface UserGroupApiItem {
  id: number;
  name: string;
  status: number;
  userIds: number[];
  userNames: string[];
  createdTime: number;
  [key: string]: unknown;
}

export interface GroupPermissionApiItem {
  id: number;
  userGroupId: number;
  userGroupName: string;
  tunnelGroupId: number;
  tunnelGroupName: string;
  createdTime: number;
  [key: string]: unknown;
}

export interface TunnelDiagnosisApiItem {
  success: boolean;
  description: string;
  nodeName: string;
  nodeId: string;
  targetIp: string;
  targetPort?: number;
  message?: string;
  averageTime?: number;
  packetLoss?: number;
  fromChainType?: number;
  fromInx?: number;
  toChainType?: number;
  toInx?: number;
  [key: string]: unknown;
}

export interface TunnelDiagnosisApiData {
  tunnelName: string;
  tunnelType: string;
  timestamp: number;
  results: TunnelDiagnosisApiItem[];
}

export interface ForwardDiagnosisApiData {
  forwardName: string;
  timestamp: number;
  results: TunnelDiagnosisApiItem[];
}

export interface NodeReleaseApiItem {
  version: string;
  name: string;
  publishedAt: string;
  prerelease: boolean;
  channel: "stable" | "dev";
}

export interface SystemUpgradeReleaseApiItem {
  version: string;
  name: string;
  publishedAt: string;
  prerelease: boolean;
  channel: "stable" | "dev";
}

export interface UserPackageInfoApiData {
  userInfo: {
    flow: number;
    inFlow: number;
    outFlow: number;
    num: number;
    expTime?: string;
    flowResetTime?: number;
    maxConn?: number;
    [key: string]: unknown;
  };
  tunnelPermissions: UserTunnelPermissionApiItem[];
  forwards: ForwardApiItem[];
  statisticsFlows: StatisticsFlowApiItem[];
  [key: string]: unknown;
}

export interface BatchOperationResult {
  successCount: number;
  failCount: number;
  failures?: BatchOperationFailure[];
  [key: string]: unknown;
}

export interface BatchOperationFailure {
  id?: number;
  name?: string;
  reason?: string;
  [key: string]: unknown;
}

export interface TunnelDeletePreviewForwardApiItem {
  id: number;
  name: string;
  userId: number;
  userName: string;
  inPort: number;
  [key: string]: unknown;
}

export interface TunnelDeletePreviewApiData {
  tunnelId: number;
  tunnelName: string;
  forwardCount: number;
  sampleForwards: TunnelDeletePreviewForwardApiItem[];
  [key: string]: unknown;
}

export interface TunnelBatchDeletePreviewApiData {
  tunnelCount: number;
  totalForwardCount: number;
  items: TunnelDeletePreviewApiData[];
  [key: string]: unknown;
}

export interface TunnelDeleteWithForwardsApiData {
  forwardCount: number;
  migratedCount: number;
  deletedForwardCount: number;
  portAdjustedCount: number;
  warnings?: string[];
  [key: string]: unknown;
}

export interface TunnelBatchDeleteWithForwardsApiData {
  successCount: number;
  failCount: number;
  failures?: BatchOperationFailure[];
  deletedForwardCount: number;
  migratedCount: number;
  portAdjustedCount: number;
  warnings?: string[];
  [key: string]: unknown;
}

export interface UserMutationPayload {
  id?: number;
  user?: string;
  name?: string;
  password?: string;
  status?: number;
  flow?: number;
  num?: number;
  expTime?: number | string;
  flowResetTime?: number;
  maxConn?: number;
  dailyQuotaGB?: number;
  monthlyQuotaGB?: number;
  tunnelFlow?: number;
}

export interface NodeMutationPayload {
  id?: number | null;
  name?: string;
  status?: number;
  forwardMode?: "agent" | "nftables";
  sshConfig?: NodeSshConfigMutationPayload | null;
  inx?: number;
  notifyEvents?: NodeNotifyEvents | null;
  remark?: string;
  expiryTime?: number;
  renewalCycle?: "month" | "quarter" | "year" | "";
  serverIp?: string;
  serverIpV4?: string;
  serverIpV6?: string;
  extraIPs?: string;
  port?: string;
  tcpListenAddr?: string;
  udpListenAddr?: string;
  interfaceName?: string;
  http?: number;
  tls?: number;
  socks?: number;
}

export interface NodeSshConfigApiItem {
  host?: string;
  port?: number;
  username?: string;
  authType?: "password" | "private_key";
  password?: string;
  privateKey?: string;
  passphrase?: string;
  sudoMode?: "none" | "sudo" | "sudo_su";
  [key: string]: unknown;
}

export interface NodeSshConfigMutationPayload {
  host?: string;
  port?: number;
  username?: string;
  authType?: "password" | "private_key";
  password?: string;
  privateKey?: string;
  passphrase?: string;
  sudoMode?: "none" | "sudo" | "sudo_su";
}

export interface TunnelChainNodePayload {
  nodeId: number;
  protocol?: string;
  strategy?: string;
  connectIp?: string;
  chainType?: number;
  inx?: number;
}

export interface TunnelMutationPayload {
  id?: number;
  name?: string;
  type?: number;
  status?: number;
  forwardMode?: "agent" | "nftables";
  flow?: number;
  trafficRatio?: number;
  inIp?: string;
  ipPreference?: string;
  probeTargetHost?: string;
  probeTargetPort?: number;
  engine?: string;
  inNodeId?: TunnelChainNodePayload[];
  outNodeId?: TunnelChainNodePayload[];
  chainNodes?: TunnelChainNodePayload[][];
}

export interface UserQuotaResetPayload {
  userId: number;
  scope?: "daily" | "monthly" | "all";
}

export interface UserTunnelAssignPayload {
  userId?: number;
  id?: number;
  tunnelId?: number;
  flow?: number;
  flowMiB?: number;
  num?: number;
  expTime?: number;
  flowResetTime?: number;
  maxConn?: number;
  status?: number;
  speedId?: number | null;
  tunnels?: Array<{ tunnelId: number; speedId?: number | null }>;
}

export interface UserTunnelListQuery {
  userId?: number;
  tunnelId?: number;
  current?: number;
  size?: number;
}

export interface UserTunnelRemovePayload {
  id?: number;
  userId?: number;
  tunnelId?: number;
}

export interface ForwardMutationPayload {
  id?: number;
  name?: string;
  status?: number;
  forwardMode?: "agent" | "nftables";
  tunnelId?: number | null;
  inIp?: string;
  inPort?: number | null;
  remoteAddr?: string;
  strategy?: string;
  speedId?: number | null;
  ipMaxConn?: number;
  ipSpeedId?: number | null;
  maxConn?: number;
  proxyProtocol?: number;
  proxyProtocolReceive?: number;
  proxyProtocolSend?: number;
}

export interface SpeedLimitMutationPayload {
  id?: number;
  name?: string;
  speed?: number;
  status?: number;
}

export interface UpdatePasswordPayload {
  currentPassword: string;
  newPassword: string;
  newUsername?: string;
}

export interface BackupImportPayload {
  types: string[];
  [key: string]: unknown;
}

export interface NodeMetricApiItem {
  id: number;
  nodeId: number;
  timestamp: number;
  cpuUsage: number;
  memoryUsage: number;
  diskUsage: number;
  netInBytes: number;
  netOutBytes: number;
  netInSpeed: number;
  netOutSpeed: number;
  load1: number;
  load5: number;
  load15: number;
  tcpConns: number;
  udpConns: number;
  uptime: number;
}

export interface TunnelMetricApiItem {
  id: number;
  tunnelId: number;
  nodeId: number;
  timestamp: number;
  bytesIn: number;
  bytesOut: number;
  connections: number;
  errors: number;
  avgLatencyMs: number;
}

export interface ServiceMonitorApiItem {
  id: number;
  name: string;
  // Keep as string for forward-compatibility.
  type: string;
  target: string;
  intervalSec: number;
  timeoutSec: number;
  nodeId: number;
  enabled: number;
  createdTime: number;
  updatedTime: number;
}

export interface ServiceMonitorResultApiItem {
  id: number;
  monitorId: number;
  timestamp: number;
  success: number;
  latencyMs: number;
  statusCode: number;
  errorMessage: string;
}

export interface ServiceMonitorMutationPayload {
  id?: number;
  name: string;
  type: "tcp" | "icmp";
  target: string;
  intervalSec?: number;
  timeoutSec?: number;
  nodeId?: number;
  enabled?: number;
}

export interface ServiceMonitorLimitsApiData {
  checkerScanIntervalSec: number;
  minIntervalSec: number;
  defaultIntervalSec: number;
  minTimeoutSec: number;
  defaultTimeoutSec: number;
  maxTimeoutSec: number;
}

export interface StorageSummaryApiData {
  dbType: string;
  databaseSizeBytes: number;
  databaseSizeText: string;
}

export interface SystemUpgradeCapabilityApiData {
  capable: boolean;
  reasons: string[];
  deployDir: string;
  backendContainer: string;
}

export interface SystemUpgradeVersionApiData {
  currentVersion: string;
  latestVersion: string;
  hasUpdate: boolean;
  channel: "stable" | "dev";
  reason?: string;
  capability: SystemUpgradeCapabilityApiData;
}

export interface SystemUpgradeCheckApiData extends SystemUpgradeVersionApiData {
  releases: SystemUpgradeReleaseApiItem[];
}

export interface SystemUpgradeRunApiData {
  version: string;
  channel: "stable" | "dev";
  composeAsset: string;
  helperContainer: string;
  backendImageId: string;
  message: string;
}

export interface MonitorNodeApiItem {
  id: number;
  inx: number;
  name: string;
  status: number;
  version?: string;
  updatedTime: number;
}

export interface MonitorTunnelApiItem {
  id: number;
  inx: number;
  name: string;
  status: number;
  updatedTime: number;
}

export interface MonitorPermissionApiItem {
  id: number;
  userId: number;
  createdTime: number;
}

export interface MonitorAccessApiData {
  allowed: boolean;
  reason?: string;
}

export interface TunnelQualityHopApiItem {
  fromNodeId: number;
  fromNodeName: string;
  toNodeId: number;
  toNodeName: string;
  latency: number;
  loss: number;
  targetIp?: string;
  targetPort?: number;
}

export interface TunnelQualityCandidateHopApiItem
  extends TunnelQualityHopApiItem {
  fromRole: "entry" | "middle" | "exit";
  toRole: "middle" | "exit" | "target";
  hopIndex: number;
  selected: boolean;
  errorMessage?: string;
}

export interface TunnelQualityChainDetailsApiItem {
  primaryPath?: TunnelQualityHopApiItem[];
  candidateHops?: TunnelQualityCandidateHopApiItem[];
}

export interface TunnelQualityApiItem {
  tunnelId: number;
  entryToExitLatency: number;
  exitToBingLatency: number;
  entryToExitLoss: number;
  exitToBingLoss: number;
  probeTargetHost?: string;
  probeTargetPort?: number;
  success: boolean;
  errorMessage?: string;
  timestamp: number;
  chainDetails?: string;
}

// ---- 代理内核 / 入站 ----

export interface KernelTransportCap {
  type: string;
  securities: string[];
}

export interface KernelProtocolCap {
  protocol: string;
  transports: KernelTransportCap[];
}

export interface KernelInfo {
  type: string;
  name: string;
  implemented: boolean;
  /** Runs realm-engine tunnel forwards; installed by the panel itself. */
  relay?: boolean;
  protocols: KernelProtocolCap[] | null;
  versions?: string[];
  certModes?: string[];
}

/** A preset 分流 category (geosite list, China IPs, BT). */
export interface SplitCategory {
  key: string;
  label: string;
  group: string;
}

/** A 分流 rule as stored on an inbound. */
export interface SplitRule {
  categories?: string[];
  domains?: string[];
  ips?: string[];
  action: "direct" | "block" | "proxy";
  outboundId?: number;
}

export interface KernelCapabilitiesApiData {
  kernels: KernelInfo[];
  ssMethods: string[];
  splitCategories?: SplitCategory[];
  defaults: {
    realitySni: string;
    fingerprint: string;
    ssMethod: string;
    flow: string;
  };
}

export interface KernelApiItem {
  id: number;
  nodeId: number;
  nodeName: string;
  nodeOnline: boolean;
  kernelType: string;
  version: string;
  installedVersion: string;
  status: "pending" | "running" | "stopped" | "error" | string;
  lastError: string;
  synced: boolean;
  enabled: boolean;
  inboundCount: number;
  updatedTime: number;
}

export interface KernelStateApiData {
  type: string;
  running: boolean;
  enabled: boolean;
  pid: number;
  version: string;
  error: string;
  restarts: number;
  startedAt: number;
  logs?: string[];
}

export interface InboundSettings {
  uuid?: string;
  flow?: string;
  password?: string;
  method?: string;
  snellVersion?: number;
  /** Snell v6 traffic shaping: unset (default) or "unshaped". */
  snellMode?: string;
  masquerade?: string;
  backendPort?: number;
  portHopping?: string;
  username?: string;
  decryption?: string;
  encryption?: string;
  privateKey?: string;
  publicKey?: string;
  peerPrivateKey?: string;
  peerPublicKey?: string;
  peerAddress?: string;
  mtu?: number;
}

export interface InboundTransport {
  type: string;
  path?: string;
  host?: string;
  serviceName?: string;
  mode?: string;
}

export interface InboundSecurity {
  type: string;
  sni?: string;
  alpn?: string[];
  fingerprint?: string;
  certMode?: string;
  certFile?: string;
  keyFile?: string;
  realityDest?: string;
  realityPrivateKey?: string;
  realityPublicKey?: string;
  realityShortIds?: string[];
  realityMinClientVer?: string;
  shadowTlsPassword?: string;
  acmeEmail?: string;
  acmeDns?: string;
  acmeDnsKey?: string;
  acmeDnsSecret?: string;
  /** List output only: DNS credentials are stored but not returned. */
  acmeDnsSet?: boolean;
}

export interface InboundProbe {
  ok: boolean;
  skipped?: boolean;
  latencyMs?: number;
  /** Packet loss over the probe's samples, when made by a node agent. */
  lossPct?: number;
  /** Vantage point the probe was made from (e.g. "浙江移动"); empty means the panel server. */
  from?: string;
  error?: string;
  checkedAt: number;
  /** Per reference point results, when the node measured several. */
  points?: InboundProbePoint[];
}

export interface InboundProbePoint {
  name: string;
  /** Chosen in settings to always show on cards. */
  pinned?: boolean;
  ok: boolean;
  latencyMs?: number;
  lossPct?: number;
  error?: string;
  /** Return route to this point, e.g. "电信 CN2 GIA". */
  route?: string;
  /** The IPv6 return route of a dual-stack node, when traced. */
  routeV6?: string;
}

export interface NodeRouteHop {
  ttl: number;
  ip?: string;
  rttMs?: number;
  /** Backbone the hop belongs to: CN2, 163, 4837, 9929, CMI (CT = IPv6 ChinaNet). */
  net?: string;
}

export interface NodeRoutePoint {
  name: string;
  route?: string;
  reached: boolean;
  /** "IPv6" when traced over IPv6. */
  family?: string;
  hops?: NodeRouteHop[];
  error?: string;
  /** A dual-stack node's IPv6 trace to the same point. */
  ipv6?: NodeRoutePoint;
}

export interface LatencyTrendPoint {
  name: string;
  /** Averaged buckets; ms is null when every probe in it failed. */
  samples: { t: number; ms: number | null; lossPct: number }[];
  avgMs: number;
  maxMs: number;
  lossPct: number;
}

export interface LatencyTrend {
  since: number;
  until: number;
  points: LatencyTrendPoint[];
}

export interface NodeRoute {
  checkedAt: number;
  points: NodeRoutePoint[];
}

export interface InboundApiItem {
  /** Local egress address family, independent of the inbound listener. */
  outboundIpStrategy?:
    | "auto"
    | "ipv4_only"
    | "ipv6_only"
    | "prefer_ipv4"
    | "prefer_ipv6";
  id: number;
  kernelId: number;
  kernelType: string;
  kernelStatus: string;
  /** Imported node: not deployed by the panel, only relayed and shared. */
  external: boolean;
  /** Remote subscription the imported node came from. */
  sourceId?: number;
  sourceName?: string;
  /** Imported nodes: protocol summary and flattened parameters. */
  importedSummary?: string;
  importedParams?: [string, string][];
  nodeId: number;
  nodeName: string;
  nodeServerIp: string;
  name: string;
  protocol: string;
  listen: string;
  port: number;
  network: string;
  settings: InboundSettings;
  transport: InboundTransport;
  security: InboundSecurity;
  extra: string;
  shareAddressMode?: "auto" | "ipv4" | "ipv6" | "custom";
  shareAddress?: string;
  shareHost: string;
  remark: string;
  relayOnly: boolean;
  relayCount: number;
  acceptProxyProtocol: boolean;
  relayWhitelist: boolean;
  whitelistExtra: string;
  enabled: boolean;
  uploadBytes: number;
  downloadBytes: number;
  createdTime: number;
  updatedTime: number;
  /** Last latency / reachability check. */
  probe?: InboundProbe;
  /** Inbound this one's traffic is chained through; 0 leaves directly. */
  outboundId: number;
  outboundName?: string;
  /** The chain target was deleted; traffic is dropped until changed. */
  outboundMissing?: boolean;
  /** 分流 rules, tried in order before the default exit. */
  splitRules: SplitRule[];
  /** The user's labels, for telling nodes apart in the list. */
  tags?: string[];
}

export interface InboundMutationPayload {
  outboundIpStrategy?:
    | "auto"
    | "ipv4_only"
    | "ipv6_only"
    | "prefer_ipv4"
    | "prefer_ipv6";
  id?: number;
  nodeId?: number;
  kernelType?: string;
  name: string;
  protocol: string;
  listen: string;
  port: number;
  settings: InboundSettings;
  transport: InboundTransport;
  security: InboundSecurity;
  extra: string;
  shareAddressMode?: "auto" | "ipv4" | "ipv6" | "custom";
  shareHost: string;
  remark: string;
  relayOnly: boolean;
  acceptProxyProtocol: boolean;
  relayWhitelist: boolean;
  whitelistExtra: string;
  outboundId: number;
  splitRules: SplitRule[];
  enabled?: boolean;
}

export interface InboundShareEntry {
  name: string;
  kind: "direct" | "relay";
  host: string;
  port: number;
  uri: string;
  surge: string;
  /** WireGuard client config (wg-quick format). */
  conf?: string;
}

export interface InboundRelayApiItem {
  id: number;
  inboundId: number;
  forwardId: number;
  name: string;
  shareHost: string;
  managed: boolean;
  mode: "port" | "tunnel";
  forwardName: string;
  forwardStatus: number;
  forwardExists: boolean;
  tunnelId: number;
  tunnelName: string;
  remoteAddr: string;
  entries: { Host: string; Port: number }[];
  createdTime: number;
  /** Last measurement of the line, when made. */
  latency?: RelayLatency;
}

/** One leg of a relay line, measured from its first node. */
export interface RelayHop {
  from: string;
  to: string;
  ok: boolean;
  latencyMs?: number;
  lossPct?: number;
  error?: string;
}

/**
 * A relay line's estimated latency to each reference point: the entry
 * node's latency plus every leg to the landing. Without reference points
 * the only point is the path itself.
 */
export interface RelayLatency extends InboundProbe {
  entry?: string;
  entryNodeId?: number;
  hops: RelayHop[];
  pathMs?: number;
}

export interface SubInfoApiData {
  token: string;
  path: string;
}
