import type {
  BatchOperationResult,
  ForwardDiagnosisApiData,
  ForwardApiItem,
  NodeReleaseApiItem,
  NodeApiItem,
  TunnelBatchDeletePreviewApiData,
  TunnelBatchDeleteWithForwardsApiData,
  TunnelDeletePreviewApiData,
  TunnelDeleteWithForwardsApiData,
  TunnelDiagnosisApiData,
  TunnelApiItem,
  UserTunnelApiItem,
  NodeMutationPayload,
  TunnelMutationPayload,
  ForwardMutationPayload,
  UpdatePasswordPayload,
  BackupImportPayload,
  NodeMetricApiItem,
  TunnelMetricApiItem,
  ServiceMonitorApiItem,
  ServiceMonitorResultApiItem,
  ServiceMonitorLimitsApiData,
  ServiceMonitorMutationPayload,
  MonitorNodeApiItem,
  MonitorTunnelApiItem,
  TunnelQualityApiItem,
  StorageSummaryApiData,
  SystemUpgradeCheckApiData,
  SystemUpgradeRunApiData,
  SystemUpgradeVersionApiData,
  KernelCapabilitiesApiData,
  KernelApiItem,
  KernelStateApiData,
  InboundApiItem,
  InboundProbe,
  LatencyTrend,
  NodeRoute,
  InboundMutationPayload,
  SplitRule,
  InboundShareEntry,
  InboundRelayApiItem,
  SubInfoApiData,
  RelayLatency,
} from "./types";

import axios from "axios";

import Network from "./network";
import { extractApiErrorMessage } from "./error-message";

import { getToken } from "@/utils/session";

export type ReleaseChannel = "stable" | "dev";

// 登陆相关接口
export interface LoginData {
  username: string;
  password: string;
  captchaId: string;
}

export interface LoginResponse {
  token: string;
  role_id: number;
  name: string;
  requirePasswordChange?: boolean;
}

export const login = (data: LoginData) =>
  Network.post<LoginResponse>("/user/login", data);

export interface PasskeyOptions {
  sessionId: string;
  options: { publicKey: Record<string, unknown> };
}

export interface PasskeyItem {
  id: string;
  name: string;
  createdAt: number;
  lastUsedAt: number;
}

export const getPasskeyStatus = () =>
  Network.post<{ enabled: boolean }>("/user/passkey/status");
export const beginPasskeyLogin = () =>
  Network.post<PasskeyOptions>("/user/passkey/login/begin", {});
export const finishPasskeyLogin = (sessionId: string, credential: unknown) =>
  Network.post<LoginResponse>("/user/passkey/login/finish", {
    sessionId,
    credential,
  });
export const beginPasskeyRegistration = (password: string) =>
  Network.post<PasskeyOptions>("/user/passkey/register/begin", { password });
export const finishPasskeyRegistration = (
  sessionId: string,
  credential: unknown,
  name: string,
) =>
  Network.post("/user/passkey/register/finish", {
    sessionId,
    credential,
    name,
  });
export const listPasskeys = () =>
  Network.post<PasskeyItem[]>("/user/passkey/list");
export const deletePasskey = (id: string, password: string) =>
  Network.post("/user/passkey/delete", { id, password });

export interface GithubInfo {
  enabled: boolean;
  bound: boolean;
  login?: string;
  createdAt?: number;
}

/** Which ways in the login page offers; no login needed. */
export const getLoginMethods = () =>
  Network.post<{ password: boolean; github: boolean; passkey: boolean }>(
    "/user/login/methods",
  );
export const getGithubStatus = () =>
  Network.post<{ enabled: boolean }>("/user/github/status");
export const beginGithubLogin = (redirectUri: string) =>
  Network.post<{ url: string }>("/user/github/login/begin", { redirectUri });
export const finishGithubLogin = (code: string, state: string) =>
  Network.post<LoginResponse>("/user/github/login/finish", { code, state });
export const getGithubInfo = () =>
  Network.post<GithubInfo>("/user/github/info");
export const beginGithubBind = (redirectUri: string, password: string) =>
  Network.post<{ url: string }>("/user/github/bind/begin", {
    redirectUri,
    password,
  });
export const finishGithubBind = (code: string, state: string) =>
  Network.post<{ login: string }>("/user/github/bind/finish", { code, state });
export const unbindGithub = (password: string) =>
  Network.post("/user/github/unbind", { password });

// 节点CRUD操作 - 全部使用POST请求
export const createNode = (data: NodeMutationPayload) =>
  Network.post("/node/create", data);
export const getNodeList = () => Network.post<NodeApiItem[]>("/node/list");
/** Each node's traffic since the 1st of this month (viewer's time zone): id → [in, out]. */
export const getNodeMonthTraffic = () =>
  Network.post<Record<string, [number, number]>>("/node/traffic/month", {
    tzOffset: -new Date().getTimezoneOffset(),
  });
export const getDashboardNodeExpiryList = () =>
  Network.post<NodeApiItem[]>("/node/list", {});
export const updateNode = (data: NodeMutationPayload) =>
  Network.post("/node/update", data);
/** Outcome of uninstalling the agent while deleting a server. */
export type NodeUninstallOutcome = "done" | "pending" | "unsupported";
export interface NodeDeleteResult {
  uninstall?: NodeUninstallOutcome;
  warnings?: string[];
}
export interface NodeBatchDeleteResult {
  deleted: number;
  failed: number;
  uninstalled: number;
  pending: number;
  unsupported: number;
  warnings?: string[];
}
/** `uninstall` also removes the agent and what it installed on the server. */
export const deleteNode = (id: number, uninstall = false) =>
  Network.post<NodeDeleteResult>("/node/delete", { id, uninstall });
export interface NodeInstallCommands {
  script: string;
  docker: string;
  dockerPrivileged: string;
  dockerCompose: string;
  dockerComposePrivileged: string;
}
export const getNodeInstallCommands = (
  id: number,
  channel: ReleaseChannel = "stable",
) =>
  Network.post<NodeInstallCommands>("/node/install", {
    id,
    channel,
    mode: "all",
  });
