import type {
  BatchOperationFailure,
  TunnelBatchDeletePreviewApiData,
  TunnelDeletePreviewApiData,
} from "@/api/types";

import { useState, useEffect, useMemo, useRef, useCallback } from "react";
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
  useSortable,
} from "@dnd-kit/sortable";
import { CSS } from "@dnd-kit/utilities";
import { GripVertical } from "lucide-react";

import { BatchActionResultModal } from "@/components/batch-action-result-modal";
import { Chip } from "@/shadcn-bridge/heroui/chip";
import { Alert } from "@/shadcn-bridge/heroui/alert";
import { Progress } from "@/shadcn-bridge/heroui/progress";
import {
  createTunnel,
  batchDeleteTunnelsWithForwards,
  getTunnelList,
  updateTunnel,
  deleteTunnelWithForwards,
  getNodeList,
  diagnoseTunnel,
  updateTunnelOrder,
  batchRedeployTunnels,
  previewBatchTunnelDelete,
  previewTunnelDelete,
} from "@/api";
import { PageLoadingState } from "@/components/page-state";
import {
  markPageCached,
  pageCached,
  useCachedState,
} from "@/hooks/use-cached-state";
import { ActionSheet } from "@/components/action-sheet";
import {
  EmptyHint,
  ListRow,
  PageHeader,
  PageToolbar,
  SelectionBar,
} from "@/components/page-kit";
import {
  buildDiagnosisFallbackResult,
  type DiagnosisResult,
} from "@/pages/tunnel/diagnosis";
import { diagnoseTunnelStream } from "@/api/diagnosis-stream";
import { getTunnelTypeDisplay, validateTunnelForm } from "@/pages/tunnel/form";
import { useLocalStorageState } from "@/hooks/use-local-storage-state";
import { loadStoredOrder, saveOrder } from "@/utils/order-storage";
import {
  buildBatchFailureMessage,
  extractBatchFailures,
  extractApiErrorMessage,
} from "@/api/error-message";
import {
  ChainTunnel,
  Tunnel,
  Node,
  TunnelForm,
  BatchProgressState,
  BatchResultModalState,
  TunnelDeleteAction,
  EMPTY_BATCH_RESULT_MODAL_STATE,
  DEFAULT_TUNNEL_DELETE_ACTION,
} from "@/pages/tunnel/types";
import {
  getTunnelDiagnosisTarget,
  getTunnelForwardMode,
  createTypedTunnelFormDefaults,
  TUNNEL_ORDER_KEY,
  renderBestExitState,
  mapTunnelApiItems,
  PageShell,
} from "@/pages/tunnel/helpers";
import { TunnelFormModal } from "@/pages/tunnel/tunnel-form-modal";
import {
  BatchDeleteTunnelsModal,
  DeleteTunnelModal,
} from "@/pages/tunnel/delete-modals";
import { TunnelDiagnosisModal } from "@/pages/tunnel/diagnosis-modal";

// A revisit shows the last data at once and reloads in the background.
const PAGE = "tunnel";

