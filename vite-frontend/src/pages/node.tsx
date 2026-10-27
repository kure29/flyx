import { useState, useEffect, useMemo, useCallback, useRef } from "react";
import toast from "react-hot-toast";
import {
  DndContext,
  KeyboardSensor,
  MouseSensor,
  TouchSensor,
  type DragEndEvent,
  useSensor,
  useSensors,
} from "@dnd-kit/core";
import {
  SortableContext,
  arrayMove,
  rectSortingStrategy,
  sortableKeyboardCoordinates,
} from "@dnd-kit/sortable";
import { GripVertical } from "lucide-react";

import { formatTraffic } from "@/utils/traffic";
import { Button } from "@/shadcn-bridge/heroui/button";
import { Card, CardBody, CardHeader } from "@/shadcn-bridge/heroui/card";
import { Checkbox } from "@/shadcn-bridge/heroui/checkbox";
import { Chip } from "@/shadcn-bridge/heroui/chip";
import { Alert } from "@/shadcn-bridge/heroui/alert";
import { Progress } from "@/shadcn-bridge/heroui/progress";
import {
  createNode,
  getNodeList,
  updateNode,
  deleteNode,
  getNodeInstallCommand,
  updateNodeOrder,
  batchDeleteNodes,
  upgradeNode,
  batchUpgradeNodes,
  getNodeReleases,
  rollbackNode,
  getPeerRemoteUsageList,
  dismissNodeExpiryReminder,
  probeNodeLatency,
  getNodeMonthTraffic,
  type ReleaseChannel,
} from "@/api";
import { LatencyCell, probePoints } from "@/components/latency-cell";
import { RouteModal } from "@/components/route-modal";
import { LatencyTrendModal } from "@/components/lazy-charts";
import { LatencyTargetsModal } from "@/pages/node/latency-targets-modal";
import { PageLoadingState } from "@/components/page-state";
import { ActionSheet } from "@/components/action-sheet";
import {
  EmptyHint,
  ListRow,
  PageHeader,
  PageShell,
  PageTabs,
  PageToolbar,
  ToolbarButton,
  SelectionBar,
} from "@/components/page-kit";
import { CARD_GRID } from "@/components/info-card";
import { useViewMode } from "@/components/view-mode";
import {
  getConnectionStatusMeta,
  getRemoteSyncErrorMessage,
} from "@/pages/node/display";
import { NodeTuneModal } from "@/pages/node/tune-modal";
import { copyWithToast } from "@/utils/clipboard";
import {
  formatNodeRenewalTime,
  getNodeRenewalCycleLabel,
} from "@/pages/node/renewal";
import { buildNodeSystemInfo } from "@/pages/node/system-info";
import { useNodeOfflineTimers } from "@/pages/node/use-node-offline-timers";
import { useNodeRealtime } from "@/pages/node/use-node-realtime";
import {
  markPageCached,
  pageCached,
  useCachedState,
} from "@/hooks/use-cached-state";
import { useVisiblePolling } from "@/hooks/use-visible-polling";
import { latestProbe } from "@/utils/latest-probe";
import { useLocalStorageState } from "@/hooks/use-local-storage-state";
import { loadStoredOrder, saveOrder } from "@/utils/order-storage";
import {
  Node,
  NodeForm,
  NodeTab,
  RemoteUsageNode,
  NodeFilterMode,
} from "@/pages/node/types";
import {
  getNodeReminderEnabled,
  getNodeExpiryMeta,
  mergeNodeRealtimeState,
  SortableItem,
} from "@/pages/node/helpers";
import { NodeFormModal } from "@/pages/node/node-form-modal";
import {
  BatchDeleteNodesModal,
  DeleteNodeModal,
  RollbackNodeModal,
} from "@/pages/node/confirm-modals";
import {
  InstallCommandModal,
  type InstallCommands,
  UpgradeNodeModal,
} from "@/pages/node/agent-modals";
import { NodeFilterModal } from "@/pages/node/filter-modal";
import { NodeLiveStats } from "@/pages/node/node-live-stats";

const PAGE = "node";