export const updateNodeOrder = (data: {
  nodes: Array<{ id: number; inx: number }>;
}) => Network.post("/node/update-order", data);
export const dismissNodeExpiryReminder = (id: number) =>
  Network.post("/node/dismiss-expiry-reminder", { id });
export const checkNodeStatus = (nodeId?: number) => {
  const params = nodeId ? { nodeId } : {};

  return Network.post("/node/check-status", params);
};

export const upgradeNode = (
  id: number,
  version?: string,
  channel: ReleaseChannel = "stable",
) =>
  Network.post(
    "/node/upgrade",
    { id, version: version || "", channel },
    { timeout: 5 * 60 * 1000 },
  );
export const batchUpgradeNodes = (
  ids: number[],
  version?: string,
  channel: ReleaseChannel = "stable",
) =>
  Network.post(
    "/node/batch-upgrade",
    { ids, version: version || "", channel },
    { timeout: 15 * 60 * 1000 },
  );
export const getNodeReleases = (channel: ReleaseChannel = "stable") =>
  Network.post<NodeReleaseApiItem[]>("/node/releases", { channel });
export const rollbackNode = (id: number) =>
  Network.post("/node/rollback", { id });

export interface NodeTuneSetting {
  key: string;
  current: string;
  target: string;
  supported: boolean;
  /** Already meets the recommendation (limits are only ever raised). */
  ok: boolean;
}
export interface NodeTuneGroup {
  id: string;
  name: string;
  desc: string;
  applied: boolean;
  settings: NodeTuneSetting[];
}
/** The line the buffers are sized for. */
export interface NodeTuneLine {
  bandwidthMbps: number;
  rttMs: number;
}
export interface NodeTuneSizes {
  ramMb: number;
  bdp: number;
  bufMax: number;
  bufDefault: number;
  /** Why bufMax has its value (2×BDP, memory cap or the 4 MB floor). */
  bufReason: string;
  tcpMem: string;
}
export interface NodeTuneStatus {
  automatic?: boolean;
  detection?: {
    bandwidthMbps: number;
    bandwidthSource: string;
    rttSource: string;
    interface: string;
    peer: string;
    port: number;
    family: string;
    measuredAt: number;
    applied: boolean;
    before?: NodeTuneSample;
    single?: NodeTuneSample;
    parallel?: NodeTuneSample;
    shapeMbps: number;
    deepSummary?: string;
    estimatedGB: number;
    warnings?: string[];
  };
  job?: {
    id: string;
    mode: "auto" | "deep" | "restore";
    state: "running" | "succeeded" | "failed" | "cancelled";
    stage: string;
    percent: number;
    message: string;
    startedAt: number;
    finishedAt?: number;
    logs?: string;
  };
  /** Missing from agents before line-based sizing. */
  params?: NodeTuneLine;
  sizes?: NodeTuneSizes;
  kernel: string;
  congestion: string;
  available: string;
  hasBackup: boolean;
  groups: NodeTuneGroup[];
  warnings?: string[];
}
export interface NodeTuneSample {
  mbps: number;
  retransmits: number;
  lossPercent: number;
}
export type NodeTuneAction =
  | "status"
  | "auto"
  | "deep"
  | "cancel"
  | "restoreAuto"
  | "restore";
/** Long operations run on the agent; status requests remain short. */
export const nodeTune = (
  id: number,
  action: NodeTuneAction,
  options: { maxTrafficGB?: number } = {},
) =>
  Network.post<NodeTuneStatus>(
    "/node/tune",
    { id, action, ...options },
    { timeout: 40 * 1000 },
  );
export const testNodeNftables = (nodeId: number) =>
  Network.post("/node/nftables/test", { nodeId });
export const reconcileNodeNftables = (nodeId: number) =>
  Network.post("/node/nftables/reconcile", { nodeId });
export const clearNodeNftables = (nodeId: number) =>
  Network.post("/node/nftables/clear", { nodeId });

// 隧道CRUD操作 - 全部使用POST请求
export const createTunnel = (data: TunnelMutationPayload) =>
  Network.post("/tunnel/create", data);
export const getTunnelList = () =>
  Network.post<TunnelApiItem[]>("/tunnel/list");
export const getTunnelById = (id: number) =>
  Network.post<TunnelApiItem>("/tunnel/get", { id });
export const updateTunnel = (data: TunnelMutationPayload) =>
  Network.post("/tunnel/update", data, { timeout: 120_000 });
export const deleteTunnel = (id: number) =>
  Network.post("/tunnel/delete", { id });
export const previewTunnelDelete = (id: number) =>
  Network.post<TunnelDeletePreviewApiData>("/tunnel/delete-preview", { id });
export const deleteTunnelWithForwards = (data: {
  id: number;
  action: "replace" | "delete_forwards";
  targetTunnelId?: number;
}) =>
  Network.post<TunnelDeleteWithForwardsApiData | BatchOperationResult>(
    "/tunnel/delete-with-forwards",
    data,
  );
export const previewBatchTunnelDelete = (ids: number[]) =>
  Network.post<TunnelBatchDeletePreviewApiData>(
    "/tunnel/batch-delete-preview",
    {
      ids,
    },
  );
export const batchDeleteTunnelsWithForwards = (data: {
  ids: number[];
  action: "replace" | "delete_forwards";
  targetTunnelId?: number;
}) =>
  Network.post<TunnelBatchDeleteWithForwardsApiData>(
    "/tunnel/batch-delete-with-forwards",
    data,
  );
export const diagnoseTunnel = (tunnelId: number) =>
  Network.post<TunnelDiagnosisApiData>(
    "/tunnel/diagnose",
    { tunnelId },
    { timeout: 120 * 1000 },
  );
export const updateTunnelOrder = (data: {
  tunnels: Array<{ id: number; inx: number }>;
}) => Network.post("/tunnel/update-order", data);

export const batchAssignUserTunnel = (data: {
  userId: number;
  tunnels: Array<{ tunnelId: number; speedId?: number | null }>;
}) => Network.post("/tunnel/user/batch-assign", data);
export const userTunnel = () =>
  Network.post<UserTunnelApiItem[]>("/tunnel/user/tunnel");

// 转发CRUD操作 - 全部使用POST请求
export const createForward = (data: ForwardMutationPayload) =>
  Network.post("/forward/create", data);