export default function TunnelPage({
  embedded = false,
}: {
  embedded?: boolean;
}) {
  const [loading, setLoading] = useState(true);
  const [tunnels, setTunnels] = useCachedState<Tunnel[]>(PAGE, "tunnels", []);
  const [tunnelOrder, setTunnelOrder] = useCachedState<number[]>(
    PAGE,
    "tunnelOrder",
    [],
  );
  const [nodes, setNodes] = useCachedState<Node[]>(PAGE, "nodes", []);
  const [searchKeyword, setSearchKeyword] = useLocalStorageState(
    "tunnel-search-keyword",
    "",
  );
  const [sheetTunnelId, setSheetTunnelId] = useState<number | null>(null);

  // 模态框状态
  const [modalOpen, setModalOpen] = useState(false);
  const [deleteModalOpen, setDeleteModalOpen] = useState(false);
  const [diagnosisModalOpen, setDiagnosisModalOpen] = useState(false);
  const [isEdit, setIsEdit] = useState(false);
  const [submitLoading, setSubmitLoading] = useState(false);
  const [deleteLoading, setDeleteLoading] = useState(false);
  const [deletePreviewLoading, setDeletePreviewLoading] = useState(false);
  const [diagnosisLoading, setDiagnosisLoading] = useState(false);
  const [tunnelToDelete, setTunnelToDelete] = useState<Tunnel | null>(null);
  const [tunnelDeletePreview, setTunnelDeletePreview] =
    useState<TunnelDeletePreviewApiData | null>(null);
  const [deleteAction, setDeleteAction] = useState<TunnelDeleteAction>(
    DEFAULT_TUNNEL_DELETE_ACTION,
  );
  const [deleteTargetTunnelId, setDeleteTargetTunnelId] = useState<
    number | null
  >(null);
  const [currentDiagnosisTunnel, setCurrentDiagnosisTunnel] =
    useState<Tunnel | null>(null);
  const [diagnosisResult, setDiagnosisResult] =
    useState<DiagnosisResult | null>(null);
  const [diagnosisProgress, setDiagnosisProgress] = useState({
    total: 0,
    completed: 0,
    success: 0,
    failed: 0,
    timedOut: false,
  });
  const diagnosisAbortRef = useRef<AbortController | null>(null);

  const getNodeIpOptions = (nodeId: number): string[] => {
    const node = nodes.find((item) => item.id === nodeId);

    if (!node) {
      return [];
    }

    const values: string[] = [];
    const push = (value?: string) => {
      const trimmed = (value || "").trim();

      if (trimmed) {
        values.push(trimmed);
      }
    };

    push(node.serverIpV4);
    push(node.serverIpV6);
    push(node.serverIp);

    (node.extraIPs || "")
      .split(",")
      .map((v) => v.trim())
      .filter((v) => v)
      .forEach((v) => values.push(v));

    return Array.from(new Set(values));
  };

  const getCommonIpOptions = (nodeIds: number[]): string[] => {
    if (nodeIds.length === 0) {
      return [];
    }

    const optionSets = nodeIds.map(
      (nodeId) => new Set(getNodeIpOptions(nodeId)),
    );
    const base = optionSets[0];

    return Array.from(base).filter((ip) =>
      optionSets.every((set) => set.has(ip)),
    );
  };

  // 表单状态
  const [form, setForm] = useState<TunnelForm>(createTypedTunnelFormDefaults());

  // 表单验证错误
  const [errors, setErrors] = useState<{ [key: string]: string }>({});

  // 批量操作相关状态
  const [selectMode, setSelectMode] = useState(false);
  const [selectedIds, setSelectedIds] = useState<Set<number>>(new Set());
  const [batchDeleteModalOpen, setBatchDeleteModalOpen] = useState(false);
  const [batchDeletePreviewLoading, setBatchDeletePreviewLoading] =
    useState(false);
  const [batchDeletePreview, setBatchDeletePreview] =
    useState<TunnelBatchDeletePreviewApiData | null>(null);
  const [batchDeleteAction, setBatchDeleteAction] =
    useState<TunnelDeleteAction>(DEFAULT_TUNNEL_DELETE_ACTION);
  const [batchDeleteTargetTunnelId, setBatchDeleteTargetTunnelId] = useState<
    number | null
  >(null);
  const [batchLoading, setBatchLoading] = useState(false);
  const [batchProgress, setBatchProgress] = useState<BatchProgressState>({
    active: false,
    label: "",
    percent: 0,
  });
  const [batchResultModal, setBatchResultModal] =
    useState<BatchResultModalState>(EMPTY_BATCH_RESULT_MODAL_STATE);

  useEffect(() => {
    return () => {
      diagnosisAbortRef.current?.abort();
      diagnosisAbortRef.current = null;
    };
  }, []);

  const applyTunnelList = useCallback((items: Tunnel[]) => {
    setTunnels(items);

    const hasDbOrdering = items.some(
      (tunnel) => tunnel.inx !== undefined && tunnel.inx !== 0,
    );

    if (hasDbOrdering) {
      const dbOrder = [...items]
        .sort((a, b) => (a.inx ?? 0) - (b.inx ?? 0))
        .map((tunnel) => tunnel.id);

      setTunnelOrder(dbOrder);

      return;
    }

    setTunnelOrder(
      loadStoredOrder(
        TUNNEL_ORDER_KEY,
        items.map((tunnel) => tunnel.id),
      ),
    );
  }, []);

  const refreshTunnelList = useCallback(
    async (withLoading = true) => {
      if (withLoading) {
        setLoading(true);
      }

      try {
        const tunnelsRes = await getTunnelList();

        if (tunnelsRes.code === 0) {
          applyTunnelList(mapTunnelApiItems(tunnelsRes.data || []));
          markPageCached(PAGE);
        } else {
          toast.error(tunnelsRes.msg || "获取隧道列表失败");
        }
      } catch {
        toast.error("获取隧道列表失败");
      } finally {
        if (withLoading) {
          setLoading(false);
        }
      }
    },
    [applyTunnelList],
  );

  const refreshNodes = useCallback(async () => {
    try {
      const nodesRes = await getNodeList();

      if (nodesRes.code === 0) {
        setNodes(nodesRes.data || []);
      }
    } catch {}
  }, []);

  // 加载所有数据
  const loadData = useCallback(async () => {
    setLoading(true);
    try {
      await Promise.all([refreshTunnelList(false), refreshNodes()]);
    } catch {
      toast.error("加载数据失败");
    } finally {
      setLoading(false);
    }
  }, [refreshNodes, refreshTunnelList]);

  useEffect(() => {
    loadData();
  }, [loadData]);

  const resetDeleteState = useCallback(() => {
    setDeleteLoading(false);
    setDeletePreviewLoading(false);
    setTunnelToDelete(null);
    setTunnelDeletePreview(null);
    setDeleteAction(DEFAULT_TUNNEL_DELETE_ACTION);
    setDeleteTargetTunnelId(null);
  }, []);

  const handleDeleteModalOpenChange = useCallback(
    (open: boolean) => {
      setDeleteModalOpen(open);
      if (!open) {
        resetDeleteState();
      }
    },
    [resetDeleteState],
  );

  const resetBatchDeleteState = useCallback(() => {
    setBatchDeletePreviewLoading(false);
    setBatchDeletePreview(null);
    setBatchDeleteAction(DEFAULT_TUNNEL_DELETE_ACTION);
    setBatchDeleteTargetTunnelId(null);
  }, []);

  const handleBatchDeleteModalOpenChange = useCallback(
    (open: boolean) => {
      setBatchDeleteModalOpen(open);
      if (!open) {
        resetBatchDeleteState();
      }
    },
    [resetBatchDeleteState],
  );

  // 表单验证
  const validateForm = (): boolean => {
    const newErrors = validateTunnelForm(
      {
        ...form,
        forwardMode: getTunnelForwardMode(form.inNodeId, nodes),
      },
      nodes,
      isEdit,
    );

    setErrors(newErrors);

    return Object.keys(newErrors).length === 0;
  };

  // 新增隧道
  const handleAdd = () => {
    setIsEdit(false);
    setForm(createTypedTunnelFormDefaults());
    setErrors({});
    setModalOpen(true);
  };

  // 编辑隧道 - 只能修改部分字段
  const handleEdit = (tunnel: Tunnel) => {
    setIsEdit(true);

    // 直接使用列表数据，getAllTunnels 已经包含完整的节点信息
    setForm({
      id: tunnel.id,
      name: tunnel.name,
      type: tunnel.type,
      forwardMode: tunnel.forwardMode === "nftables" ? "nftables" : "agent",
      inNodeId: tunnel.inNodeId || [],
      outNodeId: tunnel.outNodeId || [],
      chainNodes: tunnel.chainNodes || [],
      flow: tunnel.flow,
      trafficRatio: tunnel.trafficRatio,
      inIp: tunnel.inIp
        ? tunnel.inIp
            .split(",")
            .map((ip: string) => ip.trim())
            .join("\n")
        : "",
      ipPreference: tunnel.ipPreference || "",
      probeTargetHost: tunnel.probeTargetHost || "",
      probeTargetPort: tunnel.probeTargetPort || 0,
      engine: tunnel.engine || "",
      status: tunnel.status,
    });
    setErrors({});
    setModalOpen(true);
  };

  // 删除隧道
  const handleDelete = async (tunnel: Tunnel) => {
    setTunnelToDelete(tunnel);
    setDeleteModalOpen(true);

    setDeletePreviewLoading(true);
    setTunnelDeletePreview(null);
    setDeleteAction(DEFAULT_TUNNEL_DELETE_ACTION);
    setDeleteTargetTunnelId(null);

    try {
      const response = await previewTunnelDelete(tunnel.id);

      if (response.code !== 0 || !response.data) {
        toast.error(response.msg || "获取删除依赖失败");
        setDeleteModalOpen(false);
        resetDeleteState();

        return;
      }

      setTunnelDeletePreview(response.data);
    } catch (error) {
      toast.error(extractApiErrorMessage(error, "获取删除依赖失败"));
      setDeleteModalOpen(false);
      resetDeleteState();
    } finally {
      setDeletePreviewLoading(false);
    }
  };

  const confirmDelete = async () => {
    if (!tunnelToDelete) return;

    const forwardCount = tunnelDeletePreview?.forwardCount ?? 0;
    const action: TunnelDeleteAction =
      forwardCount > 0 ? deleteAction : "delete_forwards";

    if (
      action === "replace" &&
      forwardCount > 0 &&
      (!deleteTargetTunnelId ||
        !deleteReplacementTunnels.some(
          (tunnel) => tunnel.id === deleteTargetTunnelId,
        ))
    ) {
      toast.error("请选择替换规则的目标隧道");

      return;
    }

    setDeleteLoading(true);
    try {
      const response = await deleteTunnelWithForwards({
        id: tunnelToDelete.id,
        action,
        targetTunnelId:
          action === "replace"
            ? (deleteTargetTunnelId ?? undefined)
            : undefined,
      });

      if (response.code === 0) {
        const deleteResult = (response.data || null) as {
          warnings?: string[];
        } | null;

        if ((deleteResult?.warnings?.length ?? 0) > 0) {
          toast.success(
            `删除成功，另有 ${deleteResult?.warnings?.length ?? 0} 条服务器清理提示`,
          );
        } else {
          toast.success("删除成功");
        }
        setDeleteModalOpen(false);
        setTunnels((prev) =>
          prev.filter((tunnel) => tunnel.id !== tunnelToDelete.id),
        );
        setTunnelOrder((prev) => {
          const next = prev.filter((id) => id !== tunnelToDelete.id);

          saveOrder(TUNNEL_ORDER_KEY, next);

          return next;
        });
        setSelectedIds((prev) => {
          const next = new Set(prev);

          next.delete(tunnelToDelete.id);

          return next;
        });
        resetDeleteState();
      } else if (
        response.data &&
        typeof response.data === "object" &&
        Number((response.data as { failCount?: number }).failCount ?? 0) > 0
      ) {
        const result = response.data as {
          failCount?: number;
          successCount?: number;
        };
        const failures = extractBatchFailures(response.data);

        if (failures.length > 0) {
          setBatchResultModal({
            failures,
            open: true,
            summary: `成功 ${Number(result.successCount ?? 0)} 项，失败 ${Number(result.failCount ?? failures.length)} 项`,
            title: "规则处理失败",
          });
        }
        toast.error(response.msg || "删除失败");
      } else {
        toast.error(response.msg || "删除失败");
      }
    } catch (error) {
      toast.error(extractApiErrorMessage(error, "删除失败"));
    } finally {
      setDeleteLoading(false);
    }
  };

  // 隧道类型改变时的处理
  const handleTypeChange = (type: number) => {
    setForm((prev) => ({
      ...prev,
      type,
      outNodeId: type === 1 ? [] : prev.outNodeId,
      chainNodes: type === 1 ? [] : prev.chainNodes,
    }));
  };

  // 删除转发链中的某一跳（删除整个分组）
  const removeChainNode = (groupIndex: number) => {
    setForm((prev) => ({
      ...prev,
      chainNodes: (prev.chainNodes || []).filter(
        (_, index) => index !== groupIndex,
      ),
    }));
  };

  const toSelectedNodeIds = (keys: Iterable<unknown>): number[] => {
    return Array.from(keys)
      .map((key) => Number.parseInt(String(key), 10))
      .filter((nodeId) => Number.isFinite(nodeId));
  };

  // 更新某一跳的所有节点的协议
  const updateChainProtocol = (groupIndex: number, protocol: string) => {
    setForm((prev) => {
      const chainNodes = [...(prev.chainNodes || [])];

      chainNodes[groupIndex] = (chainNodes[groupIndex] || []).map((node) => ({
        ...node,
        protocol,
      }));

      return { ...prev, chainNodes };
    });
  };

  // 更新某一跳的所有节点的策略
  const updateChainStrategy = (groupIndex: number, strategy: string) => {
    setForm((prev) => {
      const chainNodes = [...(prev.chainNodes || [])];

      chainNodes[groupIndex] = (chainNodes[groupIndex] || []).map((node) => ({
        ...node,
        strategy,
      }));

      return { ...prev, chainNodes };
    });
  };

  // 更新某一跳的所有节点的连接IP
  const updateChainConnectIp = (groupIndex: number, connectIp: string) => {
    setForm((prev) => {
      const chainNodes = [...(prev.chainNodes || [])];

      chainNodes[groupIndex] = (chainNodes[groupIndex] || []).map((node) => ({
        ...node,
        connectIp,
      }));

      return { ...prev, chainNodes };
    });
  };

  // 获取所有转发链中已选择的节点ID列表
  const getSelectedChainNodeIds = (): number[] => {
    return (form.chainNodes || []).flatMap((group) =>
      group.map((node) => node.nodeId),
    );
  };

  // 获取转发链分组（已经是二维数组）
  const getChainGroups = (): ChainTunnel[][] => {
    return form.chainNodes || [];
  };

  const mergeOrderedNodes = (
    currentNodes: ChainTunnel[],
    selectedNodeIds: number[],
    buildDefault: (nodeId: number) => ChainTunnel,
  ): ChainTunnel[] => {
    const selectedSet = new Set(selectedNodeIds);
    const kept = currentNodes.filter((node) => selectedSet.has(node.nodeId));
    const keptIds = new Set(kept.map((node) => node.nodeId));
    const added = selectedNodeIds
      .filter((nodeId) => !keptIds.has(nodeId))
      .map((nodeId) => buildDefault(nodeId));

    return [...kept, ...added];
  };

  const syncChainGroupNodes = (
    groupIndex: number,
    selectedNodeIds: number[],
  ) => {
    setForm((prev) => {
      const chainNodes = [...(prev.chainNodes || [])];
      const currentGroup = chainNodes[groupIndex] || [];
      const protocol = currentGroup[0]?.protocol || "tls";
      const strategy = currentGroup[0]?.strategy || "round";
      const realNodes = currentGroup.filter((node) => node.nodeId !== -1);
      const mergedNodes = mergeOrderedNodes(
        realNodes,
        selectedNodeIds,
        (nodeId) => ({
          nodeId,
          chainType: 2,
          protocol,
          strategy,
        }),
      );

      chainNodes[groupIndex] =
        mergedNodes.length > 0
          ? mergedNodes
          : [{ nodeId: -1, chainType: 2, protocol, strategy }];

      return { ...prev, chainNodes };
    });
  };

  // 提交表单
  const handleSubmit = async () => {
    if (!validateForm()) return;

    setSubmitLoading(true);
    try {
      // 过滤掉占位节点（nodeId === -1 的节点）
      const cleanedChainNodes = (form.chainNodes || [])
        .map((group) => group.filter((node) => node.nodeId !== -1))
        .filter((group) => group.length > 0); // 移除空组

      // 过滤掉出口节点中的占位节点
      const cleanedOutNodeId = (form.outNodeId || []).filter(
        (node) => node.nodeId !== -1,
      );

      // 将换行符分隔的IP转换为逗号分隔
      const inIpString = form.inIp
        .split("\n")
        .map((ip) => ip.trim())
        .filter((ip) => ip)
        .join(",");
      const probeTargetHost = (form.probeTargetHost || "").trim();
      const probeTargetPort = probeTargetHost
        ? Number(form.probeTargetPort || 0)
        : 0;

      const data = {
        ...form,
        forwardMode: getTunnelForwardMode(form.inNodeId, nodes),
        inIp: inIpString,
        outNodeId: cleanedOutNodeId,
        chainNodes: cleanedChainNodes,
        probeTargetHost,
        probeTargetPort,
        // nftables entries forward by themselves.
        engine:
          form.type === 1 &&
          getTunnelForwardMode(form.inNodeId, nodes) !== "nftables"
            ? form.engine || ""
            : "",
      };

      const response = isEdit
        ? await updateTunnel(data)
        : await createTunnel(data);

      if (response.code === 0) {
        toast.success(isEdit ? "更新成功" : "创建成功");
        setModalOpen(false);
        await refreshTunnelList(false);
      } else {
        toast.error(response.msg || (isEdit ? "更新失败" : "创建失败"));
      }
    } catch {
      toast.error("网络错误，请重试");
    } finally {
      setSubmitLoading(false);
    }
  };

  // 诊断隧道
  const handleDiagnose = async (tunnel: Tunnel) => {
    diagnosisAbortRef.current?.abort();
    const abortController = new AbortController();
    const diagnosisTarget = getTunnelDiagnosisTarget(tunnel);

    diagnosisAbortRef.current = abortController;

    setCurrentDiagnosisTunnel(tunnel);
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
      tunnelName: tunnel.name,
      tunnelType: tunnel.type === 1 ? "端口转发" : "隧道转发",
      timestamp: Date.now(),
      results: [],
    });

    try {
      let streamErrorMessage = "";
      const streamResult = await diagnoseTunnelStream(
        tunnel.id,
        {
          onStart: (payload) => {
            const startTunnelName =
              typeof payload.tunnelName === "string" &&
              payload.tunnelName.trim() !== ""
                ? payload.tunnelName
                : tunnel.name;
            const startTunnelType =
              typeof payload.tunnelType === "string" &&
              payload.tunnelType.trim() !== ""
                ? payload.tunnelType
                : tunnel.type === 1
                  ? "端口转发"
                  : "隧道转发";
            const startTotal = Number(payload.total);
            const startItems = Array.isArray(payload.items)
              ? (payload.items as DiagnosisResult["results"])
              : [];

            setDiagnosisResult((prev) => ({
              tunnelName: startTunnelName,
              tunnelType: startTunnelType,
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
              const base: DiagnosisResult = prev || {
                tunnelName: tunnel.name,
                tunnelType: tunnel.type === 1 ? "端口转发" : "隧道转发",
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
        const response = await diagnoseTunnel(tunnel.id);

        if (response.code === 0) {
          const resultData = response.data as DiagnosisResult;
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
            buildDiagnosisFallbackResult({
              tunnelName: tunnel.name,
              tunnelType: tunnel.type,
              description: "诊断失败",
              message: response.msg || "诊断过程中发生错误",
              ...diagnosisTarget,
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
        buildDiagnosisFallbackResult({
          tunnelName: tunnel.name,
          tunnelType: tunnel.type,
          description: "网络错误",
          message: "无法连接到服务器",
          ...diagnosisTarget,
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

  // 处理拖拽结束
  const handleDragEnd = async (event: DragEndEvent) => {
    const { active, over } = event;

    if (!active || !over || active.id === over.id) return;
    if (!tunnelOrder || tunnelOrder.length === 0) return;

    const activeId = Number(active.id);
    const overId = Number(over.id);

    if (isNaN(activeId) || isNaN(overId)) return;

    const oldIndex = tunnelOrder.indexOf(activeId);
    const newIndex = tunnelOrder.indexOf(overId);

    if (oldIndex === -1 || newIndex === -1 || oldIndex === newIndex) return;

    const newOrder = arrayMove(tunnelOrder, oldIndex, newIndex);

    setTunnelOrder(newOrder);

    saveOrder(TUNNEL_ORDER_KEY, newOrder);

    // 持久化到数据库
    try {
      const tunnelsToUpdate = newOrder.map((id, index) => ({ id, inx: index }));
      const response = await updateTunnelOrder({ tunnels: tunnelsToUpdate });

      if (response.code === 0) {
        setTunnels((prev) =>
          prev.map((tunnel) => {
            const updated = tunnelsToUpdate.find((t) => t.id === tunnel.id);

            return updated ? { ...tunnel, inx: updated.inx } : tunnel;
          }),
        );
      } else {
        toast.error("保存排序失败：" + (response.msg || "未知错误"));
      }
    } catch {
      toast.error("保存排序失败，请重试");
    }
  };

  const toggleSelectMode = () => {
    setSelectMode(!selectMode);
    if (selectMode) {
      setSelectedIds(new Set());
    }
  };

  const toggleSelect = (id: number) => {
    const newSet = new Set(selectedIds);

    if (newSet.has(id)) {
      newSet.delete(id);
    } else {
      newSet.add(id);
    }
    setSelectedIds(newSet);
  };

  const selectAll = () => {
    const allIds = sortedTunnels.map((t) => t.id);

    setSelectedIds(new Set(allIds));
  };

  const deselectAll = () => {
    setSelectedIds(new Set());
  };

  const openBatchResultModal = useCallback(
    (title: string, summary: string, failures: BatchOperationFailure[]) => {
      setBatchResultModal({
        failures,
        open: true,
        summary,
        title,
      });
    },
    [],
  );

  const handleOpenBatchDeleteModal = async () => {
    if (selectedIds.size === 0) return;

    setBatchDeleteModalOpen(true);
    setBatchDeletePreviewLoading(true);
    setBatchDeletePreview(null);
    setBatchDeleteAction(DEFAULT_TUNNEL_DELETE_ACTION);
    setBatchDeleteTargetTunnelId(null);

    try {
      const response = await previewBatchTunnelDelete(selectedTunnelIdList);

      if (response.code !== 0 || !response.data) {
        toast.error(response.msg || "获取批量删除依赖失败");
        setBatchDeleteModalOpen(false);
        resetBatchDeleteState();

        return;
      }

      setBatchDeletePreview(response.data);
    } catch (error) {
      toast.error(extractApiErrorMessage(error, "获取批量删除依赖失败"));
      setBatchDeleteModalOpen(false);
      resetBatchDeleteState();
    } finally {
      setBatchDeletePreviewLoading(false);
    }
  };

  const handleBatchDelete = async () => {
    if (selectedIds.size === 0) return;
    if (
      batchDeleteHasForwardDependencies &&
      batchDeleteAction === "replace" &&
      (!batchDeleteTargetTunnelId || batchDeleteReplaceUnavailable)
    ) {
      toast.error("请选择替换规则的目标隧道");

      return;
    }

    setBatchLoading(true);
    setBatchProgress({
      active: true,
      label: `正在删除 ${selectedIds.size} 条隧道...`,
      percent: 30,
    });
    try {
      const res = await batchDeleteTunnelsWithForwards({
        ids: selectedTunnelIdList,
        action: batchDeleteHasForwardDependencies
          ? batchDeleteAction
          : "delete_forwards",
        targetTunnelId:
          batchDeleteHasForwardDependencies && batchDeleteAction === "replace"
            ? (batchDeleteTargetTunnelId ?? undefined)
            : undefined,
      });

      if (res.code === 0) {
        const result = (res.data || {
          successCount: 0,
          failCount: 0,
          warnings: [],
        }) as {
          successCount: number;
          failCount: number;
          warnings?: string[];
        };
        const warningCount = result?.warnings?.length ?? 0;

        if (result.failCount === 0) {
          toast.success(
            warningCount > 0
              ? `成功删除 ${result.successCount} 项，另有 ${warningCount} 条服务器清理提示`
              : `成功删除 ${result.successCount} 项`,
          );
          setBatchProgress({
            active: true,
            label: `删除完成：成功 ${result.successCount} 项`,
            percent: 100,
          });
        } else {
          const failures = extractBatchFailures(result);

          if (failures.length > 0) {
            openBatchResultModal(
              "批量删除结果",
              `成功 ${result.successCount} 项，失败 ${result.failCount} 项`,
              failures,
            );
          } else {
            toast.error(
              `成功 ${result.successCount} 项，失败 ${result.failCount} 项`,
            );
          }
          setBatchProgress({
            active: true,
            label: `部分完成：成功 ${result.successCount} 项，正在刷新列表...`,
            percent: 75,
          });
        }
        await refreshTunnelList(false);
        setSelectedIds(new Set());
        setSelectMode(false);
        setBatchDeleteModalOpen(false);
        resetBatchDeleteState();
      } else {
        toast.error(res.msg || "删除失败");
      }
    } catch (error) {
      toast.error(extractApiErrorMessage(error, "删除失败"));
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
      label: `正在重新下发 ${selectedIds.size} 条隧道...`,
      percent: 30,
    });
    try {
      const res = await batchRedeployTunnels(Array.from(selectedIds));

      if (res.code === 0) {
        const result = res.data;

        if (result.failCount === 0) {
          toast.success(`成功重新下发 ${result.successCount} 项`);
        } else {
          const failures = extractBatchFailures(result);

          if (failures.length > 0) {
            openBatchResultModal(
              "批量下发结果",
              `成功 ${result.successCount} 项，失败 ${result.failCount} 项`,
              failures,
            );
          } else {
            toast.error(
              buildBatchFailureMessage(
                result,
                `成功 ${result.successCount} 项，失败 ${result.failCount} 项`,
              ),
            );
          }
        }
        setSelectedIds(new Set());
        setSelectMode(false);
        setBatchProgress({
          active: true,
          label: `重新下发完成：成功 ${result.successCount} 项，正在刷新列表...`,
          percent: 100,
        });
        await refreshTunnelList(false);
      } else {
        toast.error(res.msg || "下发失败");
      }
    } catch (error) {
      toast.error(extractApiErrorMessage(error, "下发失败"));
    } finally {
      setBatchProgress({ active: false, label: "", percent: 0 });
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

  // 根据排序顺序获取隧道列表
  const sortedTunnels = useMemo((): Tunnel[] => {
    if (!tunnels || tunnels.length === 0) return [];

    let filteredTunnels = tunnels;

    if (searchKeyword.trim()) {
      const lowerKeyword = searchKeyword.toLowerCase();

      filteredTunnels = filteredTunnels.filter(
        (t) =>
          (t.name && t.name.toLowerCase().includes(lowerKeyword)) ||
          (t.inIp && t.inIp.toLowerCase().includes(lowerKeyword)),
      );
    }

    const sortedByDb = [...filteredTunnels].sort((a, b) => {
      const aInx = a.inx ?? 0;
      const bInx = b.inx ?? 0;

      return aInx - bInx;
    });

    // 如果数据库中没有排序信息，则使用本地存储的顺序
    if (
      tunnelOrder &&
      tunnelOrder.length > 0 &&
      sortedByDb.every((t) => t.inx === undefined || t.inx === 0)
    ) {
      const tunnelMap = new Map(filteredTunnels.map((t) => [t.id, t] as const));
      const localSorted: Tunnel[] = [];

      tunnelOrder.forEach((id) => {
        const tunnel = tunnelMap.get(id);

        if (tunnel) localSorted.push(tunnel);
      });

      filteredTunnels.forEach((tunnel) => {
        if (!tunnelOrder.includes(tunnel.id)) {
          localSorted.push(tunnel);
        }
      });

      return localSorted;
    }

    return sortedByDb;
  }, [tunnels, tunnelOrder, searchKeyword]);

  const sortableTunnelIds = useMemo(
    () => sortedTunnels.map((t) => t.id),
    [sortedTunnels],
  );

  const deleteReplacementTunnels = useMemo(() => {
    if (!tunnelToDelete) {
      return [] as Tunnel[];
    }

    return tunnels
      .filter(
        (tunnel) => tunnel.id !== tunnelToDelete.id && tunnel.status === 1,
      )
      .sort((a, b) => {
        const aInx = a.inx ?? 0;
        const bInx = b.inx ?? 0;

        return aInx - bInx;
      });
  }, [tunnelToDelete, tunnels]);

  useEffect(() => {
    if (!deleteModalOpen) {
      return;
    }

    if ((tunnelDeletePreview?.forwardCount ?? 0) <= 0) {
      return;
    }

    if (deleteReplacementTunnels.length === 0) {
      setDeleteAction("delete_forwards");
      setDeleteTargetTunnelId(null);

      return;
    }

    if (deleteAction !== "replace") {
      return;
    }

    setDeleteTargetTunnelId((prev) => {
      if (
        prev &&
        deleteReplacementTunnels.some((tunnel) => tunnel.id === prev)
      ) {
        return prev;
      }

      return deleteReplacementTunnels[0]?.id ?? null;
    });
  }, [
    deleteAction,
    deleteModalOpen,
    deleteReplacementTunnels,
    tunnelDeletePreview?.forwardCount,
  ]);

  const deletePreviewForwardCount = tunnelDeletePreview?.forwardCount ?? 0;
  const deleteHasForwardDependencies = deletePreviewForwardCount > 0;
  const deleteReplaceUnavailable =
    deleteHasForwardDependencies && deleteReplacementTunnels.length === 0;
  const deleteConfirmLabel = deleteHasForwardDependencies
    ? deleteAction === "replace"
      ? "迁移规则后删除该隧道"
      : "删除规则并删除该隧道"
    : "删除该隧道";

  const selectedTunnelIdList = useMemo(
    () => Array.from(selectedIds),
    [selectedIds],
  );
  const batchDeleteReplacementTunnels = useMemo(() => {
    if (selectedIds.size === 0) {
      return [] as Tunnel[];
    }

    return tunnels
      .filter((tunnel) => !selectedIds.has(tunnel.id) && tunnel.status === 1)
      .sort((a, b) => {
        const aInx = a.inx ?? 0;
        const bInx = b.inx ?? 0;

        return aInx - bInx;
      });
  }, [selectedIds, tunnels]);

  useEffect(() => {
    if (!batchDeleteModalOpen) {
      return;
    }

    if ((batchDeletePreview?.totalForwardCount ?? 0) <= 0) {
      return;
    }

    if (batchDeleteReplacementTunnels.length === 0) {
      setBatchDeleteAction("delete_forwards");
      setBatchDeleteTargetTunnelId(null);

      return;
    }

    if (batchDeleteAction !== "replace") {
      return;
    }

    setBatchDeleteTargetTunnelId((prev) => {
      if (
        prev &&
        batchDeleteReplacementTunnels.some((tunnel) => tunnel.id === prev)
      ) {
        return prev;
      }

      return batchDeleteReplacementTunnels[0]?.id ?? null;
    });
  }, [
    batchDeleteAction,
    batchDeleteModalOpen,
    batchDeletePreview?.totalForwardCount,
    batchDeleteReplacementTunnels,
  ]);

  const batchDeleteTotalForwardCount =
    batchDeletePreview?.totalForwardCount ?? 0;
  const batchDeleteHasForwardDependencies = batchDeleteTotalForwardCount > 0;
  const batchDeleteDependentTunnelCount =
    batchDeletePreview?.items?.filter((item) => item.forwardCount > 0).length ??
    0;
  const batchDeleteDirectDeleteTunnelCount = Math.max(
    selectedTunnelIdList.length - batchDeleteDependentTunnelCount,
    0,
  );
  const batchDeletePreviewItems = useMemo(() => {
    return [...(batchDeletePreview?.items ?? [])].sort((a, b) => {
      if (a.forwardCount > 0 === b.forwardCount > 0) {
        return a.tunnelName.localeCompare(b.tunnelName, "zh-CN");
      }

      return a.forwardCount > 0 ? -1 : 1;
    });
  }, [batchDeletePreview?.items]);
  const batchDeleteDependentItems = useMemo(
    () => batchDeletePreviewItems.filter((item) => item.forwardCount > 0),
    [batchDeletePreviewItems],
  );
  const batchDeleteReplaceUnavailable =
    batchDeleteHasForwardDependencies &&
    batchDeleteReplacementTunnels.length === 0;
  const batchDeleteConfirmLabel = batchDeleteHasForwardDependencies
    ? batchDeleteAction === "replace"
      ? `迁移规则后删除这 ${selectedTunnelIdList.length} 条隧道`
      : `删除规则并删除 ${selectedTunnelIdList.length} 条隧道`
    : `删除这 ${selectedTunnelIdList.length} 条隧道`;

  const SortableItem = ({
    id,
    children,
  }: {
    id: number;
    children: (listeners: any) => any;
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
      <div
        ref={setNodeRef}
        className="list-row-wrap"
        style={style}
        {...attributes}
      >
        {children(listeners)}
      </div>
    );
  };

  const nodeNameMap = new Map(nodes.map((n) => [n.id, n.name] as const));
  const describeNodes = (items?: ChainTunnel[]) => {
    const names = (items ?? []).map(
      (item) => nodeNameMap.get(item.nodeId) || `#${item.nodeId}`,
    );

    if (names.length === 0) return "-";

    return names.length > 1 ? `${names[0]} 等 ${names.length} 个` : names[0];
  };
  const listNodes = (items?: ChainTunnel[]) =>
    (items ?? [])
      .map((item) => nodeNameMap.get(item.nodeId) || `#${item.nodeId}`)
      .join("、") || "-";
  const describeRoute = (tunnel: Tunnel) => {
    if (tunnel.type !== 2) return describeNodes(tunnel.inNodeId);
    const hops = tunnel.chainNodes?.length || 0;

    return [
      describeNodes(tunnel.inNodeId),
      ...(hops > 0 ? [`${hops} 跳`] : []),
      describeNodes(tunnel.outNodeId),
    ].join(" → ");
  };
  const isSearching = searchKeyword.trim() !== "";
  const addAction = { key: "add", label: "新建隧道", onPress: handleAdd };
  const sheetTunnel = tunnels.find((t) => t.id === sheetTunnelId) ?? null;

  if (loading && !pageCached(PAGE)) {
    return <PageLoadingState message="正在加载..." />;
  }

  return (
    <PageShell embedded={embedded}>
      {!embedded && (
        <PageHeader
          description="入口到出口的转发线路，转发规则建在隧道上"
          primary={addAction}
          title="隧道"
        />
      )}

      {selectMode ? (
        <SelectionBar
          actions={[
            {
              key: "redeploy",
              label: "下发",
              disabled: batchLoading,
              onPress: handleBatchRedeploy,
            },
            {
              key: "delete",
              label: "删除",
              danger: true,
              onPress: handleOpenBatchDeleteModal,
            },
          ]}
          allSelected={
            sortedTunnels.length > 0 &&
            sortedTunnels.every((t) => selectedIds.has(t.id))
          }
          count={selectedIds.size}
          onExit={toggleSelectMode}
          onToggleAll={() =>
            sortedTunnels.every((t) => selectedIds.has(t.id))
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
              disabled: sortedTunnels.length === 0,
              onPress: toggleSelectMode,
            },
          ]}
          placeholder="搜索隧道名称或 IP"
          primary={embedded ? addAction : undefined}
          search={searchKeyword}
          onSearch={setSearchKeyword}
        />
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

      {tunnels.length === 0 ? (
        <EmptyHint
          action={addAction}
          text="还没有隧道。新建一条，选好入口和出口服务器，就能在上面添加转发规则"
        />
      ) : sortedTunnels.length === 0 ? (
        <EmptyHint text="没有匹配的隧道" />
      ) : (
        <DndContext sensors={sensors} onDragEnd={handleDragEnd}>
          <SortableContext
            items={sortableTunnelIds}
            strategy={rectSortingStrategy}
          >
            <div className="flex flex-col">
              {sortedTunnels.map((tunnel) => {
                const bestExit = tunnel.bestExitState?.enabled
                  ? tunnel.bestExitState
                  : undefined;

                return (
                  <SortableItem key={tunnel.id} id={tunnel.id}>
                    {(listeners) => (
                      <ListRow
                        badges={
                          <>
                            <Chip
                              className="shrink-0 whitespace-nowrap"
                              color="secondary"
                              size="sm"
                              variant="flat"
                            >
                              {getTunnelTypeDisplay(tunnel.type).text}
                            </Chip>
                            {tunnel.engine === "realm" && (
                              <Chip
                                className="shrink-0"
                                color="primary"
                                size="sm"
                                variant="flat"
                              >
                                realm
                              </Chip>
                            )}
                            {bestExit && (
                              <Chip
                                className="shrink-0"
                                color={
                                  bestExit.status === "waiting"
                                    ? "warning"
                                    : "success"
                                }
                                size="sm"
                                variant="flat"
                              >
                                最优出口
                              </Chip>
                            )}
                          </>
                        }
                        meta={describeRoute(tunnel)}
                        right={
                          !isSearching && (
                            <span
                              className="cursor-grab touch-none p-1 text-default-300 transition-opacity hover:text-default-500 active:cursor-grabbing sm:opacity-0 sm:group-hover:opacity-100"
                              title="拖动排序"
                              {...listeners}
                            >
                              <GripVertical className="h-4 w-4" />
                            </span>
                          )
                        }
                        selectMode={selectMode}
                        selected={selectedIds.has(tunnel.id)}
                        status={tunnel.status === 1 ? "ok" : "bad"}
                        statusTitle={tunnel.status === 1 ? "正常" : "不可用"}
                        title={tunnel.name}
                        onPress={() => setSheetTunnelId(tunnel.id)}
                        onSelect={() => toggleSelect(tunnel.id)}
                      />
                    )}
                  </SortableItem>
                );
              })}
            </div>
          </SortableContext>
        </DndContext>
      )}

      <ActionSheet
        actions={
          sheetTunnel
            ? [
                {
                  key: "edit",
                  label: "编辑",
                  color: "primary" as const,
                  onPress: () => handleEdit(sheetTunnel),
                },
                {
                  key: "diagnose",
                  label: "诊断",
                  onPress: () => handleDiagnose(sheetTunnel),
                },
                {
                  key: "delete",
                  label: "删除",
                  color: "danger" as const,
                  onPress: () => handleDelete(sheetTunnel),
                },
              ]
            : []
        }
        isOpen={Boolean(sheetTunnel)}
        subtitle={
          sheetTunnel &&
          `${getTunnelTypeDisplay(sheetTunnel.type).text}${
            sheetTunnel.engine === "realm" ? " · realm" : ""
          } · ${sheetTunnel.status === 1 ? "正常" : "不可用"}`
        }
        title={sheetTunnel?.name ?? ""}
        onClose={() => setSheetTunnelId(null)}
      >
        {sheetTunnel && (
          <div className="space-y-2 text-sm">
            {(
              [
                ["入口", listNodes(sheetTunnel.inNodeId)],
                ...(sheetTunnel.type === 2
                  ? ([
                      ...(sheetTunnel.chainNodes ?? []).map(
                        (hop, i) =>
                          [`第 ${i + 1} 跳`, listNodes(hop)] as [
                            string,
                            string,
                          ],
                      ),
                      ["出口", listNodes(sheetTunnel.outNodeId)],
                    ] as Array<[string, string]>)
                  : []),
                ...(sheetTunnel.type === 2 && sheetTunnel.ipPreference
                  ? ([
                      [
                        "连接偏好",
                        sheetTunnel.ipPreference === "v4" ? "IPv4" : "IPv6",
                      ],
                    ] as Array<[string, string]>)
                  : []),
              ] as Array<[string, string]>
            ).map(([k, v]) => (
              <div key={k} className="flex justify-between gap-3">
                <span className="shrink-0 text-default-500">{k}</span>
                <span className="min-w-0 break-all text-right text-xs leading-5">
                  {v}
                </span>
              </div>
            ))}
            {renderBestExitState(sheetTunnel.bestExitState)}
          </div>
        )}
      </ActionSheet>
      {/* 新增/编辑模态框 */}
      <TunnelFormModal
        errors={errors}
        form={form}
        getChainGroups={getChainGroups}
        getCommonIpOptions={getCommonIpOptions}
        getSelectedChainNodeIds={getSelectedChainNodeIds}
        handleSubmit={handleSubmit}
        handleTypeChange={handleTypeChange}
        isEdit={isEdit}
        mergeOrderedNodes={mergeOrderedNodes}
        modalOpen={modalOpen}
        nodes={nodes}
        removeChainNode={removeChainNode}
        setForm={setForm}
        setModalOpen={setModalOpen}
        submitLoading={submitLoading}
        syncChainGroupNodes={syncChainGroupNodes}
        toSelectedNodeIds={toSelectedNodeIds}
        updateChainConnectIp={updateChainConnectIp}
        updateChainProtocol={updateChainProtocol}
        updateChainStrategy={updateChainStrategy}
      />

      {/* 删除确认模态框 */}
      <DeleteTunnelModal
        confirmDelete={confirmDelete}
        deleteAction={deleteAction}
        deleteConfirmLabel={deleteConfirmLabel}
        deleteHasForwardDependencies={deleteHasForwardDependencies}
        deleteLoading={deleteLoading}
        deleteModalOpen={deleteModalOpen}
        deletePreviewForwardCount={deletePreviewForwardCount}
        deletePreviewLoading={deletePreviewLoading}
        deleteReplaceUnavailable={deleteReplaceUnavailable}
        deleteReplacementTunnels={deleteReplacementTunnels}
        deleteTargetTunnelId={deleteTargetTunnelId}
        handleDeleteModalOpenChange={handleDeleteModalOpenChange}
        setDeleteAction={setDeleteAction}
        setDeleteTargetTunnelId={setDeleteTargetTunnelId}
        tunnelDeletePreview={tunnelDeletePreview}
        tunnelToDelete={tunnelToDelete}
      />

      {/* 诊断结果模态框 */}
      <TunnelDiagnosisModal
        currentDiagnosisTunnel={currentDiagnosisTunnel}
        diagnosisAbortRef={diagnosisAbortRef}
        diagnosisLoading={diagnosisLoading}
        diagnosisModalOpen={diagnosisModalOpen}
        diagnosisProgress={diagnosisProgress}
        diagnosisResult={diagnosisResult}
        handleDiagnose={handleDiagnose}
        setDiagnosisLoading={setDiagnosisLoading}
        setDiagnosisModalOpen={setDiagnosisModalOpen}
      />

      <BatchDeleteTunnelsModal
        batchDeleteAction={batchDeleteAction}
        batchDeleteConfirmLabel={batchDeleteConfirmLabel}
        batchDeleteDependentItems={batchDeleteDependentItems}
        batchDeleteDependentTunnelCount={batchDeleteDependentTunnelCount}
        batchDeleteDirectDeleteTunnelCount={batchDeleteDirectDeleteTunnelCount}
        batchDeleteHasForwardDependencies={batchDeleteHasForwardDependencies}
        batchDeleteModalOpen={batchDeleteModalOpen}
        batchDeletePreviewLoading={batchDeletePreviewLoading}
        batchDeleteReplaceUnavailable={batchDeleteReplaceUnavailable}
        batchDeleteReplacementTunnels={batchDeleteReplacementTunnels}
        batchDeleteTargetTunnelId={batchDeleteTargetTunnelId}
        batchDeleteTotalForwardCount={batchDeleteTotalForwardCount}
        batchLoading={batchLoading}
        handleBatchDelete={handleBatchDelete}
        handleBatchDeleteModalOpenChange={handleBatchDeleteModalOpenChange}
        selectedTunnelIdList={selectedTunnelIdList}
        setBatchDeleteAction={setBatchDeleteAction}
        setBatchDeleteTargetTunnelId={setBatchDeleteTargetTunnelId}
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
    </PageShell>
  );
}