export default function NodePage({
  embedded = false,
  addRequest = 0,
  onOpenMonitor,
}: {
  embedded?: boolean;
  /** Bumped by the 节点 page header's 新建节点 when embedded. */
  addRequest?: number;
  /** Opens a node's history charts and service monitors. */
  onOpenMonitor?: (nodeId: number) => void;
}) {
  // A revisit shows the last data at once and reloads in the background;
  // live metrics then arrive over the WebSocket as usual.
  const [nodeList, setNodeList] = useCachedState<Node[]>(PAGE, "nodes", []);
  const [nodeOrder, setNodeOrder] = useCachedState<number[]>(PAGE, "order", []);
  const [loading, setLoading] = useState(() => !pageCached(PAGE));
  const [localSearchKeyword, setLocalSearchKeyword] = useLocalStorageState(
    "node-search-keyword-local",
    "",
  );
  const [remoteSearchKeyword, setRemoteSearchKeyword] = useLocalStorageState(
    "node-search-keyword-remote",
    "",
  );
  const [activeTab, setActiveTab] = useLocalStorageState<NodeTab>(
    "node-active-tab",
    "local",
  );

  // Backward-compat: older versions stored extra tab values.
  useEffect(() => {
    if (activeTab !== "local" && activeTab !== "remote") {
      setActiveTab("local");
    }
  }, [activeTab, setActiveTab]);
  const [remoteUsageMap, setRemoteUsageMap] = useState<
    Record<number, RemoteUsageNode>
  >({});
  const [nodeFilterMode, setNodeFilterMode, resetNodeFilterMode] =
    useLocalStorageState<NodeFilterMode>("node-expiry-filter-mode", "all");
  const [isFilterModalOpen, setIsFilterModalOpen] = useState(false);
  const [dialogVisible, setDialogVisible] = useState(false);
  const [dialogTitle, setDialogTitle] = useState("");
  const [isEdit, setIsEdit] = useState(false);
  const [submitLoading, setSubmitLoading] = useState(false);
  const [deleteModalOpen, setDeleteModalOpen] = useState(false);
  const [deleteLoading, setDeleteLoading] = useState(false);
  const [rollbackModalOpen, setRollbackModalOpen] = useState(false);
  const [tuneNode, setTuneNode] = useState<Node | null>(null);
  const [nodeToRollback, setNodeToRollback] = useState<Node | null>(null);
  const [nodeToDelete, setNodeToDelete] = useState<Node | null>(null);
  const [protocolDisabled, setProtocolDisabled] = useState(false);
  const [protocolDisabledReason, setProtocolDisabledReason] = useState("");
  const [form, setForm] = useState<NodeForm>({
    id: null,
    name: "",
    remark: "",
    notifyEvents: {},
    expiryTime: 0,
    renewalCycle: "",
    serverHost: "",
    serverIpV4: "",
    serverIpV6: "",
    port: "10000-65535",
    tcpListenAddr: "[::]",
    udpListenAddr: "[::]",
    interfaceName: "",
    extraIPs: "",
    forwardMode: "agent",
    sshHost: "",
    sshPort: "22",
    sshUsername: "",
    sshAuthType: "private_key",
    sshPassword: "",
    sshPrivateKey: "",
    sshPassphrase: "",
    sshSudoMode: "none",
    http: 0,
    tls: 0,
    socks: 0,
  });
  const [errors, setErrors] = useState<Record<string, string>>({});
  const isNftablesMode = form.forwardMode === "nftables";
  const protocolControlsDisabled = protocolDisabled || isNftablesMode;
  const protocolControlsReason = isNftablesMode
    ? "nftables 模式不支持 agent 协议开关"
    : protocolDisabledReason || "等待服务器上线后再设置";

  const [selectMode, setSelectMode] = useState(false);
  const [viewMode, setViewMode] = useViewMode("node");
  const card = viewMode === "card";
  const [infoPopoverPlacement, setInfoPopoverPlacement] = useState<
    Record<number, "left" | "bottom">
  >({});

  const updateInfoPopoverPlacement = useCallback(
    (nodeId: number, triggerElement: HTMLElement | null) => {
      if (!triggerElement) {
        return;
      }

      const rect = triggerElement.getBoundingClientRect();
      const cardElement = triggerElement.closest("[data-node-card='true']");
      const cardRect =
        cardElement instanceof HTMLElement
          ? cardElement.getBoundingClientRect()
          : null;
      const estimatedPanelWidth = 288;
      const containerPadding = 16;
      const availableLeftSpace = cardRect
        ? rect.left - cardRect.left
        : rect.left;
      const nextPlacement: "left" | "bottom" =
        availableLeftSpace >= estimatedPanelWidth + containerPadding
          ? "left"
          : "bottom";

      setInfoPopoverPlacement((prev) =>
        prev[nodeId] === nextPlacement
          ? prev
          : { ...prev, [nodeId]: nextPlacement },
      );
    },
    [],
  );

  const [sheetNodeId, setSheetNodeId] = useState<number | null>(null);
  const [probingIds, setProbingIds] = useState<Set<number>>(new Set());
  const [routeNode, setRouteNode] = useState<Node | null>(null);
  const [trendNode, setTrendNode] = useState<Node | null>(null);
  const [targetNodes, setTargetNodes] = useState<Node[]>([]);
  const [selectedIds, setSelectedIds] = useState<Set<number>>(new Set());
  const [batchDeleteModalOpen, setBatchDeleteModalOpen] = useState(false);
  const [batchLoading, setBatchLoading] = useState(false);

  // 安装命令相关状态
  const [installCommandModal, setInstallCommandModal] = useState(false);
  const [installCommands, setInstallCommands] = useState<InstallCommands>({
    script: "",
    docker: "",
  });
  const [currentNodeName, setCurrentNodeName] = useState("");

  // 升级相关状态
  const [upgradeModalOpen, setUpgradeModalOpen] = useState(false);
  const [upgradeTarget, setUpgradeTarget] = useState<"single" | "batch">(
    "single",
  );
  const [upgradeTargetNodeId, setUpgradeTargetNodeId] = useState<number | null>(
    null,
  );
  const [releases, setReleases] = useState<
    Array<{
      version: string;
      name: string;
      publishedAt: string;
      prerelease: boolean;
      channel: ReleaseChannel;
    }>
  >([]);
  const [releasesLoading, setReleasesLoading] = useState(false);
  const [releaseChannel, setReleaseChannel] =
    useState<ReleaseChannel>("stable");
  const [selectedVersion, setSelectedVersion] = useState("");
  const [batchUpgradeLoading, setBatchUpgradeLoading] = useState(false);
  // This month's traffic per node, for the rows.
  const [monthTraffic, setMonthTraffic] = useState<
    Record<string, [number, number]>
  >({});

  useEffect(() => {
    let active = true;
    const load = async () => {
      const res = await getNodeMonthTraffic();

      if (active && res.code === 0 && res.data) setMonthTraffic(res.data);
    };

    void load();
    const timer = window.setInterval(load, 60_000);

    return () => {
      active = false;
      window.clearInterval(timer);
    };
  }, []);

  const [upgradeProgress, setUpgradeProgress] = useState<
    Record<number, { stage: string; percent: number; message: string }>
  >({});

  const handleNodeOffline = useCallback((nodeId: number) => {
    setNodeList((prev) =>
      prev.map((node) => {
        if (node.id !== nodeId) return node;
        if (node.connectionStatus === "offline" && node.systemInfo === null) {
          return node;
        }

        return { ...node, connectionStatus: "offline", systemInfo: null };
      }),
    );
  }, []);

  const { clearOfflineTimer, scheduleNodeOffline } = useNodeOfflineTimers({
    delayMs: 3000,
    onNodeOffline: handleNodeOffline,
  });

  const loadRemoteUsage = useCallback(async () => {
    try {
      const res = await getPeerRemoteUsageList();

      if (res.code === 0 && Array.isArray(res.data)) {
        const nextMap: Record<number, RemoteUsageNode> = {};

        (res.data as unknown as RemoteUsageNode[]).forEach((item) => {
          if (!item || typeof item.nodeId !== "number") return;
          nextMap[item.nodeId] = item;
        });

        setRemoteUsageMap(nextMap);
      }
    } catch {
      // ignore remote usage errors in node page
    }
  }, []);

  // 加载节点列表
  const nodesLoadingRef = useRef(false);
  // A reload asked for during a silent poll (after an edit) runs after it.
  const nodesReloadRef = useRef(false);
  const loadNodes = useCallback(async (options?: { silent?: boolean }) => {
    const silent = options?.silent ?? false;

    if (nodesLoadingRef.current) {
      if (!silent) nodesReloadRef.current = true;

      return;
    }
    nodesLoadingRef.current = true;

    if (!silent) {
      setLoading(true);
    }

    try {
      const res = await getNodeList();

      if (res.code === 0) {
        const nodesData: Node[] = (res.data || []).map((node: any) => ({
          ...node,
          inx: node.inx ?? 0,
          connectionStatus: node.syncError
            ? "offline"
            : node.status === 1
              ? "online"
              : "offline",
          syncError: node.syncError || undefined,
          systemInfo: null,
          copyLoading: false,
        }));

        markPageCached(PAGE);
        setNodeList((prev) => {
          const previousById = new Map(prev.map((node) => [node.id, node]));

          return nodesData.map((node) =>
            mergeNodeRealtimeState(node, previousById.get(node.id)),
          );
        });

        // 优先使用数据库中的 inx 字段进行排序，否则回退到本地排序
        const hasDbOrdering = nodesData.some(
          (n) => n.inx !== undefined && n.inx !== 0,
        );

        if (hasDbOrdering) {
          const dbOrder = [...nodesData]
            .sort((a, b) => (a.inx ?? 0) - (b.inx ?? 0))
            .map((n) => n.id);

          setNodeOrder(dbOrder);
        } else {
          setNodeOrder(
            loadStoredOrder(
              "node-order",
              nodesData.map((n) => n.id),
            ),
          );
        }
      } else {
        if (!silent) {
          toast.error(res.msg || "加载服务器列表失败");
        }
      }
    } catch {
      if (!silent) {
        toast.error("网络错误，请重试");
      }
    } finally {
      nodesLoadingRef.current = false;
      if (!silent) {
        setLoading(false);
      }
      if (nodesReloadRef.current) {
        nodesReloadRef.current = false;
        void loadNodes({ silent: true });
      }
    }
  }, []);

  // 处理WebSocket消息
  const handleWebSocketMessage = (data: any) => {
    const { id, type, data: messageData } = data;
    const nodeId = Number(id);

    if (Number.isNaN(nodeId)) return;

    if (type === "status") {
      if (messageData === 1) {
        clearOfflineTimer(nodeId);
        setNodeList((prev) =>
          prev.map((node) => {
            if (node.id !== nodeId) return node;
            if (node.connectionStatus === "online") return node;

            return { ...node, connectionStatus: "online" };
          }),
        );
      } else {
        // 离线事件做延迟处理，避免短抖动导致频繁闪烁
        scheduleNodeOffline(nodeId);
      }
    } else if (type === "info") {
      clearOfflineTimer(nodeId);
      setNodeList((prev) =>
        prev.map((node) => {
          if (node.id === nodeId) {
            const systemInfo = buildNodeSystemInfo(
              messageData,
              node.systemInfo,
            );

            if (!systemInfo) {
              return node;
            }

            return {
              ...node,
              connectionStatus: "online",
              systemInfo,
            };
          }

          return node;
        }),
      );
    } else if (type === "upgrade_progress") {
      try {
        const progressData =
          typeof messageData === "string"
            ? JSON.parse(messageData)
            : messageData;

        if (progressData?.data) {
          setUpgradeProgress((prev) => ({
            ...prev,
            [nodeId]: {
              stage: progressData.data.stage || "",
              percent: progressData.data.percent || 0,
              message: progressData.message || "",
            },
          }));
        }
      } catch {
        // ignore parse errors
      }
    } else if (type === "metric") {
      clearOfflineTimer(nodeId);
      setNodeList((prev) =>
        prev.map((node) => {
          if (node.id !== nodeId) return node;

          const metric =
            typeof messageData === "string"
              ? JSON.parse(messageData)
              : messageData;

          if (!metric || typeof metric !== "object") return node;

          const incomingUptime = metric.uptime ?? 0;

          return {
            ...node,
            connectionStatus: "online",
            systemInfo: {
              cpuUsage: metric.cpuUsage ?? metric.cpu_usage ?? 0,
              memoryUsage: metric.memoryUsage ?? metric.memory_usage ?? 0,
              uploadTraffic:
                metric.netOutBytes ?? metric.bytes_transmitted ?? 0,
              downloadTraffic: metric.netInBytes ?? metric.bytes_received ?? 0,
              uploadSpeed: metric.netOutSpeed ?? metric.net_out_speed ?? 0,
              downloadSpeed: metric.netInSpeed ?? metric.net_in_speed ?? 0,
              uptime: incomingUptime || node.systemInfo?.uptime || 0,
              diskUsage: metric.diskUsage ?? metric.disk_usage,
              load1: metric.load1,
              load5: metric.load5,
              load15: metric.load15,
              tcpConns: metric.tcpConns ?? metric.tcp_conns,
              udpConns: metric.udpConns ?? metric.udp_conns,
              netInSpeed: metric.netInSpeed ?? metric.net_in_speed,
              netOutSpeed: metric.netOutSpeed ?? metric.net_out_speed,
            },
          };
        }),
      );
    }
  };

  const { wsConnected, wsConnecting, wsStarting, usingPollingFallback } =
    useNodeRealtime({
      onMessage: handleWebSocketMessage,
    });

  useEffect(() => {
    // With cached data on screen, reload without the loading state.
    loadNodes({ silent: pageCached(PAGE) });
    loadRemoteUsage();
  }, [loadNodes, loadRemoteUsage]);

  useEffect(() => {
    setSelectedIds(new Set());
  }, [activeTab]);

  // Metrics arrive over WebSocket, but cached latency is only in node/list.
  useVisiblePolling(
    useCallback(() => loadNodes({ silent: true }), [loadNodes]),
  );

  // 格式化速度
  const formatSpeed = (bytesPerSecond: number): string => {
    if (bytesPerSecond === 0) return "0 B/s";

    const k = 1024;
    const sizes = ["B/s", "KB/s", "MB/s", "GB/s", "TB/s"];
    const i = Math.floor(Math.log(bytesPerSecond) / Math.log(k));

    return (
      parseFloat((bytesPerSecond / Math.pow(k, i)).toFixed(2)) + " " + sizes[i]
    );
  };

  // 格式化开机时间
  const formatUptime = (seconds: number): string => {
    if (seconds === 0) return "-";

    const days = Math.floor(seconds / 86400);
    const hours = Math.floor((seconds % 86400) / 3600);
    const minutes = Math.floor((seconds % 3600) / 60);

    if (days > 0) {
      return `${days}天${hours}小时`;
    } else if (hours > 0) {
      return `${hours}小时${minutes}分钟`;
    } else {
      return `${minutes}分钟`;
    }
  };

  // 格式化流量

  const formatFlow = (bytes: number): string => {
    return formatTraffic(bytes);
  };

  const formatChainType = (chainType: number, hopInx: number) => {
    if (chainType === 1) {
      return "入口服务器";
    }
    if (chainType === 2) {
      return `中继跳点 #${hopInx}`;
    }
    if (chainType === 3) {
      return "出口服务器";
    }

    return "未知链路";
  };

  // IPv4/IPv6 格式验证（仅用于判定地址族）
  const ipv4Regex =
    /^(25[0-5]|2[0-4][0-9]|[01]?[0-9][0-9]?)\.(25[0-5]|2[0-4][0-9]|[01]?[0-9][0-9]?)\.(25[0-5]|2[0-4][0-9]|[01]?[0-9][0-9]?)\.(25[0-5]|2[0-4][0-9]|[01]?[0-9][0-9]?)$/;
  const ipv6Regex =
    /^(([0-9a-fA-F]{1,4}:){7,7}[0-9a-fA-F]{1,4}|([0-9a-fA-F]{1,4}:){1,7}:|([0-9a-fA-F]{1,4}:){1,6}:[0-9a-fA-F]{1,4}|([0-9a-fA-F]{1,4}:){1,5}(:[0-9a-fA-F]{1,4}){1,2}|([0-9a-fA-F]{1,4}:){1,4}(:[0-9a-fA-F]{1,4}){1,3}|([0-9a-fA-F]{1,4}:){1,3}(:[0-9a-fA-F]{1,4}){1,4}|([0-9a-fA-F]{1,4}:){1,2}(:[0-9a-fA-F]{1,4}){1,5}|[0-9a-fA-F]{1,4}:((:[0-9a-fA-F]{1,4}){1,6})|:((:[0-9a-fA-F]{1,4}){1,7}|:)|fe80:(:[0-9a-fA-F]{0,4}){0,4}%[0-9a-zA-Z]{1,}|::(ffff(:0{1,4}){0,1}:){0,1}((25[0-5]|(2[0-4]|1{0,1}[0-9]){0,1}[0-9])\.){3,3}(25[0-5]|(2[0-4]|1{0,1}[0-9]){0,1}[0-9])|([0-9a-fA-F]{1,4}:){1,4}:((25[0-5]|(2[0-4]|1{0,1}[0-9]){0,1}[0-9])\.){3,3}(25[0-5]|(2[0-4]|1{0,1}[0-9]){0,1}[0-9]))$/;

  const validateIpv4Literal = (ip: string): boolean =>
    ipv4Regex.test(ip.trim());
  const validateIpv6Literal = (ip: string): boolean =>
    ipv6Regex.test(ip.trim());

  // Hostname/domain validation (no scheme/port)
  const hostnameRegex =
    /^(?=.{1,253}$)(?:[a-zA-Z0-9](?:[a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?)(?:\.(?:[a-zA-Z0-9](?:[a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?))*$/;
  const validateHostname = (host: string): boolean => {
    const v = host.trim();

    if (!v) return false;
    if (v === "localhost") return true;

    return hostnameRegex.test(v);
  };

  // 验证端口格式：支持 80,443,100-600
  const validatePort = (
    portStr: string,
  ): { valid: boolean; error?: string } => {
    if (!portStr || !portStr.trim()) {
      return { valid: false, error: "请输入端口" };
    }

    const trimmed = portStr.trim();
    const parts = trimmed
      .split(",")
      .map((p) => p.trim())
      .filter((p) => p);

    if (parts.length === 0) {
      return { valid: false, error: "请输入有效的端口" };
    }

    for (const part of parts) {
      // 检查是否是端口范围 (如 100-600)
      if (part.includes("-")) {
        const range = part.split("-").map((p) => p.trim());

        if (range.length !== 2) {
          return { valid: false, error: `端口范围格式错误: ${part}` };
        }

        const start = parseInt(range[0]);
        const end = parseInt(range[1]);

        if (isNaN(start) || isNaN(end)) {
          return { valid: false, error: `端口必须是数字: ${part}` };
        }

        if (start < 1 || start > 65535 || end < 1 || end > 65535) {
          return {
            valid: false,
            error: `端口范围必须在 1-65535 之间: ${part}`,
          };
        }

        if (start >= end) {
          return { valid: false, error: `起始端口必须小于结束端口: ${part}` };
        }
      } else {
        // 单个端口
        const port = parseInt(part);

        if (isNaN(port)) {
          return { valid: false, error: `端口必须是数字: ${part}` };
        }

        if (port < 1 || port > 65535) {
          return { valid: false, error: `端口必须在 1-65535 之间: ${part}` };
        }
      }
    }

    return { valid: true };
  };

  // 表单验证
  const validateForm = (): boolean => {
    const newErrors: Record<string, string> = {};

    if (!form.name.trim()) {
      newErrors.name = "请输入服务器名称";
    } else if (form.name.trim().length < 2) {
      newErrors.name = "服务器名称长度至少2位";
    } else if (form.name.trim().length > 50) {
      newErrors.name = "服务器名称长度不能超过50位";
    }

    if (
      (form.renewalCycle && !form.expiryTime) ||
      (!form.renewalCycle && form.expiryTime)
    ) {
      newErrors.expiryTime = "请同时设置续费周期和续费基准时间";
    }

    const v4 = form.serverIpV4.trim();
    const v6 = form.serverIpV6.trim();
    const host = form.serverHost.trim();

    if (!v4 && !v6 && !host) {
      const msg = "请至少填写一个 IPv4/IPv6 地址或域名";

      newErrors.serverIpV4 = msg;
      newErrors.serverIpV6 = msg;
      newErrors.serverHost = msg;
    } else {
      if (v4 && !validateIpv4Literal(v4)) {
        newErrors.serverIpV4 = "请输入有效的IPv4地址";
      }
      if (v6 && !validateIpv6Literal(v6)) {
        newErrors.serverIpV6 = "请输入有效的IPv6地址";
      }
      if (host && !validateHostname(host)) {
        newErrors.serverHost = "请输入有效的域名/主机名";
      }
    }

    const portValidation = validatePort(form.port);

    if (!portValidation.valid) {
      newErrors.port = portValidation.error || "端口格式错误";
    }

    if (form.forwardMode === "nftables") {
      if (!form.sshHost.trim()) {
        newErrors.sshHost = "请输入 SSH 主机";
      }
      const sshPort = Number(form.sshPort);

      if (!Number.isInteger(sshPort) || sshPort < 1 || sshPort > 65535) {
        newErrors.sshPort = "SSH 端口必须为 1-65535";
      }
      if (!form.sshUsername.trim()) {
        newErrors.sshUsername = "请输入 SSH 用户名";
      }
      if (form.sshAuthType === "password") {
        if (!isEdit && !form.sshPassword.trim()) {
          newErrors.sshPassword = "请输入 SSH 密码";
        }
      } else if (!isEdit && !form.sshPrivateKey.trim()) {
        newErrors.sshPrivateKey = "请输入 SSH 私钥";
      }
    }

    setErrors(newErrors);

    return Object.keys(newErrors).length === 0;
  };

  // 新增节点
  const handleAdd = () => {
    setDialogTitle("新增服务器");
    setIsEdit(false);
    setDialogVisible(true);
    resetForm();
    setProtocolDisabled(true);
    setProtocolDisabledReason("服务器未在线，等待服务器上线后再设置");
  };

  useEffect(() => {
    if (addRequest > 0) handleAdd();
    // Only a new request opens the dialog.
  }, [addRequest]);

  // 编辑节点
  const handleEdit = (node: Node) => {
    setDialogTitle("编辑服务器");
    setIsEdit(true);

    const legacy = (node.serverIp || "").trim();
    const normalizedV4 =
      node.serverIpV4?.trim() || (validateIpv4Literal(legacy) ? legacy : "");
    const normalizedV6 =
      node.serverIpV6?.trim() || (validateIpv6Literal(legacy) ? legacy : "");
    const normalizedHost =
      !normalizedV4 && !normalizedV6 && legacy ? legacy : "";

    setForm({
      id: node.id,
      name: node.name,
      remark: node.remark || "",
      notifyEvents: { ...node.notifyEvents },
      expiryTime: node.expiryTime || 0,
      renewalCycle: node.renewalCycle || "",
      serverHost: normalizedHost,
      serverIpV4: normalizedV4,
      serverIpV6: normalizedV6,
      port: node.port || "10000-65535",
      tcpListenAddr: node.tcpListenAddr || "[::]",
      udpListenAddr: node.udpListenAddr || "[::]",
      interfaceName: (node as any).interfaceName || "",
      extraIPs: node.extraIPs || "",
      forwardMode: node.forwardMode === "nftables" ? "nftables" : "agent",
      sshHost: node.sshConfig?.host || normalizedHost || legacy,
      sshPort: String(node.sshConfig?.port || 22),
      sshUsername: node.sshConfig?.username || "",
      sshAuthType: node.sshConfig?.authType || "private_key",
      sshPassword: node.sshConfig?.password || "",
      sshPrivateKey: node.sshConfig?.privateKey || "",
      sshPassphrase: node.sshConfig?.passphrase || "",
      sshSudoMode: node.sshConfig?.sudoMode || "none",
      http: typeof node.http === "number" ? node.http : 1,
      tls: typeof node.tls === "number" ? node.tls : 1,
      socks: typeof node.socks === "number" ? node.socks : 1,
    });
    const offline = node.connectionStatus !== "online";

    setProtocolDisabled(offline || node.forwardMode === "nftables");
    setProtocolDisabledReason(
      node.forwardMode === "nftables"
        ? "nftables 模式不支持 agent 协议开关"
        : offline
          ? "服务器未在线，等待服务器上线后再设置"
          : "",
    );
    setDialogVisible(true);
  };

  // 删除节点
  const handleDelete = (node: Node) => {
    setNodeToDelete(node);
    setDeleteModalOpen(true);
  };

  const confirmDelete = async () => {
    if (!nodeToDelete) return;

    setDeleteLoading(true);
    try {
      const res = await deleteNode(nodeToDelete.id);

      if (res.code === 0) {
        toast.success("删除成功");
        setNodeList((prev) => prev.filter((n) => n.id !== nodeToDelete.id));
        setDeleteModalOpen(false);
        setNodeToDelete(null);
      } else {
        toast.error(res.msg || "删除失败");
      }
    } catch {
      toast.error("网络错误，请重试");
    } finally {
      setDeleteLoading(false);
    }
  };

  const handleDismissExpiryReminder = async (nodeId: number) => {
    try {
      const res = await dismissNodeExpiryReminder(nodeId);

      if (res.code === 0) {
        setNodeList((prev) =>
          prev.map((n) =>
            n.id === nodeId ? { ...n, expiryReminderDismissed: 1 } : n,
          ),
        );
      } else {
        toast.error(res.msg || "操作失败");
      }
    } catch {
      toast.error("网络错误，请重试");
    }
  };

  // Only the stable channel is published, so there is nothing to choose.
  const openInstallSelector = (node: Node) =>
    handleCopyInstallCommand(node, "stable");

  // 复制安装命令
  const handleCopyInstallCommand = async (
    node: Node,
    channel: ReleaseChannel,
  ) => {
    setNodeList((prev) =>
      prev.map((n) => (n.id === node.id ? { ...n, copyLoading: true } : n)),
    );

    try {
      const [res, dockerRes] = await Promise.all([
        getNodeInstallCommand(node.id, channel),
        getNodeInstallCommand(node.id, channel, "docker").catch(() => null),
      ]);

      if (res.code === 0 && res.data) {
        setInstallCommands({
          script: res.data,
          docker: dockerRes?.code === 0 ? dockerRes.data || "" : "",
        });
        setCurrentNodeName(node.name);
        setInstallCommandModal(true);
      } else {
        toast.error(res.msg || "获取安装命令失败");
      }
    } catch {
      toast.error("获取安装命令失败");
    } finally {
      setNodeList((prev) =>
        prev.map((n) => (n.id === node.id ? { ...n, copyLoading: false } : n)),
      );
    }
  };

  const handleCopyCommand = async (command: string) => {
    if (await copyWithToast(command, "安装命令已复制到剪贴板")) {
      setInstallCommandModal(false);
    }
  };

  const loadReleasesByChannel = useCallback(async (channel: ReleaseChannel) => {
    setReleasesLoading(true);
    try {
      const res = await getNodeReleases(channel);

      if (res.code === 0 && Array.isArray(res.data)) {
        setReleases(res.data);
      } else {
        toast.error(res.msg || "获取版本列表失败");
      }
    } catch {
      toast.error("获取版本列表失败");
    } finally {
      setReleasesLoading(false);
    }
  }, []);

  // 打开版本选择弹窗
  const openUpgradeModal = async (
    target: "single" | "batch",
    nodeId?: number,
  ) => {
    const defaultChannel: ReleaseChannel = "stable";

    setUpgradeTarget(target);
    setUpgradeTargetNodeId(nodeId || null);
    setReleaseChannel(defaultChannel);
    setSelectedVersion("");
    setUpgradeModalOpen(true);
    await loadReleasesByChannel(defaultChannel);
  };

  // 确认升级（从版本弹窗）
  const handleConfirmUpgrade = async () => {
    const version = selectedVersion || undefined;

    if (upgradeTarget === "single" && upgradeTargetNodeId) {
      setUpgradeModalOpen(false);
      // Find the node
      const node = nodeList.find((n) => n.id === upgradeTargetNodeId);

      if (!node) return;
      setNodeList((prev) =>
        prev.map((n) =>
          n.id === upgradeTargetNodeId ? { ...n, upgradeLoading: true } : n,
        ),
      );
      try {
        const res = await upgradeNode(
          upgradeTargetNodeId,
          version,
          releaseChannel,
        );

        if (res.code === 0) {
          toast.success(`服务器升级命令已发送，服务器将自动重启`);
        } else {
          toast.error(res.msg || "升级失败");
        }
      } catch {
        toast.error("网络错误，请重试");
      } finally {
        setNodeList((prev) =>
          prev.map((n) =>
            n.id === upgradeTargetNodeId ? { ...n, upgradeLoading: false } : n,
          ),
        );
      }
    } else if (upgradeTarget === "batch") {
      const selectedLocalIds = Array.from(selectedIds).filter((id) => {
        const matchedNode = nodeList.find((node) => node.id === id);

        return matchedNode?.isRemote !== 1;
      });

      if (selectedLocalIds.length === 0) {
        toast.error("请选择本地服务器进行升级");
        setUpgradeModalOpen(false);

        return;
      }

      setBatchUpgradeLoading(true);
      setUpgradeModalOpen(false);
      try {
        const res = await batchUpgradeNodes(
          selectedLocalIds,
          version,
          releaseChannel,
        );

        if (res.code === 0) {
          toast.success(
            `批量升级命令已发送到 ${selectedLocalIds.length} 个服务器`,
          );
        } else {
          toast.error(res.msg || "批量升级失败");
        }
      } catch {
        toast.error("网络错误，请重试");
      } finally {
        setBatchUpgradeLoading(false);
      }
    }
  };

  // 回退节点
  const handleRollbackNode = (node: Node) => {
    setNodeToRollback(node);
    setRollbackModalOpen(true);
  };

  const confirmRollback = async () => {
    if (!nodeToRollback) return;
    const node = nodeToRollback;

    setRollbackModalOpen(false);
    setNodeList((prev) =>
      prev.map((n) => (n.id === node.id ? { ...n, rollbackLoading: true } : n)),
    );
    try {
      const res = await rollbackNode(node.id);

      if (res.code === 0) {
        toast.success(`服务器 ${node.name} 回退命令已发送，服务器将自动重启`);
      } else {
        toast.error(res.msg || "回退失败");
      }
    } catch {
      toast.error("网络错误，请重试");
    } finally {
      setNodeList((prev) =>
        prev.map((n) =>
          n.id === node.id ? { ...n, rollbackLoading: false } : n,
        ),
      );
      setNodeToRollback(null);
    }
  };

  // 提交表单
  const handleSubmit = async () => {
    if (!validateForm()) return;

    setSubmitLoading(true);

    try {
      const apiCall = isEdit ? updateNode : createNode;
      const { serverHost, ...rest } = form;
      const sshConfig =
        form.forwardMode === "nftables"
          ? {
              host: form.sshHost.trim(),
              port: Number(form.sshPort || 22),
              username: form.sshUsername.trim(),
              authType: form.sshAuthType,
              password:
                form.sshAuthType === "password"
                  ? form.sshPassword.trim()
                  : undefined,
              privateKey:
                form.sshAuthType === "private_key"
                  ? form.sshPrivateKey
                  : undefined,
              passphrase: form.sshPassphrase,
              sudoMode: form.sshSudoMode,
            }
          : null;
      const data = {
        ...rest,
        remark: form.remark.trim(),
        expiryTime: form.expiryTime,
        renewalCycle: form.renewalCycle,
        extraIPs: form.extraIPs,
        forwardMode: form.forwardMode,
        sshConfig,
        serverIp:
          form.serverIpV4?.trim() ||
          form.serverIpV6?.trim() ||
          serverHost?.trim() ||
          "",
      };

      const res = await apiCall(data);

      if (res.code === 0) {
        toast.success(isEdit ? "更新成功" : "创建成功");
        setDialogVisible(false);

        if (isEdit) {
          setNodeList((prev) =>
            prev.map((n) =>
              n.id === form.id
                ? {
                    ...n,
                    name: form.name,
                    remark: form.remark.trim(),
                    notifyEvents: { ...form.notifyEvents },
                    expiryTime: form.expiryTime,
                    renewalCycle: form.renewalCycle,
                    serverIp:
                      form.serverIpV4?.trim() ||
                      form.serverIpV6?.trim() ||
                      form.serverHost?.trim() ||
                      "",
                    serverIpV4: form.serverIpV4,
                    serverIpV6: form.serverIpV6,
                    port: form.port,
                    tcpListenAddr: form.tcpListenAddr,
                    udpListenAddr: form.udpListenAddr,
                    interfaceName: form.interfaceName,
                    forwardMode: form.forwardMode,
                    sshConfig,
                    http: form.http,
                    tls: form.tls,
                    socks: form.socks,
                  }
                : n,
            ),
          );
        } else {
          loadNodes();
        }
      } else {
        toast.error(res.msg || (isEdit ? "更新失败" : "创建失败"));
      }
    } catch {
      toast.error("网络错误，请重试");
    } finally {
      setSubmitLoading(false);
    }
  };

  // 重置表单
  const resetForm = () => {
    setForm({
      id: null,
      name: "",
      remark: "",
      notifyEvents: {},
      expiryTime: 0,
      renewalCycle: "",
      serverHost: "",
      serverIpV4: "",
      serverIpV6: "",
      port: "10000-65535",
      tcpListenAddr: "[::]",
      udpListenAddr: "[::]",
      interfaceName: "",
      extraIPs: "",
      forwardMode: "agent",
      sshHost: "",
      sshPort: "22",
      sshUsername: "",
      sshAuthType: "password",
      sshPassword: "",
      sshPrivateKey: "",
      sshPassphrase: "",
      sshSudoMode: "none",
      http: 0,
      tls: 0,
      socks: 0,
    });
    setErrors({});
  };

  // 处理拖拽结束
  // Probes one node's latency again and patches its result in place.
  const probeNode = async (id: number) => {
    setProbingIds((prev) => new Set(prev).add(id));
    try {
      const res = await probeNodeLatency([id]);
      const latency = res.code === 0 ? res.data?.[String(id)] : undefined;

      if (!latency) {
        toast.error(res.msg || "检测失败");

        return;
      }
      setNodeList((prev) =>
        prev.map((n) =>
          n.id === id ? { ...n, latency: latestProbe(latency, n.latency) } : n,
        ),
      );
    } catch {
      toast.error("检测失败");
    } finally {
      setProbingIds((prev) => {
        const next = new Set(prev);

        next.delete(id);

        return next;
      });
    }
  };

  const renderLatency = (node: Node, variant: "card" | "list") =>
    node.isRemote !== 1 && node.connectionStatus === "online" ? (
      node.latencyTargets?.points.length === 0 ? (
        <button
          className="rounded-full bg-default-100 px-2 py-0.5 text-xs text-default-500 hover:text-default-700"
          title="这台服务器没选测试点，点此设置"
          type="button"
          onClick={(e) => {
            e.stopPropagation();
            setTargetNodes([node]);
          }}
        >
          不测延迟
        </button>
      ) : probePoints(node.latency).length === 0 ? null : (
        // Nothing until the first round (every 3 minutes) has a result;
        // re-probing is in the result's menu.
        <LatencyCell
          note={node.latencyTargets ? "自定义" : undefined}
          probe={node.latency}
          probing={probingIds.has(node.id)}
          variant={variant}
          onEditTargets={() => setTargetNodes([node])}
          onProbe={() => probeNode(node.id)}
          onShowRoute={() => setRouteNode(node)}
          onShowTrend={() => setTrendNode(node)}
        />
      )
    ) : null;

  const handleDragEnd = async (event: DragEndEvent) => {
    const { active, over } = event;

    if (!active || !over || active.id === over.id) return;
    if (!nodeOrder || nodeOrder.length === 0) return;

    const activeId = Number(active.id);
    const overId = Number(over.id);

    if (isNaN(activeId) || isNaN(overId)) return;

    const displayNodeIds = displayNodes.map((node) => node.id);
    const oldIndex = displayNodeIds.indexOf(activeId);
    const newIndex = displayNodeIds.indexOf(overId);

    if (oldIndex === -1 || newIndex === -1 || oldIndex === newIndex) return;

    const reorderedDisplayIds = arrayMove(displayNodeIds, oldIndex, newIndex);
    const displayIdSet = new Set(displayNodeIds);
    let reorderedDisplayIndex = 0;

    const newOrder = nodeOrder.map((id) => {
      if (!displayIdSet.has(id)) {
        return id;
      }

      const nextId = reorderedDisplayIds[reorderedDisplayIndex];

      reorderedDisplayIndex += 1;

      return nextId;
    });

    setNodeOrder(newOrder);

    saveOrder("node-order", newOrder);

    // 持久化到数据库
    try {
      const nodesToUpdate = newOrder.map((id, index) => ({ id, inx: index }));
      const response = await updateNodeOrder({ nodes: nodesToUpdate });

      if (response.code === 0) {
        setNodeList((prev) =>
          prev.map((node) => {
            const updated = nodesToUpdate.find((n) => n.id === node.id);

            return updated ? { ...node, inx: updated.inx } : node;
          }),
        );
      } else {
        toast.error("保存排序失败：" + (response.msg || "未知错误"));
      }
    } catch {
      toast.error("保存排序失败，请重试");
    }
  };

  // 批量操作处理函数
  const toggleSelectMode = () => {
    setSelectMode((prev) => {
      if (prev) {
        setSelectedIds(new Set());
      }

      return !prev;
    });
  };

  const toggleSelect = (id: number) => {
    setSelectedIds((prev) => {
      const next = new Set(prev);

      if (next.has(id)) {
        next.delete(id);
      } else {
        next.add(id);
      }

      return next;
    });
  };

  const selectAll = () => {
    setSelectedIds(new Set(displayNodes.map((n) => n.id)));
  };

  const deselectAll = () => {
    setSelectedIds(new Set());
  };

  const handleBatchDelete = async () => {
    if (selectedIds.size === 0) return;
    setBatchLoading(true);
    try {
      const res = await batchDeleteNodes(Array.from(selectedIds));

      if (res.code === 0) {
        toast.success(`成功删除 ${selectedIds.size} 个服务器`);
        setNodeList((prev) => prev.filter((n) => !selectedIds.has(n.id)));
        setSelectedIds(new Set());
        setBatchDeleteModalOpen(false);
        setSelectMode(false);
      } else {
        toast.error(res.msg || "删除失败");
      }
    } catch {
      toast.error("网络错误，请重试");
    } finally {
      setBatchLoading(false);
    }
  };

  // 传感器配置
  const sensors = useSensors(
    useSensor(MouseSensor, {
      activationConstraint: {
        distance: 8,
      },
    }),
    useSensor(TouchSensor, {
      activationConstraint: {
        delay: 250,
        tolerance: 8,
      },
    }),
    useSensor(KeyboardSensor, {
      coordinateGetter: sortableKeyboardCoordinates,
    }),
  );

  const nodeExpiryStats = useMemo(() => {
    return nodeList.reduce(
      (acc, node) => {
        if (node.isRemote === 1) {
          return acc;
        }

        const meta = getNodeExpiryMeta(node.expiryTime, node.renewalCycle);

        if (meta.state === "expired") acc.expired += 1;
        if (meta.state === "expiringSoon") acc.expiringSoon += 1;
        if (getNodeReminderEnabled(node)) {
          acc.withExpiry += 1;
        }

        return acc;
      },
      { expired: 0, expiringSoon: 0, withExpiry: 0 },
    );
  }, [nodeList]);

  // 根据排序顺序获取节点列表
  const sortedNodes = useMemo((): Node[] => {
    if (!nodeList || nodeList.length === 0) return [];

    const sortedByDb = [...nodeList].sort((a, b) => {
      const aInx = a.inx ?? 0;
      const bInx = b.inx ?? 0;

      return aInx - bInx;
    });

    // 如果数据库中没有排序信息，则使用本地存储的顺序
    if (
      nodeOrder &&
      nodeOrder.length > 0 &&
      sortedByDb.every((n) => n.inx === undefined || n.inx === 0)
    ) {
      const nodeMap = new Map(nodeList.map((n) => [n.id, n] as const));
      const localSorted: Node[] = [];

      nodeOrder.forEach((id) => {
        const node = nodeMap.get(id);

        if (node) localSorted.push(node);
      });

      nodeList.forEach((node) => {
        if (!nodeOrder.includes(node.id)) {
          localSorted.push(node);
        }
      });

      return localSorted;
    }

    return sortedByDb;
  }, [nodeList, nodeOrder]);

  const filterNodesByKeyword = useCallback((nodes: Node[], keyword: string) => {
    const normalizedKeyword = keyword.trim().toLowerCase();

    if (!normalizedKeyword) {
      return nodes;
    }

    return nodes.filter(
      (node) =>
        (node.name && node.name.toLowerCase().includes(normalizedKeyword)) ||
        (node.remark &&
          node.remark.toLowerCase().includes(normalizedKeyword)) ||
        (node.serverIp &&
          node.serverIp.toLowerCase().includes(normalizedKeyword)) ||
        (node.serverIpV4 &&
          node.serverIpV4.toLowerCase().includes(normalizedKeyword)) ||
        (node.serverIpV6 &&
          node.serverIpV6.toLowerCase().includes(normalizedKeyword)),
    );
  }, []);

  const localNodes = useMemo(
    () => sortedNodes.filter((node) => node.isRemote !== 1),
    [sortedNodes],
  );

  const remoteNodes = useMemo(
    () => sortedNodes.filter((node) => node.isRemote === 1),
    [sortedNodes],
  );

  const filteredLocalNodes = useMemo(() => {
    const keywordFiltered = filterNodesByKeyword(
      localNodes,
      localSearchKeyword,
    );

    if (nodeFilterMode === "all") {
      return keywordFiltered;
    }

    return keywordFiltered.filter((node) => {
      const expiryMeta = getNodeExpiryMeta(node.expiryTime, node.renewalCycle);

      switch (nodeFilterMode) {
        case "expiringSoon":
          return expiryMeta.state === "expiringSoon";
        case "expired":
          return expiryMeta.state === "expired";
        case "withExpiry":
          return getNodeReminderEnabled(node);
        default:
          return true;
      }
    });
  }, [filterNodesByKeyword, localNodes, localSearchKeyword, nodeFilterMode]);

  const filteredRemoteNodes = useMemo(
    () => filterNodesByKeyword(remoteNodes, remoteSearchKeyword),
    [filterNodesByKeyword, remoteNodes, remoteSearchKeyword],
  );

  const currentSearchKeyword =
    activeTab === "remote" ? remoteSearchKeyword : localSearchKeyword;

  const setCurrentSearchKeyword =
    activeTab === "remote" ? setRemoteSearchKeyword : setLocalSearchKeyword;

  const displayNodes = useMemo(
    () => (activeTab === "remote" ? filteredRemoteNodes : filteredLocalNodes),
    [activeTab, filteredLocalNodes, filteredRemoteNodes],
  );

  const canBatchUpgrade = activeTab === "local";
  const canUseExpiryFilter = activeTab === "local";
  const hasKeywordSearch = currentSearchKeyword.trim().length > 0;
  const hasActiveFilters = nodeFilterMode !== "all";
  const isDisplayFiltered =
    hasKeywordSearch || (canUseExpiryFilter && hasActiveFilters);

  const sortableNodeIds = useMemo(
    () => displayNodes.map((n) => n.id),
    [displayNodes],
  );

  return (
    <PageShell embedded={embedded}>
      {!embedded && (
        <PageHeader
          description="装了 agent 的机器：搭建节点和中转、看状态和监控"
          primary={{ key: "add", label: "新建服务器", onPress: handleAdd }}
          title="服务器"
        />
      )}
      {remoteNodes.length > 0 && (
        <PageTabs
          tabs={[
            { key: "local", label: "本地服务器", badge: localNodes.length },
            { key: "remote", label: "远程服务器", badge: remoteNodes.length },
          ]}
          value={activeTab}
          onChange={setActiveTab}
        />
      )}

      {selectMode ? (
        <SelectionBar
          actions={[
            {
              key: "upgrade",
              label: "升级",
              disabled: !canBatchUpgrade || batchUpgradeLoading,
              onPress: () => openUpgradeModal("batch"),
            },
            {
              key: "targets",
              label: "测试点",
              disabled: !displayNodes.some(
                (n) => selectedIds.has(n.id) && n.isRemote !== 1,
              ),
              onPress: () =>
                setTargetNodes(
                  displayNodes.filter(
                    (n) => selectedIds.has(n.id) && n.isRemote !== 1,
                  ),
                ),
            },
            {
              key: "delete",
              label: "删除",
              danger: true,
              onPress: () => setBatchDeleteModalOpen(true),
            },
          ]}
          allSelected={
            displayNodes.length > 0 &&
            displayNodes.every((n) => selectedIds.has(n.id))
          }
          count={selectedIds.size}
          onExit={toggleSelectMode}
          onToggleAll={() =>
            displayNodes.every((n) => selectedIds.has(n.id))
              ? deselectAll()
              : selectAll()
          }
        />
      ) : (
        <PageToolbar
          more={[
            {
              key: "batch",
              label: "批量操作",
              disabled: displayNodes.length === 0,
              onPress: toggleSelectMode,
            },
          ]}
          placeholder={
            activeTab === "remote" ? "搜索远程服务器名称或 IP" : "搜索名称或 IP"
          }
          search={currentSearchKeyword}
          view={{ value: viewMode, onChange: setViewMode }}
          onSearch={setCurrentSearchKeyword}
        >
          {canUseExpiryFilter && (
            <ToolbarButton
              active={nodeFilterMode !== "all"}
              onPress={() => setIsFilterModalOpen(true)}
            >
              {nodeFilterMode !== "all" ? "已筛选" : "筛选"}
            </ToolbarButton>
          )}
        </PageToolbar>
      )}

      {/* A connection that comes up within a moment needs no notice. */}
      {!wsConnected && !wsStarting && (
        <Alert
          className="mb-3"
          color="warning"
          description={
            wsConnecting
              ? "监控连接中..."
              : usingPollingFallback
                ? "监控连接已断开，已切换为列表自动刷新兜底模式。"
                : "监控连接已断开，正在重连..."
          }
          variant="flat"
        />
      )}

      {loading ? (
        <PageLoadingState message="正在加载..." />
      ) : nodeList.length === 0 ? (
        <EmptyHint
          action={{ key: "add", label: "新建服务器", onPress: handleAdd }}
          text="还没有服务器。新建后在服务器上运行安装命令，服务器就会连上面板"
        />
      ) : displayNodes.length === 0 ? (
        <EmptyHint
          text={
            isDisplayFiltered
              ? "没有匹配的服务器"
              : activeTab === "remote"
                ? "暂无远程服务器"
                : "暂无本地服务器"
          }
        />
      ) : (
        <DndContext sensors={sensors} onDragEnd={handleDragEnd}>
          <SortableContext
            items={sortableNodeIds}
            strategy={rectSortingStrategy}
          >
            <div
              className={
                card ? CARD_GRID : "grid grid-cols-1 gap-2 2xl:grid-cols-2"
              }
            >
              {displayNodes.map((node) => {
                if (card) {
                  const isRemoteNode = node.isRemote === 1;
                  const remoteUsage = isRemoteNode
                    ? remoteUsageMap[node.id]
                    : null;
                  const expiryMeta = getNodeExpiryMeta(
                    node.expiryTime,
                    node.renewalCycle,
                  );
                  const connectionStatusMeta = getConnectionStatusMeta(
                    node.connectionStatus,
                  );
                  const hasRemark = Boolean(node.remark?.trim());
                  const hasExpiryInfo = Boolean(
                    node.expiryTime &&
                      node.expiryTime > 0 &&
                      node.renewalCycle &&
                      !node.expiryReminderDismissed,
                  );
                  const hasInfoTrigger = hasRemark || hasExpiryInfo;
                  const infoCount = Number(hasExpiryInfo) + Number(hasRemark);
                  const infoPlacement = infoPopoverPlacement[node.id] ?? "left";

                  return (
                    <SortableItem key={node.id} id={node.id}>
                      {(listeners) => (
                        <Card
                          key={node.id}
                          className={`group relative overflow-visible h-full flex flex-col ${node.expiryReminderDismissed ? "" : expiryMeta.accentClassName}`}
                          data-node-card="true"
                        >
                          <CardHeader className="pb-3 md:pb-3">
                            <div className="flex justify-between items-start w-full gap-3">
                              <div className="flex items-start gap-2 flex-1 min-w-0">
                                <div
                                  className="cursor-grab active:cursor-grabbing p-2 -ml-2 -mt-1 text-default-400 hover:text-default-600 transition-colors touch-manipulation opacity-100 sm:opacity-0 sm:group-hover:opacity-100 flex-shrink-0"
                                  {...listeners}
                                  style={{ touchAction: "none" }}
                                  title="拖拽排序"
                                >
                                  <svg
                                    aria-hidden="true"
                                    className="w-4 h-4"
                                    fill="currentColor"
                                    viewBox="0 0 20 20"
                                  >
                                    <path d="M7 2a2 2 0 1 1 .001 4.001A2 2 0 0 1 7 2zm0 6a2 2 0 1 1 .001 4.001A2 2 0 0 1 7 8zm0 6a2 2 0 1 1 .001 4.001A2 2 0 0 1 7 14zm6-8a2 2 0 1 1-.001-4.001A2 2 0 0 1 13 6zm0 2a2 2 0 1 1 .001 4.001A2 2 0 0 1 13 8zm0 6a2 2 0 1 1 .001 4.001A2 2 0 0 1 13 14z" />
                                  </svg>
                                </div>
                                {selectMode && (
                                  <Checkbox
                                    className="mt-0.5"
                                    isSelected={selectedIds.has(node.id)}
                                    onValueChange={() => toggleSelect(node.id)}
                                  />
                                )}
                                <div className="min-w-0 flex-1 pt-0.5">
                                  <div className="flex items-center gap-2 min-w-0">
                                    <h3 className="font-semibold text-foreground truncate text-lg leading-5">
                                      {node.name}
                                    </h3>
                                    <div
                                      className={`flex items-center gap-1.5 px-2 py-0.5 rounded-full text-xs font-medium flex-shrink-0
                                      ${connectionStatusMeta.color === "success" ? "bg-emerald-500/20 text-emerald-600" : "bg-rose-500/20 text-rose-600"}
                                    `}
                                      title={connectionStatusMeta.text}
                                    >
                                      <span
                                        className={`h-1.5 w-1.5 rounded-full ${connectionStatusMeta.color === "success" ? "bg-emerald-500" : "bg-rose-500"}`}
                                      />
                                      {connectionStatusMeta.text}
                                    </div>
                                  </div>
                                </div>
                              </div>
                              <div className="ml-2 flex-shrink-0 self-start">
                                {hasInfoTrigger && (
                                  <div className="group/info relative">
                                    <button
                                      aria-label={`查看服务器信息，共 ${infoCount} 项`}
                                      className="relative flex h-7 w-7 items-center justify-center rounded-full border border-divider/80 bg-background/95 text-default-500 shadow-sm transition hover:border-default-300 hover:text-foreground focus-visible:border-default-300 focus-visible:text-foreground focus-visible:outline-none"
                                      type="button"
                                      onFocus={(event) =>
                                        updateInfoPopoverPlacement(
                                          node.id,
                                          event.currentTarget,
                                        )
                                      }
                                      onMouseEnter={(event) =>
                                        updateInfoPopoverPlacement(
                                          node.id,
                                          event.currentTarget,
                                        )
                                      }
                                    >
                                      <svg
                                        aria-hidden="true"
                                        className="h-3.5 w-3.5"
                                        fill="none"
                                        stroke="currentColor"
                                        viewBox="0 0 24 24"
                                      >
                                        <path
                                          d="M13 16h-1v-4h-1m1-4h.01M21 12a9 9 0 11-18 0 9 9 0 0118 0z"
                                          strokeLinecap="round"
                                          strokeLinejoin="round"
                                          strokeWidth={1.8}
                                        />
                                      </svg>
                                      {hasRemark && (
                                        <span className="absolute -right-1 -top-1 flex h-2.5 w-2.5 rounded-full border border-background bg-default-300 shadow-sm dark:bg-default-500" />
                                      )}
                                    </button>
                                    <div
                                      className={`pointer-events-none invisible absolute z-[60] w-72 max-w-[min(18rem,calc(100vw-4rem))] rounded-xl border border-divider/80 bg-background/98 p-3 opacity-0 shadow-xl backdrop-blur transition-all duration-150 group-hover/info:visible group-hover/info:pointer-events-auto group-hover/info:opacity-100 group-focus-within/info:visible group-focus-within/info:pointer-events-auto group-focus-within/info:opacity-100 ${
                                        infoPlacement === "bottom"
                                          ? "right-0 top-[calc(100%+0.75rem)] translate-y-1 group-hover/info:translate-y-0 group-focus-within/info:translate-y-0"
                                          : "right-[calc(100%+0.75rem)] top-1/2 -translate-y-1/2 translate-x-1 group-hover/info:translate-x-0 group-focus-within/info:translate-x-0"
                                      }`}
                                    >
                                      <div className="space-y-3">
                                        {hasExpiryInfo && (
                                          <div className="space-y-2">
                                            <div className="flex items-center justify-between">
                                              <div className="text-[11px] font-medium text-default-500">
                                                到期提醒
                                              </div>
                                              <button
                                                className="text-[10px] text-default-400 hover:text-default-600 transition-colors"
                                                type="button"
                                                onClick={(e) => {
                                                  e.stopPropagation();
                                                  handleDismissExpiryReminder(
                                                    node.id,
                                                  );
                                                }}
                                              >
                                                关闭提醒
                                              </button>
                                            </div>
                                            <div className="flex flex-wrap gap-1.5">
                                              <Chip
                                                className="text-[10px] h-5 px-1 flex-shrink-0"
                                                color={expiryMeta.tone}
                                                size="sm"
                                                title={`${formatNodeRenewalTime(expiryMeta.nextDueTime)} (${getNodeRenewalCycleLabel(node.renewalCycle)})`}
                                                variant="flat"
                                              >
                                                {expiryMeta.label}
                                              </Chip>
                                            </div>
                                            <div className="rounded-lg border border-divider/80 bg-default-50/80 px-3 py-2 text-xs leading-5 text-default-700">
                                              {formatNodeRenewalTime(
                                                expiryMeta.nextDueTime,
                                              )}
                                            </div>
                                          </div>
                                        )}

                                        {hasRemark && (
                                          <div className="space-y-2">
                                            <div className="text-[11px] font-medium text-default-500">
                                              备注
                                            </div>
                                            <div
                                              className="max-h-32 overflow-y-auto rounded-lg border border-divider/80 bg-default-50/80 px-3 py-2 text-xs leading-5 text-default-700 break-all [scrollbar-width:thin]"
                                              title={node.remark?.trim()}
                                            >
                                              {node.remark?.trim()}
                                            </div>
                                          </div>
                                        )}
                                      </div>
                                    </div>
                                  </div>
                                )}
                              </div>
                            </div>
                          </CardHeader>

                          <CardBody className="pt-0 pb-3 md:pt-0 md:pb-3 flex-1 flex flex-col">
                            {isRemoteNode && node.syncError && (
                              <div className="mb-3 px-2 py-1.5 rounded-md bg-warning-50 dark:bg-warning-100/10 text-warning-700 dark:text-warning-400 text-xs">
                                {getRemoteSyncErrorMessage(node.syncError)}
                              </div>
                            )}
                            {isRemoteNode ? (
                              <div className="space-y-2 mb-4">
                                <div className="flex justify-between items-center text-sm min-w-0">
                                  <span className="text-default-600 flex-shrink-0">
                                    IP
                                  </span>
                                  <div className="text-right text-xs min-w-0 flex-1 ml-2 min-h-[2.125rem]">
                                    {node.serverIpV4?.trim() ||
                                    node.serverIpV6?.trim() ? (
                                      <div className="space-y-0.5">
                                        {node.serverIpV4?.trim() && (
                                          <span
                                            className="font-mono truncate block"
                                            title={node.serverIpV4.trim()}
                                          >
                                            {node.serverIpV4.trim()}
                                          </span>
                                        )}
                                        {node.serverIpV6?.trim() && (
                                          <span
                                            className="font-mono truncate block"
                                            title={node.serverIpV6.trim()}
                                          >
                                            {node.serverIpV6.trim()}
                                          </span>
                                        )}
                                      </div>
                                    ) : (
                                      <span
                                        className="font-mono truncate block"
                                        title={node.serverIp.trim()}
                                      >
                                        {node.serverIp.trim()}
                                      </span>
                                    )}
                                  </div>
                                </div>
                              </div>
                            ) : (
                              <div className="mb-4 space-y-3">
                                <p className="truncate font-mono text-xs text-default-500">
                                  {[
                                    node.serverIpV4?.trim() ||
                                      node.serverIpV6?.trim() ||
                                      node.serverIp.trim(),
                                    node.version || "未连接过",
                                    node.connectionStatus === "online" &&
                                    node.systemInfo
                                      ? `已运行 ${formatUptime(node.systemInfo.uptime)}`
                                      : "",
                                  ]
                                    .filter(Boolean)
                                    .join(" · ")}
                                </p>
                                {upgradeProgress[node.id] &&
                                  upgradeProgress[node.id].percent < 100 && (
                                    <Progress
                                      showValueLabel
                                      aria-label="升级进度"
                                      color="warning"
                                      label={upgradeProgress[node.id].message}
                                      size="sm"
                                      value={upgradeProgress[node.id].percent}
                                    />
                                  )}
                                <NodeLiveStats
                                  info={
                                    node.connectionStatus === "online"
                                      ? (node.systemInfo ?? null)
                                      : null
                                  }
                                  month={monthTraffic[String(node.id)]}
                                  variant="card"
                                />
                              </div>
                            )}

                            {isRemoteNode && (
                              <div className="space-y-3 mb-4">
                                {remoteUsage ? (
                                  <>
                                    <div className="text-xs rounded-md border border-default-200 dark:border-default-100/30 bg-default-50 dark:bg-default-100/20 p-2.5 space-y-2">
                                      <div className="flex justify-between gap-2">
                                        <span className="text-default-500">
                                          远程地址
                                        </span>
                                        <span
                                          className="font-mono text-right truncate"
                                          title={
                                            remoteUsage.remoteUrl ||
                                            node.remoteUrl ||
                                            "-"
                                          }
                                        >
                                          {remoteUsage.remoteUrl ||
                                            node.remoteUrl ||
                                            "-"}
                                        </span>
                                      </div>
                                      <div className="flex justify-between gap-2">
                                        <span className="text-default-500">
                                          共享ID
                                        </span>
                                        <span className="font-mono">
                                          #{remoteUsage.shareId}
                                        </span>
                                      </div>
                                      <div className="flex justify-between gap-2">
                                        <span className="text-default-500">
                                          流量
                                        </span>
                                        <span className="font-mono">
                                          {formatFlow(remoteUsage.currentFlow)}
                                        </span>
                                      </div>
                                      <div className="flex justify-between gap-2">
                                        <span className="text-default-500">
                                          带宽上限
                                        </span>
                                        <span className="font-mono">
                                          {remoteUsage.maxBandwidth > 0
                                            ? formatSpeed(
                                                remoteUsage.maxBandwidth,
                                              )
                                            : "不限"}
                                        </span>
                                      </div>
                                    </div>

                                    <div className="text-xs rounded-md border border-default-200 dark:border-default-100/30 bg-default-50 dark:bg-default-100/20 p-2.5">
                                      <div className="flex items-center justify-between mb-2">
                                        <span className="text-default-500">
                                          占用端口
                                        </span>
                                        <span className="font-mono text-default-700 dark:text-default-300">
                                          {remoteUsage.usedPorts.length}/
                                          {Math.max(
                                            remoteUsage.portRangeEnd -
                                              remoteUsage.portRangeStart +
                                              1,
                                            0,
                                          )}
                                        </span>
                                      </div>
                                      <div className="max-h-20 overflow-y-auto rounded bg-white/10 dark:bg-black/10 p-1.5 [scrollbar-width:thin] [&::-webkit-scrollbar]:w-1">
                                        {remoteUsage.usedPorts.length > 0 ? (
                                          <div className="flex flex-wrap gap-1">
                                            {remoteUsage.usedPorts.map(
                                              (port) => (
                                                <Chip
                                                  key={`${node.id}-port-${port}`}
                                                  className="font-mono shrink-0 whitespace-nowrap"
                                                  size="sm"
                                                  variant="flat"
                                                >
                                                  {port}
                                                </Chip>
                                              ),
                                            )}
                                          </div>
                                        ) : (
                                          <div className="text-default-400">
                                            暂无占用端口
                                          </div>
                                        )}
                                      </div>
                                    </div>

                                    <div className="text-xs rounded-md border border-default-200 dark:border-default-100/30 bg-default-50 dark:bg-default-100/20 p-2.5">
                                      <div className="flex items-center justify-between mb-2">
                                        <span className="text-default-500">
                                          绑定明细
                                        </span>
                                        <span className="font-mono text-default-700 dark:text-default-300">
                                          {remoteUsage.activeBindingNum}
                                        </span>
                                      </div>
                                      <div className="max-h-32 overflow-y-auto space-y-1.5 pr-1 [scrollbar-width:thin] [&::-webkit-scrollbar]:w-1">
                                        {remoteUsage.bindings.length > 0 ? (
                                          remoteUsage.bindings.map(
                                            (binding) => (
                                              <div
                                                key={binding.bindingId}
                                                className="rounded border border-default-200 dark:border-default-100/30 bg-white/10 dark:bg-black/10 p-2"
                                              >
                                                <div className="flex items-center justify-between gap-2">
                                                  <span
                                                    className="font-medium truncate"
                                                    title={binding.tunnelName}
                                                  >
                                                    {binding.tunnelName}
                                                  </span>
                                                  <span className="font-mono text-[11px]">
                                                    #{binding.tunnelId}
                                                  </span>
                                                </div>
                                                <div className="mt-1 text-[11px] text-default-500 flex items-center justify-between gap-2">
                                                  <span>
                                                    {formatChainType(
                                                      binding.chainType,
                                                      binding.hopInx,
                                                    )}
                                                  </span>
                                                  <span className="font-mono">
                                                    端口 {binding.allocatedPort}
                                                  </span>
                                                </div>
                                              </div>
                                            ),
                                          )
                                        ) : (
                                          <div className="text-default-400">
                                            暂无绑定明细
                                          </div>
                                        )}
                                      </div>
                                    </div>
                                  </>
                                ) : (
                                  <div className="text-xs rounded-md border border-default-200 dark:border-default-100/30 bg-default-50 dark:bg-default-100/20 p-2.5 text-default-500">
                                    暂未获取到远程占用数据
                                  </div>
                                )}
                              </div>
                            )}

                            {node.latency && (
                              <div className="mb-4 empty:hidden">
                                {renderLatency(node, "card")}
                              </div>
                            )}

                            <div className="mt-auto space-y-3">
                              {/* 操作按钮 */}
                              <div
                                className={`grid gap-1.5 ${isRemoteNode ? "grid-cols-1" : "grid-cols-3"}`}
                              >
                                {!isRemoteNode && (
                                  <>
                                    {onOpenMonitor && (
                                      <Button
                                        className="min-h-8"
                                        color="primary"
                                        size="sm"
                                        variant="flat"
                                        onPress={() => onOpenMonitor(node.id)}
                                      >
                                        监控
                                      </Button>
                                    )}
                                    <Button
                                      className="min-h-8"
                                      color="success"
                                      isLoading={node.copyLoading}
                                      size="sm"
                                      variant="flat"
                                      onPress={() => openInstallSelector(node)}
                                    >
                                      安装
                                    </Button>
                                    <Button
                                      className="min-h-8 bg-default-100 text-default-700"
                                      size="sm"
                                      variant="flat"
                                      onPress={() => setTargetNodes([node])}
                                    >
                                      测试点
                                    </Button>
                                    <Button
                                      className="min-h-8"
                                      color="warning"
                                      isDisabled={
                                        node.connectionStatus !== "online"
                                      }
                                      isLoading={node.upgradeLoading}
                                      size="sm"
                                      variant="flat"
                                      onPress={() =>
                                        openUpgradeModal("single", node.id)
                                      }
                                    >
                                      升级
                                    </Button>
                                    <Button
                                      className="min-h-8"
                                      color="secondary"
                                      isDisabled={
                                        node.connectionStatus !== "online"
                                      }
                                      isLoading={node.rollbackLoading}
                                      size="sm"
                                      variant="flat"
                                      onPress={() => handleRollbackNode(node)}
                                    >
                                      回退
                                    </Button>
                                    <Button
                                      className="min-h-8"
                                      color="success"
                                      isDisabled={
                                        node.connectionStatus !== "online"
                                      }
                                      size="sm"
                                      variant="flat"
                                      onPress={() => setTuneNode(node)}
                                    >
                                      调优
                                    </Button>
                                    <Button
                                      className="min-h-8"
                                      color="primary"
                                      size="sm"
                                      variant="flat"
                                      onPress={() => handleEdit(node)}
                                    >
                                      编辑
                                    </Button>
                                  </>
                                )}
                                <Button
                                  className="min-h-8"
                                  color="danger"
                                  size="sm"
                                  variant="flat"
                                  onPress={() => handleDelete(node)}
                                >
                                  删除
                                </Button>
                              </div>
                            </div>
                          </CardBody>
                        </Card>
                      )}
                    </SortableItem>
                  );
                }

                const isRemoteNode = node.isRemote === 1;
                const online = node.connectionStatus === "online";
                const expiryMeta = getNodeExpiryMeta(
                  node.expiryTime,
                  node.renewalCycle,
                );
                const showExpiry = Boolean(
                  node.expiryTime &&
                    node.expiryTime > 0 &&
                    node.renewalCycle &&
                    !node.expiryReminderDismissed &&
                    expiryMeta.isHighlighted,
                );
                const progress = upgradeProgress[node.id];
                const upgrading = progress && progress.percent < 100;
                const address =
                  node.serverIpV4?.trim() ||
                  node.serverIpV6?.trim() ||
                  node.serverIp.trim();
                const meta = isRemoteNode
                  ? [address, node.syncError ? "同步失败" : ""]
                  : [
                      address,
                      upgrading
                        ? `升级中 ${progress.percent}%`
                        : node.version || "未连接过",
                      online && node.systemInfo
                        ? `已运行 ${formatUptime(node.systemInfo.uptime)}`
                        : "",
                    ];

                return (
                  <SortableItem key={node.id} id={node.id}>
                    {(listeners) => (
                      <ListRow
                        badges={
                          <>
                            {isRemoteNode && (
                              <Chip
                                className="shrink-0"
                                size="sm"
                                variant="flat"
                              >
                                远程
                              </Chip>
                            )}
                            {showExpiry && (
                              <Chip
                                className="shrink-0"
                                color={expiryMeta.tone}
                                size="sm"
                                variant="flat"
                              >
                                {expiryMeta.label}
                              </Chip>
                            )}
                          </>
                        }
                        footer={
                          !isRemoteNode && (
                            <NodeLiveStats
                              info={online ? (node.systemInfo ?? null) : null}
                              month={monthTraffic[String(node.id)]}
                              variant="footer"
                            />
                          )
                        }
                        meta={meta.filter(Boolean).join(" · ")}
                        right={
                          <>
                            {!isRemoteNode && (
                              <NodeLiveStats
                                info={online ? (node.systemInfo ?? null) : null}
                                month={monthTraffic[String(node.id)]}
                                variant="row"
                              />
                            )}
                            {/* A fixed slot keeps the stats in line across rows. */}
                            <div className="contents lg:flex lg:w-36 lg:justify-end">
                              {renderLatency(node, "list")}
                            </div>
                            {!isDisplayFiltered && (
                              <span
                                className="cursor-grab touch-none p-1 text-default-300 hover:text-default-500 active:cursor-grabbing"
                                title="拖动排序"
                                {...listeners}
                              >
                                <GripVertical className="h-4 w-4" />
                              </span>
                            )}
                          </>
                        }
                        selectMode={selectMode}
                        selected={selectedIds.has(node.id)}
                        status={upgrading ? "warn" : online ? "ok" : "bad"}
                        statusTitle={
                          getConnectionStatusMeta(node.connectionStatus).text
                        }
                        title={node.name}
                        onPress={() => setSheetNodeId(node.id)}
                        onSelect={() => toggleSelect(node.id)}
                      />
                    )}
                  </SortableItem>
                );
              })}
            </div>
          </SortableContext>
        </DndContext>
      )}

      {(() => {
        const node = nodeList.find((n) => n.id === sheetNodeId) ?? null;
        const isRemoteNode = node?.isRemote === 1;
        const online = node?.connectionStatus === "online";
        const usage = node ? remoteUsageMap[node.id] : undefined;
        const progress = node ? upgradeProgress[node.id] : undefined;
        const expiryMeta = node
          ? getNodeExpiryMeta(node.expiryTime, node.renewalCycle)
          : null;
        const close = () => setSheetNodeId(null);
        const rows: Array<[string, React.ReactNode]> = node
          ? isRemoteNode
            ? [
                ["地址", usage?.remoteUrl || node.remoteUrl || "-"],
                ...(usage
                  ? ([
                      ["流量", formatFlow(usage.currentFlow)],
                      [
                        "带宽上限",
                        usage.maxBandwidth > 0
                          ? formatSpeed(usage.maxBandwidth)
                          : "不限",
                      ],
                      [
                        "占用端口",
                        `${usage.usedPorts.length} / ${Math.max(
                          usage.portRangeEnd - usage.portRangeStart + 1,
                          0,
                        )}`,
                      ],
                      ["绑定", `${usage.activeBindingNum} 条`],
                    ] as Array<[string, React.ReactNode]>)
                  : []),
              ]
            : [
                [
                  "IP",
                  [node.serverIpV4?.trim(), node.serverIpV6?.trim()]
                    .filter(Boolean)
                    .join(" / ") || node.serverIp.trim(),
                ],
                ["版本", node.version || "未知"],
                ...(online && node.systemInfo
                  ? ([
                      ["已运行", formatUptime(node.systemInfo.uptime)],
                    ] as Array<[string, React.ReactNode]>)
                  : []),
                ...(node.expiryTime && node.renewalCycle && expiryMeta
                  ? ([
                      [
                        "续费",
                        `${expiryMeta.label} · ${formatNodeRenewalTime(
                          expiryMeta.nextDueTime,
                        )}（${getNodeRenewalCycleLabel(node.renewalCycle)}）`,
                      ],
                    ] as Array<[string, React.ReactNode]>)
                  : []),
              ]
          : [];

        return (
          <ActionSheet
            actions={
              !node
                ? []
                : isRemoteNode
                  ? [
                      {
                        key: "delete",
                        label: "删除",
                        color: "danger" as const,
                        onPress: () => handleDelete(node),
                      },
                    ]
                  : [
                      ...(onOpenMonitor
                        ? [
                            {
                              key: "monitor",
                              label: "监控",
                              color: "primary" as const,
                              onPress: () => onOpenMonitor(node.id),
                            },
                          ]
                        : []),
                      {
                        key: "install",
                        label: "安装命令",
                        color: "primary" as const,
                        onPress: () => openInstallSelector(node),
                      },
                      {
                        key: "tune",
                        label: "调优",
                        disabled: !online,
                        onPress: () => setTuneNode(node),
                      },
                      {
                        key: "edit",
                        label: "编辑",
                        onPress: () => handleEdit(node),
                      },
                      {
                        key: "targets",
                        label: "测试点",
                        onPress: () => setTargetNodes([node]),
                      },
                      {
                        key: "upgrade",
                        label: "升级",
                        disabled: !online,
                        onPress: () => openUpgradeModal("single", node.id),
                      },
                      {
                        key: "rollback",
                        label: "回退",
                        disabled: !online,
                        onPress: () => handleRollbackNode(node),
                      },
                      ...(node.expiryTime &&
                      node.renewalCycle &&
                      !node.expiryReminderDismissed &&
                      expiryMeta?.isHighlighted
                        ? [
                            {
                              key: "dismiss",
                              label: "关闭到期提醒",
                              onPress: () =>
                                handleDismissExpiryReminder(node.id),
                            },
                          ]
                        : []),
                      {
                        key: "delete",
                        label: "删除",
                        color: "danger" as const,
                        onPress: () => handleDelete(node),
                      },
                    ]
            }
            isOpen={Boolean(node)}
            subtitle={
              node &&
              (isRemoteNode
                ? "远程服务器"
                : online
                  ? "在线"
                  : "离线 · agent 没有连上面板")
            }
            title={node?.name ?? ""}
            onClose={close}
          >
            {node && (
              <div className="space-y-2 text-sm">
                {isRemoteNode && node.syncError && (
                  <p className="rounded-lg bg-warning-50 px-2 py-1.5 text-xs text-warning-700 dark:bg-warning-100/10 dark:text-warning-400">
                    {getRemoteSyncErrorMessage(node.syncError)}
                  </p>
                )}
                {rows.map(([k, v]) => (
                  <div key={k} className="flex justify-between gap-3">
                    <span className="shrink-0 text-default-500">{k}</span>
                    <span className="min-w-0 break-all text-right font-mono text-xs leading-5">
                      {v}
                    </span>
                  </div>
                ))}
                {progress && progress.percent < 100 && (
                  <Progress
                    showValueLabel
                    aria-label="升级进度"
                    color="warning"
                    label={progress.message}
                    size="sm"
                    value={progress.percent}
                  />
                )}
                {node.remark?.trim() && (
                  <p className="whitespace-pre-wrap break-all text-xs text-default-500">
                    {node.remark.trim()}
                  </p>
                )}
              </div>
            )}
          </ActionSheet>
        );
      })()}
      {/* 新增/编辑节点对话框 */}
      <NodeFormModal
        dialogTitle={dialogTitle}
        dialogVisible={dialogVisible}
        errors={errors}
        form={form}
        handleSubmit={handleSubmit}
        protocolControlsDisabled={protocolControlsDisabled}
        protocolControlsReason={protocolControlsReason}
        setDialogVisible={setDialogVisible}
        setForm={setForm}
        submitLoading={submitLoading}
      />

      <NodeTuneModal node={tuneNode} onClose={() => setTuneNode(null)} />

      {/* 回退确认模态框 */}
      <RollbackNodeModal
        confirmRollback={confirmRollback}
        nodeToRollback={nodeToRollback}
        rollbackModalOpen={rollbackModalOpen}
        setRollbackModalOpen={setRollbackModalOpen}
      />

      {/* 删除确认模态框 */}
      <DeleteNodeModal
        confirmDelete={confirmDelete}
        deleteLoading={deleteLoading}
        deleteModalOpen={deleteModalOpen}
        nodeToDelete={nodeToDelete}
        setDeleteModalOpen={setDeleteModalOpen}
      />

      {/* 安装命令模态框 */}
      <InstallCommandModal
        currentNodeName={currentNodeName}
        handleCopyCommand={handleCopyCommand}
        installCommandModal={installCommandModal}
        installCommands={installCommands}
        setInstallCommandModal={setInstallCommandModal}
      />

      {/* 版本选择升级模态框 */}
      <UpgradeNodeModal
        handleConfirmUpgrade={handleConfirmUpgrade}
        releases={releases}
        releasesLoading={releasesLoading}
        selectedIds={selectedIds}
        selectedVersion={selectedVersion}
        setSelectedVersion={setSelectedVersion}
        setUpgradeModalOpen={setUpgradeModalOpen}
        upgradeModalOpen={upgradeModalOpen}
        upgradeTarget={upgradeTarget}
      />

      {/* 批量删除确认模态框 */}
      <BatchDeleteNodesModal
        batchDeleteModalOpen={batchDeleteModalOpen}
        batchLoading={batchLoading}
        handleBatchDelete={handleBatchDelete}
        selectedIds={selectedIds}
        setBatchDeleteModalOpen={setBatchDeleteModalOpen}
      />

      <NodeFilterModal
        isFilterModalOpen={isFilterModalOpen}
        nodeExpiryStats={nodeExpiryStats}
        nodeFilterMode={nodeFilterMode}
        resetNodeFilterMode={resetNodeFilterMode}
        setIsFilterModalOpen={setIsFilterModalOpen}
        setNodeFilterMode={setNodeFilterMode}
      />
      <RouteModal
        nodeId={routeNode?.id ?? null}
        nodeName={routeNode?.name}
        onClose={() => setRouteNode(null)}
      />
      <LatencyTrendModal
        nodeId={trendNode?.id ?? null}
        nodeName={trendNode?.name}
        onClose={() => setTrendNode(null)}
      />
      <LatencyTargetsModal
        nodes={targetNodes}
        onClose={() => setTargetNodes([])}
        onSaved={() => loadNodes({ silent: true })}
      />
    </PageShell>
  );
}