export const getForwardList = () =>
  Network.post<ForwardApiItem[]>("/forward/list");
export const updateForward = (data: ForwardMutationPayload) =>
  Network.post("/forward/update", data);
export const deleteForward = (id: number) =>
  Network.post("/forward/delete", { id });
export const forceDeleteForward = (id: number) =>
  Network.post("/forward/force-delete", { id });

// 转发服务控制操作 - 通过Java后端接口
export const pauseForwardService = (forwardId: number) =>
  Network.post("/forward/pause", { id: forwardId });
export const resumeForwardService = (forwardId: number) =>
  Network.post("/forward/resume", { id: forwardId });
export const resetForwardFlow = (forwardId: number) =>
  Network.post("/forward/reset-flow", { id: forwardId });

// 转发诊断操作
export const diagnoseForward = (forwardId: number) =>
  Network.post<ForwardDiagnosisApiData>(
    "/forward/diagnose",
    { forwardId },
    { timeout: 120 * 1000 },
  );

// 转发排序操作
export const updateForwardOrder = (data: {
  forwards: Array<{ id: number; inx: number }>;
}) => Network.post("/forward/update-order", data);

// 修改密码接口
export const updatePassword = (data: UpdatePasswordPayload) =>
  Network.post("/user/updatePassword", data);

// 重置流量接口
export const resetUserFlow = (data: { id: number; type: number }) =>
  Network.post("/user/reset", data);

// 网站配置相关接口
export const getConfigs = () =>
  Network.post<Record<string, string>>("/config/list");
export const getConfigByName = (name: string) =>
  Network.post<{ name: string; value: string }>("/config/get", { name });
export const getPublicConfigByName = (name: string) =>
  Network.post<{ name: string; value: string }>("/public/config/get", { name });
export const updateConfigs = (configMap: Record<string, string>) =>
  Network.post("/config/update", configMap);
export const updateConfig = (name: string, value: string) =>
  Network.post("/config/update-single", { name, value });

export const getStorageSummary = () =>
  Network.get<StorageSummaryApiData>("/system/storage");

export const getSystemUpgradeVersion = () =>
  Network.post<SystemUpgradeVersionApiData>("/system/version");

export const checkSystemUpgrade = (channel: ReleaseChannel = "stable") =>
  Network.post<SystemUpgradeCheckApiData>("/system/check-updates", {
    channel,
  });

export const runSystemUpgrade = (
  version?: string,
  channel: ReleaseChannel = "stable",
) =>
  Network.post<SystemUpgradeRunApiData>(
    "/system/upgrade",
    { version: version || "", channel },
    { timeout: 60 * 1000 },
  );

export const exportBackupData = () => Network.post("/backup/export");
export const importBackupData = (data: BackupImportPayload) =>
  Network.post("/backup/import", data);
export const restoreBackupData = (data: BackupImportPayload) =>
  Network.post("/backup/restore", data);

/** Backups uploaded to a private GitHub Gist (see gist_backup.go). */
export interface GistBackupStatus {
  configured: boolean;
  /** The saved token's start and end, e.g. "ghp_••••••••a1b2". */
  tokenHint: string;
  encrypted: boolean;
  /** 0 = automatic upload off. */
  intervalHours: number;
  gistUrl: string;
  /** Last attempt and last upload that made a revision (unix ms). */
  lastAt: number;
  lastUpload: number;
  lastError: string;
}
export interface GistBackupVersion {
  version: string;
  committedAt: number;
}
export interface GithubTokenStatus {
  set: boolean;
  /** Not set here, but GITHUB_TOKEN is set for the panel. */
  fromEnv: boolean;
  repo: string;
}
export const getGithubTokenStatus = () =>
  Network.post<GithubTokenStatus>("/config/github-token/status");
export const saveGithubToken = (data: { token?: string; clear?: boolean }) =>
  Network.post<null>("/config/github-token/save", data);
export const getGistBackupStatus = () =>
  Network.post<GistBackupStatus>("/backup/gist/status");
/** An empty token or password keeps the saved one. */
export const saveGistBackup = (data: {
  token?: string;
  clearToken?: boolean;
  password?: string;
  clearPassword?: boolean;
  intervalHours: number;
}) => Network.post<GistBackupStatus>("/backup/gist/save", data);
export const uploadGistBackup = () =>
  Network.post<{
    result: { uploaded: boolean; message: string };
    status: GistBackupStatus;
  }>("/backup/gist/upload", {}, { timeout: 120000 });
export const getGistBackupVersions = () =>
  Network.post<GistBackupVersion[]>("/backup/gist/versions");
export const restoreGistBackup = (data: {
  version: string;
  password: string;
}) => Network.post("/backup/gist/restore", data, { timeout: 120000 });

// 验证码相关接口
export const checkCaptcha = () => Network.post("/captcha/check");
export const generateCaptcha = () => Network.post(`/captcha/generate`);
export const verifyCaptcha = (data: { captchaId: string; trackData: string }) =>
  Network.post("/captcha/verify", data);

// 批量操作接口
export const batchDeleteForwards = (ids: number[]) =>
  Network.post<BatchOperationResult>("/forward/batch-delete", { ids });
export const batchPauseForwards = (ids: number[]) =>
  Network.post<BatchOperationResult>("/forward/batch-pause", { ids });
export const batchResumeForwards = (ids: number[]) =>
  Network.post<BatchOperationResult>("/forward/batch-resume", { ids });
export const batchDeleteTunnels = (ids: number[]) =>
  Network.post<BatchOperationResult>("/tunnel/batch-delete", { ids });
export const batchDeleteNodes = (ids: number[], uninstall = false) =>
  Network.post<NodeBatchDeleteResult>("/node/batch-delete", { ids, uninstall });
export const batchRedeployForwards = (ids: number[]) =>
  Network.post<BatchOperationResult>("/forward/batch-redeploy", { ids });
export const batchRedeployTunnels = (ids: number[]) =>
  Network.post<BatchOperationResult>("/tunnel/batch-redeploy", { ids });
