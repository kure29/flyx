import type {
  KernelApiItem,
  KernelCapabilitiesApiData,
  KernelStateApiData,
  NodeApiItem,
} from "@/api/types";

import { useEffect, useMemo, useRef, useState } from "react";
import toast from "react-hot-toast";
import {
  AlertCircle,
  AlertTriangle,
  ArrowLeftRight,
  Check,
  Copy,
  Cpu,
  Download,
  FileText,
  Layers,
  MoreHorizontal,
  Plus,
  RefreshCw,
  RotateCw,
  Terminal,
  Zap,
} from "lucide-react";

import { groupItems } from "./group";
import { eligibleNodes } from "./inbound-form-modal";
import { KERNEL_STATUS } from "./presets";

import { ActionSheet } from "@/components/action-sheet";
import { EmptyHint, PageToolbar } from "@/components/page-kit";
import { useI18n } from "@/i18n";
import { nodeRegionFlag } from "@/pages/node/region";
import { copyWithToast } from "@/utils/clipboard";
import {
  controlKernel,
  getKernelLogs,
  getKernelVersions,
  installKernel,
} from "@/api";
import { Button } from "@/shadcn-bridge/heroui/button";
import { Chip } from "@/shadcn-bridge/heroui/chip";
import { Input } from "@/shadcn-bridge/heroui/input";
import {
  Modal,
  ModalBody,
  ModalContent,
  ModalFooter,
  ModalHeader,
} from "@/shadcn-bridge/heroui/modal";
import { Select, SelectItem } from "@/shadcn-bridge/heroui/select";

const modalBase = "!w-[calc(100%-32px)] !mx-auto sm:!w-full overflow-hidden";

export interface InstallRequest {
  nodeId: number;
  kernelType: string;
}

interface Props {
  caps: KernelCapabilitiesApiData | null;
  kernels: KernelApiItem[];
  nodes: NodeApiItem[];
  installRequest: InstallRequest | null;
  onInstallRequestHandled: () => void;
  onChanged: () => void;
}

const STATUS_BADGE: Record<string, { dot: string; text: string; bg: string }> =
  {
    success: {
      dot: "bg-emerald-500",
      text: "text-emerald-700 dark:text-emerald-400",
      bg: "bg-emerald-500/10",
    },
    warning: {
      dot: "bg-amber-500",
      text: "text-amber-700 dark:text-amber-400",
      bg: "bg-amber-500/10",
    },
    danger: {
      dot: "bg-rose-500",
      text: "text-rose-700 dark:text-rose-400",
      bg: "bg-rose-500/10",
    },
    default: {
      dot: "bg-default-400",
      text: "text-default-500",
      bg: "bg-default-100 dark:bg-default-100/60",
    },
  };

function shortVersion(v: string): string {
  const m = v.match(/\d+\.\d+\.\d+/);

  return m ? m[0] : v;
}

function kernelIcon(type: string, isRelay?: boolean) {
  if (isRelay) return ArrowLeftRight;
  if (type === "singbox") return Layers;
  if (type === "snell") return Zap;

  return Cpu;
}

