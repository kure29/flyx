import type { RemoteSourceItem } from "@/api";
import type {
  InboundApiItem,
  KernelApiItem,
  KernelCapabilitiesApiData,
  NodeApiItem,
} from "@/api/types";

import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import toast from "react-hot-toast";
import { ChevronDown, Copy } from "lucide-react";
import { useNavigate } from "react-router-dom";

import { getStoredValue, setStoredValue } from "@/utils/identity-storage";
import {
  batchDeleteInbounds,
  batchToggleInbounds,
  deleteInbound,
  getInboundList,
  getKernelCapabilities,
  getKernelList,
  getNodeList,
  getRemoteSources,
  probeInbounds,
  regenerateInbound,
  resetInboundTraffic,
  toggleInbound,
} from "@/api";
import {
  AnimatedPage,
  StaggerItem,
  StaggerList,
} from "@/components/animated-page";
import { PageLoadingState } from "@/components/page-state";
import { PageHeader, PageTabs, PageToolbar } from "@/components/page-kit";
import { CARD_GRID, type CardTone, InfoCard } from "@/components/info-card";
import { useViewMode } from "@/components/view-mode";
import { useLocalStorageState } from "@/hooks/use-local-storage-state";
import {
  markPageCached,
  pageCached,
  useCachedState,
} from "@/hooks/use-cached-state";
import { useVisiblePolling } from "@/hooks/use-visible-polling";
import { GroupHeader, groupItems, useCollapsed } from "@/pages/inbound/group";
import { LatencyCell } from "@/components/latency-cell";
import { RouteModal } from "@/components/route-modal";
import { LatencyTrendModal } from "@/components/lazy-charts";
import { ActionSheet } from "@/components/action-sheet";
import { SplitModal } from "@/pages/inbound/split-modal";
import {
  EXTERNAL_LABEL,
  ExternalEditModal,
  ImportModal,
} from "@/pages/inbound/external";
import { InboundFormModal } from "@/pages/inbound/inbound-form-modal";
import { type NodeHubState } from "@/pages/node-hub";
import { RelayModal } from "@/pages/inbound/relay-modal";
import { RemoteSourcesSection } from "@/pages/inbound/sources";
import { ShareModal, copyText } from "@/pages/inbound/share";
import { TagChips, TagsModal, collectTags } from "@/pages/inbound/tags";
import {
  type InboundForm,
  KERNEL_LABELS,
  describeInbound,
  emptyForm,
  kernelStatusText,
  formFromItem,
} from "@/pages/inbound/presets";
import { Button } from "@/shadcn-bridge/heroui/button";
import { Card, CardBody } from "@/shadcn-bridge/heroui/card";
import { Checkbox } from "@/shadcn-bridge/heroui/checkbox";
import { Chip } from "@/shadcn-bridge/heroui/chip";
import {
  Modal,
  ModalBody,
  ModalContent,
  ModalFooter,
  ModalHeader,
} from "@/shadcn-bridge/heroui/modal";
import { Switch } from "@/shadcn-bridge/heroui/switch";
import { formatTraffic } from "@/utils/traffic";
import { latestProbe } from "@/utils/latest-probe";

const modalBase =
  "!w-[calc(100%-32px)] !mx-auto sm:!w-full rounded-2xl overflow-hidden";

type ConfirmAction = "delete" | "regenerate" | "reset";

type PageTab = "inbounds" | "sources";

const TAB_KEY = "flyx_inbound_tab";

/** Remembers the selected tab; a convenience only, so storage errors are ignored. */
function usePageTab(): [PageTab, (t: PageTab) => void] {
  const [tab, setTabState] = useState<PageTab>(() => {
    try {
      const v = getStoredValue(localStorage, TAB_KEY);

      return v === "sources" ? v : "inbounds";
    } catch {
      return "inbounds";
    }
  });
  const setTab = useCallback((t: PageTab) => {
    setTabState(t);
    try {
      setStoredValue(localStorage, TAB_KEY, t);
    } catch {
      // Ignored.
    }
  }, []);

  return [tab, setTab];
}

const CONFIRM_TEXT: Record<
  ConfirmAction,
  { title: string; body: string; button: string; color: "danger" | "warning" }
> = {
  delete: {
    title: "删除入站",
    body: "删除后服务器上的该服务会立即停止，且无法恢复。",
    button: "确认删除",
    color: "danger",
  },
  regenerate: {
    title: "重新生成密钥",
    body: "会重新生成 UUID / 密码 / Reality 密钥，已有客户端需要重新导入配置。",
    button: "重新生成",
    color: "warning",
  },
  reset: {
    title: "重置流量",
    body: "将该入站的上传和下载流量清零。",
    button: "重置",
    color: "warning",
  },
};