export const batchChangeTunnel = (data: {
  forwardIds: number[];
  targetTunnelId: number;
}) => Network.post<BatchOperationResult>("/forward/batch-change-tunnel", data);

// 面板共享 (Federation) 接口
export const getPeerShareList = () =>
  Network.post<Array<Record<string, unknown>>>("/federation/share/list");
export const createPeerShare = (data: {
  name: string;
  nodeId: number;
  maxBandwidth?: number;
  expiryTime?: number;
  portRangeStart?: number;
  portRangeEnd?: number;
  allowedDomains?: string;
  allowedIps?: string;
}) => Network.post("/federation/share/create", data);
export const updatePeerShare = (data: {
  id: number;
  name: string;
  maxBandwidth: number;
  expiryTime: number;
  portRangeStart: number;
  portRangeEnd: number;
  allowedDomains: string;
  allowedIps: string;
}) => Network.post("/federation/share/update", data);
export const deletePeerShare = (id: number) =>
  Network.post("/federation/share/delete", { id });
export const resetPeerShareFlow = (id: number) =>
  Network.post("/federation/share/reset-flow", { id });
export const getPeerRemoteUsageList = () =>
  Network.post<Array<Record<string, unknown>>>(
    "/federation/share/remote-usage/list",
  );
export const importRemoteNode = (data: { remoteUrl: string; token: string }) =>
  Network.post("/federation/node/import", data);

export interface BackupTypes {
  users?: boolean;
  nodes?: boolean;
  tunnels?: boolean;
  forwards?: boolean;
  userTunnels?: boolean;
  speedLimits?: boolean;
  tunnelGroups?: boolean;
  userGroups?: boolean;
  permissions?: boolean;
  configs?: boolean;
  inbounds?: boolean;
}

export const exportBackup = async (types: string[] = []) => {
  const token = window.localStorage.getItem("token");
  const baseURL = axios.defaults.baseURL || "/api/v1/";

  const response = await axios.post(
    `${baseURL}/backup/export`,
    { types },
    {
      headers: {
        Authorization: token,
        "Content-Type": "application/json",
      },
      responseType: "blob",
    },
  );

  const url = window.URL.createObjectURL(new Blob([response.data]));
  const link = document.createElement("a");

  link.href = url;
  const timestamp = new Date().toISOString().slice(0, 19).replace(/[:-]/g, "");

  link.setAttribute("download", `backup_${timestamp}.json`);
  document.body.appendChild(link);
  link.click();
  document.body.removeChild(link);
  window.URL.revokeObjectURL(url);
};

export const importBackup = (data: BackupImportPayload) =>
  Network.post("/backup/import", data);

export interface AnnouncementData {
  content: string;
  enabled: number;
  update_time?: number;
}

export const getNodeMetrics = (
  nodeId: number,
  start?: number,
  end?: number,
) => {
  const params: Record<string, string> = {};

  if (start) params.start = String(start);
  if (end) params.end = String(end);

  return Network.get<NodeMetricApiItem[]>(
    `/monitor/nodes/${nodeId}/metrics`,
    params,
    { timeout: 60_000 },
  );
};

export const getNodeMetricsLatest = (nodeId: number) =>
  Network.get<NodeMetricApiItem>(`/monitor/nodes/${nodeId}/metrics/latest`);

export const getTunnelMetrics = (
  tunnelId: number,
  start?: number,
  end?: number,
) => {
  const params: Record<string, string> = {};

  if (start) params.start = String(start);
  if (end) params.end = String(end);

  return Network.get<TunnelMetricApiItem[]>(
    `/monitor/tunnels/${tunnelId}/metrics`,
    params,
  );
};

export const getMonitorTunnels = () =>
  Network.get<MonitorTunnelApiItem[]>("/monitor/tunnels");

export const getMonitorTunnelQuality = () =>
  Network.get<TunnelQualityApiItem[]>("/monitor/tunnels/quality");

export const getMonitorTunnelQualityHistory = (
  tunnelId: number,
  start?: number,
  end?: number,
) => {
  const params: Record<string, string> = {};

  if (start) params.start = String(start);
  if (end) params.end = String(end);

  return Network.get<TunnelQualityApiItem[]>(
    `/monitor/tunnels/${tunnelId}/quality`,
    params,
  );
};

export const getServiceMonitorList = () =>
  Network.get<ServiceMonitorApiItem[]>("/monitor/services");

export const getServiceMonitorLimits = () =>
  Network.get<ServiceMonitorLimitsApiData>("/monitor/services/limits");

export const createServiceMonitor = (data: ServiceMonitorMutationPayload) =>
  Network.post<ServiceMonitorApiItem>("/monitor/services/create", data);

export const updateServiceMonitor = (data: ServiceMonitorMutationPayload) =>
  Network.post<ServiceMonitorApiItem>("/monitor/services/update", data);

export const deleteServiceMonitor = (id: number) =>
  Network.post("/monitor/services/delete", { id });

export const getServiceMonitorResults = (
  monitorId: number,
  options?: { limit?: number; start?: number; end?: number },
) => {
  const params: Record<string, string> = {};

  if (options?.start != null && options?.end != null) {
    params.start = String(options.start);
    params.end = String(options.end);
  } else if (options?.limit != null) {
    params.limit = String(options.limit);
  }

  return Network.get<ServiceMonitorResultApiItem[]>(
    `/monitor/services/${monitorId}/results`,
    params,
  );
};

export const getServiceMonitorLatestResults = () =>
  Network.get<ServiceMonitorResultApiItem[]>(
    "/monitor/services/latest-results",
  );

export const runServiceMonitor = (id: number) =>
  Network.post<ServiceMonitorResultApiItem>("/monitor/services/run", { id });

export const getMonitorNodes = () =>
  Network.get<MonitorNodeApiItem[]>("/monitor/nodes");

// 代理内核 / 入站（节点服务）
export const getKernelCapabilities = () =>
  Network.post<KernelCapabilitiesApiData>("/kernel/capabilities");
export const getKernelList = (nodeId?: number) =>
  Network.post<KernelApiItem[]>("/kernel/list", nodeId ? { nodeId } : {});
export const getKernelVersions = (kernelType: string) =>
  Network.post<
    { version: string; prerelease: boolean; publishedAt?: string }[]
  >("/kernel/versions", { kernelType });