export function KernelPanel({
  caps,
  kernels,
  nodes,
  installRequest,
  onInstallRequestHandled,
  onChanged,
}: Props) {
  const { t } = useI18n();
  const [search, setSearch] = useState("");
  const [filter, setFilter] = useState<string>("all");
  const [sheetFor, setSheetFor] = useState<number | null>(null);
  const [releases, setReleases] = useState<{
    type: string;
    items: { version: string; prerelease: boolean }[];
    error: string;
  } | null>(null);
  const [customVersion, setCustomVersion] = useState(false);
  const [installOpen, setInstallOpen] = useState(false);
  const [installForm, setInstallForm] = useState({
    nodeId: "",
    kernelType: "xray",
    version: "latest",
  });
  const [installing, setInstalling] = useState(false);
  const [busy, setBusy] = useState<string | null>(null);
  const [logsFor, setLogsFor] = useState<KernelApiItem | null>(null);
  const [logs, setLogs] = useState<KernelStateApiData | null>(null);
  const [logsLoading, setLogsLoading] = useState(false);
  const [logsCopied, setLogsCopied] = useState(false);
  const logsCopiedTimerRef = useRef<number | null>(null);
  const [removeTarget, setRemoveTarget] = useState<KernelApiItem | null>(null);
  const [removeFailed, setRemoveFailed] = useState(false);

  useEffect(() => {
    return () => {
      if (logsCopiedTimerRef.current !== null) {
        window.clearTimeout(logsCopiedTimerRef.current);
      }
    };
  }, []);

  useEffect(() => {
    if (!installRequest) return;
    setInstallForm({
      nodeId: String(installRequest.nodeId),
      kernelType: installRequest.kernelType,
      version: "latest",
    });
    setInstallOpen(true);
    onInstallRequestHandled();
  }, [installRequest, onInstallRequestHandled]);

  const capsByType = useMemo(
    () => new Map((caps?.kernels ?? []).map((k) => [k.type, k])),
    [caps],
  );

  const nodeById = useMemo(() => new Map(nodes.map((n) => [n.id, n])), [nodes]);

  const kernelBaseName = (type: string) => capsByType.get(type)?.name ?? type;

  const kernelName = (type: string) => {
    const info = capsByType.get(type);

    return info?.relay
      ? `${info.name}（${t("转发引擎")}）`
      : (info?.name ?? type);
  };

  const openInstall = (nodeId?: number, kernelType = "xray") => {
    setInstallForm({
      nodeId: nodeId ? String(nodeId) : "",
      kernelType,
      version: capsByType.get(kernelType)?.versions?.[0] ?? "latest",
    });
    setInstallOpen(true);
  };

  const doInstall = async () => {
    if (!installForm.nodeId) {
      toast.error(t("请选择服务器"));

      return;
    }
    setInstalling(true);
    try {
      const res = await installKernel({
        nodeId: Number(installForm.nodeId),
        kernelType: installForm.kernelType,
        version: installForm.version.trim() || "latest",
      });

      if (res.code === 0) {
        toast.success(t("内核已安装并启动"));
        setInstallOpen(false);
      } else {
        toast.error(res.msg || t("安装失败"), { duration: 8000 });
      }
      onChanged();
    } finally {
      setInstalling(false);
    }
  };

  const doControl = async (
    k: KernelApiItem,
    action: "start" | "stop" | "restart",
  ) => {
    setBusy(`${k.id}-${action}`);
    try {
      const res = await controlKernel(k.id, action);

      if (res.code === 0) {
        toast.success(t("操作成功"));
      } else {
        toast.error(res.msg || t("操作失败"));
      }
      onChanged();
    } finally {
      setBusy(null);
    }
  };

  const openLogs = async (k: KernelApiItem) => {
    setLogsFor(k);
    setLogs(null);
    setLogsCopied(false);
    setLogsLoading(true);
    try {
      const res = await getKernelLogs(k.id);

      if (res.code === 0) {
        setLogs(res.data);
      } else {
        toast.error(res.msg || t("获取日志失败"));
      }
    } finally {
      setLogsLoading(false);
    }
  };

  const copyLogs = async () => {
    const content = (logs?.logs ?? []).join("\n");

    if (!content) return;
    if (await copyWithToast(content, t("日志已复制到剪贴板"))) {
      setLogsCopied(true);
      if (logsCopiedTimerRef.current !== null) {
        window.clearTimeout(logsCopiedTimerRef.current);
      }
      logsCopiedTimerRef.current = window.setTimeout(
        () => setLogsCopied(false),
        2000,
      );
    }
  };

  const doRemove = async (force: boolean) => {
    if (!removeTarget) return;
    setBusy(`${removeTarget.id}-remove`);
    try {
      const res = await controlKernel(removeTarget.id, "remove", force);

      if (res.code === 0) {
        toast.success(t("已卸载"));
        setRemoveTarget(null);
        onChanged();
      } else {
        setRemoveFailed(true);
        toast.error(res.msg || t("卸载失败"));
      }
    } finally {
      setBusy(null);
    }
  };

  const installNodeOptions = eligibleNodes(nodes);
  const versionListType =
    installOpen && !capsByType.get(installForm.kernelType)?.versions?.length
      ? installForm.kernelType
      : "";

  useEffect(() => {
    if (!versionListType || releases?.type === versionListType) return;
    setCustomVersion(false);
    getKernelVersions(versionListType)
      .then((res) =>
        setReleases({
          type: versionListType,
          items: res.code === 0 ? res.data || [] : [],
          error: res.code === 0 ? "" : res.msg || t("获取版本列表失败"),
        }),
      )
      .catch(() =>
        setReleases({
          type: versionListType,
          items: [],
          error: t("获取版本列表失败"),
        }),
      );
  }, [versionListType, releases?.type, t]);

  const releaseItems =
    releases?.type === installForm.kernelType ? releases.items : [];
  const pinnedVersions = capsByType.get(installForm.kernelType)?.versions ?? [];

  // Summary metrics across all installed kernels
  const runningCount = kernels.filter(
    (k) => k.status === "running" && k.nodeOnline,
  ).length;
  const issueCount = kernels.filter(
    (k) =>
      k.status === "error" ||
      k.status === "stopped" ||
      !k.nodeOnline ||
      Boolean(k.lastError),
  ).length;
  const coveredNodeIds = new Set(kernels.map((k) => k.nodeId));
  const totalInbounds = kernels.reduce((acc, k) => acc + k.inboundCount, 0);

  const typeCounts = useMemo(() => {
    const counts = new Map<string, number>();

    for (const k of kernels) {
      counts.set(k.kernelType, (counts.get(k.kernelType) ?? 0) + 1);
    }

    return Array.from(counts.entries());
  }, [kernels]);

  // Filtering
  const normalizedSearch = search.trim().toLowerCase();
  const filteredKernels = useMemo(() => {
    return kernels.filter((k) => {
      if (filter === "issue") {
        if (
          !(
            k.status === "error" ||
            k.status === "stopped" ||
            !k.nodeOnline ||
            Boolean(k.lastError)
          )
        ) {
          return false;
        }
      } else if (filter !== "all" && k.kernelType !== filter) {
        return false;
      }

      if (!normalizedSearch) return true;
      const haystack = [
        k.nodeName,
        k.kernelType,
        capsByType.get(k.kernelType)?.name ?? "",
        k.installedVersion,
        k.version,
      ]
        .join(" ")
        .toLowerCase();

      return haystack.includes(normalizedSearch);
    });
  }, [kernels, filter, normalizedSearch, capsByType]);

  const nodeGroups = groupItems(
    filteredKernels,
    (k) => String(k.nodeId),
    (_key, k) => k.nodeName || t("服务器 {id}", { id: k.nodeId }),
  );
  const sheetKernel = kernels.find((k) => k.id === sheetFor) ?? null;

  const renderKernelRow = (k: KernelApiItem) => {
    const info = capsByType.get(k.kernelType);
    const isRelay = Boolean(info?.relay);
    const st = KERNEL_STATUS[k.status] ?? {
      label: k.status,
      color: "default" as const,
    };
    const isPendingSync = !k.synced && !k.lastError && k.status !== "offline";
    const statusColor = isPendingSync ? "warning" : st.color;
    const statusLabel = isPendingSync ? t("待同步") : t(st.label);
    const badge = STATUS_BADGE[statusColor] ?? STATUS_BADGE.default;
    const Icon = kernelIcon(k.kernelType, isRelay);
    const version = k.installedVersion
      ? shortVersion(k.installedVersion)
      : k.version;
    const isRestarting = busy === `${k.id}-restart`;

    return (
      <div
        key={k.id}
        className="group px-4 py-3 transition-colors hover:bg-default-50/90 dark:hover:bg-default-100/35"
      >
        <div className="flex items-center gap-3">
          <button
            className="flex min-w-0 flex-1 items-center gap-3 text-left focus:outline-none"
            type="button"
            onClick={() => setSheetFor(k.id)}
          >
            {/* Kernel icon tile with live status indicator */}
            <div className="relative flex h-9 w-9 shrink-0 items-center justify-center rounded-lg border border-divider/80 bg-default-50 text-default-600 dark:bg-default-100/50 dark:text-default-400">
              <Icon className="h-4 w-4" strokeWidth={1.75} />
              <span
                className={`absolute -bottom-0.5 -right-0.5 h-2.5 w-2.5 rounded-full ring-2 ring-background ${badge.dot}`}
              />
            </div>

            {/* Primary kernel info */}
            <div className="min-w-0 flex-1">
              <div className="flex flex-wrap items-center gap-1.5">
                <span className="truncate text-sm font-semibold text-foreground">
                  {kernelBaseName(k.kernelType)}
                </span>
                {isRelay && (
                  <span className="rounded-md bg-default-100 px-1.5 py-0.5 text-[10px] font-medium text-default-500">
                    {t("转发引擎")}
                  </span>
                )}
                {version && (
                  <span className="rounded-md bg-default-100/80 px-1.5 py-0.5 font-mono text-[11px] text-default-600 dark:bg-default-100/60 dark:text-default-400">
                    {version.startsWith("v") ? version : `v${version}`}
                  </span>
                )}
              </div>
              <div className="mt-1 flex flex-wrap items-center gap-x-2 gap-y-1 text-xs">
                <span
                  className={`inline-flex items-center gap-1 rounded-full px-2 py-0.5 text-[11px] font-medium ${badge.bg} ${badge.text}`}
                >
                  <span className={`h-1.5 w-1.5 rounded-full ${badge.dot}`} />
                  {statusLabel}
                </span>
                <span className="text-default-400 tabular-nums">
                  {isRelay && k.inboundCount === 0
                    ? t("系统转发")
                    : t("{count} 个入站", { count: k.inboundCount })}
                </span>
              </div>
            </div>
          </button>

          {/* 1-click quick actions */}
          <div className="flex shrink-0 items-center gap-0.5">
            <button
              aria-label={t("日志")}
              className="flex h-7 w-7 items-center justify-center rounded-md text-default-400 transition-colors hover:bg-default-200/70 hover:text-foreground dark:hover:bg-default-100"
              title={t("查看日志")}
              type="button"
              onClick={() => void openLogs(k)}
            >
              <FileText className="h-3.5 w-3.5" strokeWidth={1.75} />
            </button>
            <button
              aria-label={t("重启")}
              className="flex h-7 w-7 items-center justify-center rounded-md text-default-400 transition-colors hover:bg-default-200/70 hover:text-foreground disabled:pointer-events-none disabled:opacity-35 dark:hover:bg-default-100"
              disabled={!k.enabled || !k.nodeOnline || isRestarting}
              title={t("重启")}
              type="button"
              onClick={() => void doControl(k, "restart")}
            >
              <RotateCw
                className={`h-3.5 w-3.5 ${isRestarting ? "animate-spin" : ""}`}
                strokeWidth={1.75}
              />
            </button>
            <button
              aria-label={t("更多操作")}
              className="flex h-7 w-7 items-center justify-center rounded-md text-default-400 transition-colors hover:bg-default-200/70 hover:text-foreground dark:hover:bg-default-100"
              title={t("更多操作")}
              type="button"
              onClick={() => setSheetFor(k.id)}
            >
              <MoreHorizontal className="h-4 w-4" strokeWidth={1.75} />
            </button>
          </div>
        </div>

        {k.lastError && (
          <div className="mt-2.5 flex items-start gap-2 rounded-lg border border-danger/25 bg-danger/5 px-2.5 py-1.5 text-xs text-danger">
            <AlertCircle className="mt-0.5 h-3.5 w-3.5 shrink-0" />
            <p className="line-clamp-2 break-all leading-relaxed">
              {k.lastError}
            </p>
          </div>
        )}
      </div>
    );
  };

  return (
    <div className="space-y-4">
      {kernels.length > 0 && (
        <div className="grid grid-cols-2 gap-px overflow-hidden rounded-xl border border-divider bg-divider lg:grid-cols-4">
          <div className="bg-background px-4 py-3">
            <div className="text-xs text-default-400">{t("运行中内核")}</div>
            <div className="mt-1 flex items-baseline gap-1 tabular-nums">
              <span className="text-xl font-semibold tracking-tight text-foreground">
                {runningCount}
              </span>
              <span className="text-xs text-default-400">
                / {kernels.length}
              </span>
            </div>
            <div
              className={`mt-1 text-[11px] ${
                issueCount > 0
                  ? "text-amber-600 dark:text-amber-400"
                  : "text-emerald-600 dark:text-emerald-400"
              }`}
            >
              {issueCount > 0
                ? t("{count} 个需关注", { count: issueCount })
                : t("全部正常运行")}
            </div>
          </div>

          <div className="bg-background px-4 py-3">
            <div className="text-xs text-default-400">{t("已部署服务器")}</div>
            <div className="mt-1 flex items-baseline gap-1 tabular-nums">
              <span className="text-xl font-semibold tracking-tight text-foreground">
                {coveredNodeIds.size}
              </span>
              <span className="text-xs text-default-400">/ {nodes.length}</span>
            </div>
            <div className="mt-1 text-[11px] text-default-400">
              {t("{count} 台服务器已装内核", { count: coveredNodeIds.size })}
            </div>
          </div>

          <div className="bg-background px-4 py-3">
            <div className="text-xs text-default-400">{t("承载入站")}</div>
            <div className="mt-1 text-xl font-semibold tracking-tight tabular-nums text-foreground">
              {totalInbounds}
            </div>
            <div className="mt-1 text-[11px] text-default-400">
              {totalInbounds > 0
                ? t("分布于 {count} 个内核", { count: kernels.length })
                : t("可在“节点”页创建入站")}
            </div>
          </div>

          <div className="bg-background px-4 py-3">
            <div className="text-xs text-default-400">{t("内核分布")}</div>
            <div className="mt-1.5 flex flex-wrap items-center gap-1.5">
              {typeCounts.map(([type, count]) => (
                <span
                  key={type}
                  className="inline-flex items-center gap-1 rounded-md bg-default-100 px-1.5 py-0.5 text-[11px] text-default-600 dark:bg-default-100/60 dark:text-default-400"
                >
                  <span className="font-medium text-foreground">
                    {kernelBaseName(type)}
                  </span>
                  <span className="tabular-nums text-default-400">
                    ×{count}
                  </span>
                </span>
              ))}
            </div>
          </div>
        </div>
      )}

      <PageToolbar
        placeholder={t("搜索服务器、内核或版本")}
        primary={{
          key: "install-kernel",
          label: t("安装内核"),
          icon: <Plus className="h-4 w-4" />,
          onPress: () => openInstall(),
        }}
        search={kernels.length > 0 ? search : undefined}
        onSearch={kernels.length > 0 ? setSearch : undefined}
      >
        {kernels.length > 0 && (typeCounts.length > 1 || issueCount > 0) && (
          <div className="flex flex-wrap items-center gap-1">
            <button
              className={`h-7 rounded-md px-2.5 text-xs font-medium transition-colors ${
                filter === "all"
                  ? "bg-foreground text-background"
                  : "text-default-500 hover:bg-default-100 hover:text-foreground"
              }`}
              type="button"
              onClick={() => setFilter("all")}
            >
              {t("全部")}
            </button>
            {typeCounts.map(([type, count]) => (
              <button
                key={type}
                className={`h-7 rounded-md px-2.5 text-xs font-medium transition-colors ${
                  filter === type
                    ? "bg-foreground text-background"
                    : "text-default-500 hover:bg-default-100 hover:text-foreground"
                }`}
                type="button"
                onClick={() => setFilter(type)}
              >
                {kernelBaseName(type)}
                <span className="ml-1 opacity-65 tabular-nums">{count}</span>
              </button>
            ))}
            {issueCount > 0 && (
              <button
                className={`h-7 rounded-md px-2.5 text-xs font-medium transition-colors ${
                  filter === "issue"
                    ? "bg-amber-500 text-white"
                    : "text-amber-600 hover:bg-amber-500/10 dark:text-amber-400"
                }`}
                type="button"
                onClick={() => setFilter("issue")}
              >
                {t("异常")}
                <span className="ml-1 opacity-80 tabular-nums">
                  {issueCount}
                </span>
              </button>
            )}
          </div>
        )}
      </PageToolbar>

      {kernels.length === 0 ? (
        <EmptyHint
          action={{
            key: "install-kernel",
            label: t("安装内核"),
            onPress: () => openInstall(),
          }}
          text={t(
            "还没有在任何服务器上安装内核。点击“安装内核”，先在服务器上安装 Xray，再到“节点”里创建入站。",
          )}
        />
      ) : nodeGroups.length === 0 ? (
        <EmptyHint
          action={{
            key: "reset-filter",
            label: t("清除筛选"),
            onPress: () => {
              setSearch("");
              setFilter("all");
            },
          }}
          text={t("没有符合筛选条件的内核。")}
        />
      ) : (
        <div className="grid grid-cols-1 items-start gap-4 lg:grid-cols-2">
          {nodeGroups.map((g) => {
            const first = g.items[0];
            const online = first?.nodeOnline;
            const nodeMeta = first ? nodeById.get(first.nodeId) : undefined;
            const flag = nodeRegionFlag(
              nodeMeta?.region || nodeMeta?.regionDetected || "",
            );

            return (
              <section
                key={g.key}
                className="overflow-hidden rounded-xl border border-divider bg-background transition-colors hover:border-default-300 dark:hover:border-default-200"
              >
                {/* Server card header */}
                <div className="flex items-center justify-between gap-2 border-b border-divider/70 bg-default-50/60 px-4 py-2.5 dark:bg-default-50/20">
                  <div className="flex min-w-0 items-center gap-2">
                    <span
                      className={`h-2 w-2 shrink-0 rounded-full ${
                        online ? "bg-emerald-500" : "bg-default-300"
                      }`}
                    />
                    {flag && (
                      <span className="shrink-0 text-sm leading-none">
                        {flag}
                      </span>
                    )}
                    <h3 className="truncate text-sm font-semibold text-foreground">
                      {g.title}
                    </h3>
                    <span className="shrink-0 rounded-full bg-default-100 px-2 py-0.5 text-[11px] tabular-nums text-default-500 dark:bg-default-100/60">
                      {t("{count} 个内核", { count: g.items.length })}
                    </span>
                    {!online && (
                      <Chip className="shrink-0" size="sm" variant="flat">
                        {t("离线")}
                      </Chip>
                    )}
                  </div>
                  <Button
                    className="h-7 min-w-0 shrink-0 gap-1 px-2 text-xs text-default-600 hover:text-foreground"
                    size="sm"
                    startContent={<Plus className="h-3.5 w-3.5" />}
                    variant="light"
                    onPress={() => openInstall(first.nodeId)}
                  >
                    {t("安装")}
                  </Button>
                </div>

                {/* Installed kernels list */}
                <div className="divide-y divide-divider/60">
                  {g.items.map(renderKernelRow)}
                </div>
              </section>
            );
          })}
        </div>
      )}

      <ActionSheet
        actions={
          sheetKernel
            ? [
                sheetKernel.enabled
                  ? {
                      key: "stop",
                      label: t("停止"),
                      onPress: () => doControl(sheetKernel, "stop"),
                    }
                  : {
                      key: "start",
                      label: t("启动"),
                      color: "success" as const,
                      onPress: () => doControl(sheetKernel, "start"),
                    },
                {
                  key: "restart",
                  label: t("重启"),
                  disabled: !sheetKernel.enabled,
                  onPress: () => doControl(sheetKernel, "restart"),
                },
                {
                  key: "logs",
                  label: t("日志"),
                  onPress: () => openLogs(sheetKernel),
                },
                // realm's version follows the panel.
                ...(capsByType.get(sheetKernel.kernelType)?.relay
                  ? []
                  : [
                      {
                        key: "upgrade",
                        label: t("换版本"),
                        onPress: () =>
                          openInstall(
                            sheetKernel.nodeId,
                            sheetKernel.kernelType,
                          ),
                      },
                    ]),
                {
                  key: "remove",
                  label: t("卸载"),
                  color: "danger" as const,
                  onPress: () => {
                    setRemoveFailed(false);
                    setRemoveTarget(sheetKernel);
                  },
                },
              ]
            : []
        }
        isOpen={Boolean(sheetKernel)}
        subtitle={
          sheetKernel &&
          `${sheetKernel.nodeName || t("服务器 {id}", { id: sheetKernel.nodeId })} · ${
            sheetKernel.installedVersion
              ? shortVersion(sheetKernel.installedVersion)
              : sheetKernel.version
          } · ${t("{count} 个入站", { count: sheetKernel.inboundCount })}`
        }
        title={sheetKernel ? kernelName(sheetKernel.kernelType) : ""}
        onClose={() => setSheetFor(null)}
      >
        {sheetKernel?.lastError && (
          <div className="flex items-start gap-2 rounded-lg border border-danger/25 bg-danger/5 px-3 py-2 text-xs text-danger">
            <AlertCircle className="mt-0.5 h-3.5 w-3.5 shrink-0" />
            <p className="break-all leading-relaxed">{sheetKernel.lastError}</p>
          </div>
        )}
      </ActionSheet>

      {/* Install / Change Version Modal */}
      <Modal
        classNames={{ base: modalBase }}
        isOpen={installOpen}
        placement="center"
        onOpenChange={setInstallOpen}
      >
        <ModalContent>
          {(onClose) => (
            <>
              <ModalHeader>
                <div className="flex items-center gap-3">
                  <span className="flex h-9 w-9 shrink-0 items-center justify-center rounded-xl bg-default-100 text-foreground dark:bg-default-100/70">
                    <Download className="h-4 w-4" strokeWidth={1.75} />
                  </span>
                  <div className="min-w-0">
                    <h2 className="text-base font-semibold leading-tight text-foreground">
                      {t("安装内核")}
                    </h2>
                    <p className="mt-0.5 text-xs font-normal text-default-500">
                      {t(
                        "服务器会从官方 Release 下载并校验 SHA256，首次安装可能需要一两分钟",
                      )}
                    </p>
                  </div>
                </div>
              </ModalHeader>
              <ModalBody>
                <div className="space-y-3.5">
                  <Select
                    label={t("服务器")}
                    placeholder={t("选择服务器")}
                    selectedKeys={
                      installForm.nodeId ? [installForm.nodeId] : []
                    }
                    onSelectionChange={(keys) =>
                      setInstallForm((p) => ({
                        ...p,
                        nodeId: String(Array.from(keys)[0] ?? ""),
                      }))
                    }
                  >
                    {installNodeOptions.map((n) => (
                      <SelectItem
                        key={String(n.id)}
                        textValue={
                          n.status === 1 ? n.name : `${n.name}（${t("离线")}）`
                        }
                      >
                        <div className="flex items-center gap-2">
                          <span
                            className={`h-1.5 w-1.5 shrink-0 rounded-full ${
                              n.status === 1
                                ? "bg-emerald-500"
                                : "bg-default-300"
                            }`}
                          />
                          <span>
                            {n.status === 1
                              ? n.name
                              : `${n.name}（${t("离线")}）`}
                          </span>
                        </div>
                      </SelectItem>
                    ))}
                  </Select>
                  <Select
                    disabledKeys={(caps?.kernels ?? [])
                      .filter((k) => !k.implemented)
                      .map((k) => k.type)}
                    label={t("内核")}
                    selectedKeys={[installForm.kernelType]}
                    onSelectionChange={(keys) =>
                      setInstallForm((p) => ({
                        ...p,
                        kernelType: String(Array.from(keys)[0] ?? "xray"),
                        version:
                          capsByType.get(String(Array.from(keys)[0]))
                            ?.versions?.[0] ?? "latest",
                      }))
                    }
                  >
                    {(caps?.kernels ?? [])
                      .filter((k) => !k.relay)
                      .map((k) => (
                        <SelectItem
                          key={k.type}
                          textValue={
                            k.implemented
                              ? k.name
                              : `${k.name}（${t("即将支持")}）`
                          }
                        >
                          {k.implemented
                            ? k.name
                            : `${k.name}（${t("即将支持")}）`}
                        </SelectItem>
                      ))}
                  </Select>
                  {pinnedVersions.length > 0 ? (
                    <Select
                      description={t(
                        "官方渠道不提供校验文件，仅开放已核对 SHA256 的版本",
                      )}
                      label={t("版本")}
                      selectedKeys={[
                        pinnedVersions.includes(installForm.version)
                          ? installForm.version
                          : pinnedVersions[0],
                      ]}
                      onSelectionChange={(keys) =>
                        setInstallForm((p) => ({
                          ...p,
                          version: String(Array.from(keys)[0] ?? ""),
                        }))
                      }
                    >
                      {pinnedVersions.map((v) => (
                        <SelectItem key={v} textValue={v}>
                          {v}
                          {installForm.kernelType === "snell" &&
                            v.startsWith("6.") && (
                              <span className="ml-2 text-xs text-warning">
                                {t("测试版 · 只接受 v6 客户端 · 无 armv7")}
                              </span>
                            )}
                        </SelectItem>
                      ))}
                    </Select>
                  ) : (
                    <>
                      {releaseItems.length > 0 && (
                        <Select
                          description={t(
                            "最新版出问题时可以选旧版本；预发布版不建议使用",
                          )}
                          label={t("版本")}
                          selectedKeys={[
                            customVersion
                              ? "custom"
                              : installForm.version === "latest" ||
                                  !installForm.version
                                ? "latest"
                                : installForm.version,
                          ]}
                          onSelectionChange={(keys) => {
                            const v = String(Array.from(keys)[0] ?? "latest");

                            setCustomVersion(v === "custom");
                            if (v !== "custom") {
                              setInstallForm((p) => ({ ...p, version: v }));
                            }
                          }}
                        >
                          {[
                            <SelectItem
                              key="latest"
                              textValue={t("最新版（latest）")}
                            >
                              {t("最新版（latest）")}
                            </SelectItem>,
                            ...releaseItems.map((r) => (
                              <SelectItem
                                key={r.version}
                                textValue={
                                  r.prerelease
                                    ? `${r.version}（${t("预发布")}）`
                                    : r.version
                                }
                              >
                                {r.prerelease
                                  ? `${r.version}（${t("预发布")}）`
                                  : r.version}
                              </SelectItem>
                            )),
                            <SelectItem key="custom" textValue={t("手动输入…")}>
                              {t("手动输入…")}
                            </SelectItem>,
                          ]}
                        </Select>
                      )}
                      {(releaseItems.length === 0 || customVersion) && (
                        <Input
                          description={
                            releases?.error && releaseItems.length === 0
                              ? t(
                                  "{error}，可手动填写版本号，latest 表示最新版",
                                  { error: releases.error },
                                )
                              : t("latest 表示最新版，也可填写具体版本号")
                          }
                          label={
                            releaseItems.length > 0
                              ? t("自定义版本")
                              : t("版本")
                          }
                          value={installForm.version}
                          variant="bordered"
                          onChange={(e) =>
                            setInstallForm((p) => ({
                              ...p,
                              version: e.target.value,
                            }))
                          }
                        />
                      )}
                    </>
                  )}
                </div>
              </ModalBody>
              <ModalFooter>
                <Button size="sm" variant="light" onPress={onClose}>
                  {t("取消")}
                </Button>
                <Button
                  color="primary"
                  isLoading={installing}
                  size="sm"
                  onPress={doInstall}
                >
                  {t("安装")}
                </Button>
              </ModalFooter>
            </>
          )}
        </ModalContent>
      </Modal>

      {/* Kernel Logs Terminal Modal */}
      <Modal
        classNames={{ base: modalBase }}
        isOpen={Boolean(logsFor)}
        placement="center"
        scrollBehavior="inside"
        size="xl"
        onOpenChange={(open) => !open && setLogsFor(null)}
      >
        <ModalContent>
          {(onClose) => (
            <>
              <ModalHeader closable>
                <div className="flex items-center gap-3">
                  <span className="flex h-9 w-9 shrink-0 items-center justify-center rounded-xl bg-default-100 text-foreground dark:bg-default-100/70">
                    <Terminal className="h-4 w-4" strokeWidth={1.75} />
                  </span>
                  <div className="min-w-0">
                    <h2 className="truncate text-base font-semibold leading-tight text-foreground">
                      {logsFor?.nodeName} ·{" "}
                      {kernelName(logsFor?.kernelType ?? "")}
                    </h2>
                    <div className="mt-1 flex flex-wrap items-center gap-2 text-xs font-normal text-default-500">
                      {logs ? (
                        <>
                          <span
                            className={`inline-flex items-center gap-1.5 rounded-full px-2 py-0.5 text-[11px] font-medium ${
                              logs.running
                                ? "bg-emerald-500/10 text-emerald-700 dark:text-emerald-400"
                                : "bg-default-100 text-default-500"
                            }`}
                          >
                            <span
                              className={`h-1.5 w-1.5 rounded-full ${
                                logs.running
                                  ? "bg-emerald-500"
                                  : "bg-default-400"
                              }`}
                            />
                            {logs.running
                              ? t("运行中 PID {pid}", { pid: logs.pid })
                              : t("未运行")}
                          </span>
                          {logs.restarts > 0 && (
                            <span className="text-default-400">
                              {t("已自动重启 {count} 次", {
                                count: logs.restarts,
                              })}
                            </span>
                          )}
                        </>
                      ) : (
                        <span>{t("内核运行日志")}</span>
                      )}
                    </div>
                  </div>
                </div>
              </ModalHeader>
              <ModalBody className="pt-1">
                <div className="overflow-hidden rounded-xl border border-zinc-800/90 bg-zinc-950 text-zinc-100">
                  <div className="flex items-center justify-between gap-2 border-b border-white/10 bg-white/[0.04] px-3.5 py-2">
                    <span className="font-mono text-[11px] text-zinc-400">
                      {logsLoading
                        ? t("加载中...")
                        : t("{count} 行日志", {
                            count: logs?.logs?.length ?? 0,
                          })}
                    </span>
                    <div className="flex items-center gap-1.5">
                      <button
                        className={`inline-flex items-center gap-1 rounded-md px-2 py-1 text-xs font-medium transition-colors disabled:opacity-40 ${
                          logsCopied
                            ? "bg-emerald-500/20 text-emerald-300"
                            : "bg-white/10 text-zinc-200 hover:bg-white/15 hover:text-white"
                        }`}
                        disabled={logsLoading || !(logs?.logs?.length ?? 0)}
                        type="button"
                        onClick={() => void copyLogs()}
                      >
                        {logsCopied ? (
                          <>
                            <Check className="h-3 w-3" />
                            <span>{t("已复制")}</span>
                          </>
                        ) : (
                          <>
                            <Copy className="h-3 w-3" />
                            <span>{t("复制")}</span>
                          </>
                        )}
                      </button>
                      <button
                        className="inline-flex items-center gap-1 rounded-md bg-white/10 px-2 py-1 text-xs font-medium text-zinc-200 transition-colors hover:bg-white/15 hover:text-white disabled:opacity-40"
                        disabled={logsLoading}
                        type="button"
                        onClick={() => logsFor && void openLogs(logsFor)}
                      >
                        <RefreshCw
                          className={`h-3 w-3 ${logsLoading ? "animate-spin" : ""}`}
                        />
                        <span>{t("刷新")}</span>
                      </button>
                    </div>
                  </div>
                  <pre className="m-0 max-h-[55vh] overflow-auto p-4 font-mono text-xs leading-relaxed whitespace-pre-wrap break-all text-zinc-200 desktop-scrollbar">
                    {logsLoading
                      ? t("加载中...")
                      : (logs?.logs ?? []).join("\n") || t("暂无日志")}
                  </pre>
                </div>
              </ModalBody>
              <ModalFooter className="pt-2">
                <Button size="sm" variant="light" onPress={onClose}>
                  {t("关闭")}
                </Button>
              </ModalFooter>
            </>
          )}
        </ModalContent>
      </Modal>

      {/* Remove Kernel Confirm Modal */}
      <Modal
        classNames={{ base: modalBase }}
        isOpen={Boolean(removeTarget)}
        placement="center"
        onOpenChange={(open) => !open && setRemoveTarget(null)}
      >
        <ModalContent>
          {(onClose) => (
            <>
              <ModalHeader>
                <div className="flex items-center gap-3">
                  <span className="flex h-9 w-9 shrink-0 items-center justify-center rounded-xl bg-danger/10 text-danger">
                    <AlertTriangle className="h-4 w-4" strokeWidth={1.75} />
                  </span>
                  <h2 className="text-base font-semibold text-foreground">
                    {t("卸载内核")}
                  </h2>
                </div>
              </ModalHeader>
              <ModalBody className="space-y-3">
                <p className="text-sm text-default-600">
                  {t("确定从 {node} 卸载 {kernel} 吗？", {
                    node: removeTarget?.nodeName ?? "",
                    kernel: kernelName(removeTarget?.kernelType ?? ""),
                  })}
                </p>
                <div className="rounded-xl border border-danger/25 bg-danger/5 px-3.5 py-2.5 text-xs leading-relaxed text-danger">
                  {t("该内核下的 {count} 个入站会一起删除，且无法恢复。", {
                    count: removeTarget?.inboundCount ?? 0,
                  })}
                </div>
                {removeFailed && (
                  <div className="rounded-xl border border-warning/30 bg-warning/10 px-3.5 py-2.5 text-xs leading-relaxed text-warning">
                    {t(
                      "服务器卸载失败（可能离线）。可以强制删除面板记录，服务器上线后会自动清理。",
                    )}
                  </div>
                )}
              </ModalBody>
              <ModalFooter>
                <Button size="sm" variant="light" onPress={onClose}>
                  {t("取消")}
                </Button>
                {removeFailed && (
                  <Button
                    color="warning"
                    isLoading={busy === `${removeTarget?.id}-remove`}
                    size="sm"
                    onPress={() => void doRemove(true)}
                  >
                    {t("强制删除")}
                  </Button>
                )}
                <Button
                  color="danger"
                  isLoading={busy === `${removeTarget?.id}-remove`}
                  size="sm"
                  onPress={() => void doRemove(false)}
                >
                  {t("卸载")}
                </Button>
              </ModalFooter>
            </>
          )}
        </ModalContent>
      </Modal>
    </div>
  );
}
