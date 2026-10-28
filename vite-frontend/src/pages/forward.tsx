import type { BatchOperationFailure, SpeedLimitApiItem } from "@/api/types";

import { useState, useEffect, useMemo, useRef, useCallback } from "react";
import toast from "react-hot-toast";
import {
  DndContext,
  pointerWithin,
  KeyboardSensor,
  MouseSensor,
  TouchSensor,
  useSensor,
  useSensors,
  DragEndEvent,
} from "@dnd-kit/core";
import {
  arrayMove,
  SortableContext,
  sortableKeyboardCoordinates,
  rectSortingStrategy,
  verticalListSortingStrategy,
} from "@dnd-kit/sortable";

import { copyWithToast } from "@/utils/clipboard";
import { ActionSheet } from "@/components/action-sheet";
import {
  EmptyHint,
  ListGroup,
  ListRow,
  PageHeader,
  PageToolbar,
  SelectionBar,
  formatDate,
} from "@/components/page-kit";
import { formatTraffic } from "@/utils/traffic";
import { BatchActionResultModal } from "@/components/batch-action-result-modal";
import { Button } from "@/shadcn-bridge/heroui/button";
import { Chip } from "@/shadcn-bridge/heroui/chip";
import { Alert } from "@/shadcn-bridge/heroui/alert";
import { Progress } from "@/shadcn-bridge/heroui/progress";
import {
  createForward,
  getForwardList,
  getSpeedLimitList,
  getPeerShareList,
  getPeerRemoteUsageList,
  updateForward,
  deleteForward,
  forceDeleteForward,
  userTunnel,
  getTunnelList,
  getNodeList,
  pauseForwardService,
  resumeForwardService,
  resetForwardFlow,
  diagnoseForward,
  updateForwardOrder,
  getConfigByName,
  updateConfig,
} from "@/api";
import {
  type ForwardAddressItem,
  formatInAddress,
  formatRemoteAddress,
  hasMultipleAddresses,
  resolveForwardAddressAction,
} from "@/pages/forward/address";
import {
  buildForwardDiagnosisFallbackResult,
  type ForwardDiagnosisResult,
} from "@/pages/forward/diagnosis";
import { diagnoseForwardStream } from "@/api/diagnosis-stream";
import {
  executeForwardBatchChangeTunnel,
  executeForwardBatchDelete,
  executeForwardBatchRedeploy,
  executeForwardBatchToggleService,
} from "@/pages/forward/batch-actions";
import {
  convertNyItemToForwardInput,
  parseNyFormatData,
} from "@/pages/forward/import-format";
import { buildForwardOrder, FORWARD_ORDER_KEY } from "@/pages/forward/order";
import { PageLoadingState } from "@/components/page-state";
import {
  markPageCached,
  pageCached,
  useCachedState,
} from "@/hooks/use-cached-state";
// import { useMobileBreakpoint } from "@/hooks/useMobileBreakpoint";
import { saveOrder } from "@/utils/order-storage";
import { JwtUtil } from "@/utils/jwt";
import { ForwardFormModal } from "@/pages/forward/forward-form-modal";
import {
  AddressListModal,
  BatchChangeTunnelModal,
  BatchDeleteModal,
  DeleteForwardModal,
  ResetFlowModal,
} from "@/pages/forward/confirm-modals";
import { ExportModal, ImportModal } from "@/pages/forward/import-export";
import { DiagnosisModal } from "@/pages/forward/diagnosis-modal";
import { ForwardFilterModal } from "@/pages/forward/filter-modal";
import {
  ImportFormat,
  Forward,
  Tunnel,
  Node,
  ForwardForm,
  ForwardUserGroup,
  ForwardTunnelGroup,
  BatchProgressState,
  BatchResultModalState,
  EMPTY_BATCH_RESULT_MODAL_STATE,
  ForwardGroupOrderMap,
  ForwardGroupCollapsedMap,
} from "@/pages/forward/types";
import {
  UNKNOWN_FORWARD_USER_NAME,
  UNCATEGORIZED_FORWARD_TUNNEL_NAME,
  FORWARD_COMPACT_MODE_CONFIG_KEY,
  FORWARD_COMPACT_MODE_EVENT,
  FORWARD_GROUP_ORDER_CONFIG_KEY,
  FORWARD_GROUP_COLLAPSED_CONFIG_KEY,
  normalizeForwardUserName,
  compareForwardUserNameAsc,
  normalizeForwardTunnelName,
  buildForwardTunnelGroupKey,
  compareForwardTunnelNameAsc,
  normalizeTunnelTrafficRatio,
  formatTunnelTrafficRatio,
  buildForwardGroupOrderLocalKey,
  buildForwardGroupCollapsedLocalKey,
  parsePreferenceMap,
  parseGroupOrderMap,
  parseGroupCollapsedMap,
  sanitizeGroupOrderMap,
  sanitizeGroupCollapsedMap,
  buildTunnelGroupCollapseKey,
  buildTunnelGroupSortableId,
  parseTunnelGroupSortableId,
  buildAvailableGroupData,
  isSameGroupOrderMap,
  isSameGroupCollapsedMap,
  normalizeForwardItems,
  mapForwardApiItems,
} from "@/pages/forward/grouping";
import {
  SortableTunnelGroup,
  SortableForwardRow,
  getForwardRowStatus,
  ForwardPageShell,
  getForwardDisplayFlow,
} from "@/pages/forward/sortable";

// A revisit shows the last data at once and reloads in the background.
const PAGE = "forward";