export const installKernel = (data: {
  nodeId: number;
  kernelType: string;
  version: string;
}) => Network.post("/kernel/install", data);
export const controlKernel = (
  id: number,
  action: "start" | "stop" | "restart" | "remove",
  force = false,
) => Network.post("/kernel/control", { id, action, force });
export const getKernelLogs = (id: number) =>
  Network.post<KernelStateApiData>("/kernel/logs", { id });
export const getInboundList = () =>
  Network.post<InboundApiItem[]>("/inbound/list");
export const createInbound = (data: InboundMutationPayload) =>
  Network.post<{ id: number }>("/inbound/create", data);
export const updateInbound = (data: InboundMutationPayload) =>
  Network.post<{ id: number }>("/inbound/update", data);
export const deleteInbound = (id: number) =>
  Network.post("/inbound/delete", { id });
export const batchDeleteInbounds = (ids: number[]) =>
  Network.post<BatchOperationResult>("/inbound/batch-delete", { ids });
export const batchToggleInbounds = (ids: number[], enabled: boolean) =>
  Network.post<BatchOperationResult>("/inbound/batch-toggle", {
    ids,
    enabled,
  });
export interface SubPipeline {
  lines: "" | "relay" | "direct" | "prefer_relay";
  regions: string[];
  protocols: string[];
  include: string;
  exclude: string;
  rename: string;
  addFlag: boolean;
  prefix: string;
  dedupe: boolean;
  sort: "" | "name";
  regionGroups: boolean;
  /** Client-side chain: names matching chainLanding dial through the relay group. */
  chainLanding: string;
  chainRelay: string;
  /** Routing rules, DNS and the picked service groups (Mihomo / sing-box / Surge). */
  rules: boolean;
  ruleGroups: string[];
  /** Nodes picked for the chain, by their name before the pipeline. */
  chainLandingNames: string[];
  chainRelayNames: string[];
  /** Remote sources whose nodes (in the node groups) take a chain role. */
  chainLandingSources: number[];
  chainRelaySources: number[];
  /** Every self-built node is a landing node. */
  chainLandingOwn: boolean;
  /** Nodes set to 不用 that a source or chainLandingOwn would take. */
  chainNoneNames: string[];
  /** Relay nodes stay out of the node lists; they are only in the relay group. */
  chainHideRelays: boolean;
  /** Per chain source (by id): only nodes whose original name matches take its role. */
  chainSourceFilters?: Record<string, string>;
  /** A chain source's nodes that end up without a role are left out. */
  chainDropUnused: boolean;
  /** JavaScript `function operator(nodes)` run after the other steps. */
  script: string;
  /** Subscription template id per client kind; a kind without one gets the built-in profile. */
  templates?: Partial<Record<SubTemplateSlot, number>>;
  /** Expiry and traffic shown as placeholder nodes at the top of the list. */
  infoNodes?: boolean;
}
export interface SubTemplateServiceGroup {
  key: string;
  name: string;
  target: string;
  members: string[];
}
export interface SubTemplateBuilder {
  serviceGroups?: boolean;
  version: 1;
  preset: "minimal" | "balanced" | "full" | "custom";
  groups: Array<{
    name: string;
    type: "select" | "url-test";
    filter: string;
    /** Image URL; empty keeps the default (Auto for url-test, none for select). */
    icon?: string;
  }>;
  rules: Array<{
    type:
      | "category"
      | "domain"
      | "domain_suffix"
      | "domain_keyword"
      | "ip_cidr";
    value: string;
    target: string;
  }>;
  final: string;
  /** Group icon overrides by category key, "__main" or "__final". */
  icons?: Record<string, string>;
}
export interface SubTemplateCategory {
  key: string;
  name: string;
  /** Image URL, also written into exported Mihomo / Surge groups. */
  icon: string;
  target: string;
  presets: string[];
}
export const getSubTemplateCatalog = () =>
  Network.post<SubTemplateCategory[]>("/subscription/template/catalog");

/** An icon subscription (Quantumult X / Loon / BoxJs format) to pick group icons from. */
export interface SubIconLibrary {
  name: string;
  url: string;
  builtin?: boolean;
}
export interface SubIconLibraryIcon {
  name: string;
  url: string;
}
export const getSubIconLibraries = () =>
  Network.post<{
    libraries: SubIconLibrary[];
    /** Default icons for "__main", "__final" and "__auto" (url-test groups). */
    defaults: Record<string, string>;
  }>("/subscription/icon-library/list");
export const getSubIconLibraryIcons = (url: string) =>
  Network.post<{ name: string; icons: SubIconLibraryIcon[] }>(
    "/subscription/icon-library/icons",
    { url },
  );
export const saveSubIconLibrary = (url: string) =>
  Network.post<SubIconLibrary[]>("/subscription/icon-library/save", { url });
export const deleteSubIconLibrary = (url: string) =>
  Network.post<SubIconLibrary[]>("/subscription/icon-library/delete", { url });

export type SubTemplateKind = "clash" | "singbox" | "surge";
/** Clients a subscription picks a template for; Stash and Shadowrocket take Mihomo ("clash") templates. */
export type SubTemplateSlot = SubTemplateKind | "stash" | "shadowrocket";
/** A user's own Mihomo / sing-box / Surge profile that subscriptions render into. */
export interface SubTemplate {
  builder?: SubTemplateBuilder;
  id: number;
  name: string;
  kind: SubTemplateKind;
  content: string;
  /** Downloaded from here once a day; empty for pasted templates. */
  sourceUrl: string;
  lastFetch: number;
  lastError: string;
  updatedTime: number;
  usedBy: number;
  groups: number;
  autoGroups: number;
  rules: number;
}
export interface SubTemplateGroup {
  name: string;
  type: string;
  proxies: string[];
  /** Takes nodes (include-all / {all}); nodes are the ones it gets. */
  auto: boolean;
  nodes: string[] | null;
  /** The filter can't be previewed here (the client still applies it). */
  filterError?: string;
}
export interface SubTemplateSummary {
  groups: SubTemplateGroup[];
  rules: number;
  providers: number;
  warnings: string[];
}
export interface SubTemplateDraft {
  builder?: SubTemplateBuilder;
  id?: number;
  name: string;
  kind: SubTemplateKind;
  content: string;
  sourceUrl: string;
}
export const getSubTemplates = () =>
  Network.post<SubTemplate[]>("/subscription/template/list");