function detailRows(item: InboundApiItem): Array<[string, string]> {
  const host = item.shareAddress || item.shareHost || item.nodeServerIp;

  if (item.importedParams) {
    return [
      ["地址", host],
      ["端口", String(item.port)],
      ["协议", describeInbound(item)],
      ...item.importedParams,
    ];
  }
  const rows: Array<[string, string]> = [
    ["地址", host],
    ["端口", String(item.port)],
    ["协议", describeInbound(item)],
  ];
  const s = item.settings;
  const t = item.transport;
  const sec = item.security;

  if (s.uuid) rows.push(["UUID", s.uuid]);
  if (s.username) rows.push(["用户名", s.username]);
  if (s.flow) rows.push(["Flow", s.flow]);
  if (s.method) rows.push(["加密方式", s.method]);
  if (s.password)
    rows.push([item.protocol === "snell" ? "PSK" : "密码", s.password]);
  if (s.snellVersion) rows.push(["Snell 版本", `v${s.snellVersion}`]);
  if (s.snellMode) rows.push(["流量整形", s.snellMode]);
  if (s.masquerade) rows.push(["伪装网站", s.masquerade]);
  if (s.portHopping) rows.push(["端口跳跃", `${s.portHopping}（UDP）`]);
  if (t.path) rows.push(["路径", t.path]);
  if (t.host) rows.push(["Host", t.host]);
  if (t.serviceName) rows.push(["serviceName", t.serviceName]);
  if (t.mode) rows.push(["XHTTP 模式", t.mode]);
  if (sec.sni) {
    rows.push([
      sec.type === "shadowtls" ? "ShadowTLS 伪装网站" : "SNI",
      sec.sni,
    ]);
  }
  if (sec.shadowTlsPassword) {
    rows.push(["ShadowTLS 密码", sec.shadowTlsPassword]);
    rows.push(["ShadowTLS 版本", "3"]);
  }
  if (sec.fingerprint) rows.push(["指纹 (fp)", sec.fingerprint]);
  if (sec.realityPublicKey)
    rows.push(["Reality 公钥 (pbk)", sec.realityPublicKey]);
  if (sec.realityShortIds?.length) {
    rows.push(["shortId (sid)", sec.realityShortIds[0]]);
  }
  if (sec.type === "tls" && sec.certMode === "self") {
    rows.push(["证书", "自签（客户端需允许不安全证书或固定证书）"]);
  }
  if (sec.type === "tls" && sec.certMode === "acme") {
    rows.push([
      "证书",
      `自动申请（${sec.acmeDns ? `${sec.acmeDns} DNS 验证` : "HTTP 验证"}）`,
    ]);
  }

  return rows;
}

/** Servers first, then hand-imported nodes, then each remote source. */
function groupKeyOf(i: InboundApiItem): string {
  if (!i.external) return String(i.nodeId);

  return i.sourceId ? `source:${i.sourceId}` : "external";
}

function groupLabelOf(i: InboundApiItem): string {
  if (!i.external) return i.nodeName || `服务器 ${i.nodeId}`;

  return i.sourceId ? `订阅源 · ${i.sourceName || i.sourceId}` : EXTERNAL_LABEL;
}

function groupRank(key: string): number {
  return key.startsWith("source:") ? 2 : key === "external" ? 1 : 0;
}

const PAGE = "inbound";