export default function ForwardPage({
  embedded = false,
}: {
  /** Shown as a tab inside another page: no header or page padding. */
  embedded?: boolean;
} = {}) {
  const tokenUserId = JwtUtil.getUserIdFromToken();
  const tokenRoleId = JwtUtil.getRoleIdFromToken();
  const isAdmin = tokenRoleId === 0;

  const [compactMode, setCompactMode] = useState(false);

  // 在非精简模式下（compactMode=false），管理员默认展示全部用户规则（与 2.1.8-beta9 行为一致）。
  // 精简模式下默认仅展示管理员本人规则，避免在表格里混看不清所属用户。
  const defaultSearchUserId = useMemo(() => {
    if (isAdmin) {
      if (!compactMode) {
        return "all";
      }

      return tokenUserId ? tokenUserId.toString() : "all";
    }

    return tokenUserId ? tokenUserId.toString() : "all";
  }, [compactMode, isAdmin, tokenUserId]);

  const [searchParams, setSearchParams] = useState({
    name: "",
    userId: defaultSearchUserId,
    tunnelId: "all",
    inPort: "",
    remoteAddr: "",
  });
  const [isSearchModalOpen, setIsSearchModalOpen] = useState(false);

  const resetSearchParams = useCallback(() => {
    setSearchParams({
      name: "",
      userId: defaultSearchUserId,
      tunnelId: "all",
      inPort: "",
      remoteAddr: "",
    });
  }, [defaultSearchUserId]);

  const lastDefaultSearchUserIdRef = useRef(defaultSearchUserId);

  useEffect(() => {
    const previousDefault = lastDefaultSearchUserIdRef.current;

    lastDefaultSearchUserIdRef.current = defaultSearchUserId;

    if (!isAdmin || previousDefault === defaultSearchUserId) {
      return;
    }

    setSearchParams((prev) => {
      const hasOtherFilters =
        Boolean(prev.name.trim()) ||
        prev.tunnelId !== "all" ||
        Boolean(prev.inPort.trim()) ||
        Boolean(prev.remoteAddr.trim());

      if (hasOtherFilters) {
        return prev;
      }

      if (prev.userId !== previousDefault) {
        return prev;
      }

      return {
        ...prev,
        userId: defaultSearchUserId,
      };
    });
  }, [defaultSearchUserId, isAdmin]);

  const activeFilterCount =
    (searchParams.name.trim() ? 1 : 0) +
    (searchParams.userId !== defaultSearchUserId ? 1 : 0) +
    (searchParams.tunnelId !== "all" ? 1 : 0) +
    (searchParams.inPort.trim() ? 1 : 0) +
    (searchParams.remoteAddr.trim() ? 1 : 0);
  const [loading, setLoading] = useState(true);
  const [forwards, setForwards] = useCachedState<Forward[]>(
    PAGE,
    "forwards",
    [],
  );
  const [tunnels, setTunnels] = useCachedState<Tunnel[]>(PAGE, "tunnels", []);
  const [allTunnels, setAllTunnels] = useCachedState<Tunnel[]>(
    PAGE,
    "allTunnels",
    [],
  );
  const [nodes, setNodes] = useCachedState<Node[]>(PAGE, "nodes", []);
  const [speedLimits, setSpeedLimits] = useCachedState<SpeedLimitApiItem[]>(
    PAGE,
    "speedLimits",
    [],
  );
  //   const isMobile = useMobileBreakpoint();
  // searchKeyword removed
  // isSearchVisible removed

  // 筛选状态
  // filterUserId removed
  // filterTunnelId removed

  // 拖拽排序相关状态
  const [forwardOrder, setForwardOrder] = useCachedState<number[]>(
    PAGE,
    "forwardOrder",
    [],
  );

  // 模态框状态
  const [modalOpen, setModalOpen] = useState(false);
  // isFilterModalOpen removed
  const [deleteModalOpen, setDeleteModalOpen] = useState(false);
  const [resetFlowModalOpen, setResetFlowModalOpen] = useState(false);
  const [addressModalOpen, setAddressModalOpen] = useState(false);
  const [diagnosisModalOpen, setDiagnosisModalOpen] = useState(false);
  const [isEdit, setIsEdit] = useState(false);
  const [submitLoading, setSubmitLoading] = useState(false);
  const [deleteLoading, setDeleteLoading] = useState(false);
  const [resetFlowLoading, setResetFlowLoading] = useState(false);
  const [diagnosisLoading, setDiagnosisLoading] = useState(false);
  const [forwardToDelete, setForwardToDelete] = useState<Forward | null>(null);
  const [forwardToResetFlow, setForwardToResetFlow] = useState<Forward | null>(
    null,
  );
  const [currentDiagnosisForward, setCurrentDiagnosisForward] =
    useState<Forward | null>(null);
  const [diagnosisResult, setDiagnosisResult] =
    useState<ForwardDiagnosisResult | null>(null);
  const [diagnosisProgress, setDiagnosisProgress] = useState({
    total: 0,
    completed: 0,
    success: 0,
    failed: 0,
    timedOut: false,
  });
  const diagnosisAbortRef = useRef<AbortController | null>(null);
  const [addressModalTitle, setAddressModalTitle] = useState("");
  const [addressList, setAddressList] = useState<ForwardAddressItem[]>([]);

  // 导出相关状态
  const [exportModalOpen, setExportModalOpen] = useState(false);
  const [exportData, setExportData] = useState("");
  const [exportLoading, setExportLoading] = useState(false);
  const [selectedTunnelForExport, setSelectedTunnelForExport] = useState<
    number | null
  >(null);

  // 导入相关状态
  const [importModalOpen, setImportModalOpen] = useState(false);
  const [importData, setImportData] = useState("");
  const [importLoading, setImportLoading] = useState(false);
  const [importFormat, setImportFormat] = useState<ImportFormat>("flyx");
  const [selectedTunnelForImport, setSelectedTunnelForImport] = useState<
    number | null
  >(null);
  const [importResults, setImportResults] = useState<
    Array<{
      line: string;
      success: boolean;
      message: string;
      forwardName?: string;
    }>
  >([]);

  // 表单状态
  const [form, setForm] = useState<ForwardForm>({
    name: "",
    tunnelId: null,
    inPort: null,
    inIp: "",
    remoteAddr: "",
    interfaceName: "",
    strategy: "fifo",
    speedId: null,
    ipMaxConn: 0,
    ipSpeedId: null,
    maxConn: 0,
    proxyProtocol: 0,
    proxyProtocolReceive: 0,
    proxyProtocolSend: 0,
  });
  const [inIpTouched, setInIpTouched] = useState(false);

  // 表单验证错误
  const [errors, setErrors] = useState<{ [key: string]: string }>({});

  // 批量操作相关状态
  const [selectMode, setSelectMode] = useState(false);
  const [sheetForwardId, setSheetForwardId] = useState<number | null>(null);
  const [selectedIds, setSelectedIds] = useState<Set<number>>(new Set());
  const [batchDeleteModalOpen, setBatchDeleteModalOpen] = useState(false);
  const [batchChangeTunnelModalOpen, setBatchChangeTunnelModalOpen] =
    useState(false);
  const [batchTargetTunnelId, setBatchTargetTunnelId] = useState<number | null>(
    null,
  );
  const [batchLoading, setBatchLoading] = useState(false);
  const [batchProgress, setBatchProgress] = useState<BatchProgressState>({
    active: false,
    label: "",
    percent: 0,
  });
  const [batchResultModal, setBatchResultModal] =
    useState<BatchResultModalState>(EMPTY_BATCH_RESULT_MODAL_STATE);
  const [groupOrderMap, setGroupOrderMap] =
    useCachedState<ForwardGroupOrderMap>(PAGE, "groupOrderMap", {});
  const [collapsedTunnelGroups, setCollapsedTunnelGroups] =
    useCachedState<ForwardGroupCollapsedMap>(PAGE, "collapsedGroups", {});
  // A revisit keeps the cached grouping on screen while it reloads.
  const [groupPreferenceHydrated, setGroupPreferenceHydrated] = useState(() =>
    pageCached(PAGE),
  );

  const parseNodeIPs = (node?: Node): string[] => {
    if (!node) {
      return [];
    }

    const ips: string[] = [];
    const add = (value?: string) => {
      const trimmed = (value || "").trim();

      if (trimmed) {
        ips.push(trimmed);
      }
    };

    add(node.serverIpV4);
    add(node.serverIpV6);
    add(node.serverIp);

    (node.extraIPs || "")
      .split(",")
      .map((v) => v.trim())
      .filter((v) => v)
      .forEach((v) => ips.push(v));

    return Array.from(new Set(ips));
  };

  const tunnelInIpOptionMap = useMemo(() => {
    const map = new Map<number, string[]>();
    const nodeMap = new Map<number, Node>(nodes.map((n) => [n.id, n]));

    for (const tunnel of allTunnels) {
      const collected: string[] = [];
      const entryNodes = tunnel.inNodeId || [];

      for (const entry of entryNodes) {
        collected.push(...parseNodeIPs(nodeMap.get(entry.nodeId)));
      }

      if (collected.length === 0) {
        (tunnel.inIp || "")
          .split(",")
          .map((v) => v.trim())
          .filter((v) => v)
          .forEach((v) => collected.push(v));
      }

      map.set(tunnel.id, Array.from(new Set(collected)));
    }

    return map;
  }, [allTunnels, nodes]);

  const currentTunnelIpOptions = useMemo(() => {
    if (!form.tunnelId) {
      return [];
    }

    return tunnelInIpOptionMap.get(form.tunnelId) || [];
  }, [form.tunnelId, tunnelInIpOptionMap]);

  const isCurrentTunnelMultiEntrance = useMemo(() => {
    if (!form.tunnelId) {
      return false;
    }

    const currentTunnel = allTunnels.find(
      (tunnel) => tunnel.id === form.tunnelId,
    );

    return (currentTunnel?.inNodeId?.length || 0) > 1;
  }, [allTunnels, form.tunnelId]);

  const currentTunnelPortRange = useMemo(() => {
    if (!form.tunnelId) {
      return null;
    }

    const currentTunnel = allTunnels.find(
      (tunnel) => tunnel.id === form.tunnelId,
    );

    if (
      currentTunnel?.portRangeMin &&
      currentTunnel?.portRangeMax &&
      currentTunnel.portRangeMin > 0 &&
      currentTunnel.portRangeMax > 0
    ) {
      return {
        min: currentTunnel.portRangeMin,
        max: currentTunnel.portRangeMax,
      };
    }

    return null;
  }, [allTunnels, form.tunnelId]);

  useEffect(() => {
    return () => {
      diagnosisAbortRef.current?.abort();
      diagnosisAbortRef.current = null;
    };
  }, []);

  const persistGroupOrderToLocal = (nextOrderMap: ForwardGroupOrderMap) => {
    if (tokenUserId === null) {
      return;
    }

    try {
      localStorage.setItem(
        buildForwardGroupOrderLocalKey(tokenUserId),
        JSON.stringify(nextOrderMap),
      );
    } catch {}
  };

  const persistGroupCollapsedToLocal = (
    nextCollapsedMap: ForwardGroupCollapsedMap,
  ) => {
    if (tokenUserId === null) {
      return;
    }

    try {
      localStorage.setItem(
        buildForwardGroupCollapsedLocalKey(tokenUserId),
        JSON.stringify(nextCollapsedMap),
      );
    } catch {}
  };

  const persistGroupOrderToGlobal = async (
    nextOrderMap: ForwardGroupOrderMap,
  ): Promise<void> => {
    if (!isAdmin || tokenUserId === null) {
      return;
    }

    try {
      const currentRes = await getConfigByName(FORWARD_GROUP_ORDER_CONFIG_KEY);
      const globalMap =
        parsePreferenceMap<ForwardGroupOrderMap>(
          currentRes.code === 0 && typeof currentRes.data?.value === "string"
            ? currentRes.data.value
            : null,
        ) || {};

      globalMap[tokenUserId.toString()] = nextOrderMap;

      const saveRes = await updateConfig(
        FORWARD_GROUP_ORDER_CONFIG_KEY,
        JSON.stringify(globalMap),
      );

      if (saveRes.code !== 0) {
        toast.error(saveRes.msg || "保存分组排序失败");
      }
    } catch {
      toast.error("保存分组排序失败");
    }
  };

  const persistGroupCollapsedToGlobal = async (
    nextCollapsedMap: ForwardGroupCollapsedMap,
  ): Promise<void> => {
    if (!isAdmin || tokenUserId === null) {
      return;
    }

    try {
      const currentRes = await getConfigByName(
        FORWARD_GROUP_COLLAPSED_CONFIG_KEY,
      );
      const globalMap =
        parsePreferenceMap<ForwardGroupCollapsedMap>(
          currentRes.code === 0 && typeof currentRes.data?.value === "string"
            ? currentRes.data.value
            : null,
        ) || {};

      globalMap[tokenUserId.toString()] = nextCollapsedMap;

      const saveRes = await updateConfig(
        FORWARD_GROUP_COLLAPSED_CONFIG_KEY,
        JSON.stringify(globalMap),
      );

      if (saveRes.code !== 0) {
        toast.error(saveRes.msg || "保存分组折叠状态失败");
      }
    } catch {
      toast.error("保存分组折叠状态失败");
    }
  };

  useEffect(() => {
    let cancelled = false;

    const loadGroupPreferences = async () => {
      if (tokenUserId === null) {
        if (!cancelled) {
          setGroupOrderMap({});
          setCollapsedTunnelGroups({});
          setGroupPreferenceHydrated(true);
        }

        return;
      }

      let localOrderMap: ForwardGroupOrderMap = {};
      let localCollapsedMap: ForwardGroupCollapsedMap = {};

      try {
        localOrderMap = parseGroupOrderMap(
          localStorage.getItem(buildForwardGroupOrderLocalKey(tokenUserId)),
        );
      } catch {
        localOrderMap = {};
      }

      try {
        localCollapsedMap = parseGroupCollapsedMap(
          localStorage.getItem(buildForwardGroupCollapsedLocalKey(tokenUserId)),
        );
      } catch {
        localCollapsedMap = {};
      }

      if (isAdmin) {
        try {
          const [globalOrderRes, globalCollapsedRes] = await Promise.all([
            getConfigByName(FORWARD_GROUP_ORDER_CONFIG_KEY),
            getConfigByName(FORWARD_GROUP_COLLAPSED_CONFIG_KEY),
          ]);

          const globalOrderMap = parsePreferenceMap<ForwardGroupOrderMap>(
            globalOrderRes.code === 0 &&
              typeof globalOrderRes.data?.value === "string"
              ? globalOrderRes.data.value
              : null,
          );
          const globalCollapsedMap =
            parsePreferenceMap<ForwardGroupCollapsedMap>(
              globalCollapsedRes.code === 0 &&
                typeof globalCollapsedRes.data?.value === "string"
                ? globalCollapsedRes.data.value
                : null,
            );

          const globalOrderBucket = globalOrderMap?.[tokenUserId.toString()];
          const globalCollapsedBucket =
            globalCollapsedMap?.[tokenUserId.toString()];

          if (
            globalOrderBucket &&
            typeof globalOrderBucket === "object" &&
            !Array.isArray(globalOrderBucket)
          ) {
            localOrderMap = parseGroupOrderMap(
              JSON.stringify(globalOrderBucket),
            );
          }

          if (
            globalCollapsedBucket &&
            typeof globalCollapsedBucket === "object" &&
            !Array.isArray(globalCollapsedBucket)
          ) {
            localCollapsedMap = parseGroupCollapsedMap(
              JSON.stringify(globalCollapsedBucket),
            );
          }
        } catch {}
      }

      if (cancelled) {
        return;
      }

      setGroupOrderMap(localOrderMap);
      setCollapsedTunnelGroups(localCollapsedMap);
      persistGroupOrderToLocal(localOrderMap);
      persistGroupCollapsedToLocal(localCollapsedMap);
      setGroupPreferenceHydrated(true);
    };

    if (!pageCached(PAGE)) setGroupPreferenceHydrated(false);
    loadGroupPreferences();

    return () => {
      cancelled = true;
    };
  }, [tokenUserId, isAdmin]);

  useEffect(() => {
    const loadForwardCompactMode = async () => {
      try {
        const response = await getConfigByName(FORWARD_COMPACT_MODE_CONFIG_KEY);
        const enabled =
          response.code === 0 &&
          typeof response.data?.value === "string" &&
          response.data.value === "true";

        setCompactMode(enabled);
      } catch {
        setCompactMode(false);
      }
    };

    const handleCompactModeChanged = (event: Event) => {
      const customEvent = event as CustomEvent<{ enabled?: boolean }>;

      if (typeof customEvent.detail?.enabled === "boolean") {
        setCompactMode(customEvent.detail.enabled);
      }
    };

    loadForwardCompactMode();
    window.addEventListener(
      FORWARD_COMPACT_MODE_EVENT,
      handleCompactModeChanged,
    );

    return () => {
      window.removeEventListener(
        FORWARD_COMPACT_MODE_EVENT,
        handleCompactModeChanged,
      );
    };
  }, []);

  const parseShareIdFromTunnelName = (tunnelName: string): number | null => {
    const normalized = (tunnelName || "").trim();

    if (!normalized.startsWith("Share-")) {
      return null;
    }

    const raw = normalized.slice("Share-".length);
    const idx = raw.indexOf("-Port-");

    if (idx <= 0) {
      return null;
    }

    const shareId = Number(raw.slice(0, idx).trim());

    return Number.isFinite(shareId) && shareId > 0 ? shareId : null;
  };

  const mergeFederationShareFlow = useCallback(
    async (forwardsData: Forward[]): Promise<Forward[]> => {
      if (forwardsData.length === 0) {
        return forwardsData;
      }

      try {
        const [usageRes, localShareRes] = await Promise.all([
          getPeerRemoteUsageList(),
          getPeerShareList(),
        ]);

        const flowByShare = new Map<number, number>();
        const shareIdsByTunnel = new Map<number, Set<number>>();

        if (usageRes.code === 0 && Array.isArray(usageRes.data)) {
          usageRes.data.forEach((item: Record<string, unknown>) => {
            const shareId = Number(item.shareId || 0);
            const currentFlow = Number(item.currentFlow || 0);

            if (
              Number.isFinite(shareId) &&
              shareId > 0 &&
              Number.isFinite(currentFlow) &&
              currentFlow > 0
            ) {
              const prev = flowByShare.get(shareId) || 0;

              flowByShare.set(shareId, Math.max(prev, currentFlow));
            }

            if (Number.isFinite(shareId) && shareId > 0) {
              const bindings = Array.isArray(item.bindings)
                ? (item.bindings as Array<Record<string, unknown>>)
                : [];

              bindings.forEach((binding) => {
                const tunnelId = Number(binding.tunnelId || 0);
                const chainType = Number(binding.chainType || 0);

                if (!Number.isFinite(tunnelId) || tunnelId <= 0) {
                  return;
                }

                if (Number.isFinite(chainType) && chainType !== 1) {
                  return;
                }

                let shareSet = shareIdsByTunnel.get(tunnelId);

                if (!shareSet) {
                  shareSet = new Set<number>();
                  shareIdsByTunnel.set(tunnelId, shareSet);
                }

                shareSet.add(shareId);
              });
            }
          });
        }

        if (localShareRes.code === 0 && Array.isArray(localShareRes.data)) {
          localShareRes.data.forEach((item: Record<string, unknown>) => {
            const shareId = Number(item.id || 0);
            const currentFlow = Number(item.currentFlow || 0);

            if (
              Number.isFinite(shareId) &&
              shareId > 0 &&
              Number.isFinite(currentFlow) &&
              currentFlow > 0
            ) {
              const prev = flowByShare.get(shareId) || 0;

              flowByShare.set(shareId, Math.max(prev, currentFlow));
            }
          });
        }

        if (flowByShare.size === 0) {
          return forwardsData;
        }

        const resolveShareIdForForward = (forward: Forward): number | null => {
          const candidates = new Set<number>();
          const shareIdFromName = parseShareIdFromTunnelName(
            forward.tunnelName || "",
          );

          if (shareIdFromName) {
            candidates.add(shareIdFromName);
          }

          const tunnelId = Number(forward.tunnelId || 0);
          const shareSetByTunnel = shareIdsByTunnel.get(tunnelId);

          if (shareSetByTunnel && shareSetByTunnel.size > 0) {
            shareSetByTunnel.forEach((shareId) => {
              if (Number.isFinite(shareId) && shareId > 0) {
                candidates.add(shareId);
              }
            });
          }

          if (candidates.size === 0) {
            return null;
          }

          let bestShareId: number | null = null;
          let bestFlow = 0;

          candidates.forEach((shareId) => {
            const shareFlow = flowByShare.get(shareId) || 0;

            if (shareFlow > bestFlow) {
              bestFlow = shareFlow;
              bestShareId = shareId;
            }
          });

          return bestShareId;
        };

        const resolvedShareByForwardId = new Map<number, number>();

        forwardsData.forEach((forward) => {
          const shareId = resolveShareIdForForward(forward);

          if (shareId) {
            resolvedShareByForwardId.set(forward.id, shareId);
          }
        });

        const forwardCountByShare = new Map<number, number>();

        forwardsData.forEach((forward) => {
          const shareId = resolvedShareByForwardId.get(forward.id) || null;

          if (!shareId || !flowByShare.has(shareId)) {
            return;
          }

          forwardCountByShare.set(
            shareId,
            (forwardCountByShare.get(shareId) || 0) + 1,
          );
        });

        return forwardsData.map((forward) => {
          const shareId = resolvedShareByForwardId.get(forward.id) || null;

          if (!shareId) {
            return { ...forward, federationShareFlow: undefined };
          }

          const shareFlow = flowByShare.get(shareId) || 0;

          if (shareFlow <= 0) {
            return { ...forward, federationShareFlow: undefined };
          }

          const directFlow = (forward.inFlow || 0) + (forward.outFlow || 0);

          if (directFlow > 0) {
            return { ...forward, federationShareFlow: undefined };
          }

          const count = forwardCountByShare.get(shareId) || 1;
          const estimated = Math.max(1, Math.floor(shareFlow / count));

          return { ...forward, federationShareFlow: estimated };
        });
      } catch {
        return forwardsData;
      }
    },
    [],
  );

  const applyForwardList = useCallback(
    async (items: Forward[]) => {
      const mergedForwards = await mergeFederationShareFlow(
        normalizeForwardItems(items),
      );

      setForwards(mergedForwards);

      const currentUserId = JwtUtil.getUserIdFromToken();
      const { order, fromDatabase } = buildForwardOrder(
        mergedForwards,
        currentUserId,
      );

      setForwardOrder(order);

      if (fromDatabase) {
        saveOrder(FORWARD_ORDER_KEY, order);
      }
    },
    [mergeFederationShareFlow],
  );

  const refreshForwardList = useCallback(
    async (lod = true) => {
      if (lod) {
        setLoading(true);
      }

      try {
        const forwardsRes = await getForwardList();

        if (forwardsRes.code === 0) {
          await applyForwardList(mapForwardApiItems(forwardsRes.data || []));
        } else {
          toast.error(forwardsRes.msg || "获取规则列表失败");
        }
      } catch {
        toast.error("获取规则列表失败");
      } finally {
        if (lod) {
          setLoading(false);
        }
      }
    },
    [applyForwardList],
  );

  // 加载所有数据
  const loadData = useCallback(
    async (lod = true) => {
      setLoading(lod);
      try {
        // One round trip: the lists don't depend on each other.
        const [[tunnelsRes, speedLimitsRes], [allTunnelsRes, nodesRes]] =
          await Promise.all([
            Promise.all([userTunnel(), getSpeedLimitList()]),
            Promise.allSettled([getTunnelList(), getNodeList()]),
            refreshForwardList(false),
          ]);

        if (tunnelsRes.code === 0) {
          setTunnels(tunnelsRes.data || []);
        } else {
        }

        if (
          allTunnelsRes.status === "fulfilled" &&
          allTunnelsRes.value.code === 0
        ) {
          setAllTunnels((allTunnelsRes.value.data || []) as Tunnel[]);
        }

        if (nodesRes.status === "fulfilled" && nodesRes.value.code === 0) {
          setNodes((nodesRes.value.data || []) as Node[]);
        }

        if (speedLimitsRes.code === 0) {
          setSpeedLimits(speedLimitsRes.data || []);
        }
        markPageCached(PAGE);
      } catch {
        toast.error("加载数据失败");
      } finally {
        setLoading(false);
      }
    },
    [refreshForwardList],
  );

  useEffect(() => {
    loadData();
  }, [loadData]);

  // 表单验证
  const noLimitSpeedLimitIds = useMemo(() => {
    return new Set(
      speedLimits
        .filter((speedLimit) => speedLimit.name.trim() === "不限速")
        .map((speedLimit) => speedLimit.id),
    );
  }, [speedLimits]);

  const speedLimitIds = useMemo(() => {
    return new Set(speedLimits.map((speedLimit) => speedLimit.id));
  }, [speedLimits]);

  const availableSpeedLimits = useMemo(() => {
    return speedLimits.filter(
      (speedLimit) => !noLimitSpeedLimitIds.has(speedLimit.id),
    );
  }, [speedLimits, noLimitSpeedLimitIds]);

  const normalizeSpeedId = (speedId?: number | null): number | null => {
    if (speedId === null || speedId === undefined) {
      return null;
    }

    if (noLimitSpeedLimitIds.has(speedId)) {
      return null;
    }

    if (speedLimits.length > 0 && !speedLimitIds.has(speedId)) {
      return null;
    }

    return speedId;
  };

  const isMissingSpeedLimit = (speedId?: number | null): boolean => {
    if (speedId === null || speedId === undefined) {
      return false;
    }

    if (speedLimits.length === 0 || noLimitSpeedLimitIds.has(speedId)) {
      return false;
    }

    return !speedLimitIds.has(speedId);
  };

  const selectedSpeedId = normalizeSpeedId(form.speedId);
  const selectedIPSpeedId = normalizeSpeedId(form.ipSpeedId);

  const validateForm = (): boolean => {
    const newErrors: { [key: string]: string } = {};

    if (!form.name.trim()) {
      newErrors.name = "请输入规则名称";
    } else if (form.name.length < 2 || form.name.length > 50) {
      newErrors.name = "规则名称长度应在2-50个字符之间";
    }

    if (!form.tunnelId) {
      newErrors.tunnelId = "请选择关联隧道";
    }

    if (
      form.inPort !== null &&
      form.inPort !== undefined &&
      form.inPort > 0 &&
      currentTunnelPortRange
    ) {
      if (
        form.inPort < currentTunnelPortRange.min ||
        form.inPort > currentTunnelPortRange.max
      ) {
        newErrors.inPort = `端口 ${currentTunnelPortRange.min}-${currentTunnelPortRange.max} 超出允许范围`;
      }
    }

    if (!form.remoteAddr.trim()) {
      newErrors.remoteAddr = "请输入落地地址";
    } else {
      // 验证地址格式
      const addresses = form.remoteAddr
        .split("\n")
        .map((addr) => addr.trim())
        .filter((addr) => addr);
      const ipv4Pattern =
        /^(25[0-5]|2[0-4][0-9]|[01]?[0-9][0-9]?)\.(25[0-5]|2[0-4][0-9]|[01]?[0-9][0-9]?)\.(25[0-5]|2[0-4][0-9]|[01]?[0-9][0-9]?)\.(25[0-5]|2[0-4][0-9]|[01]?[0-9][0-9]?):\d+$/;
      const ipv6FullPattern =
        /^\[((([0-9a-fA-F]{1,4}:){7}([0-9a-fA-F]{1,4}|:))|(([0-9a-fA-F]{1,4}:){6}(:[0-9a-fA-F]{1,4}|((25[0-5]|2[0-4]\d|1\d\d|[1-9]?\d)(\.(25[0-5]|2[0-4]\d|1\d\d|[1-9]?\d)){3})|:))|(([0-9a-fA-F]{1,4}:){5}(((:[0-9a-fA-F]{1,4}){1,2})|:((25[0-5]|2[0-4]\d|1\d\d|[1-9]?\d)(\.(25[0-5]|2[0-4]\d|1\d\d|[1-9]?\d)){3})|:))|(([0-9a-fA-F]{1,4}:){4}(((:[0-9a-fA-F]{1,4}){1,3})|((:[0-9a-fA-F]{1,4})?:((25[0-5]|2[0-4]\d|1\d\d|[1-9]?\d)(\.(25[0-5]|2[0-4]\d|1\d\d|[1-9]?\d)){3}))|:))|(([0-9a-fA-F]{1,4}:){3}(((:[0-9a-fA-F]{1,4}){1,4})|((:[0-9a-fA-F]{1,4}){0,2}:((25[0-5]|2[0-4]\d|1\d\d|[1-9]?\d)(\.(25[0-5]|2[0-4]\d|1\d\d|[1-9]?\d)){3}))|:))|(([0-9a-fA-F]{1,4}:){2}(((:[0-9a-fA-F]{1,4}){1,5})|((:[0-9a-fA-F]{1,4}){0,3}:((25[0-5]|2[0-4]\d|1\d\d|[1-9]?\d)(\.(25[0-5]|2[0-4]\d|1\d\d|[1-9]?\d)){3}))|:))|(([0-9a-fA-F]{1,4}:){1}(((:[0-9a-fA-F]{1,4}){1,6})|((:[0-9a-fA-F]{1,4}){0,4}:((25[0-5]|2[0-4]\d|1\d\d|[1-9]?\d)(\.(25[0-5]|2[0-4]\d|1\d\d|[1-9]?\d)){3}))|:))|(:(((:[0-9a-fA-F]{1,4}){1,7})|((:[0-9a-fA-F]{1,4}){0,5}:((25[0-5]|2[0-4]\d|1\d\d|[1-9]?\d)(\.(25[0-5]|2[0-4]\d|1\d\d|[1-9]?\d)){3}))|:)))\]:\d+$/;
      const domainPattern =
        /^[a-zA-Z0-9]([a-zA-Z0-9\-]{0,61}[a-zA-Z0-9])?(\.[a-zA-Z0-9]([a-zA-Z0-9\-]{0,61}[a-zA-Z0-9])?)*:\d+$/;

      for (let i = 0; i < addresses.length; i++) {
        const addr = addresses[i];

        if (
          !ipv4Pattern.test(addr) &&
          !ipv6FullPattern.test(addr) &&
          !domainPattern.test(addr)
        ) {
          newErrors.remoteAddr = `第${i + 1}行地址格式错误`;
          break;
        }
      }
    }

    setErrors(newErrors);

    return Object.keys(newErrors).length === 0;
  };

  // 新增规则
  const handleAdd = () => {
    setIsEdit(false);
    setInIpTouched(false);
    setForm({
      name: "",
      tunnelId: null,
      inPort: null,
      inIp: "",
      remoteAddr: "",
      interfaceName: "",
      strategy: "fifo",
      speedId: null,
      ipMaxConn: 0,
      ipSpeedId: null,
      proxyProtocol: 0,
      proxyProtocolReceive: 0,
      proxyProtocolSend: 0,
    });
    setErrors({});
    setModalOpen(true);
  };

  // 编辑规则
  const handleEdit = (forward: Forward) => {
    setIsEdit(true);
    setInIpTouched(false);
    setForm({
      id: forward.id,
      userId: forward.userId,
      name: forward.name,
      tunnelId: forward.tunnelId,
      inPort: forward.inPort,
      inIp: forward.inIp || "",
      remoteAddr: forward.remoteAddr.split(",").join("\n"),
      interfaceName: forward.interfaceName || "",
      strategy: forward.strategy || "fifo",
      speedId: normalizeSpeedId(forward.speedId),
      ipMaxConn: forward.ipMaxConn ?? 0,
      ipSpeedId: normalizeSpeedId(forward.ipSpeedId),
      maxConn: forward.maxConn ?? 0,
      proxyProtocol: forward.proxyProtocol ?? 0,
      proxyProtocolReceive: forward.proxyProtocolReceive ?? 0,
      proxyProtocolSend:
        forward.proxyProtocolSend ?? forward.proxyProtocol ?? 0,
    });
    setErrors({});
    setModalOpen(true);
  };

  // 显示删除确认
  const handleDelete = (forward: Forward) => {
    setForwardToDelete(forward);
    setDeleteModalOpen(true);
  };

  const handleResetFlow = (forward: Forward) => {
    if ((forward.inFlow || 0) + (forward.outFlow || 0) <= 0) return;

    setForwardToResetFlow(forward);
    setResetFlowModalOpen(true);
  };

  const handleResetFlowModalOpenChange = (isOpen: boolean) => {
    if (resetFlowLoading) return;

    setResetFlowModalOpen(isOpen);
    if (!isOpen) {
      setForwardToResetFlow(null);
    }
  };

  const confirmResetFlow = async () => {
    if (!forwardToResetFlow) return;

    setResetFlowLoading(true);
    try {
      const res = await resetForwardFlow(forwardToResetFlow.id);

      if (res.code !== 0) {
        toast.error(res.msg || "流量清零失败");

        return;
      }

      toast.success("规则流量已清零");
      setResetFlowModalOpen(false);
      setForwardToResetFlow(null);
      await refreshForwardList(false);
    } catch {
      toast.error("流量清零失败");
    } finally {
      setResetFlowLoading(false);
    }
  };

  // 确认删除规则
  const confirmDelete = async () => {
    if (!forwardToDelete) return;

    setDeleteLoading(true);
    try {
      const res = await deleteForward(forwardToDelete.id);

      if (res.code === 0) {
        toast.success("删除成功");
        setDeleteModalOpen(false);
        setForwardToDelete(null);
        setForwards((prev) =>
          prev.filter((forward) => forward.id !== forwardToDelete.id),
        );
        setForwardOrder((prev) => {
          const next = prev.filter((id) => id !== forwardToDelete.id);

          saveOrder(FORWARD_ORDER_KEY, next);

          return next;
        });
        setSelectedIds((prev) => {
          const next = new Set(prev);

          next.delete(forwardToDelete.id);

          return next;
        });
      } else {
        // 删除失败，询问是否强制删除
        const confirmed = window.confirm(
          `常规删除失败：${res.msg || "删除失败"}\n\n是否需要强制删除？\n\n⚠️ 注意：强制删除不会去验证节点端是否已经删除对应的规则服务。`,
        );

        if (confirmed) {
          const forceRes = await forceDeleteForward(forwardToDelete.id);

          if (forceRes.code === 0) {
            toast.success("强制删除成功");
            setDeleteModalOpen(false);
            setForwardToDelete(null);
            setForwards((prev) =>
              prev.filter((forward) => forward.id !== forwardToDelete.id),
            );
            setForwardOrder((prev) => {
              const next = prev.filter((id) => id !== forwardToDelete.id);

              saveOrder(FORWARD_ORDER_KEY, next);

              return next;
            });
            setSelectedIds((prev) => {
              const next = new Set(prev);

              next.delete(forwardToDelete.id);

              return next;
            });
          } else {
            toast.error(forceRes.msg || "强制删除失败");
          }
        }
      }
    } catch {
      toast.error("删除失败");
    } finally {
      setDeleteLoading(false);
    }
  };

  // 处理隧道选择变化
  const handleTunnelChange = (tunnelId: string) => {
    const nextTunnelId = parseInt(tunnelId);
    const options = tunnelInIpOptionMap.get(nextTunnelId) || [];

    setInIpTouched(false);

    setForm((prev) => {
      const tunnelChanged = prev.tunnelId !== nextTunnelId;

      return {
        ...prev,
        tunnelId: nextTunnelId,
        inIp: tunnelChanged ? "" : options.includes(prev.inIp) ? prev.inIp : "",
      };
    });
  };

  // 提交表单
  const handleSubmit = async () => {
    if (!validateForm()) return;

    setSubmitLoading(true);
    try {
      const processedRemoteAddr = form.remoteAddr
        .split("\n")
        .map((addr) => addr.trim())
        .filter((addr) => addr)
        .join(",");

      const addressCount = processedRemoteAddr.split(",").length;

      let res: { code: number; msg: string };
      const normalizedSpeedId = normalizeSpeedId(form.speedId);
      const speedLimitAutoCleared = isMissingSpeedLimit(form.speedId);
      const normalizedIPSpeedId = normalizeSpeedId(form.ipSpeedId);
      const ipSpeedLimitAutoCleared = isMissingSpeedLimit(form.ipSpeedId);

      if (isEdit) {
        const updateData = {
          id: form.id,
          name: form.name,
          tunnelId: form.tunnelId,
          inPort: form.inPort,
          ...(inIpTouched ? { inIp: form.inIp || "" } : {}),
          remoteAddr: processedRemoteAddr,
          strategy: addressCount > 1 ? form.strategy : "fifo",
          speedId: normalizedSpeedId,
          ipMaxConn: form.ipMaxConn,
          ...(isAdmin ? { ipSpeedId: normalizedIPSpeedId } : {}),
          maxConn: form.maxConn,
          proxyProtocol: form.proxyProtocolSend,
          proxyProtocolReceive: form.proxyProtocolReceive,
          proxyProtocolSend: form.proxyProtocolSend,
        };

        res = await updateForward(updateData);
      } else {
        const createData = {
          name: form.name,
          tunnelId: form.tunnelId,
          inPort: form.inPort,
          inIp: form.inIp || undefined,
          remoteAddr: processedRemoteAddr,
          strategy: addressCount > 1 ? form.strategy : "fifo",
          speedId: normalizedSpeedId,
          ipMaxConn: form.ipMaxConn,
          ...(isAdmin ? { ipSpeedId: normalizedIPSpeedId } : {}),
          maxConn: form.maxConn,
          proxyProtocol: form.proxyProtocolSend,
          proxyProtocolReceive: form.proxyProtocolReceive,
          proxyProtocolSend: form.proxyProtocolSend,
        };

        res = await createForward(createData);
      }
      if (res.code === 0) {
        const warningItems = Array.isArray((res as any).data?.warnings)
          ? (res as any).data.warnings
              .map((item: unknown) =>
                typeof item === "string" ? item.trim() : "",
              )
              .filter((item: string) => item)
          : [];

        warningItems.forEach((warning: string) => {
          toast(warning, {
            icon: "⚠️",
            duration: 5000,
          });
        });
        if (speedLimitAutoCleared) {
          toast("所选限速规则不存在，已自动清除为不限速", {
            icon: "⚠️",
            duration: 5000,
          });
        }
        if (isAdmin && ipSpeedLimitAutoCleared) {
          toast("所选每 IP 限速规则不存在，已自动清除为不限速", {
            icon: "⚠️",
            duration: 5000,
          });
        }
        toast.success(isEdit ? "修改成功" : "创建成功");
        setModalOpen(false);
        await refreshForwardList(false);
      } else {
        toast.error(res.msg || "操作失败");
      }
    } catch {
      toast.error("操作失败");
    } finally {
      setSubmitLoading(false);
    }
  };

  // 处理服务开关
  const handleServiceToggle = async (forward: Forward) => {
    if (forward.status !== 1 && forward.status !== 0) {
      toast.error("规则状态异常，无法操作");

      return;
    }

    const targetState = !forward.serviceRunning;

    try {
      // 乐观更新UI
      setForwards((prev) =>
        prev.map((f) =>
          f.id === forward.id ? { ...f, serviceRunning: targetState } : f,
        ),
      );

      let res: { code: number; msg: string };

      if (targetState) {
        res = await resumeForwardService(forward.id);
      } else {
        res = await pauseForwardService(forward.id);
      }

      if (res.code === 0) {
        toast.success(targetState ? "服务已启动" : "服务已暂停");
        // 更新规则状态
        setForwards((prev) =>
          prev.map((f) =>
            f.id === forward.id ? { ...f, status: targetState ? 1 : 0 } : f,
          ),
        );
      } else {
        // 操作失败，恢复UI状态
        setForwards((prev) =>
          prev.map((f) =>
            f.id === forward.id ? { ...f, serviceRunning: !targetState } : f,
          ),
        );
        toast.error(res.msg || "操作失败");
      }
    } catch {
      // 操作失败，恢复UI状态
      setForwards((prev) =>
        prev.map((f) =>
          f.id === forward.id ? { ...f, serviceRunning: !targetState } : f,
        ),
      );
      toast.error("网络错误，操作失败");
    }
  };

  // 诊断规则
  const handleDiagnose = async (forward: Forward) => {
    diagnosisAbortRef.current?.abort();
    const abortController = new AbortController();

    diagnosisAbortRef.current = abortController;

    setCurrentDiagnosisForward(forward);
    setDiagnosisModalOpen(true);
    setDiagnosisLoading(true);
    setDiagnosisProgress({
      total: 0,
      completed: 0,
      success: 0,
      failed: 0,
      timedOut: false,
    });
    setDiagnosisResult({
      forwardName: forward.name,
      timestamp: Date.now(),
      results: [],
    });

    try {
      let streamErrorMessage = "";
      const streamResult = await diagnoseForwardStream(
        forward.id,
        {
          onStart: (payload) => {
            const startForwardName =
              typeof payload.forwardName === "string" &&
              payload.forwardName.trim() !== ""
                ? payload.forwardName
                : forward.name;
            const startTotal = Number(payload.total);
            const startItems = Array.isArray(payload.items)
              ? (payload.items as ForwardDiagnosisResult["results"])
              : [];

            setDiagnosisResult((prev) => ({
              forwardName: startForwardName,
              timestamp: Date.now(),
              results: startItems.length > 0 ? startItems : prev?.results || [],
            }));
            if (Number.isFinite(startTotal) && startTotal >= 0) {
              setDiagnosisProgress((prev) => ({
                ...prev,
                total: startTotal,
              }));
            }
          },
          onItem: ({ result, progress }) => {
            setDiagnosisResult((prev) => {
              const base: ForwardDiagnosisResult = prev || {
                forwardName: forward.name,
                timestamp: Date.now(),
                results: [],
              };
              const nextResults = [...base.results];
              const existingIndex = nextResults.findIndex(
                (item) =>
                  item.description === result.description &&
                  item.nodeId === result.nodeId &&
                  item.targetIp === result.targetIp &&
                  item.targetPort === result.targetPort,
              );

              if (existingIndex >= 0) {
                nextResults[existingIndex] = {
                  ...result,
                  diagnosing: false,
                };
              } else {
                nextResults.push({
                  ...result,
                  diagnosing: false,
                });
              }

              return {
                ...base,
                timestamp: Date.now(),
                results: nextResults,
              };
            });
            setDiagnosisProgress({
              total: progress.total,
              completed: progress.completed,
              success: progress.success,
              failed: progress.failed,
              timedOut: Boolean(progress.timedOut),
            });
          },
          onDone: (progress) => {
            setDiagnosisProgress({
              total: progress.total,
              completed: progress.completed,
              success: progress.success,
              failed: progress.failed,
              timedOut: Boolean(progress.timedOut),
            });
          },
          onError: (message) => {
            streamErrorMessage = message;
          },
        },
        abortController.signal,
      );

      if (streamResult.fallback) {
        const response = await diagnoseForward(forward.id);

        if (response.code === 0) {
          const resultData = response.data as ForwardDiagnosisResult;
          const successCount = resultData.results.filter(
            (r) => r.success,
          ).length;
          const failedCount = resultData.results.length - successCount;

          setDiagnosisResult(resultData);
          setDiagnosisProgress({
            total: resultData.results.length,
            completed: resultData.results.length,
            success: successCount,
            failed: failedCount,
            timedOut: false,
          });
        } else {
          toast.error(response.msg || "诊断失败");
          setDiagnosisResult(
            buildForwardDiagnosisFallbackResult({
              forwardName: forward.name,
              remoteAddr: forward.remoteAddr,
              description: "诊断失败",
              message: response.msg || "诊断过程中发生错误",
            }),
          );
          setDiagnosisProgress({
            total: 1,
            completed: 1,
            success: 0,
            failed: 1,
            timedOut: false,
          });
        }

        return;
      }

      if (streamErrorMessage) {
        toast.error(streamErrorMessage);
      }
      if (streamResult.timedOut) {
        toast.error("诊断超时（单条30秒 / 整体2分钟），已返回当前结果");
      }
    } catch {
      if (abortController.signal.aborted) {
        return;
      }
      toast.error("网络错误，请重试");
      setDiagnosisResult(
        buildForwardDiagnosisFallbackResult({
          forwardName: forward.name,
          remoteAddr: forward.remoteAddr,
          description: "网络错误",
          message: "无法连接到服务器",
        }),
      );
      setDiagnosisProgress({
        total: 1,
        completed: 1,
        success: 0,
        failed: 1,
        timedOut: false,
      });
    } finally {
      if (diagnosisAbortRef.current === abortController) {
        diagnosisAbortRef.current = null;
      }
      setDiagnosisLoading(false);
    }
  };

  // 格式化流量
  const formatFlow = (value: number): string => {
    return formatTraffic(value);
  };

  // 显示地址列表弹窗
  const showAddressModal = (
    addressString: string,
    port: number | null,
    title: string,
  ) => {
    const action = resolveForwardAddressAction(addressString, port, title);

    if (action.type === "none") {
      return;
    }

    if (action.type === "copy") {
      copyToClipboard(action.text, action.label);

      return;
    }

    setAddressList(action.items);
    setAddressModalTitle(action.title);
    setAddressModalOpen(true);
  };

  // 复制到剪贴板
  const copyToClipboard = async (text: string, label: string = "内容") => {
    await copyWithToast(text, `已复制${label}`);
  };

  // 复制地址
  const copyAddress = async (addressItem: ForwardAddressItem) => {
    try {
      setAddressList((prev) =>
        prev.map((item) =>
          item.id === addressItem.id ? { ...item, copying: true } : item,
        ),
      );
      await copyToClipboard(addressItem.address, "地址");
    } catch {
      toast.error("复制失败");
    } finally {
      setAddressList((prev) =>
        prev.map((item) =>
          item.id === addressItem.id ? { ...item, copying: false } : item,
        ),
      );
    }
  };

  // 复制所有地址
  const copyAllAddresses = async () => {
    if (addressList.length === 0) return;
    const allAddresses = addressList.map((item) => item.address).join("\n");

    await copyToClipboard(allAddresses, "所有地址");
  };

  // 导出规则数据
  const handleExport = () => {
    setSelectedTunnelForExport(null);
    setExportData("");
    setExportModalOpen(true);
  };

  // 执行导出
  const executeExport = () => {
    if (!selectedTunnelForExport) {
      toast.error("请选择要导出的隧道");

      return;
    }

    setExportLoading(true);

    try {
      // 获取要导出的规则列表
      const forwardsToExport = sortedForwards.filter(
        (forward) => forward.tunnelId === selectedTunnelForExport,
      );

      if (forwardsToExport.length === 0) {
        toast.error("所选隧道没有规则数据");
        setExportLoading(false);

        return;
      }

      // 格式化导出数据：remoteAddr|name|inPort
      const exportLines = forwardsToExport.map((forward) => {
        return `${forward.remoteAddr}|${forward.name}|${forward.inPort}`;
      });

      const exportText = exportLines.join("\n");

      setExportData(exportText);
    } catch {
      toast.error("导出失败");
    } finally {
      setExportLoading(false);
    }
  };

  // 复制导出数据
  const copyExportData = async () => {
    await copyToClipboard(exportData, "规则数据");
  };

  // 导入规则数据
  const handleImport = () => {
    setImportData("");
    setImportResults([]);
    setSelectedTunnelForImport(null);
    setImportModalOpen(true);
  };

  // 执行导入
  const executeImport = async () => {
    if (!importData.trim()) {
      toast.error("请输入要导入的数据");

      return;
    }

    if (!selectedTunnelForImport) {
      toast.error("请选择要导入的隧道");

      return;
    }

    setImportLoading(true);
    setImportResults([]);

    try {
      if (importFormat === "ny") {
        const parsedItems = parseNyFormatData(importData);

        if (parsedItems.length === 0) {
          toast.error("未解析到有效的ny格式数据");

          setImportLoading(false);

          return;
        }

        for (const item of parsedItems) {
          if (item.error) {
            setImportResults((prev) => [
              {
                line: item.line,
                success: false,
                message: item.error || "解析失败",
              },
              ...prev,
            ]);

            continue;
          }

          if (!item.parsed) {
            setImportResults((prev) => [
              {
                line: item.line,
                success: false,
                message: "解析失败",
              },
              ...prev,
            ]);

            continue;
          }

          const parsedNyItem = item.parsed;
          const nyForwardInput = convertNyItemToForwardInput(parsedNyItem);

          try {
            const response = await createForward({
              name: nyForwardInput.name,
              tunnelId: selectedTunnelForImport,
              inPort: nyForwardInput.inPort,
              remoteAddr: nyForwardInput.remoteAddr,
              strategy: nyForwardInput.strategy,
            });

            if (response.code === 0) {
              setImportResults((prev) => [
                {
                  line: item.line,
                  success: true,
                  message: `创建成功 (${parsedNyItem.dest.length}个目标)`,
                  forwardName: nyForwardInput.name,
                },
                ...prev,
              ]);
            } else {
              setImportResults((prev) => [
                {
                  line: item.line,
                  success: false,
                  message: response.msg || "创建失败",
                },
                ...prev,
              ]);
            }
          } catch {
            setImportResults((prev) => [
              {
                line: item.line,
                success: false,
                message: "网络错误，创建失败",
              },
              ...prev,
            ]);
          }
        }
      } else {
        const lines = importData
          .trim()
          .split("\n")
          .filter((line) => line.trim());

        for (let i = 0; i < lines.length; i++) {
          const line = lines[i].trim();
          const parts = line.split("|");

          if (parts.length < 2) {
            setImportResults((prev) => [
              {
                line,
                success: false,
                message: "格式错误：需要至少包含目标地址和规则名称",
              },
              ...prev,
            ]);
            continue;
          }

          const [remoteAddr, name, inPort] = parts;

          if (!remoteAddr.trim() || !name.trim()) {
            setImportResults((prev) => [
              {
                line,
                success: false,
                message: "目标地址和规则名称不能为空",
              },
              ...prev,
            ]);
            continue;
          }

          const addresses = remoteAddr.trim().split(",");
          const addressPattern = /^[^:]+:\d+$/;
          const isValidFormat = addresses.every((addr) =>
            addressPattern.test(addr.trim()),
          );

          if (!isValidFormat) {
            setImportResults((prev) => [
              {
                line,
                success: false,
                message:
                  "目标地址格式错误，应为 地址:端口 格式，多个地址用逗号分隔",
              },
              ...prev,
            ]);
            continue;
          }

          try {
            let portNumber: number | null = null;

            if (inPort && inPort.trim()) {
              const port = parseInt(inPort.trim());

              if (isNaN(port) || port < 1 || port > 65535) {
                setImportResults((prev) => [
                  {
                    line,
                    success: false,
                    message: "入口端口格式错误，应为1-65535之间的数字",
                  },
                  ...prev,
                ]);
                continue;
              }
              portNumber = port;
            }

            const response = await createForward({
              name: name.trim(),
              tunnelId: selectedTunnelForImport,
              inPort: portNumber,
              remoteAddr: remoteAddr.trim(),
              strategy: "fifo",
            });

            if (response.code === 0) {
              setImportResults((prev) => [
                {
                  line,
                  success: true,
                  message: "创建成功",
                  forwardName: name.trim(),
                },
                ...prev,
              ]);
            } else {
              setImportResults((prev) => [
                {
                  line,
                  success: false,
                  message: response.msg || "创建失败",
                },
                ...prev,
              ]);
            }
          } catch {
            setImportResults((prev) => [
              {
                line,
                success: false,
                message: "网络错误，创建失败",
              },
              ...prev,
            ]);
          }
        }
      }

      toast.success("导入执行完成");

      await refreshForwardList(false);
    } catch {
      toast.error("导入过程中发生错误");
    } finally {
      setImportLoading(false);
    }
  };

  // 获取状态显示
  const getStatusDisplay = (status: number) => {
    switch (status) {
      case 1:
        return { color: "success", text: "正常" };
      case 0:
        return { color: "warning", text: "暂停" };
      case -1:
        return { color: "danger", text: "异常" };
      default:
        return { color: "default", text: "未知" };
    }
  };

  // 获取策略显示
  const getStrategyDisplay = (strategy: string) => {
    switch (strategy) {
      case "fifo":
        return { color: "primary", text: "主备" };
      case "round":
        return { color: "success", text: "轮询" };
      case "rand":
        return { color: "warning", text: "随机" };
      default:
        return { color: "default", text: "未知" };
    }
  };

  // 获取地址数量
  const getAddressCount = (addressString: string): number => {
    if (!addressString) return 0;
    const addresses = addressString
      .split("\n")
      .map((addr) => addr.trim())
      .filter((addr) => addr);

    return addresses.length;
  };

  // 处理拖拽结束
  const handleDragEnd = async (event: DragEndEvent) => {
    const { active, over } = event;

    if (!active || !over || active.id === over.id) return;

    const activeGroup = parseTunnelGroupSortableId(active.id);
    const overGroup = parseTunnelGroupSortableId(over.id);

    if (activeGroup && overGroup) {
      if (compactMode || !groupPreferenceHydrated) {
        return;
      }

      if (activeGroup.userId !== overGroup.userId) {
        return;
      }

      const userIdKey = activeGroup.userId.toString();
      const currentOrder = groupOrderMap[userIdKey] || [];
      const oldIndex = currentOrder.indexOf(activeGroup.tunnelKey);
      const newIndex = currentOrder.indexOf(overGroup.tunnelKey);

      if (oldIndex < 0 || newIndex < 0 || oldIndex === newIndex) {
        return;
      }

      const moved = arrayMove(currentOrder, oldIndex, newIndex);
      const nextOrderMap: ForwardGroupOrderMap = {
        ...groupOrderMap,
        [userIdKey]: moved,
      };

      setGroupOrderMap(nextOrderMap);
      persistGroupOrderToLocal(nextOrderMap);
      void persistGroupOrderToGlobal(nextOrderMap);

      return;
    }

    // 确保 forwardOrder 存在且有效
    if (!forwardOrder || forwardOrder.length === 0) return;

    const activeId = Number(active.id);
    const overId = Number(over.id);

    // 检查 ID 是否有效
    if (isNaN(activeId) || isNaN(overId)) return;

    const activeForward = forwards.find((forward) => forward.id === activeId);
    const overForward = forwards.find((forward) => forward.id === overId);
    const activeUserId = activeForward?.userId ?? 0;
    const overUserId = overForward?.userId ?? 0;
    const activeTunnelGroupKey = buildForwardTunnelGroupKey(
      activeForward?.tunnelName,
    );
    const overTunnelGroupKey = buildForwardTunnelGroupKey(
      overForward?.tunnelName,
    );

    // 非精简模式仅允许在同一用户+隧道分组内拖拽，避免混排
    if (!compactMode) {
      if (
        activeUserId !== overUserId ||
        activeTunnelGroupKey !== overTunnelGroupKey
      ) {
        return;
      }
    }

    let oldIndex: number;
    let newIndex: number;
    let currentOrder: number[];

    if (compactMode) {
      currentOrder = sortedForwards.map((f) => f.id);
      oldIndex = currentOrder.indexOf(activeId);
      newIndex = currentOrder.indexOf(overId);
    } else {
      currentOrder = forwardOrder;
      oldIndex = forwardOrder.indexOf(activeId);
      newIndex = forwardOrder.indexOf(overId);
    }

    if (oldIndex !== -1 && newIndex !== -1 && oldIndex !== newIndex) {
      const newOrder = arrayMove(currentOrder, oldIndex, newIndex);

      if (!compactMode) {
        setForwardOrder(newOrder);
        saveOrder(FORWARD_ORDER_KEY, newOrder);
      }

      // 持久化到数据库
      try {
        const forwardsToUpdate = newOrder.map((id, index) => ({
          id,
          inx: index,
        }));

        const response = await updateForwardOrder({
          forwards: forwardsToUpdate,
        });

        if (response.code === 0) {
          // 更新本地数据中的 inx 字段
          setForwards((prev) =>
            prev.map((forward) => {
              const updatedForward = forwardsToUpdate.find(
                (f) => f.id === forward.id,
              );

              if (updatedForward) {
                return { ...forward, inx: updatedForward.inx };
              }

              return forward;
            }),
          );
        } else {
          toast.error("保存排序失败：" + (response.msg || "未知错误"));
        }
      } catch {
        toast.error("保存排序失败，请重试");
      }
    }
  };
  const toggleSelect = (id: number, explicitSelected?: boolean) => {
    setSelectedIds((prev) => {
      const next = new Set(prev);
      const shouldSelect =
        typeof explicitSelected === "boolean"
          ? explicitSelected
          : !next.has(id);

      if (shouldSelect) {
        next.add(id);
      } else {
        next.delete(id);
      }

      return next;
    });
  };
  const deselectAll = () => {
    setSelectedIds(new Set());
  };

  const presentBatchOutcome = useCallback(
    (outcome: {
      failureDetails?: BatchOperationFailure[];
      resultSummary?: string;
      resultTitle?: string;
      toastMessage: string;
      toastVariant: "success" | "error";
    }) => {
      const failureDetails = outcome.failureDetails || [];

      if (failureDetails.length > 0) {
        setBatchResultModal({
          failures: failureDetails,
          open: true,
          summary: outcome.resultSummary || outcome.toastMessage,
          title: outcome.resultTitle || "批量操作结果",
        });

        return;
      }

      if (outcome.toastVariant === "success") {
        toast.success(outcome.toastMessage);
      } else {
        toast.error(outcome.toastMessage);
      }
    },
    [],
  );

  const handleBatchDelete = async () => {
    if (selectedIds.size === 0) return;
    setBatchLoading(true);
    setBatchProgress({
      active: true,
      label: `正在删除 ${selectedIds.size} 项规则...`,
      percent: 30,
    });
    try {
      const outcome = await executeForwardBatchDelete(Array.from(selectedIds));

      presentBatchOutcome(outcome);

      if (outcome.shouldRefresh) {
        setBatchProgress({
          active: true,
          label: outcome.progressLabel || "正在刷新规则列表...",
          percent: outcome.progressPercent ?? 75,
        });
        setSelectedIds(new Set());
        setSelectMode(false);
        if (outcome.closeDeleteModal) {
          setBatchDeleteModalOpen(false);
        }
        await refreshForwardList(false);
      }
    } finally {
      setBatchProgress({ active: false, label: "", percent: 0 });
      setBatchLoading(false);
    }
  };

  const handleBatchToggleService = async (enable: boolean) => {
    if (selectedIds.size === 0) return;
    setBatchLoading(true);
    setBatchProgress({
      active: true,
      label: `正在${enable ? "启用" : "停用"} ${selectedIds.size} 项规则...`,
      percent: 30,
    });
    try {
      const outcome = await executeForwardBatchToggleService(
        Array.from(selectedIds),
        enable,
      );

      if (outcome.toastVariant === "success") {
        toast.success(outcome.toastMessage);
      } else {
        toast.error(outcome.toastMessage);
      }

      if (outcome.shouldRefresh) {
        setBatchProgress({
          active: true,
          label: outcome.progressLabel || "正在刷新规则列表...",
          percent: outcome.progressPercent ?? 75,
        });
        setSelectedIds(new Set());
        setSelectMode(false);
        await refreshForwardList(false);
      }
    } finally {
      setBatchProgress({ active: false, label: "", percent: 0 });
      setBatchLoading(false);
    }
  };

  const handleBatchRedeploy = async () => {
    if (selectedIds.size === 0) return;
    setBatchLoading(true);
    setBatchProgress({
      active: true,
      label: `正在重新下发 ${selectedIds.size} 项规则...`,
      percent: 30,
    });
    try {
      const outcome = await executeForwardBatchRedeploy(
        Array.from(selectedIds),
      );

      if (outcome.toastVariant === "success") {
        toast.success(outcome.toastMessage);
      } else {
        toast.error(outcome.toastMessage);
      }

      if (outcome.shouldRefresh) {
        setBatchProgress({
          active: true,
          label: outcome.progressLabel || "正在刷新规则列表...",
          percent: outcome.progressPercent ?? 75,
        });
        setSelectedIds(new Set());
        setSelectMode(false);
        await refreshForwardList(false);
      }
    } finally {
      setBatchProgress({ active: false, label: "", percent: 0 });
      setBatchLoading(false);
    }
  };

  const handleBatchChangeTunnel = async () => {
    if (selectedIds.size === 0 || !batchTargetTunnelId) return;
    setBatchLoading(true);
    setBatchProgress({
      active: true,
      label: `正在为 ${selectedIds.size} 项规则切换隧道...`,
      percent: 30,
    });
    try {
      const outcome = await executeForwardBatchChangeTunnel(
        Array.from(selectedIds),
        batchTargetTunnelId,
      );

      if (outcome.toastVariant === "success") {
        toast.success(outcome.toastMessage);
      } else {
        toast.error(outcome.toastMessage);
      }

      if (outcome.shouldRefresh) {
        setBatchProgress({
          active: true,
          label: outcome.progressLabel || "正在刷新规则列表...",
          percent: outcome.progressPercent ?? 75,
        });
        setSelectedIds(new Set());
        setSelectMode(false);
        if (outcome.closeChangeTunnelModal) {
          setBatchChangeTunnelModalOpen(false);
        }
        if (outcome.resetTargetTunnel) {
          setBatchTargetTunnelId(null);
        }
        await refreshForwardList(false);
      }
    } finally {
      setBatchProgress({ active: false, label: "", percent: 0 });
      setBatchLoading(false);
    }
  };

  // 传感器配置 - 使用默认配置避免错误
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

  // 根据排序顺序获取规则列表
  const orderedForwards = useMemo((): Forward[] => {
    // 确保 forwards 数组存在且有效
    if (!forwards || forwards.length === 0) {
      return [];
    }

    let filteredForwards = forwards;

    if (searchParams.userId !== "all") {
      const targetUserId = parseInt(searchParams.userId);

      filteredForwards = filteredForwards.filter(
        (f) => f.userId === targetUserId || (targetUserId === 0 && !f.userId),
      );
    }
    if (searchParams.tunnelId !== "all") {
      const targetTunnelId = parseInt(searchParams.tunnelId);

      filteredForwards = filteredForwards.filter(
        (f) => f.tunnelId === targetTunnelId,
      );
    }
    if (searchParams.name.trim()) {
      const lowerName = searchParams.name.toLowerCase();

      filteredForwards = filteredForwards.filter(
        (f) => f.name && f.name.toLowerCase().includes(lowerName),
      );
    }
    if (searchParams.inPort.trim()) {
      const targetPort = parseInt(searchParams.inPort.trim());

      if (!isNaN(targetPort)) {
        filteredForwards = filteredForwards.filter(
          (f) => f.inPort === targetPort,
        );
      }
    }
    if (searchParams.remoteAddr.trim()) {
      const lowerAddr = searchParams.remoteAddr.toLowerCase();

      filteredForwards = filteredForwards.filter(
        (f) => f.remoteAddr && f.remoteAddr.toLowerCase().includes(lowerAddr),
      );
    }

    // 确保过滤后的规则列表有效
    if (!filteredForwards || filteredForwards.length === 0) {
      return [];
    }

    // 优先使用数据库中的 inx 字段进行排序
    const sortedByDb = [...filteredForwards].sort((a, b) => {
      const aInx = a.inx ?? 0;
      const bInx = b.inx ?? 0;

      if (aInx !== bInx) {
        return aInx - bInx;
      }

      return (a.id ?? 0) - (b.id ?? 0);
    });

    // 如果数据库中没有排序信息，则使用本地存储的顺序
    if (
      forwardOrder &&
      forwardOrder.length > 0 &&
      sortedByDb.every((f) => f.inx === undefined || f.inx === 0)
    ) {
      const forwardMap = new Map(filteredForwards.map((f) => [f.id, f]));
      const localSortedForwards: Forward[] = [];

      forwardOrder.forEach((id) => {
        const forward = forwardMap.get(id);

        if (forward) {
          localSortedForwards.push(forward);
        }
      });

      // 添加不在排序列表中的规则（新添加的）
      filteredForwards.forEach((forward) => {
        if (!forwardOrder.includes(forward.id)) {
          localSortedForwards.push(forward);
        }
      });

      return localSortedForwards;
    }

    return sortedByDb;
  }, [forwards, forwardOrder, searchParams]);

  const availableGroupData = useMemo(
    () => buildAvailableGroupData(forwards),
    [forwards],
  );

  const sanitizedGroupOrderMap = useMemo(
    () =>
      sanitizeGroupOrderMap(
        groupOrderMap,
        availableGroupData.availableTunnelKeysByUser,
      ),
    [groupOrderMap, availableGroupData],
  );

  const sanitizedCollapsedTunnelGroups = useMemo(
    () =>
      sanitizeGroupCollapsedMap(
        collapsedTunnelGroups,
        availableGroupData.availableCollapseKeys,
      ),
    [collapsedTunnelGroups, availableGroupData],
  );

  useEffect(() => {
    if (!groupPreferenceHydrated || tokenUserId === null) {
      return;
    }

    if (forwards.length === 0) {
      return;
    }

    if (!isSameGroupOrderMap(groupOrderMap, sanitizedGroupOrderMap)) {
      setGroupOrderMap(sanitizedGroupOrderMap);
      persistGroupOrderToLocal(sanitizedGroupOrderMap);
      void persistGroupOrderToGlobal(sanitizedGroupOrderMap);
    }

    if (
      !isSameGroupCollapsedMap(
        collapsedTunnelGroups,
        sanitizedCollapsedTunnelGroups,
      )
    ) {
      setCollapsedTunnelGroups(sanitizedCollapsedTunnelGroups);
      persistGroupCollapsedToLocal(sanitizedCollapsedTunnelGroups);
      void persistGroupCollapsedToGlobal(sanitizedCollapsedTunnelGroups);
    }
  }, [
    groupPreferenceHydrated,
    tokenUserId,
    forwards,
    groupOrderMap,
    sanitizedGroupOrderMap,
    collapsedTunnelGroups,
    sanitizedCollapsedTunnelGroups,
  ]);

  const groupedForwards = useMemo((): ForwardUserGroup[] => {
    if (orderedForwards.length === 0) {
      return [];
    }

    type MutableForwardUserGroup = {
      userId: number;
      userName: string;
      tunnelMap: Map<string, ForwardTunnelGroup>;
    };

    const userGroupMap = new Map<number, MutableForwardUserGroup>();

    orderedForwards.forEach((forward) => {
      const userId = forward.userId ?? 0;
      const userName = normalizeForwardUserName(forward.userName);
      const tunnelName = normalizeForwardTunnelName(forward.tunnelName);
      const tunnelKey = buildForwardTunnelGroupKey(forward.tunnelName);

      let existingGroup = userGroupMap.get(userId);

      if (!existingGroup) {
        existingGroup = {
          userId,
          userName,
          tunnelMap: new Map<string, ForwardTunnelGroup>(),
        };
        userGroupMap.set(userId, existingGroup);
      } else if (
        existingGroup.userName === UNKNOWN_FORWARD_USER_NAME &&
        userName !== UNKNOWN_FORWARD_USER_NAME
      ) {
        existingGroup.userName = userName;
      }

      const existingTunnelGroup = existingGroup.tunnelMap.get(tunnelKey);

      if (!existingTunnelGroup) {
        existingGroup.tunnelMap.set(tunnelKey, {
          tunnelKey,
          tunnelName,
          tunnelTrafficRatio: normalizeTunnelTrafficRatio(
            forward.tunnelTrafficRatio,
          ),
          items: [forward],
        });

        return;
      }

      existingTunnelGroup.items.push(forward);

      if (
        existingTunnelGroup.tunnelName === UNCATEGORIZED_FORWARD_TUNNEL_NAME &&
        tunnelName !== UNCATEGORIZED_FORWARD_TUNNEL_NAME
      ) {
        existingTunnelGroup.tunnelName = tunnelName;
      }

      if (
        normalizeTunnelTrafficRatio(existingTunnelGroup.tunnelTrafficRatio) ===
          1 &&
        normalizeTunnelTrafficRatio(forward.tunnelTrafficRatio) !== 1
      ) {
        existingTunnelGroup.tunnelTrafficRatio = normalizeTunnelTrafficRatio(
          forward.tunnelTrafficRatio,
        );
      }
    });

    const groups = Array.from(userGroupMap.values()).map((group) => {
      const tunnels = Array.from(group.tunnelMap.values());
      const tunnelOrder = sanitizedGroupOrderMap[group.userId.toString()] || [];
      const tunnelOrderIndex = new Map<string, number>();

      tunnelOrder.forEach((key, index) => {
        tunnelOrderIndex.set(key, index);
      });

      tunnels.sort((a, b) => {
        const aIndex = tunnelOrderIndex.get(a.tunnelKey);
        const bIndex = tunnelOrderIndex.get(b.tunnelKey);

        if (aIndex !== undefined || bIndex !== undefined) {
          if (aIndex === undefined) {
            return 1;
          }

          if (bIndex === undefined) {
            return -1;
          }

          return aIndex - bIndex;
        }

        const nameCompare = compareForwardTunnelNameAsc(
          a.tunnelName,
          b.tunnelName,
        );

        if (nameCompare !== 0) {
          return nameCompare;
        }

        return compareForwardTunnelNameAsc(a.tunnelKey, b.tunnelKey);
      });

      return {
        userId: group.userId,
        userName: group.userName,
        tunnels,
      };
    });

    groups.sort((a, b) => {
      if (isAdmin && tokenUserId !== null) {
        const aIsSelf = a.userId === tokenUserId;
        const bIsSelf = b.userId === tokenUserId;

        if (aIsSelf !== bIsSelf) {
          return aIsSelf ? -1 : 1;
        }
      }

      const nameCompare = compareForwardUserNameAsc(a.userName, b.userName);

      if (nameCompare !== 0) {
        return nameCompare;
      }

      return a.userId - b.userId;
    });

    return groups;
  }, [orderedForwards, isAdmin, tokenUserId, sanitizedGroupOrderMap]);

  const sortedForwards = useMemo(() => {
    if (compactMode) {
      return orderedForwards;
    }

    return groupedForwards.flatMap((group) =>
      group.tunnels.flatMap((tunnel) => tunnel.items),
    );
  }, [compactMode, orderedForwards, groupedForwards]);

  const sortableForwardIds = useMemo(
    () => sortedForwards.map((f) => f.id).filter((id) => id > 0),
    [sortedForwards],
  );

  const selectAll = () => {
    const allIds = sortedForwards.map((f) => f.id);

    setSelectedIds(new Set(allIds));
  };

  const isAllSelected = useMemo(() => {
    return (
      sortedForwards &&
      sortedForwards.length > 0 &&
      selectedIds.size === sortedForwards.length
    );
  }, [sortedForwards, selectedIds]);

  const toggleTunnelGroupCollapsed = (userId: number, tunnelKey: string) => {
    const collapseKey = buildTunnelGroupCollapseKey(userId, tunnelKey);
    const nextCollapsedMap: ForwardGroupCollapsedMap = {
      ...sanitizedCollapsedTunnelGroups,
    };

    if (nextCollapsedMap[collapseKey] === true) {
      delete nextCollapsedMap[collapseKey];
    } else {
      nextCollapsedMap[collapseKey] = true;
    }

    setCollapsedTunnelGroups(nextCollapsedMap);
    persistGroupCollapsedToLocal(nextCollapsedMap);
    void persistGroupCollapsedToGlobal(nextCollapsedMap);
  };

  // 生成用作筛选项的用户和隧道列表
  const uniqueUsers = useMemo(() => {
    const userMap = new Map<number, { id: number; name: string }>();

    forwards.forEach((f) => {
      const uId = f.userId ?? 0;
      const userName = normalizeForwardUserName(f.userName);
      const existingUser = userMap.get(uId);

      if (!existingUser) {
        userMap.set(uId, { id: uId, name: userName });

        return;
      }

      if (
        existingUser.name === UNKNOWN_FORWARD_USER_NAME &&
        userName !== UNKNOWN_FORWARD_USER_NAME
      ) {
        existingUser.name = userName;
      }
    });

    const users = Array.from(userMap.values());

    users.sort((a, b) => {
      if (isAdmin && tokenUserId !== null) {
        const aIsSelf = a.id === tokenUserId;
        const bIsSelf = b.id === tokenUserId;

        if (aIsSelf !== bIsSelf) {
          return aIsSelf ? -1 : 1;
        }
      }

      const nameCompare = compareForwardUserNameAsc(a.name, b.name);

      if (nameCompare !== 0) {
        return nameCompare;
      }

      return a.id - b.id;
    });

    return users;
  }, [forwards, isAdmin, tokenUserId]);

  const exitSelectMode = () => {
    setSelectMode(false);
    setSelectedIds(new Set());
  };

  const addAction = { key: "add", label: "新建规则", onPress: handleAdd };
  const filterCount = activeFilterCount - (searchParams.name.trim() ? 1 : 0);

  const getForwardFlowText = (forward: Forward): string => {
    if ((forward.inFlow || 0) + (forward.outFlow || 0) > 0) {
      return `↑${formatFlow(forward.inFlow || 0)} ↓${formatFlow(forward.outFlow || 0)}`;
    }

    if ((forward.federationShareFlow || 0) > 0) {
      return `共享 ${formatFlow(forward.federationShareFlow || 0)}`;
    }

    return formatFlow(0);
  };

  // 渲染规则行
  const renderForwardRow = (forward: Forward, showTunnel: boolean) => {
    const ratio = normalizeTunnelTrafficRatio(forward.tunnelTrafficRatio);
    const statusDisplay = getStatusDisplay(forward.status);
    const meta = [
      `${formatInAddress(forward.inIp, forward.inPort) || "-"} → ${
        formatRemoteAddress(forward.remoteAddr) || "-"
      }`,
      showTunnel ? forward.tunnelName : "",
      formatFlow(getForwardDisplayFlow(forward)),
    ];

    return (
      <SortableForwardRow key={forward.id} id={forward.id}>
        {(handle) => (
          <ListRow
            badges={
              <>
                {forward.status !== 1 && (
                  <Chip
                    className="shrink-0"
                    color={forward.status === -1 ? "danger" : "default"}
                    size="sm"
                    variant="flat"
                  >
                    {statusDisplay.text}
                  </Chip>
                )}
                {hasMultipleAddresses(forward.remoteAddr) && (
                  <Chip className="shrink-0" size="sm" variant="flat">
                    {getStrategyDisplay(forward.strategy).text}
                  </Chip>
                )}
                {showTunnel && ratio !== 1 && (
                  <Chip className="shrink-0" size="sm" variant="flat">
                    {formatTunnelTrafficRatio(ratio)}
                  </Chip>
                )}
              </>
            }
            meta={meta.filter(Boolean).join(" · ")}
            right={handle}
            selectMode={selectMode}
            selected={selectedIds.has(forward.id)}
            status={getForwardRowStatus(forward.status)}
            statusTitle={statusDisplay.text}
            title={forward.name}
            onPress={() => setSheetForwardId(forward.id)}
            onSelect={() => toggleSelect(forward.id)}
          />
        )}
      </SortableForwardRow>
    );
  };

  if ((loading && !pageCached(PAGE)) || !groupPreferenceHydrated) {
    return <PageLoadingState message="正在加载..." />;
  }

  return (
    <ForwardPageShell embedded={embedded}>
      {!embedded && (
        <PageHeader
          description="把入口端口收到的流量经隧道转发到目标地址"
          primary={addAction}
          title="转发规则"
        />
      )}

      {selectMode ? (
        <SelectionBar
          actions={[
            {
              key: "enable",
              label: "启用",
              disabled: batchLoading,
              onPress: () => handleBatchToggleService(true),
            },
            {
              key: "disable",
              label: "停用",
              disabled: batchLoading,
              onPress: () => handleBatchToggleService(false),
            },
            {
              key: "redeploy",
              label: "重新下发",
              disabled: batchLoading,
              onPress: handleBatchRedeploy,
            },
            {
              key: "tunnel",
              label: "换隧道",
              disabled: batchLoading,
              onPress: () => setBatchChangeTunnelModalOpen(true),
            },
            {
              key: "delete",
              label: "删除",
              danger: true,
              disabled: batchLoading,
              onPress: () => setBatchDeleteModalOpen(true),
            },
          ]}
          allSelected={isAllSelected}
          count={selectedIds.size}
          onExit={exitSelectMode}
          onToggleAll={() => (isAllSelected ? deselectAll() : selectAll())}
        />
      ) : (
        <PageToolbar
          more={[
            { key: "import", label: "导入", onPress: handleImport },
            {
              key: "export",
              label: "导出",
              disabled: exportLoading,
              onPress: handleExport,
            },
            {
              key: "batch",
              label: "批量操作",
              disabled: sortedForwards.length === 0,
              onPress: () => setSelectMode(true),
            },
          ]}
          placeholder="搜索规则名称"
          primary={embedded ? addAction : undefined}
          search={searchParams.name}
          onSearch={(name) => setSearchParams((prev) => ({ ...prev, name }))}
        >
          <Button
            className="shrink-0"
            color={filterCount > 0 ? "primary" : "default"}
            size="sm"
            variant="flat"
            onPress={() => setIsSearchModalOpen(true)}
          >
            {filterCount > 0 ? `筛选 · ${filterCount}` : "筛选"}
          </Button>
        </PageToolbar>
      )}

      {batchProgress.active && (
        <div className="mb-3">
          <Alert
            color="primary"
            description={batchProgress.label}
            variant="flat"
          />
          <Progress
            aria-label={batchProgress.label}
            className="mt-3"
            color="primary"
            size="sm"
            value={batchProgress.percent}
          />
        </div>
      )}

      {forwards.length === 0 ? (
        <EmptyHint
          action={addAction}
          text="还没有转发规则。新建一条，把入口端口的流量转发到目标地址"
        />
      ) : sortedForwards.length === 0 ? (
        <EmptyHint
          action={{
            key: "reset",
            label: "清空条件",
            onPress: resetSearchParams,
          }}
          text="没有匹配的规则"
        />
      ) : compactMode ? (
        <DndContext
          collisionDetection={pointerWithin}
          sensors={sensors}
          onDragEnd={handleDragEnd}
        >
          <SortableContext
            items={sortableForwardIds}
            strategy={rectSortingStrategy}
          >
            <ListGroup>
              {sortedForwards.map((forward) => renderForwardRow(forward, true))}
            </ListGroup>
          </SortableContext>
        </DndContext>
      ) : (
        <div className="space-y-6">
          {groupedForwards.map((group) => {
            const isSelfGroup =
              isAdmin && tokenUserId !== null && group.userId === tokenUserId;
            const groupForwardCount = group.tunnels.reduce(
              (total, tunnel) => total + tunnel.items.length,
              0,
            );

            return (
              <section
                key={`group-${group.userId}-${group.userName}`}
                className="space-y-4"
              >
                {groupedForwards.length > 1 && (
                  <div className="flex items-center gap-2 px-1">
                    <span className="text-sm font-semibold">
                      {group.userName}
                    </span>
                    {isSelfGroup && (
                      <Chip size="sm" variant="flat">
                        我的
                      </Chip>
                    )}
                    <span className="text-xs text-default-400">
                      {groupForwardCount} 条规则
                    </span>
                  </div>
                )}
                <DndContext
                  collisionDetection={pointerWithin}
                  sensors={sensors}
                  onDragEnd={handleDragEnd}
                >
                  <SortableContext
                    items={group.tunnels.map((tunnel) =>
                      buildTunnelGroupSortableId(
                        group.userId,
                        tunnel.tunnelKey,
                      ),
                    )}
                    strategy={verticalListSortingStrategy}
                  >
                    <div className="space-y-4">
                      {group.tunnels.map((tunnel) => (
                        <SortableTunnelGroup
                          key={`group-${group.userId}-${tunnel.tunnelKey}`}
                          collapsed={
                            sanitizedCollapsedTunnelGroups[
                              buildTunnelGroupCollapseKey(
                                group.userId,
                                tunnel.tunnelKey,
                              )
                            ] === true
                          }
                          groupUserId={group.userId}
                          sensors={sensors}
                          tunnel={tunnel}
                          onDragEnd={handleDragEnd}
                          onToggleCollapsed={() =>
                            toggleTunnelGroupCollapsed(
                              group.userId,
                              tunnel.tunnelKey,
                            )
                          }
                        >
                          {tunnel.items.map((forward) =>
                            renderForwardRow(forward, false),
                          )}
                        </SortableTunnelGroup>
                      ))}
                    </div>
                  </SortableContext>
                </DndContext>
              </section>
            );
          })}
        </div>
      )}

      {(() => {
        const forward = forwards.find((f) => f.id === sheetForwardId) ?? null;
        const close = () => setSheetForwardId(null);
        const directFlow = forward
          ? (forward.inFlow || 0) + (forward.outFlow || 0)
          : 0;
        const rows: Array<[string, React.ReactNode]> = forward
          ? ([
              [
                "入口",
                forward.inIp
                  .split(",")
                  .map((ip) => ip.trim())
                  .filter(Boolean)
                  .map((ip) => formatInAddress(ip, forward.inPort))
                  .join("\n") || "-",
              ],
              [
                "目标",
                forward.remoteAddr
                  .split(",")
                  .map((addr) => addr.trim())
                  .filter(Boolean)
                  .join("\n") || "-",
              ],
              [
                "隧道",
                `${forward.tunnelName || UNCATEGORIZED_FORWARD_TUNNEL_NAME}${
                  normalizeTunnelTrafficRatio(forward.tunnelTrafficRatio) !== 1
                    ? `（倍率 ${formatTunnelTrafficRatio(forward.tunnelTrafficRatio)}）`
                    : ""
                }`,
              ],
              isAdmin
                ? ["用户", normalizeForwardUserName(forward.userName)]
                : null,
              hasMultipleAddresses(forward.remoteAddr)
                ? ["策略", getStrategyDisplay(forward.strategy).text]
                : null,
              ["流量", getForwardFlowText(forward)],
              (forward.maxConn || 0) > 0
                ? ["最大连接", String(forward.maxConn)]
                : null,
              (forward.ipMaxConn || 0) > 0
                ? ["每 IP 连接", String(forward.ipMaxConn)]
                : null,
              forward.ipSpeedLimitName
                ? ["每 IP 限速", forward.ipSpeedLimitName]
                : null,
              formatDate(forward.createdTime, true)
                ? ["创建时间", formatDate(forward.createdTime, true)]
                : null,
            ].filter(Boolean) as Array<[string, React.ReactNode]>)
          : [];

        return (
          <ActionSheet
            actions={
              !forward
                ? []
                : [
                    {
                      key: "edit",
                      label: "编辑",
                      color: "primary" as const,
                      onPress: () => handleEdit(forward),
                    },
                    {
                      key: "toggle",
                      label: forward.serviceRunning ? "暂停" : "启用",
                      disabled: forward.status !== 1 && forward.status !== 0,
                      onPress: () => handleServiceToggle(forward),
                    },
                    {
                      key: "diagnose",
                      label: "诊断",
                      onPress: () => handleDiagnose(forward),
                    },
                    {
                      key: "copy-in",
                      label: "复制入口",
                      disabled: !formatInAddress(forward.inIp, forward.inPort),
                      onPress: () =>
                        showAddressModal(
                          forward.inIp,
                          forward.inPort,
                          "入口端口",
                        ),
                    },
                    {
                      key: "copy-target",
                      label: "复制目标",
                      disabled: !forward.remoteAddr,
                      onPress: () =>
                        showAddressModal(forward.remoteAddr, null, "目标地址"),
                    },
                    {
                      key: "copy-name",
                      label: "复制名称",
                      onPress: () => copyToClipboard(forward.name, "规则名"),
                    },
                    {
                      key: "reset-flow",
                      label: "流量清零",
                      disabled: directFlow <= 0,
                      onPress: () => handleResetFlow(forward),
                    },
                    {
                      key: "delete",
                      label: "删除",
                      color: "danger" as const,
                      onPress: () => handleDelete(forward),
                    },
                  ]
            }
            isOpen={Boolean(forward)}
            subtitle={forward && getStatusDisplay(forward.status).text}
            title={forward?.name ?? ""}
            onClose={close}
          >
            {forward && (
              <div className="space-y-2 text-sm">
                {rows.map(([k, v]) => (
                  <div key={k} className="flex justify-between gap-3">
                    <span className="shrink-0 text-default-500">{k}</span>
                    <span className="min-w-0 whitespace-pre-line break-all text-right font-mono text-xs leading-5">
                      {v}
                    </span>
                  </div>
                ))}
              </div>
            )}
          </ActionSheet>
        );
      })()}

      {/* 新增/编辑模态框 */}
      <ForwardFormModal
        availableSpeedLimits={availableSpeedLimits}
        currentTunnelIpOptions={currentTunnelIpOptions}
        currentTunnelPortRange={currentTunnelPortRange}
        errors={errors}
        form={form}
        getAddressCount={getAddressCount}
        handleSubmit={handleSubmit}
        handleTunnelChange={handleTunnelChange}
        isAdmin={isAdmin}
        isCurrentTunnelMultiEntrance={isCurrentTunnelMultiEntrance}
        isEdit={isEdit}
        modalOpen={modalOpen}
        selectedIPSpeedId={selectedIPSpeedId}
        selectedSpeedId={selectedSpeedId}
        setForm={setForm}
        setInIpTouched={setInIpTouched}
        setModalOpen={setModalOpen}
        submitLoading={submitLoading}
        tunnels={tunnels}
      />

      {/* 删除确认模态框 */}
      <DeleteForwardModal
        confirmDelete={confirmDelete}
        deleteLoading={deleteLoading}
        deleteModalOpen={deleteModalOpen}
        forwardToDelete={forwardToDelete}
        setDeleteModalOpen={setDeleteModalOpen}
      />

      {/* 规则流量清零确认模态框 */}
      <ResetFlowModal
        confirmResetFlow={confirmResetFlow}
        forwardToResetFlow={forwardToResetFlow}
        handleResetFlowModalOpenChange={handleResetFlowModalOpenChange}
        resetFlowLoading={resetFlowLoading}
        resetFlowModalOpen={resetFlowModalOpen}
      />

      {/* 地址列表弹窗 */}
      <AddressListModal
        addressList={addressList}
        addressModalOpen={addressModalOpen}
        addressModalTitle={addressModalTitle}
        copyAddress={copyAddress}
        copyAllAddresses={copyAllAddresses}
        setAddressModalOpen={setAddressModalOpen}
      />

      {/* 导出数据模态框 */}
      <ExportModal
        copyExportData={copyExportData}
        executeExport={executeExport}
        exportData={exportData}
        exportLoading={exportLoading}
        exportModalOpen={exportModalOpen}
        selectedTunnelForExport={selectedTunnelForExport}
        setExportData={setExportData}
        setExportModalOpen={setExportModalOpen}
        setSelectedTunnelForExport={setSelectedTunnelForExport}
        tunnels={tunnels}
      />

      {/* 导入数据模态框 */}
      <ImportModal
        executeImport={executeImport}
        importData={importData}
        importFormat={importFormat}
        importLoading={importLoading}
        importModalOpen={importModalOpen}
        importResults={importResults}
        selectedTunnelForImport={selectedTunnelForImport}
        setImportData={setImportData}
        setImportFormat={setImportFormat}
        setImportModalOpen={setImportModalOpen}
        setImportResults={setImportResults}
        setSelectedTunnelForImport={setSelectedTunnelForImport}
        tunnels={tunnels}
      />

      {/* 诊断结果模态框 */}
      <DiagnosisModal
        currentDiagnosisForward={currentDiagnosisForward}
        diagnosisAbortRef={diagnosisAbortRef}
        diagnosisLoading={diagnosisLoading}
        diagnosisModalOpen={diagnosisModalOpen}
        diagnosisProgress={diagnosisProgress}
        diagnosisResult={diagnosisResult}
        handleDiagnose={handleDiagnose}
        setDiagnosisLoading={setDiagnosisLoading}
        setDiagnosisModalOpen={setDiagnosisModalOpen}
      />

      {/* 批量删除确认模态框 */}
      <BatchDeleteModal
        batchDeleteModalOpen={batchDeleteModalOpen}
        batchLoading={batchLoading}
        handleBatchDelete={handleBatchDelete}
        selectedIds={selectedIds}
        setBatchDeleteModalOpen={setBatchDeleteModalOpen}
      />

      {/* 批量换隧道模态框 */}
      <BatchChangeTunnelModal
        batchChangeTunnelModalOpen={batchChangeTunnelModalOpen}
        batchLoading={batchLoading}
        batchTargetTunnelId={batchTargetTunnelId}
        handleBatchChangeTunnel={handleBatchChangeTunnel}
        selectedIds={selectedIds}
        setBatchChangeTunnelModalOpen={setBatchChangeTunnelModalOpen}
        setBatchTargetTunnelId={setBatchTargetTunnelId}
        tunnels={tunnels}
      />

      {/* 搜索与筛选五合一模态框 */}
      <ForwardFilterModal
        isAdmin={isAdmin}
        isSearchModalOpen={isSearchModalOpen}
        resetSearchParams={resetSearchParams}
        searchParams={searchParams}
        setIsSearchModalOpen={setIsSearchModalOpen}
        setSearchParams={setSearchParams}
        tunnels={tunnels}
        uniqueUsers={uniqueUsers}
      />

      <BatchActionResultModal
        failures={batchResultModal.failures}
        isOpen={batchResultModal.open}
        summary={batchResultModal.summary}
        title={batchResultModal.title}
        onOpenChange={(open) => {
          if (open) {
            setBatchResultModal((prev) => ({ ...prev, open: true }));

            return;
          }

          setBatchResultModal(EMPTY_BATCH_RESULT_MODAL_STATE);
        }}
      />
    </ForwardPageShell>
  );
}