export const checkSubTemplate = (
  data: Omit<SubTemplateDraft, "id" | "name"> & { subscriptionId?: number },
) =>
  Network.post<{
    serviceGroups: SubTemplateServiceGroup[];
    summary: SubTemplateSummary;
    autoGroups: number;
    sample: string;
    nodes: number;
    content: string;
  }>("/subscription/template/check", data);
export const saveSubTemplate = (data: SubTemplateDraft) =>
  Network.post<SubTemplate>("/subscription/template/save", data);
export const refreshSubTemplate = (id: number) =>
  Network.post<SubTemplate>("/subscription/template/refresh", { id });
export const deleteSubTemplate = (id: number) =>
  Network.post("/subscription/template/delete", { id });
export interface SubRuleGroup {
  key: string;
  name: string;
}
export interface SubscriptionItem {
  id: number;
  name: string;
  kind: "own" | "share";
  token: string;
  path: string;
  /** Node groups it lists, in order. */
  groupIds: number[];
  sharedIds: number[];
  options: SubPipeline;
  expireTime: number;
  enabled: boolean;
  active: boolean;
  remark: string;
  nodeCount: number;
  uploadBytes: number;
  downloadBytes: number;
  /** Bytes; 0 = unlimited. */
  trafficLimit: number;
  /** Day of month (1-28) the traffic resets; 0 = never. */
  resetDay: number;
  overQuota: boolean;
  /** When the used traffic started counting: creation or the last reset (ms). */
  trafficSince: number;
  /** Panel user it is assigned to; 0 = none. */
  userId: number;
  userName?: string;
}
export interface SubscriptionDraft {
  id?: number;
  name: string;
  kind: "own" | "share";
  groupIds: number[];
  options: SubPipeline;
  expireTime: number;
  enabled?: boolean;
  remark: string;
  trafficLimit: number;
  resetDay: number;
}
export interface SubPreviewItem {
  name: string;
  original: string;
  region: string;
  protocol: string;
  line: "direct" | "relay";
  chain?: "landing" | "relay";
  /** Remote source of an imported node. */
  sourceId?: number;
  /** A self-built node. */
  own?: boolean;
}
export const getSubscriptions = () =>
  Network.post<{
    items: SubscriptionItem[];
    regions: string[];
    ruleGroups: SubRuleGroup[];
  }>("/subscription/list");
export const saveSubscription = (data: SubscriptionDraft) =>
  Network.post<SubscriptionItem>("/subscription/save", data);
export const deleteSubscription = (id: number) =>
  Network.post("/subscription/delete", { id });
export const resetSubscription = (id: number) =>
  Network.post<SubscriptionItem>("/subscription/reset", { id });
/** A subscription as the user it is assigned to sees it. */
export interface UserSubscription {
  id: number;
  name: string;
  path: string;
  status: "ok" | "disabled" | "expired" | "over_quota";
  expireTime: number;
  nodeCount: number;
  uploadBytes: number;
  downloadBytes: number;
  /** Bytes; 0 = unlimited. */
  trafficLimit: number;
  /** Day of month the traffic resets; 0 = never. */
  resetDay: number;
  /** Next monthly reset (ms); 0 = none. */
  nextResetTime: number;
  /** When the used traffic started counting: creation or the last reset (ms). */
  trafficSince: number;
  /** Routing rules on: some clients import a full config. */
  rules: boolean;
  /** Clients rendered from a template (Shadowrocket then imports a full config). */
  templates?: string[];
}
export type SubTrafficRange = "hour" | "day" | "month";
/** A subscription's traffic: 24 hours, 30 days or 12 months in the viewer's time zone. */
export interface SubTraffic {
  /** Coarse history crossed a local time boundary. */
  estimated?: boolean;
  range: SubTrafficRange;
  /** Each bucket's start (unix ms) and bytes. */
  points: Array<{ time: number; up: number; down: number }>;
  up: number;
  down: number;
  /** When the subscription's used traffic started counting (ms). */
  since: number;
}
const trafficQuery = (id: number, range: SubTrafficRange) => ({
  id,
  range,
  tzOffset: -new Date().getTimezoneOffset(),
});

export const getSubscriptionTraffic = (id: number, range: SubTrafficRange) =>
  Network.post<SubTraffic>("/subscription/traffic", trafficQuery(id, range));
/** What a source's nodes are for in a chain: plain nodes, relays (前置) or landing nodes. */
export type RemoteSourceRole = "" | "relay" | "landing";
export interface RemoteSourceItem {
  id: number;
  name: string;
  url: string;
  /** "file" sources are updated by uploading again. */
  kind: "url" | "file";
  role: RemoteSourceRole;
  /** 0 = refresh by hand only. */
  intervalHours: number;
  relayOnly: boolean;
  enabled: boolean;
  lastFetch: number;
  lastError: string;
  nodeCount: number;
  /** The provider's Subscription-Userinfo header. */
  userinfo: string;
  /** Sent when fetching; "" uses the panel's default (a Mihomo UA). */
  userAgent: string;
}
export interface RemoteSyncResult {
  added: number;
  updated: number;
  removed: number;
  kept: number;
  errors: string[];
}
export const getRemoteSources = () =>
  Network.post<RemoteSourceItem[]>("/subscription/source/list");
export const saveRemoteSource = (data: {
  id?: number;
  name: string;
  url: string;
  kind?: "url" | "file";
  role?: RemoteSourceRole;
  /** The uploaded file (file sources). */
  content?: string;
  intervalHours: number;
  relayOnly: boolean;
  enabled?: boolean;
  userAgent?: string;
}) =>
  Network.post<
    RemoteSourceItem | { source: RemoteSourceItem; result: RemoteSyncResult }
  >("/subscription/source/save", data, { timeout: 60000 });