export default function InboundPage() {
  // A revisit shows the last data at once and reloads in the background.
  const [loading, setLoading] = useState(() => !pageCached(PAGE));
  const [caps, setCaps] = useCachedState<KernelCapabilitiesApiData | null>(
    PAGE,
    "caps",
    null,
  );
  const [nodes, setNodes] = useCachedState<NodeApiItem[]>(PAGE, "nodes", []);
  const [kernels, setKernels] = useCachedState<KernelApiItem[]>(
    PAGE,
    "kernels",
    [],
  );
  const [inbounds, setInbounds] = useCachedState<InboundApiItem[]>(
    PAGE,
    "inbounds",
    [],
  );
  const [isCollapsed, toggleCollapsed] = useCollapsed();
  const [searchKeyword, setSearchKeyword] = useLocalStorageState(
    "inbound-search-keyword",
    "",
  );
  const [nodeFilter, setNodeFilter] = useState("");
  const [tagFilter, setTagFilter] = useLocalStorageState(
    "inbound-tag-filter",
    "",
  );
  const [tagTarget, setTagTarget] = useState<{
    ids: number[];
    tags: string[];
    title: string;
  } | null>(null);
  const [tab, setTab] = usePageTab();
  const navigate = useNavigate();
  const [sheetFor, setSheetFor] = useState<number | null>(null);
  const [importOpen, setImportOpen] = useState(false);
  const [externalEdit, setExternalEdit] = useState<InboundApiItem | null>(null);

  const [formOpen, setFormOpen] = useState(false);
  const [formInitial, setFormInitial] = useState<InboundForm>(() =>
    emptyForm(null),
  );
  const [details, setDetails] = useState<InboundApiItem | null>(null);
  const [sharing, setSharing] = useState<InboundApiItem | null>(null);
  const [relaying, setRelaying] = useState<InboundApiItem | null>(null);
  const [splitting, setSplitting] = useState<InboundApiItem | null>(null);
  const [confirm, setConfirm] = useState<{
    action: ConfirmAction;
    item: InboundApiItem;
  } | null>(null);
  const [confirmLoading, setConfirmLoading] = useState(false);
  const [toggling, setToggling] = useState<number | null>(null);
  const [selectMode, setSelectMode] = useState(false);
  const [viewMode, setViewMode] = useViewMode("inbound");
  const card = viewMode === "card";
  const [selectedIds, setSelectedIds] = useState<Set<number>>(new Set());
  const [batchDeleteOpen, setBatchDeleteOpen] = useState(false);
  const [batchDeleting, setBatchDeleting] = useState(false);
  const [batchToggling, setBatchToggling] = useState(false);
  const [probing, setProbing] = useState(false);
  const [probingIds, setProbingIds] = useState<Set<number>>(new Set());
  const [routeNode, setRouteNode] = useState<{
    id: number;
    name: string;
  } | null>(null);
  const [trendNode, setTrendNode] = useState<{
    id: number;
    name: string;
  } | null>(null);
  const [sources, setSources] = useCachedState<RemoteSourceItem[]>(
    PAGE,
    "sources",
    [],
  );
  const loadingRef = useRef(false);
  const reloadPendingRef = useRef(false);
  const metadataLoadedAtRef = useRef(0);

  const loadData = useCallback(
    async (initial = false, options?: { silent?: boolean }): Promise<void> => {
      const silent = options?.silent ?? false;

      if (loadingRef.current) {
        if (!silent && !initial) reloadPendingRef.current = true;

        return;
      }
      loadingRef.current = true;
      if (initial) setLoading(true);
      try {
        // Status comes with every poll; form capabilities and subscription
        // sources at most every thirty seconds.
        const metadata =
          !silent || Date.now() - metadataLoadedAtRef.current >= 30_000;
        const [capsRes, nodeRes, kernelRes, inboundRes, sourceRes] =
          await Promise.all([
            metadata ? getKernelCapabilities() : null,
            metadata ? getNodeList() : null,
            metadata ? getKernelList() : null,
            getInboundList(),
            metadata ? getRemoteSources() : null,
          ]);

        if (metadata) metadataLoadedAtRef.current = Date.now();
        if (capsRes?.code === 0) setCaps(capsRes.data);
        if (sourceRes?.code === 0) setSources(sourceRes.data ?? []);
        if (nodeRes?.code === 0) setNodes(nodeRes.data || []);
        if (kernelRes?.code === 0) setKernels(kernelRes.data || []);
        if (inboundRes.code === 0) {
          const rows = inboundRes.data || [];

          setInbounds((prev) => {
            const previous = new Map(prev.map((i) => [i.id, i]));

            return rows.map((i) => ({
              ...i,
              probe: latestProbe(i.probe, previous.get(i.id)?.probe),
            }));
          });
          markPageCached(PAGE);
          setSelectedIds((prev) => {
            if (prev.size === 0) return prev;
            const alive = new Set(rows.map((i) => i.id));

            return new Set([...prev].filter((id) => alive.has(id)));
          });
        } else if (!silent) {
          toast.error(inboundRes.msg || "获取入站列表失败");
        }
      } catch {
        if (!silent) toast.error("加载数据失败");
      } finally {
        loadingRef.current = false;
        if (initial) setLoading(false);
        if (reloadPendingRef.current) {
          reloadPendingRef.current = false;
          void loadData(false);
        }
      }
    },
    [],
  );

  useEffect(() => {
    // With cached data on screen, reload without the loading state.
    loadData(!pageCached(PAGE));
  }, [loadData]);
  useVisiblePolling(
    useCallback(() => loadData(false, { silent: true }), [loadData]),
  );

  const nodeOptions = useMemo(() => {
    const map = new Map<string, string>();

    for (const i of inbounds) {
      map.set(groupKeyOf(i), groupLabelOf(i));
    }

    return [...map.entries()].sort((a, b) => a[1].localeCompare(b[1], "zh-CN"));
  }, [inbounds]);

  const allTags = useMemo(() => collectTags(inbounds), [inbounds]);
  // A remembered tag that no node carries any more filters nothing.
  const activeTag = allTags.includes(tagFilter) ? tagFilter : "";

  const filtered = useMemo(() => {
    const kw = searchKeyword.trim().toLowerCase();

    return inbounds.filter(
      (i) =>
        (!nodeFilter || groupKeyOf(i) === nodeFilter) &&
        (!activeTag || (i.tags ?? []).includes(activeTag)) &&
        (!kw ||
          [
            i.name,
            i.nodeName,
            i.protocol,
            String(i.port),
            i.remark,
            ...(i.tags ?? []),
          ]
            .join(" ")
            .toLowerCase()
            .includes(kw)),
    );
  }, [inbounds, searchKeyword, nodeFilter, activeTag]);

  // Grouped by server, imported nodes last; one group shows as a flat list.
  const inboundGroups = groupItems(filtered, groupKeyOf, (_key, i) =>
    groupLabelOf(i),
  ).sort((a, b) => groupRank(a.key) - groupRank(b.key));
  const grouped = new Set(inbounds.map(groupKeyOf)).size > 1;

  const openCreate = () => {
    const firstKernelNode = kernels[0]?.nodeId;

    setFormInitial(
      emptyForm(caps, firstKernelNode ? String(firstKernelNode) : ""),
    );
    setFormOpen(true);
  };

  const openTags = (item: InboundApiItem) =>
    setTagTarget({ ids: [item.id], tags: item.tags ?? [], title: item.name });

  const openEdit = (item: InboundApiItem) => {
    setFormInitial(formFromItem(item));
    setFormOpen(true);
  };

  // Kernels live under 节点 → 内核.
  const handleInstallFromForm = useCallback(
    (nodeId: number, kernelType: string) => {
      setFormOpen(false);
      navigate("/node", {
        state: { install: { nodeId, kernelType } } satisfies NodeHubState,
      });
    },
    [navigate],
  );

  const handleToggle = async (item: InboundApiItem) => {
    setToggling(item.id);
    try {
      const res = await toggleInbound(item.id, !item.enabled);

      if (res.code !== 0) {
        toast.error(res.msg || "操作失败");
      } else if (res.msg && res.msg !== "操作成功") {
        toast(res.msg, { icon: "⚠️" });
      }
      await loadData(false);
    } finally {
      setToggling(null);
    }
  };

  const runConfirm = async () => {
    if (!confirm) return;
    setConfirmLoading(true);
    try {
      const { action, item } = confirm;
      const res =
        action === "delete"
          ? await deleteInbound(item.id)
          : action === "regenerate"
            ? await regenerateInbound(item.id)
            : await resetInboundTraffic(item.id);

      if (res.code !== 0) {
        toast.error(res.msg || "操作失败");

        return;
      }
      if (res.msg && res.msg !== "操作成功") {
        toast(res.msg, { icon: "⚠️" });
      } else {
        toast.success("操作成功");
      }
      setConfirm(null);
      await loadData(false);
    } finally {
      setConfirmLoading(false);
    }
  };

  const allFilteredSelected =
    filtered.length > 0 && filtered.every((i) => selectedIds.has(i.id));

  const toggleSelect = (id: number) => {
    setSelectedIds((prev) => {
      const next = new Set(prev);

      if (next.has(id)) next.delete(id);
      else next.add(id);

      return next;
    });
  };

  const toggleSelectAll = () => {
    setSelectedIds((prev) => {
      const next = new Set(prev);

      for (const i of filtered) {
        if (allFilteredSelected) next.delete(i.id);
        else next.add(i.id);
      }

      return next;
    });
  };

  const exitSelectMode = () => {
    setSelectMode(false);
    setSelectedIds(new Set());
  };

  const runBatchDelete = async () => {
    if (selectedIds.size === 0) return;
    setBatchDeleting(true);
    try {
      const res = await batchDeleteInbounds([...selectedIds]);

      if (res.code !== 0) {
        toast.error(res.msg || "删除失败");

        return;
      }
      const { successCount = 0, failCount = 0 } = res.data ?? {};

      if (res.msg && res.msg !== "操作成功") {
        toast(res.msg, { icon: "⚠️" });
      } else if (failCount > 0) {
        toast(`已删除 ${successCount} 个，${failCount} 个失败`, {
          icon: "⚠️",
        });
      } else {
        toast.success(`已删除 ${successCount} 个入站`);
      }
      setBatchDeleteOpen(false);
      exitSelectMode();
      await loadData(false);
    } catch {
      toast.error("删除失败");
    } finally {
      setBatchDeleting(false);
    }
  };

  const runBatchToggle = async (enabled: boolean) => {
    if (selectedIds.size === 0) return;
    setBatchToggling(true);
    try {
      const res = await batchToggleInbounds([...selectedIds], enabled);

      if (res.code !== 0) {
        toast.error(res.msg || "操作失败");

        return;
      }
      const { successCount = 0, failCount = 0 } = res.data ?? {};
      const verb = enabled ? "启用" : "停用";

      if (res.msg && res.msg !== "操作成功") {
        toast(res.msg, { icon: "⚠️" });
      } else if (failCount > 0) {
        toast(`已${verb} ${successCount} 个，${failCount} 个失败`, {
          icon: "⚠️",
        });
      } else {
        toast.success(`已${verb} ${successCount} 个入站`);
      }
      exitSelectMode();
      await loadData(false);
    } catch {
      toast.error("操作失败");
    } finally {
      setBatchToggling(false);
    }
  };

  const runProbe = async () => {
    setProbing(true);
    try {
      const res = await probeInbounds();

      if (res.code !== 0) {
        toast.error(res.msg || "检测失败");

        return;
      }
      const results = Object.values(res.data ?? {}).filter((p) => !p.skipped);
      const down = results.filter((p) => !p.ok).length;

      if (down > 0) {
        toast(`${results.length - down} 个可连接，${down} 个连不上`, {
          icon: "⚠️",
        });
      } else {
        toast.success(`${results.length} 个节点都能连接`);
      }
      await loadData(false);
    } catch {
      toast.error("检测失败");
    } finally {
      setProbing(false);
    }
  };

  // Probes one inbound again and patches its result in place.
  const probeOne = async (id: number) => {
    setProbingIds((prev) => new Set(prev).add(id));
    try {
      const res = await probeInbounds([id]);
      const probe = res.code === 0 ? res.data?.[String(id)] : undefined;

      if (!probe) {
        toast.error(res.msg || "检测失败");

        return;
      }
      setInbounds((prev) =>
        prev.map((i) =>
          i.id === id ? { ...i, probe: latestProbe(probe, i.probe) } : i,
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

  const gridClass = card ? CARD_GRID : "grid grid-cols-1 gap-2 2xl:grid-cols-2";

  const renderInbound = (item: InboundApiItem) => {
    const probe = item.probe?.skipped ? undefined : item.probe;
    const kernelBad =
      item.enabled && !item.external && item.kernelStatus !== "running";
    // A stopped kernel explains a failed probe, so it is shown instead.
    const unreachable =
      item.enabled && !kernelBad && probe !== undefined && !probe.ok;
    const running = item.enabled && !kernelBad && !unreachable;
    // The group header already names the server.
    const where = item.external
      ? grouped
        ? item.listen
        : EXTERNAL_LABEL
      : grouped
        ? (KERNEL_LABELS[item.kernelType] ?? item.kernelType)
        : item.nodeName;

    const selected = selectMode && selectedIds.has(item.id);
    // Built inbounds show their node's latency and routes on the 节点
    // page; only imported nodes, which have no node here, measure their own.
    const latency =
      item.external && item.enabled && item.probe?.skipped !== true ? (
        <LatencyCell
          probe={probe}
          probing={probingIds.has(item.id)}
          variant={card ? "card" : "list"}
          onProbe={() => probeOne(item.id)}
        />
      ) : null;
    // Cards give measured points their own block under the rows.
    const latencyTiles = card && latency !== null && probe !== undefined;
    const activate = () =>
      selectMode ? toggleSelect(item.id) : setSheetFor(item.id);
    const dotClass = unreachable
      ? "bg-danger"
      : running
        ? "bg-success"
        : item.enabled
          ? "bg-warning"
          : "bg-default-300";
    const stateText = unreachable
      ? `连不上：${probe?.error ?? ""}`
      : running
        ? probe
          ? item.external
            ? `可连接 · ${probe.latencyMs} ms`
            : "可连接"
          : "运行中"
        : item.enabled
          ? kernelStatusText(item.kernelStatus)
          : "已停用";

    const badges = (
      <>
        <TagChips max={card ? undefined : 3} tags={item.tags} />
        {item.outboundIpStrategy && item.outboundIpStrategy !== "auto" && (
          <Chip
            className="shrink-0 whitespace-nowrap"
            color="primary"
            size="sm"
            variant="flat"
          >
            {
              {
                ipv4_only: "IPv4 出站",
                ipv6_only: "IPv6 出站",
                prefer_ipv4: "优先 IPv4",
                prefer_ipv6: "优先 IPv6",
              }[item.outboundIpStrategy]
            }
          </Chip>
        )}
        {item.relayCount > 0 && (
          <Chip
            className="shrink-0 whitespace-nowrap"
            color="secondary"
            size="sm"
            variant="flat"
          >
            {item.relayCount} 中转
          </Chip>
        )}
        {item.outboundId > 0 && (
          <Chip
            className="shrink-0 whitespace-nowrap"
            color={item.outboundMissing ? "danger" : "warning"}
            size="sm"
            title={
              item.outboundMissing
                ? "出站节点已删除"
                : `经 ${item.outboundName} 出站`
            }
            variant="flat"
          >
            {item.outboundMissing ? "出站已删除" : "链式"}
          </Chip>
        )}
        {item.splitRules?.length > 0 && (
          <Chip
            className="shrink-0 whitespace-nowrap"
            color="primary"
            size="sm"
            variant="flat"
          >
            分流
          </Chip>
        )}
        {item.relayOnly && (
          <Chip className="shrink-0 whitespace-nowrap" size="sm" variant="flat">
            仅中转
          </Chip>
        )}
        {kernelBad && (
          <Chip color="warning" size="sm" variant="flat">
            {kernelStatusText(item.kernelStatus)}
          </Chip>
        )}
      </>
    );

    if (card) {
      const tone: CardTone = unreachable
        ? "danger"
        : running
          ? "success"
          : item.enabled
            ? "warning"
            : "default";

      return (
        <InfoCard
          key={item.id}
          actions={[
            {
              key: "share",
              label: "分享",
              color: "primary",
              onPress: () => setSharing(item),
            },
            {
              key: "relay",
              label: "中转",
              color: "secondary",
              onPress: () => setRelaying(item),
            },
            {
              key: "more",
              label: "更多",
              onPress: () => setSheetFor(item.id),
            },
          ]}
          badges={badges}
          headerRight={
            <Switch
              isDisabled={toggling === item.id}
              isSelected={item.enabled}
              size="sm"
              onValueChange={() => handleToggle(item)}
            />
          }
          rows={[
            ["协议", describeInbound(item)],
            [
              "端口",
              <span key="port" className="font-mono">
                {item.port}
              </span>,
            ],
            [
              item.external ? "地址" : "服务器",
              <span key="where" className="font-mono">
                {item.external ? item.listen : item.nodeName}
              </span>,
            ],
            [
              "流量",
              <span key="traffic" className="font-mono">
                ↑ {formatTraffic(item.uploadBytes)} · ↓{" "}
                {formatTraffic(item.downloadBytes)}
              </span>,
            ],
            ...(latency && !latencyTiles
              ? ([["延迟", latency]] as Array<[string, React.ReactNode]>)
              : []),
          ]}
          selectMode={selectMode}
          selected={selected}
          status={
            unreachable
              ? "连不上"
              : running
                ? "运行中"
                : item.enabled
                  ? "异常"
                  : "已停用"
          }
          title={item.name}
          tone={tone}
          onPress={() => setSheetFor(item.id)}
          onSelect={() => toggleSelect(item.id)}
        >
          {latencyTiles ? latency : null}
        </InfoCard>
      );
    }

    return (
      <div
        key={item.id}
        aria-pressed={selectMode ? selected : undefined}
        className={`flex w-full cursor-pointer items-center gap-3 rounded-xl border px-3 py-2.5 text-left shadow-sm transition-colors ${
          selected
            ? "border-primary bg-primary-50/70 dark:bg-primary-900/40"
            : "border-divider bg-content1 hover:bg-default-100"
        }`}
        role="button"
        tabIndex={0}
        onClick={activate}
        onKeyDown={(e) => {
          if (e.target !== e.currentTarget) return;
          if (e.key === "Enter" || e.key === " ") {
            e.preventDefault();
            activate();
          }
        }}
      >
        {selectMode && (
          <div
            className="flex shrink-0"
            role="presentation"
            onClick={(e) => e.stopPropagation()}
          >
            <Checkbox
              aria-label={`选择 ${item.name}`}
              isSelected={selected}
              onValueChange={() => toggleSelect(item.id)}
            />
          </div>
        )}
        <span
          className={`h-2 w-2 shrink-0 rounded-full ${dotClass}`}
          title={stateText}
        />
        <div className="min-w-0 flex-1">
          {/* Badges wrap under the name rather than squeezing it out. */}
          <div className="flex flex-wrap items-center gap-x-1.5 gap-y-1">
            <span className="min-w-0 max-w-full truncate font-medium">
              {item.name}
            </span>
            {badges}
          </div>
          <div className="truncate text-xs text-default-500">
            {describeInbound(item)} · {item.port}
            {where ? ` · ${where}` : ""}
            <span className="sm:hidden">
              {" "}
              · {formatTraffic(item.uploadBytes + item.downloadBytes)}
            </span>
          </div>
        </div>
        {latency && (
          <div className="flex shrink-0 justify-end sm:w-32">{latency}</div>
        )}
        <div className="hidden shrink-0 text-right font-mono text-xs text-default-500 sm:block">
          <div>↑ {formatTraffic(item.uploadBytes)}</div>
          <div>↓ {formatTraffic(item.downloadBytes)}</div>
        </div>
        <div
          className={`hidden shrink-0 gap-1 ${selectMode ? "" : "md:flex"}`}
          role="presentation"
          onClick={(e) => e.stopPropagation()}
        >
          <Button
            color="primary"
            size="sm"
            variant="flat"
            onPress={() => setSharing(item)}
          >
            分享
          </Button>
          <Button
            color="secondary"
            size="sm"
            variant="flat"
            onPress={() => setRelaying(item)}
          >
            中转
          </Button>
        </div>
        <div
          className={`shrink-0 ${selectMode ? "hidden" : ""}`}
          role="presentation"
          onClick={(e) => e.stopPropagation()}
        >
          <Switch
            isDisabled={toggling === item.id}
            isSelected={item.enabled}
            size="sm"
            onValueChange={() => handleToggle(item)}
          />
        </div>
      </div>
    );
  };

  if (loading) {
    return <PageLoadingState message="正在加载..." />;
  }

  const sheetItem = inbounds.find((i) => i.id === sheetFor) ?? null;
  const tabs: Array<{ key: PageTab; label: string; badge?: React.ReactNode }> =
    [
      { key: "inbounds", label: "入站", badge: inbounds.length || undefined },
      {
        key: "sources",
        label: "订阅源",
        badge: sources.some((s) => s.lastError) ? (
          <span className="text-danger">异常</span>
        ) : (
          sources.length || undefined
        ),
      },
    ];

  return (
    <AnimatedPage className="px-3 lg:px-6 py-6">
      <PageHeader
        description="客户端最终连上的节点：在自己的服务器上搭建，或导入现成的"
        title="节点"
      />
      <PageTabs tabs={tabs} value={tab} onChange={setTab} />

      {tab === "sources" && (
        <RemoteSourcesSection
          sources={sources}
          onChanged={() => loadData(false)}
        />
      )}

      {tab === "inbounds" && (
        <>
          {!selectMode && (
            <PageToolbar
              more={[
                {
                  key: "probe",
                  label: probing ? "检测中…" : "检测节点连通性",
                  disabled: probing || inbounds.length === 0,
                  onPress: runProbe,
                },
                {
                  key: "import",
                  label: "导入节点",
                  onPress: () => setImportOpen(true),
                },
                {
                  key: "batch",
                  label: "批量操作",
                  disabled: inbounds.length === 0,
                  onPress: () => setSelectMode(true),
                },
              ]}
              placeholder="搜索名称、服务器、协议、端口、标签"
              primary={{
                key: "create",
                label: "新建入站",
                disabled: kernels.length === 0,
                onPress: openCreate,
              }}
              search={searchKeyword}
              view={{ value: viewMode, onChange: setViewMode }}
              onSearch={setSearchKeyword}
            >
              {nodeOptions.length > 1 && (
                // appearance-none: the browser's own arrow ignored the
                // padding and stuck out of the pill.
                <div className="relative max-w-[10rem] shrink-0">
                  <select
                    aria-label="按服务器筛选"
                    className="h-9 w-full appearance-none truncate rounded-full bg-default-100 pl-3.5 pr-8 text-sm outline-none"
                    value={nodeFilter}
                    onChange={(e) => setNodeFilter(e.target.value)}
                  >
                    <option value="">全部服务器</option>
                    {nodeOptions.map(([id, name]) => (
                      <option key={id} value={id}>
                        {name}
                      </option>
                    ))}
                  </select>
                  <ChevronDown className="pointer-events-none absolute right-3 top-1/2 h-4 w-4 -translate-y-1/2 text-default-500" />
                </div>
              )}
              {allTags.length > 0 && (
                <div className="relative max-w-[8rem] shrink-0">
                  <select
                    aria-label="按标签筛选"
                    className="h-9 w-full appearance-none truncate rounded-full bg-default-100 pl-3.5 pr-8 text-sm outline-none"
                    value={activeTag}
                    onChange={(e) => setTagFilter(e.target.value)}
                  >
                    <option value="">全部标签</option>
                    {allTags.map((t) => (
                      <option key={t} value={t}>
                        {t}
                      </option>
                    ))}
                  </select>
                  <ChevronDown className="pointer-events-none absolute right-3 top-1/2 h-4 w-4 -translate-y-1/2 text-default-500" />
                </div>
              )}
            </PageToolbar>
          )}

          {selectMode && (
            <div className="sticky top-0 z-10 mb-3 flex flex-wrap items-center gap-2 rounded-xl border border-divider bg-background/95 px-3 py-2 shadow-sm backdrop-blur">
              <span className="text-sm text-default-600">
                已选 {selectedIds.size} 个
              </span>
              <div className="ml-auto flex flex-wrap justify-end gap-2">
                <Button
                  color="primary"
                  isDisabled={filtered.length === 0}
                  size="sm"
                  variant="flat"
                  onPress={toggleSelectAll}
                >
                  {allFilteredSelected ? "取消全选" : "全选"}
                </Button>
                <Button
                  color="success"
                  isDisabled={selectedIds.size === 0 || batchToggling}
                  size="sm"
                  variant="flat"
                  onPress={() => runBatchToggle(true)}
                >
                  启用
                </Button>
                <Button
                  color="warning"
                  isDisabled={selectedIds.size === 0 || batchToggling}
                  size="sm"
                  variant="flat"
                  onPress={() => runBatchToggle(false)}
                >
                  停用
                </Button>
                <Button
                  isDisabled={selectedIds.size === 0}
                  size="sm"
                  variant="flat"
                  onPress={() => {
                    const chosen = inbounds.filter((i) =>
                      selectedIds.has(i.id),
                    );
                    // Start from the tags they all share.
                    const shared = (chosen[0]?.tags ?? []).filter((t) =>
                      chosen.every((i) => (i.tags ?? []).includes(t)),
                    );

                    setTagTarget({
                      ids: chosen.map((i) => i.id),
                      tags: shared,
                      title: `已选 ${chosen.length} 个`,
                    });
                  }}
                >
                  标签
                </Button>
                <Button
                  color="danger"
                  isDisabled={selectedIds.size === 0}
                  size="sm"
                  variant="flat"
                  onPress={() => setBatchDeleteOpen(true)}
                >
                  删除
                </Button>
                <Button size="sm" variant="light" onPress={exitSelectMode}>
                  完成
                </Button>
              </div>
            </div>
          )}

          {filtered.length === 0 ? (
            <Card className="shadow-sm bg-default-50/50">
              <CardBody className="text-center py-16 flex flex-col items-center justify-center gap-3">
                <h3 className="text-lg font-medium">
                  {inbounds.length === 0 ? "暂无入站" : "没有匹配的入站"}
                </h3>
                <p className="text-default-500 text-sm max-w-sm">
                  {kernels.length === 0 && inbounds.length === 0
                    ? "先在服务器上安装内核，然后在这里创建 VLESS-Reality、SS2022 等入站；也可以点“导入”，导入别处搭好的 ss 节点用来对接中转。"
                    : inbounds.length === 0
                      ? "点击“新建入站”，选择一个模板即可快速创建。"
                      : "换个关键词、服务器或标签试试。"}
                </p>
                {kernels.length === 0 && inbounds.length === 0 && (
                  <Button
                    color="primary"
                    size="sm"
                    variant="flat"
                    onPress={() =>
                      navigate("/node", {
                        state: { tab: "kernels" } satisfies NodeHubState,
                      })
                    }
                  >
                    去安装内核
                  </Button>
                )}
              </CardBody>
            </Card>
          ) : !grouped ? (
            <StaggerList className={gridClass}>
              {filtered.map((i) => (
                <StaggerItem key={i.id}>{renderInbound(i)}</StaggerItem>
              ))}
            </StaggerList>
          ) : (
            <div className="space-y-1">
              {inboundGroups.map((g) => {
                const id = `inbound:node:${g.key}`;
                const folded = isCollapsed(id);
                const off = g.items.filter((i) => !i.enabled).length;

                return (
                  <div key={g.key}>
                    <GroupHeader
                      collapsed={folded}
                      count={g.items.length}
                      extra={
                        off > 0 ? (
                          <Chip size="sm" variant="flat">
                            {off} 个已停用
                          </Chip>
                        ) : undefined
                      }
                      title={g.title}
                      unit="个入站"
                      onToggle={() => toggleCollapsed(id)}
                    />
                    {!folded && (
                      <div className={`${gridClass} pb-3`}>
                        {g.items.map(renderInbound)}
                      </div>
                    )}
                  </div>
                );
              })}
            </div>
          )}
        </>
      )}

      <ActionSheet
        actions={
          sheetItem?.external
            ? [
                {
                  key: "share",
                  label: "分享",
                  color: "primary" as const,
                  onPress: () => setSharing(sheetItem),
                },
                {
                  key: "relay",
                  label: "中转",
                  color: "secondary" as const,
                  onPress: () => setRelaying(sheetItem),
                },
                {
                  key: "details",
                  label: "参数",
                  onPress: () => setDetails(sheetItem),
                },
                {
                  key: "edit",
                  label: "编辑",
                  onPress: () => setExternalEdit(sheetItem),
                },
                {
                  key: "tags",
                  label: "标签",
                  onPress: () => openTags(sheetItem),
                },
                {
                  key: "delete",
                  label: "删除",
                  color: "danger" as const,
                  onPress: () =>
                    setConfirm({ action: "delete", item: sheetItem }),
                },
              ]
            : sheetItem
              ? [
                  {
                    key: "share",
                    label: "分享",
                    color: "primary" as const,
                    onPress: () => setSharing(sheetItem),
                  },
                  {
                    key: "relay",
                    label: "中转",
                    color: "secondary" as const,
                    onPress: () => setRelaying(sheetItem),
                  },
                  ...(sheetItem.kernelType === "snell"
                    ? []
                    : [
                        {
                          key: "split",
                          label: "出站",
                          color: "success" as const,
                          onPress: () => setSplitting(sheetItem),
                        },
                      ]),
                  {
                    key: "details",
                    label: "参数",
                    onPress: () => setDetails(sheetItem),
                  },
                  {
                    key: "edit",
                    label: "编辑",
                    onPress: () => openEdit(sheetItem),
                  },
                  {
                    key: "tags",
                    label: "标签",
                    onPress: () => openTags(sheetItem),
                  },
                  {
                    key: "reset",
                    label: "重置流量",
                    onPress: () =>
                      setConfirm({ action: "reset", item: sheetItem }),
                  },
                  {
                    key: "regenerate",
                    label: "换密钥",
                    onPress: () =>
                      setConfirm({ action: "regenerate", item: sheetItem }),
                  },
                  {
                    key: "delete",
                    label: "删除",
                    color: "danger" as const,
                    onPress: () =>
                      setConfirm({ action: "delete", item: sheetItem }),
                  },
                ]
              : []
        }
        isOpen={Boolean(sheetItem)}
        subtitle={
          sheetItem &&
          `${sheetItem.external ? EXTERNAL_LABEL : sheetItem.nodeName} · ${
            sheetItem.listen === "::" || sheetItem.listen === "0.0.0.0"
              ? ""
              : `${sheetItem.listen}:`
          }${sheetItem.port}`
        }
        title={sheetItem?.name ?? ""}
        onClose={() => setSheetFor(null)}
      >
        {sheetItem && (
          <div className="space-y-2">
            <div className="flex flex-wrap gap-1.5">
              <Chip color="primary" size="sm" variant="flat">
                {describeInbound(sheetItem)}
              </Chip>
              <TagChips tags={sheetItem.tags} />
              <Chip size="sm" variant="flat">
                {sheetItem.external
                  ? "导入"
                  : (KERNEL_LABELS[sheetItem.kernelType] ??
                    sheetItem.kernelType)}
              </Chip>
              {sheetItem.relayOnly && (
                <Chip size="sm" variant="flat">
                  仅中转
                </Chip>
              )}
              {sheetItem.acceptProxyProtocol && (
                <Chip size="sm" variant="flat">
                  真实 IP
                </Chip>
              )}
              {sheetItem.relayWhitelist && (
                <Chip color="warning" size="sm" variant="flat">
                  白名单
                </Chip>
              )}
            </div>
            <div className="flex justify-between text-sm">
              <span className="text-default-500">流量</span>
              <span className="font-mono text-xs">
                ↑ {formatTraffic(sheetItem.uploadBytes)} · ↓{" "}
                {formatTraffic(sheetItem.downloadBytes)}
              </span>
            </div>
            {sheetItem.external && (
              <p className="text-xs text-default-500">
                没有流量统计；原服务器上的配置变了，请在“编辑”里同步
              </p>
            )}
            {sheetItem.remark && (
              <p className="text-xs text-default-500">{sheetItem.remark}</p>
            )}
          </div>
        )}
      </ActionSheet>

      <ShareModal item={sharing} onClose={() => setSharing(null)} />
      <SplitModal
        categories={caps?.splitCategories ?? []}
        inbounds={inbounds}
        item={splitting}
        onClose={() => setSplitting(null)}
        onSaved={() => loadData(false)}
      />
      <ImportModal
        isOpen={importOpen}
        ssMethods={caps?.ssMethods ?? []}
        tagSuggestions={allTags}
        onClose={() => setImportOpen(false)}
        onImported={() => loadData(false)}
        onSourceAdded={() => {
          setTab("sources");
          loadData(false);
        }}
      />
      <TagsModal
        suggestions={allTags}
        target={tagTarget}
        onClose={() => setTagTarget(null)}
        onSaved={() => loadData(false)}
      />
      <ExternalEditModal
        item={externalEdit}
        ssMethods={caps?.ssMethods ?? []}
        onClose={() => setExternalEdit(null)}
        onSaved={() => loadData(false)}
      />
      <RelayModal
        item={relaying}
        onChanged={() => loadData(false)}
        onClose={() => setRelaying(null)}
      />

      <InboundFormModal
        caps={caps}
        inbounds={inbounds}
        initial={formInitial}
        isOpen={formOpen}
        kernels={kernels}
        nodes={nodes}
        onInstallKernel={handleInstallFromForm}
        onOpenChange={setFormOpen}
        onSaved={() => loadData(false)}
      />

      <Modal
        backdrop="blur"
        classNames={{ base: modalBase }}
        isOpen={Boolean(details)}
        placement="center"
        scrollBehavior="inside"
        size="2xl"
        onOpenChange={(open) => !open && setDetails(null)}
      >
        <ModalContent>
          {() => (
            <>
              <ModalHeader closable className="flex flex-col gap-1">
                <h2 className="text-lg font-bold">{details?.name}</h2>
                <p className="text-small text-default-500">
                  手动配置客户端时使用；一般直接用“分享”或订阅导入即可
                </p>
              </ModalHeader>
              <ModalBody>
                <div className="divide-y divide-divider">
                  {details &&
                    detailRows(details).map(([k, v]) => (
                      <div
                        key={k}
                        className="flex items-center justify-between gap-3 py-2"
                      >
                        <span className="text-sm text-default-500 shrink-0">
                          {k}
                        </span>
                        <div className="flex items-center gap-1 min-w-0">
                          <span className="text-sm font-mono break-all text-right">
                            {v}
                          </span>
                          <Button
                            isIconOnly
                            aria-label={`复制${k}`}
                            size="sm"
                            variant="light"
                            onPress={() => copyText(v)}
                          >
                            <Copy className="w-3.5 h-3.5" />
                          </Button>
                        </div>
                      </div>
                    ))}
                </div>
              </ModalBody>
            </>
          )}
        </ModalContent>
      </Modal>

      <Modal
        backdrop="blur"
        classNames={{ base: modalBase }}
        isOpen={Boolean(confirm)}
        placement="center"
        onOpenChange={(open) => !open && setConfirm(null)}
      >
        <ModalContent>
          {(onClose) =>
            confirm && (
              <>
                <ModalHeader>
                  <h2
                    className={`text-lg font-bold ${
                      confirm.action === "delete" ? "text-danger" : ""
                    }`}
                  >
                    {CONFIRM_TEXT[confirm.action].title}
                  </h2>
                </ModalHeader>
                <ModalBody>
                  <p className="text-default-600">
                    入站{" "}
                    <span className="font-semibold text-foreground">
                      {confirm.item.name}
                    </span>
                  </p>
                  <p className="text-small text-default-500">
                    {confirm.item.external && confirm.action === "delete"
                      ? "只删除面板里的记录和相关中转，原服务器上的节点不受影响。"
                      : CONFIRM_TEXT[confirm.action].body}
                  </p>
                </ModalBody>
                <ModalFooter>
                  <Button variant="light" onPress={onClose}>
                    取消
                  </Button>
                  <Button
                    color={CONFIRM_TEXT[confirm.action].color}
                    isLoading={confirmLoading}
                    onPress={runConfirm}
                  >
                    {CONFIRM_TEXT[confirm.action].button}
                  </Button>
                </ModalFooter>
              </>
            )
          }
        </ModalContent>
      </Modal>

      <Modal
        backdrop="blur"
        classNames={{ base: modalBase }}
        isOpen={batchDeleteOpen}
        placement="center"
        onOpenChange={(open) => !open && setBatchDeleteOpen(false)}
      >
        <ModalContent>
          {(onClose) => {
            const chosen = inbounds.filter((i) => selectedIds.has(i.id));
            const panelOnly = chosen.every((i) => i.external);

            return (
              <>
                <ModalHeader>
                  <h2 className="text-lg font-bold text-danger">
                    删除 {chosen.length} 个入站
                  </h2>
                </ModalHeader>
                <ModalBody>
                  <p className="max-h-40 overflow-y-auto text-small text-default-600 break-all">
                    {chosen.map((i) => i.name).join("、")}
                  </p>
                  <p className="text-small text-default-500">
                    {panelOnly
                      ? "只删除面板里的记录和相关中转，原服务器上的节点不受影响。"
                      : "相关中转会一并删除；面板创建的入站会在服务器上立即停止，且无法恢复。"}
                  </p>
                </ModalBody>
                <ModalFooter>
                  <Button variant="light" onPress={onClose}>
                    取消
                  </Button>
                  <Button
                    color="danger"
                    isLoading={batchDeleting}
                    onPress={runBatchDelete}
                  >
                    删除
                  </Button>
                </ModalFooter>
              </>
            );
          }}
        </ModalContent>
      </Modal>
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
    </AnimatedPage>
  );
}