export const refreshRemoteSource = (id: number) =>
  Network.post<{ source: RemoteSourceItem; result: RemoteSyncResult }>(
    "/subscription/source/refresh",
    { id },
    { timeout: 60000 },
  );
export const deleteRemoteSource = (id: number, keepNodes: boolean) =>
  Network.post("/subscription/source/delete", { id, keepNodes });
export const resetSubscriptionTraffic = (id: number) =>
  Network.post<SubscriptionItem>("/subscription/reset-traffic", { id });
export interface SubPreview {
  items: SubPreviewItem[];
  total: number;
  dropped: number;
  /** Relays of the picked inbounds left out because their forward is paused or deleted. */
  inactiveRelays?: { name: string; reason: string }[];
  /** The picked templates' groups with these nodes. */
  templates?: Partial<
    Record<
      SubTemplateSlot,
      {
        id: number;
        name: string;
        groups?: SubTemplateGroup[];
        warnings?: string[];
        error?: string;
      }
    >
  >;
}
export const previewSubscription = (data: SubscriptionDraft) =>
  Network.post<SubPreview>("/subscription/preview", data);

/** A node group's processing (the node half of SubPipeline). */
export type NodeGroupOptions = Pick<
  SubPipeline,
  | "lines"
  | "regions"
  | "protocols"
  | "include"
  | "exclude"
  | "rename"
  | "addFlag"
  | "prefix"
  | "dedupe"
  | "sort"
  | "script"
>;
/** A reusable node list: picked inbounds and remote sources, processed. */
export interface NodeGroupItem {
  id: number;
  name: string;
  sourceAll: boolean;
  inboundIds: number[];
  sourceIds: number[];
  options: NodeGroupOptions;
  nodeCount: number;
  /** Subscriptions that list it. */
  subscriptions: Array<{ id: number; name: string }>;
}
export interface NodeGroupDraft {
  id?: number;
  name: string;
  sourceAll: boolean;
  inboundIds: number[];
  sourceIds: number[];
  options: NodeGroupOptions;
}
export interface NodeGroupPreview {
  items: SubPreviewItem[];
  total: number;
  dropped: number;
  scriptError: string;
}
export const getNodeGroups = () =>
  Network.post<{ items: NodeGroupItem[]; regions: string[] }>(
    "/subscription/group/list",
  );
export const saveNodeGroup = (data: NodeGroupDraft) =>
  Network.post<NodeGroupItem>("/subscription/group/save", data);
export const deleteNodeGroup = (id: number) =>
  Network.post("/subscription/group/delete", { id });
export const orderNodeGroups = (ids: number[]) =>
  Network.post("/subscription/group/order", { ids });
export const previewNodeGroup = (data: NodeGroupDraft) =>
  Network.post<NodeGroupPreview>("/subscription/group/preview", data);

export const testNotify = (data: {
  telegramToken: string;
  telegramChatId: string;
  telegramApiBase: string;
  webhookUrl: string;
}) => Network.post("/notify/test", data, { timeout: 30000 });
export const probeInbounds = (ids: number[] = []) =>
  Network.post<Record<string, InboundProbe>>(
    "/inbound/probe",
    { ids },
    { timeout: 60000 },
  );
/** Probes the nodes' latency to the reference points now. */
export const probeNodeLatency = (ids: number[] = []) =>
  Network.post<Record<string, InboundProbe>>(
    "/node/latency/probe",
    { ids },
    { timeout: 60000 },
  );
/** Sets the nodes' reference points: their own selection, or the defaults. */
export const setNodeLatencyTargets = (data: {
  ids: number[];
  custom: boolean;
  points: string[];
  pinned: string[];
}) => Network.post("/node/latency/targets", data, { timeout: 60000 });
/** A node's latency to each reference point over the last hours. */
export const getNodeLatencyHistory = (id: number, hours: number) =>
  Network.post<LatencyTrend>("/node/latency/history", { id, hours });
/** A node's last return-route traces to the reference points. */
export const getNodeRoute = (id: number) =>
  Network.post<NodeRoute | null>("/node/route", { id });
/** Traces a node's return routes now (one reference point at a time). */
export const probeNodeRoute = (id: number) =>
  Network.post<NodeRoute | null>(
    "/node/route/probe",
    { id },
    { timeout: 300000 },
  );
export const updateInboundSplit = (data: {
  id: number;
  outboundId: number;
  splitRules: SplitRule[];
  outboundIpStrategy?:
    | "auto"
    | "ipv4_only"
    | "ipv6_only"
    | "prefer_ipv4"
    | "prefer_ipv6";
}) => Network.post("/inbound/split", data);
export const toggleInbound = (id: number, enabled: boolean) =>
  Network.post("/inbound/toggle", { id, enabled });
export const resetInboundTraffic = (id: number) =>
  Network.post("/inbound/reset-traffic", { id });
export const regenerateInbound = (id: number) =>
  Network.post("/inbound/regenerate", { id });
export const importInbounds = (
  links: string,
  relayOnly: boolean,
  tags: string[] = [],
) =>
  Network.post<{ created: number[]; errors: string[] }>("/inbound/import", {
    links,
    relayOnly,
    tags,
  });
export const setInboundTags = (ids: number[], tags: string[]) =>
  Network.post("/inbound/tags", { ids, tags });
export const updateExternalInbound = (data: {
  id: number;
  name: string;
  host: string;
  port: number;
  method?: string;
  password?: string;
  /** Replaces the node's parameters with this share link. */
  link?: string;
  remark: string;
  relayOnly: boolean;
}) => Network.post("/inbound/external/update", data);
export const getInboundShare = (id: number) =>
  Network.post<InboundShareEntry[]>("/inbound/share", { id });
export const getSubInfo = (reset = false) =>
  Network.post<SubInfoApiData>("/inbound/sub/info", { reset });
export const getInboundRelays = (inboundId: number) =>
  Network.post<InboundRelayApiItem[]>("/inbound/relay/list", { inboundId });
/** Measures relay lines (all when ids is empty) and their entry nodes now. */
export const probeInboundRelays = (ids: number[] = []) =>
  Network.post<Record<string, RelayLatency>>(
    "/inbound/relay/probe",
    { ids },
    { timeout: 90000 },
  );
export const createInboundRelay = (data: {
  inboundId: number;
  tunnelId: number;
  inPort?: number;
  name?: string;
  shareHost?: string;
}) => Network.post<{ id: number }>("/inbound/relay/create", data);
/** Renames a relay and changes its share host; the forward is untouched. */
export const updateInboundRelay = (data: {
  id: number;
  name: string;
  shareHost: string;
}) => Network.post("/inbound/relay/update", data);
export const deleteInboundRelay = (id: number, force = false) =>
  Network.post("/inbound/relay/delete", { id, force });

// ---- public status page (no login) ----
export interface PublicStatusAvailability {
  h24: number | null;
  d30: number | null;
  days: Array<number | null>;
  down: Array<number | null>;
}

export interface PublicStatusNode {
  id: number;
  name: string;
  online: boolean;
  availability: PublicStatusAvailability;
  cpu?: number;
  mem?: number;
  disk?: number;
  load?: [number, number, number];
  speedUp?: number;
  speedDown?: number;
  uptime?: number;
  tcp?: number;
  udp?: number;
  /** Capacity, from agents that report it. */
  cores?: number;
  memUsed?: number;
  memTotal?: number;
  diskUsed?: number;
  diskTotal?: number;
  monthUp?: number;
  monthDown?: number;
  expiry?: string;
  /** 国家代码（HK、JP…）：管理员选的，否则按服务器地址识别；没有时不出现。 */
  region?: string;
  ping?: PublicStatusPing[];
  // From agents of 3.9.0 on.
  swap?: number;
  swapUsed?: number;
  swapTotal?: number;
  procs?: number;
  // Only while the admin shows system information.
  os?: string;
  arch?: string;
  virtualization?: string;
  cpuName?: string;
}

/** GET /public/status/history?node=&range= (node is PublicStatusNode.id). */
export type PublicStatusRange = "1h" | "6h" | "24h" | "7d";

export interface PublicStatusMetricPoint {
  t: number;
  cpu: number;
  mem: number;
  swap: number;
  disk: number;
  load: number;
  tcp: number;
  udp: number;
  procs: number;
  /** Absent while the page hides traffic. */
  netIn?: number;
  netOut?: number;
}

export interface PublicStatusMetricHistory {
  range: PublicStatusRange;
  bucketSec: number;
  points: PublicStatusMetricPoint[];
}

/** GET /public/status/ping-history?node=&range= */
export interface PublicStatusPingHistory {
  range: PublicStatusRange;
  bucketSec: number;
  points: Array<{
    name: string;
    samples: Array<{ t: number; ms: number | null; loss: number }>;
  }>;
}

export interface PublicStatusPing {
  name: string;
  /** Latest round, null when it failed. */
  ms: number | null;
  /** Average loss over the hour, percent. */
  loss: number;
  /** The hour's rounds, oldest first; -1 = failed. */
  samples: number[];
  /** Return route line ("CN2 GIA", "9929"…), once traced. */
  route?: string;
  /** A dual-stack node's IPv6 line, when it differs. */
  routeV6?: string;
}

export interface PublicStatus {
  title: string;
  timezone: string;
  generatedAt: number;
  days: string[];
  showTraffic: boolean;
  nodes: PublicStatusNode[];
  totals: {
    nodes: number;
    online: number;
    speedUp: number;
    speedDown: number;
    monthUp: number;
    monthDown: number;
    d30: number | null;
  };
}

export const getPublicStatus = () =>
  Network.get<PublicStatus>("/public/status");

// ---------------------------------------------------------------- status page themes

/** A status page theme as the panel lists it (theme.json plus state). */
export interface StatusTheme {
  name: string;
  short: string;
  description: string;
  version: string;
  author: string;
  url: string;
  flyxApi: number;
  /** The settings form declared in theme.json, passed through unparsed. */
  config?: unknown;
  selected: boolean;
  builtin: boolean;
  preview: boolean;
}

export interface StatusThemeList {
  themes: StatusTheme[];
  apiVersion: number;
}

// Installing from GitHub downloads the package: allow for a slow link.
const THEME_FETCH_TIMEOUT = 150_000;

export const listStatusThemes = () =>
  Network.post<StatusThemeList>("/status-theme/list");
export const selectStatusTheme = (short: string) =>
  Network.post<null>("/status-theme/select", { short });
export const deleteStatusTheme = (short: string) =>
  Network.post<null>("/status-theme/delete", { short });
export const installStatusTheme = (url: string) =>
  Network.post<StatusTheme>(
    "/status-theme/install",
    { url },
    { timeout: THEME_FETCH_TIMEOUT },
  );
export const updateStatusTheme = (short: string) =>
  Network.post<{ updated: boolean; version: string; theme: StatusTheme }>(
    "/status-theme/update",
    { short },
    { timeout: THEME_FETCH_TIMEOUT },
  );

export const getStatusThemeConfig = (short: string) =>
  Network.post<{ theme: string; values: unknown }>("/status-theme/config/get", {
    short,
  });
export const saveStatusThemeConfig = (
  short: string,
  values: Record<string, unknown>,
) => Network.post<null>("/status-theme/config/save", { short, values });

/** Uploads a theme.tar.gz (multipart, so not through Network.post). */
export async function uploadStatusTheme(
  file: File,
): Promise<{ code: number; msg: string; data: StatusTheme | null }> {
  const form = new FormData();

  form.append("file", file);
  try {
    const res = await axios.post("/status-theme/upload", form, {
      timeout: THEME_FETCH_TIMEOUT,
      headers: { Authorization: getToken() ?? "" },
    });

    return res.data;
  } catch (error) {
    return { code: -1, msg: extractApiErrorMessage(error), data: null };
  }
}

/** A theme's thumbnail as an object URL (the endpoint needs the token). */
export async function fetchStatusThemePreview(
  short: string,
): Promise<string | null> {
  try {
    const res = await axios.get<Blob>("/status-theme/preview", {
      params: { short },
      responseType: "blob",
      timeout: 30_000,
      headers: { Authorization: getToken() ?? "" },
    });

    return res.data.type.startsWith("image/")
      ? URL.createObjectURL(res.data)
      : null;
  } catch {
    return null;
  }
}
