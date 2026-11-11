import type {
  NodeGroupDraft,
  NodeGroupItem,
  RemoteSourceItem,
  SubPipeline,
  SubPreview,
  SubTemplate,
  SubTemplateSlot,
  SubPreviewItem,
  SubRuleGroup,
  SubscriptionDraft,
  SubscriptionItem,
} from "@/api";
import type { InboundApiItem } from "@/api/types";

import { useCallback, useEffect, useMemo, useState } from "react";
import toast from "react-hot-toast";
import {
  ArrowDown,
  ArrowUp,
  Copy,
  LayoutTemplate,
  QrCode,
  X,
} from "lucide-react";

import {
  deleteSubscription,
  getInboundList,
  getNodeGroups,
  orderNodeGroups,
  orderSubscriptions,
  getRemoteSources,
  getSubscriptions,
  getSubTemplates,
  previewSubscription,
  resetSubscription,
  resetSubscriptionTraffic,
  saveSubscription,
} from "@/api";
import { AnimatedPage } from "@/components/animated-page";
import {
  EmptyHint,
  PageHeader,
  PageTabs,
  PageToolbar,
  ToolbarButton,
} from "@/components/page-kit";
import { PageLoadingState } from "@/components/page-state";
import {
  markPageCached,
  pageCached,
  useCachedState,
} from "@/hooks/use-cached-state";
import { QrImage, copyText, panelOrigin } from "@/pages/inbound/share";
import { Button } from "@/shadcn-bridge/heroui/button";
import { Checkbox } from "@/shadcn-bridge/heroui/checkbox";
import { Chip } from "@/shadcn-bridge/heroui/chip";
import { Input } from "@/shadcn-bridge/heroui/input";
import { SubTrafficStats } from "@/components/lazy-charts";
import {
  Modal,
  ModalBody,
  ModalContent,
  ModalFooter,
  ModalHeader,
} from "@/shadcn-bridge/heroui/modal";
import { Switch } from "@/shadcn-bridge/heroui/switch";
import { formatTraffic } from "@/utils/traffic";
import { SubClientGrid } from "@/pages/subscription/client-grid";
import {
  TEMPLATE_SLOTS,
  TemplateGroups,
  TemplatesModal,
  slotLabel,
} from "@/pages/subscription/templates";
import {
  EditorTabs,
  QuickPickRow,
  Section,
  Segmented,
  ToggleRow,
} from "@/pages/subscription/controls";
import {
  NodeGroupEditor,
  NodeGroupList,
  groupDraftFrom,
  groupSourceText,
} from "@/pages/subscription/node-groups";

const modalBase = "!w-[calc(100%-32px)] !mx-auto sm:!w-full overflow-hidden";

const EMPTY_PIPELINE: SubPipeline = {
  lines: "",
  regions: [],
  protocols: [],
  include: "",
  exclude: "",
  rename: "",
  addFlag: false,
  prefix: "",
  dedupe: false,
  sort: "",
  regionGroups: false,
  chainLanding: "",
  chainRelay: "",
  rules: false,
  ruleGroups: [],
  chainLandingNames: [],
  chainRelayNames: [],
  chainLandingSources: [],
  chainRelaySources: [],
  chainLandingOwn: false,
  chainNoneNames: [],
  chainHideRelays: false,
  chainSourceFilters: {},
  chainDropUnused: false,
  script: "",
  templates: {},
  infoNodes: false,
};

function toDateInput(ms: number): string {
  if (!ms) return "";
  const d = new Date(ms);
  const pad = (n: number) => String(n).padStart(2, "0");

  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}`;
}

/** The end of the chosen local day, so a link stays valid through it. */
function fromDateInput(value: string): number {
  if (!value) return 0;
  const [y, m, d] = value.split("-").map(Number);

  return new Date(y, m - 1, d, 23, 59, 59).getTime();
}

function expiryLabel(ms: number): string {
  if (!ms) return "永不过期";
  const days = Math.ceil((ms - Date.now()) / 86400000);

  if (days <= 0) return `${toDateInput(ms)} 已过期`;

  return `${toDateInput(ms)} 到期 · 剩 ${days} 天`;
}

/** How much of its traffic limit a subscription has used. */
function UsageBar({ s, track }: { s: SubscriptionItem; track: string }) {
  const used = s.uploadBytes + s.downloadBytes;
  const pct = s.trafficLimit ? Math.min(100, (used / s.trafficLimit) * 100) : 0;
  const tone =
    pct >= 100 ? "bg-danger" : pct >= 90 ? "bg-warning" : "bg-foreground";

  return (
    <div className={`h-[3px] overflow-hidden rounded-full ${track}`}>
      <div
        className={`h-full rounded-full ${tone}`}
        style={{ width: `${Math.max(2, pct)}%` }}
      />
    </div>
  );
}

type EditorPage = "basic" | "nodes" | "rules" | "preview";

type ChainRole = "" | "landing" | "relay";

interface Draft extends SubscriptionDraft {
  expire: string;
  /** Traffic limit typed in GB ("" = unlimited). */
  limitGB: string;
}

const GB = 1024 ** 3;

function gbText(bytes: number): string {
  if (!bytes) return "";
  const v = bytes / GB;

  return String(Number.isInteger(v) ? v : Number(v.toFixed(2)));
}

function draftFrom(
  s?: SubscriptionItem,
  sources: RemoteSourceItem[] = [],
  groups: NodeGroupItem[] = [],
): Draft {
  if (!s) {
    const ids = (role: string) =>
      sources.filter((x) => x.role === role).map((x) => x.id);
    const relays = ids("relay");
    // A group with every node is the natural start; else the first one.
    const start = groups.find((g) => g.sourceAll) ?? groups[0];

    return {
      name: "",
      // New subscriptions get their own credentials, so any of them can
      // expire, be capped or cut off; older own-credential ones keep working.
      kind: "share",
      groupIds: start ? [start.id] : [],
      // Sources marked as relays (an airport in front of the own nodes)
      // set up the chain: they relay, the own nodes and landing sources
      // are the exits.
      options: {
        ...EMPTY_PIPELINE,
        chainRelaySources: relays,
        chainLandingSources: ids("landing"),
        chainLandingOwn: relays.length > 0,
        chainHideRelays: true,
      },
      expireTime: 0,
      expire: "",
      enabled: true,
      remark: "",
      trafficLimit: 0,
      limitGB: "",
      resetDay: 0,
    };
  }

  return {
    id: s.id,
    name: s.name,
    kind: s.kind,
    groupIds: s.groupIds ?? [],
    options: { ...EMPTY_PIPELINE, ...s.options },
    expireTime: s.expireTime,
    expire: toDateInput(s.expireTime),
    enabled: s.enabled,
    remark: s.remark,
    trafficLimit: s.trafficLimit,
    limitGB: gbText(s.trafficLimit),
    resetDay: s.resetDay,
  };
}

/** The API payload: the backend rejects unknown fields like `expire`. */
function toPayload(d: Draft): SubscriptionDraft {
  const { expire, limitGB, ...rest } = d;
  const gb = Number(limitGB);

  return {
    ...rest,
    kind: "share",
    expireTime: fromDateInput(expire),
    trafficLimit: gb > 0 ? Math.round(gb * GB) : 0,
    resetDay: d.resetDay,
  };
}

function Editor({
  draft,
  setDraft,
  groups,
  sources,
  ruleGroups,
  templates,
  saving,
  onSave,
  onClose,
}: {
  draft: Draft;
  setDraft: (d: Draft) => void;
  groups: NodeGroupItem[];
  sources: RemoteSourceItem[];
  ruleGroups: SubRuleGroup[];
  templates: SubTemplate[];
  saving: boolean;
  onSave: () => void;
  onClose: () => void;
}) {
  const [preview, setPreview] = useState<SubPreview | null>(null);
  const [previewKind, setPreviewKind] = useState<SubTemplateSlot | null>(null);
  // Nodes to pick the chain from; loaded when the chain page opens and kept
  // while roles are picked (picking changes options, which drops preview).
  const [chainNodes, setChainNodes] = useState<SubPreviewItem[] | null>(null);
  const [previewing, setPreviewing] = useState(false);
  const [page, setPage] = useState<EditorPage>("basic");
  const o = draft.options;
  const setOpts = (patch: Partial<SubPipeline>) => {
    setDraft({ ...draft, options: { ...o, ...patch } });
    setPreview(null);
  };
  const set = (patch: Partial<Draft>) => {
    setDraft({ ...draft, ...patch });
    setPreview(null);
  };

  const setGroupIds = (groupIds: number[]) => {
    set({ groupIds });
    // The chain picks among the groups' nodes.
    setChainNodes(null);
  };
  const groupById = useMemo(
    () => new Map(groups.map((g) => [g.id, g])),
    [groups],
  );
  const pickedGroups = draft.groupIds
    .map((id) => groupById.get(id))
    .filter((g): g is NodeGroupItem => Boolean(g));
  const moveGroup = (i: number, delta: -1 | 1) => {
    const ids = [...draft.groupIds];

    [ids[i], ids[i + delta]] = [ids[i + delta], ids[i]];
    setGroupIds(ids);
  };

  const runPreview = async () => {
    // The chain editor lists the nodes dropping would hide too, so they
    // can be picked back in.
    const forChain = page === "rules" && o.chainDropUnused;

    setPreviewing(true);
    try {
      const res = await previewSubscription(
        toPayload(
          forChain
            ? { ...draft, options: { ...o, chainDropUnused: false } }
            : draft,
        ),
      );

      if (res.code !== 0) {
        toast.error(res.msg || "预览失败");

        return;
      }
      if (!forChain) setPreview(res.data);
      if (page === "rules") setChainNodes(res.data.items);
    } finally {
      setPreviewing(false);
    }
  };

  // The preview, and the rules page (for the chain node list), load by
  // themselves when opened.
  useEffect(() => {
    if (page === "preview" && !preview && !previewing) void runPreview();
    if (page === "rules" && !chainNodes && !previewing) void runPreview();
  }, [page, preview, chainNodes]);

  const sourceFilter = (id: number) => o.chainSourceFilters?.[id] ?? "";
  // The role a node gets from its source (narrowed by the source's name
  // filter) or being self-built, before any per-node choice.
  const groupRole = (n: SubPreviewItem): ChainRole => {
    if (n.sourceId) {
      const match = nameMatcher(sourceFilter(n.sourceId));

      if (match && !match(n.original)) return "";
      if (o.chainLandingSources.includes(n.sourceId)) return "landing";
      if (o.chainRelaySources.includes(n.sourceId)) return "relay";
    }

    return n.own && o.chainLandingOwn ? "landing" : "";
  };
  const pickedRole = (original: string): ChainRole | null =>
    o.chainLandingNames.includes(original)
      ? "landing"
      : o.chainRelayNames.includes(original)
        ? "relay"
        : o.chainNoneNames.includes(original)
          ? ""
          : null;
  const chainRole = (n: SubPreviewItem): ChainRole =>
    pickedRole(n.original) ?? groupRole(n);
  // A choice equal to what the source gives follows the source again.
  const setChainRole = (n: SubPreviewItem, role: ChainRole) => {
    const without = (list: string[]) => list.filter((x) => x !== n.original);
    const own = role !== groupRole(n);

    setOpts({
      chainLandingNames:
        own && role === "landing"
          ? [...without(o.chainLandingNames), n.original]
          : without(o.chainLandingNames),
      chainRelayNames:
        own && role === "relay"
          ? [...without(o.chainRelayNames), n.original]
          : without(o.chainRelayNames),
      chainNoneNames:
        own && role === ""
          ? [...without(o.chainNoneNames), n.original]
          : without(o.chainNoneNames),
      // Picking replaces the older name expressions.
      chainLanding: "",
      chainRelay: "",
    });
  };
  const setSourceRole = (id: number, role: ChainRole) => {
    const without = (list: number[]) => list.filter((x) => x !== id);

    const filters = { ...o.chainSourceFilters };

    if (!role) delete filters[id];
    setOpts({
      chainLandingSources:
        role === "landing"
          ? [...without(o.chainLandingSources), id]
          : without(o.chainLandingSources),
      chainRelaySources:
        role === "relay"
          ? [...without(o.chainRelaySources), id]
          : without(o.chainRelaySources),
      chainSourceFilters: filters,
      chainLanding: "",
      chainRelay: "",
    });
  };
  // Sources with nodes in the picked groups, and any already in the chain.
  const chainSources = sources.filter(
    (src) =>
      o.chainLandingSources.includes(src.id) ||
      o.chainRelaySources.includes(src.id) ||
      chainNodes?.some((n) => n.sourceId === src.id),
  );
  const picked = o.templates ?? {};
  const templateOn = Object.values(picked).some(Boolean);
  const setTemplate = (kind: SubTemplateSlot, id: number) =>
    setOpts({ templates: { ...picked, [kind]: id || undefined } });
  const chainPicked =
    o.chainLandingNames.length > 0 ||
    o.chainRelayNames.length > 0 ||
    o.chainLandingSources.length > 0 ||
    o.chainRelaySources.length > 0 ||
    o.chainLandingOwn;
  const chainCount = (role: ChainRole) =>
    chainNodes?.filter((n) => chainRole(n) === role).length ?? 0;
  const chainOn = chainNodes
    ? chainCount("landing") > 0 && chainCount("relay") > 0
    : chainPicked
      ? (o.chainLandingNames.length > 0 ||
          o.chainLandingSources.length > 0 ||
          o.chainLandingOwn) &&
        (o.chainRelayNames.length > 0 || o.chainRelaySources.length > 0)
      : Boolean(o.chainLanding && o.chainRelay);

  return (
    <>
      <ModalHeader>
        <h2 className="text-lg font-bold">
          {draft.id ? "编辑订阅" : "新建订阅"}
        </h2>
      </ModalHeader>
      {/* A steady height, so switching pages doesn't resize the dialog. */}
      <ModalBody className="min-h-[min(65vh,34rem)] space-y-4">
        <div className="sticky top-0 z-10">
          <EditorTabs
            tabs={[
              { key: "basic", label: "基本" },
              {
                key: "nodes",
                label: "节点",
                badge: draft.groupIds.length || "",
              },
              {
                key: "rules",
                label: "规则",
                badge: o.rules || chainOn || templateOn ? "开" : "",
              },
              {
                key: "preview",
                label: "预览",
                badge: preview ? preview.items.length : "",
              },
            ]}
            value={page}
            onChange={setPage}
          />
        </div>

        {page === "basic" && (
          <div className="space-y-4">
            <Section title="基本">
              <Input
                label="名称"
                placeholder="例如 我的手机 / 小明"
                value={draft.name}
                variant="bordered"
                onChange={(e) => set({ name: e.target.value })}
              />
              <p className="text-xs text-default-500">
                有独立的 UUID /
                密码：停用、到期或流量用完后从服务器上删除，已导入的配置也连不上
              </p>
              <div className="space-y-1">
                <label className="text-sm font-medium" htmlFor="sub-expire">
                  到期日期
                </label>
                <QuickPickRow
                  picks={[30, 90, 365].map((d) => ({
                    label: d === 365 ? "1年" : `${d}天`,
                    onPick: () =>
                      set({ expire: toDateInput(Date.now() + d * 86400000) }),
                  }))}
                >
                  {/* appearance-none: iOS otherwise draws the date field
                      taller than a text field. */}
                  <input
                    className="block h-10 w-full appearance-none rounded-lg border border-divider bg-background px-2 text-left text-sm"
                    id="sub-expire"
                    type="date"
                    value={draft.expire}
                    onChange={(e) => set({ expire: e.target.value })}
                  />
                </QuickPickRow>
                <p className="text-xs text-default-500">
                  {draft.expire ? "到期当天 23:59 后断开" : "留空表示永不过期"}
                </p>
              </div>
              <div className="space-y-1">
                <label className="text-sm font-medium" htmlFor="sub-limit">
                  流量上限
                </label>
                <QuickPickRow
                  picks={[50, 100, 500].map((g) => ({
                    label: `${g}G`,
                    onPick: () => set({ limitGB: String(g) }),
                  }))}
                >
                  <input
                    className="block h-10 w-full rounded-lg border border-divider bg-background px-2 pr-9 text-sm"
                    id="sub-limit"
                    inputMode="decimal"
                    placeholder="不限"
                    value={draft.limitGB}
                    onChange={(e) =>
                      set({ limitGB: e.target.value.replace(/[^\d.]/g, "") })
                    }
                  />
                  <span className="pointer-events-none absolute right-2 top-1/2 -translate-y-1/2 text-xs text-default-500">
                    GB
                  </span>
                </QuickPickRow>
                <div className="flex items-center justify-between gap-2 pt-1">
                  <label className="text-sm" htmlFor="sub-reset-day">
                    每月重置
                  </label>
                  <select
                    className="h-10 rounded-lg border border-divider bg-background px-2 text-sm"
                    id="sub-reset-day"
                    value={draft.resetDay}
                    onChange={(e) => set({ resetDay: Number(e.target.value) })}
                  >
                    <option value={0}>不重置</option>
                    {Array.from({ length: 28 }, (_, i) => i + 1).map((d) => (
                      <option key={d} value={d}>
                        每月 {d} 号
                      </option>
                    ))}
                  </select>
                </div>
                <p className="text-xs text-default-500">
                  上传 +
                  下载合计，用完自动断开；在服务器上按用户统计，仅支持多用户协议
                </p>
              </div>
            </Section>
            <Input
              label="备注"
              placeholder="可选"
              value={draft.remark}
              variant="bordered"
              onChange={(e) => set({ remark: e.target.value })}
            />
            <ToggleRow
              hint="在节点列表最前面加两个占位节点，名字显示到期日期和剩余流量，给不显示订阅信息的客户端看；它们连不上，不要选。停用、到期或流量用完后，订阅只返回一个写明原因的占位节点（不受这个开关影响）"
              id="sub-info-nodes"
              label="在节点列表显示到期和流量"
              value={!!o.infoNodes}
              onChange={(infoNodes) => setOpts({ infoNodes })}
            />
          </div>
        )}

        {page === "nodes" && (
          <div className="space-y-4">
            <Section
              hint="订阅按顺序列出所选节点组的节点；筛选、改名在「节点组」里设置，多个订阅可以共用"
              title="节点组"
            >
              {pickedGroups.length > 0 && (
                <div className="divide-y divide-divider rounded-lg border border-divider">
                  {pickedGroups.map((g, i) => (
                    <div
                      key={g.id}
                      className="flex items-center gap-2 px-3 py-2"
                    >
                      <span className="w-5 shrink-0 text-xs text-default-400">
                        {i + 1}
                      </span>
                      <div className="min-w-0 flex-1">
                        <div className="truncate text-sm">{g.name}</div>
                        <div className="truncate text-xs text-default-400">
                          {g.nodeCount} 个节点 · {groupSourceText(g)}
                        </div>
                      </div>
                      <Button
                        isIconOnly
                        aria-label={`上移 ${g.name}`}
                        isDisabled={i === 0}
                        size="sm"
                        variant="light"
                        onPress={() => moveGroup(i, -1)}
                      >
                        <ArrowUp className="h-4 w-4" />
                      </Button>
                      <Button
                        isIconOnly
                        aria-label={`下移 ${g.name}`}
                        isDisabled={i === pickedGroups.length - 1}
                        size="sm"
                        variant="light"
                        onPress={() => moveGroup(i, 1)}
                      >
                        <ArrowDown className="h-4 w-4" />
                      </Button>
                      <Button
                        isIconOnly
                        aria-label={`移除 ${g.name}`}
                        size="sm"
                        variant="light"
                        onPress={() =>
                          setGroupIds(draft.groupIds.filter((x) => x !== g.id))
                        }
                      >
                        <X className="h-4 w-4" />
                      </Button>
                    </div>
                  ))}
                </div>
              )}
              {groups.length === 0 ? (
                <p className="text-xs text-default-500">
                  还没有节点组，先在订阅页上方切到「节点组」新建一个
                </p>
              ) : (
                groups.some((g) => !draft.groupIds.includes(g.id)) && (
                  <div className="space-y-1">
                    <p className="text-xs text-default-500">
                      {pickedGroups.length ? "再加入" : "选择节点组"}
                    </p>
                    {groups
                      .filter((g) => !draft.groupIds.includes(g.id))
                      .map((g) => (
                        <Checkbox
                          key={g.id}
                          className="w-full"
                          isSelected={false}
                          onValueChange={() =>
                            setGroupIds([...draft.groupIds, g.id])
                          }
                        >
                          <span className="text-sm">{g.name}</span>
                          <span className="ml-1 text-xs text-default-400">
                            {g.nodeCount} 个节点 · {groupSourceText(g)}
                          </span>
                        </Checkbox>
                      ))}
                  </div>
                )
              )}
              {pickedGroups.length > 1 && (
                <p className="text-xs text-default-500">
                  几个组里有同一个节点时它会出现多次，可以在节点组里用筛选区分
                </p>
              )}
            </Section>
          </div>
        )}

        {page === "rules" && (
          <div className="space-y-4">
            <Section
              hint="用自己的 Mihomo / sing-box / Surge 配置当骨架：规则、策略组、DNS 按模板来，面板只放节点进去；没选模板的客户端用下面的内置规则"
              title="订阅模板"
            >
              {templates.length === 0 ? (
                <p className="text-xs text-default-500">
                  还没有模板，在订阅页右上角「模板」里添加
                </p>
              ) : (
                <div className="space-y-2">
                  {TEMPLATE_SLOTS.map(([kind, label, format]) => {
                    const options = templates.filter((t) => t.kind === format);

                    return (
                      <div
                        key={kind}
                        className="grid grid-cols-[7.5rem_1fr] items-center gap-2"
                      >
                        <label className="text-sm" htmlFor={`sub-tpl-${kind}`}>
                          {label}
                        </label>
                        <select
                          className="block h-9 w-full rounded-lg border border-divider bg-background px-2 text-sm disabled:opacity-50"
                          disabled={options.length === 0}
                          id={`sub-tpl-${kind}`}
                          value={picked[kind] ?? 0}
                          onChange={(e) =>
                            setTemplate(kind, Number(e.target.value))
                          }
                        >
                          <option value={0}>
                            {options.length === 0
                              ? "没有这种格式的模板"
                              : "不使用模板（内置规则）"}
                          </option>
                          {options.map((t) => (
                            <option key={t.id} value={t.id}>
                              {t.name}
                            </option>
                          ))}
                        </select>
                      </div>
                    );
                  })}
                  <p className="text-xs text-default-500">
                    Stash、Shadowrocket 用 Mihomo 格式的模板（可以和 Mihomo
                    共用），面板会把 include-all / filter
                    展开成节点列表；Loon、Quantumult X
                    暂不支持模板，照旧用内置规则
                  </p>
                </div>
              )}
            </Section>
            <Section
              hint={`给 mihomo、sing-box（1.12 及以上）、Surge、Shadowrocket、Loon、Quantumult X 加上分流规则；后三个会改为下发完整配置。Stash 和通用链接只有节点${
                templateOn
                  ? `。${TEMPLATE_SLOTS.filter(([k]) => picked[k])
                      .map(([, l]) => l)
                      .join("、")} 已用模板，这里的设置对它们不生效`
                  : ""
              }`}
              title={templateOn ? "内置分组与规则" : "分组与规则"}
            >
              <ToggleRow
                hint="国内直连、境外走代理，另加下面勾选的分组"
                id="sub-rules"
                label="开启分流规则"
                value={o.rules}
                onChange={(rules) =>
                  setOpts(
                    rules && !o.rules && o.ruleGroups.length === 0
                      ? { rules, ruleGroups: ruleGroups.map((g) => g.key) }
                      : { rules },
                  )
                }
              />
              <ToggleRow
                disabled={o.rules}
                hint={
                  o.rules
                    ? "开启分流后已自动包含，无需单独打开"
                    : "mihomo / sing-box / Surge 里每个地区一个自动测速组"
                }
                id="sub-groups"
                label="按地区分组"
                value={o.rules || o.regionGroups}
                onChange={(regionGroups) => setOpts({ regionGroups })}
              />
              {o.rules && (
                <div className="space-y-1.5">
                  <p className="text-xs font-medium">分组</p>
                  <p className="text-xs text-default-500">
                    每组可在客户端里单独选节点：AI
                    服务默认美国，微软、苹果、游戏默认直连，广告拦截默认拦截
                  </p>
                  <div className="grid grid-cols-2 gap-2 sm:grid-cols-3">
                    {ruleGroups.map((g) => (
                      <Checkbox
                        key={g.key}
                        isSelected={o.ruleGroups.includes(g.key)}
                        onValueChange={(on) =>
                          setOpts({
                            ruleGroups: on
                              ? [...o.ruleGroups, g.key]
                              : o.ruleGroups.filter((x) => x !== g.key),
                          })
                        }
                      >
                        <span className="text-sm">{g.name}</span>
                      </Checkbox>
                    ))}
                  </div>
                </div>
              )}
            </Section>
            <Section
              collapsible
              defaultOpen={false}
              summary={
                !chainOn
                  ? "未开启"
                  : chainNodes
                    ? `已开启 · 落地 ${chainCount("landing")} 个 · 中转 ${chainCount("relay")} 个`
                    : "已开启"
              }
              title="链式代理"
            >
              <div className="border-l-2 border-divider py-1 pl-3">
                <div className="flex items-center gap-2">
                  <span
                    className={`text-xs font-medium ${chainOn ? "text-success" : "text-default-500"}`}
                  >
                    {chainOn ? "已开启" : "未开启"}
                  </span>
                </div>
                <div className="mt-3 flex flex-wrap items-center gap-2 text-xs">
                  {["客户端", "中转节点", "落地节点", "网站"].map((s, i) => (
                    <span key={s} className="flex items-center gap-2">
                      {i > 0 && <span className="text-default-400">→</span>}
                      <span
                        className={
                          s === "中转节点" || s === "落地节点"
                            ? "font-medium text-foreground"
                            : "text-default-500"
                        }
                      >
                        {s}
                      </span>
                    </span>
                  ))}
                </div>
                <p className="mt-3 text-xs leading-relaxed text-default-500">
                  网站看到的是落地节点的
                  IP。客户端里会多出「落地节点」「中转节点」两个组，分别选出口和中转。落地和中转都至少选一个才生效；通用分享链接（Base64）无法表达链式
                </p>
              </div>

              {o.chainLanding && o.chainRelay && (
                <p className="rounded-lg bg-warning-50 px-3 py-2 text-xs text-warning-700 dark:bg-warning-500/10">
                  现在用的是旧的关键词方式（落地：{o.chainLanding}，中转：
                  {o.chainRelay}）。在下面勾选节点后会改用勾选的节点
                </p>
              )}

              <div className="space-y-3">
                <p className="text-xs font-medium">按来源</p>
                <ToggleRow
                  hint="自己面板上的节点（含它们的中转线路）都作为出口"
                  id="chain-landing-own"
                  label="自建节点都作为落地"
                  value={o.chainLandingOwn}
                  onChange={(chainLandingOwn) => {
                    setOpts({
                      chainLandingOwn,
                      chainLanding: "",
                      chainRelay: "",
                    });
                  }}
                />
                {chainSources.length > 0 && (
                  <div className="divide-y divide-divider rounded-lg border border-divider">
                    {chainSources.map((src) => {
                      const role: ChainRole = o.chainLandingSources.includes(
                        src.id,
                      )
                        ? "landing"
                        : o.chainRelaySources.includes(src.id)
                          ? "relay"
                          : "";

                      const filter = sourceFilter(src.id);
                      const ofSource =
                        chainNodes?.filter((n) => n.sourceId === src.id) ?? [];
                      const match = nameMatcher(filter);
                      const matched = match
                        ? ofSource.filter((n) => match(n.original)).length
                        : ofSource.length;

                      return (
                        <div key={src.id} className="space-y-2 px-3 py-2">
                          <div className="flex items-center gap-3">
                            <div className="min-w-0 flex-1">
                              <div className="truncate text-sm">{src.name}</div>
                              <div className="truncate text-xs text-default-400">
                                {role && filter && chainNodes
                                  ? `匹配 ${matched} / ${ofSource.length} 个节点`
                                  : `${src.nodeCount} 个节点`}
                              </div>
                            </div>
                            <ChainRoleButtons
                              value={role}
                              onChange={(r) => setSourceRole(src.id, r)}
                            />
                          </div>
                          {role && (
                            <Input
                              aria-label={`${src.name} 名称筛选`}
                              placeholder="名称筛选，如 香港|日本；留空用全部"
                              size="sm"
                              value={filter}
                              variant="bordered"
                              onChange={(e) =>
                                setOpts({
                                  chainSourceFilters: {
                                    ...o.chainSourceFilters,
                                    [src.id]: e.target.value,
                                  },
                                })
                              }
                            />
                          )}
                        </div>
                      );
                    })}
                  </div>
                )}
                <ToggleRow
                  hint="中转节点只出现在「中转节点」组里，不混进节点选择、自动选择和地区组"
                  id="chain-hide-relays"
                  label="中转节点不单独列出"
                  value={o.chainHideRelays}
                  onChange={(chainHideRelays) => setOpts({ chainHideRelays })}
                />
                {(o.chainLandingSources.length > 0 ||
                  o.chainRelaySources.length > 0) && (
                  <ToggleRow
                    hint="链式代理里的订阅源，没筛选上（或单独设成不用）的节点不放进订阅；不影响其他来源"
                    id="chain-drop-unused"
                    label="没用上的节点不放进订阅"
                    value={o.chainDropUnused}
                    onChange={(chainDropUnused) => setOpts({ chainDropUnused })}
                  />
                )}
              </div>

              <div className="space-y-2">
                <p className="text-xs font-medium">按节点</p>
                <p className="text-xs text-default-500">
                  单独调整某个节点，优先于来源设置；用的是处理前的原名，改名不影响
                </p>
                {!chainNodes ? (
                  <p className="py-6 text-center text-xs text-default-500">
                    正在加载节点…
                  </p>
                ) : chainNodes.length === 0 ? (
                  <p className="py-6 text-center text-xs text-default-500">
                    所选节点组里还没有节点
                  </p>
                ) : (
                  <div className="max-h-80 divide-y divide-divider overflow-y-auto rounded-lg border border-divider">
                    {chainNodes.map((n, i) => {
                      const role = chainRole(n);
                      const overridden = pickedRole(n.original) !== null;

                      return (
                        <div
                          key={`${n.original}-${i}`}
                          className="flex items-center gap-3 px-3 py-2"
                        >
                          <div className="min-w-0 flex-1">
                            <div className="truncate text-sm">{n.original}</div>
                            <div className="truncate text-xs text-default-400">
                              {[
                                overridden && "单独设置",
                                n.region,
                                n.protocol,
                                n.line === "relay" ? "中转线路" : "直连",
                              ]
                                .filter(Boolean)
                                .join(" · ")}
                            </div>
                          </div>
                          <ChainRoleButtons
                            value={role}
                            onChange={(r) => setChainRole(n, r)}
                          />
                        </div>
                      );
                    })}
                  </div>
                )}
              </div>
            </Section>
          </div>
        )}

        {page === "preview" && (
          <div className="space-y-4">
            <Section title="预览">
              {previewing && !preview && (
                <p className="py-6 text-center text-xs text-default-500">
                  正在生成…
                </p>
              )}
              {preview && (
                <>
                  <p className="text-xs text-default-500">
                    共 {preview.items.length} 个节点
                    {preview.dropped > 0 && `，过滤掉 ${preview.dropped} 个`}
                    <button
                      className="ml-2 text-primary"
                      type="button"
                      onClick={runPreview}
                    >
                      刷新
                    </button>
                  </p>
                  {preview.inactiveRelays &&
                    preview.inactiveRelays.length > 0 && (
                      <p className="text-xs text-warning">
                        另有 {preview.inactiveRelays.length}{" "}
                        条中转未包含在订阅中：
                        {preview.inactiveRelays
                          .map((r) => `${r.name}（${r.reason}）`)
                          .join("、")}
                        。在中转页恢复转发后会自动加入
                      </p>
                    )}
                  <div className="max-h-96 overflow-y-auto rounded-lg border border-divider">
                    {preview.items.length === 0 ? (
                      <p className="p-3 text-xs text-default-500">没有节点</p>
                    ) : (
                      preview.items.map((p, i) => (
                        <div
                          key={`${p.original}-${i}`}
                          className="flex items-center gap-2 border-b border-divider px-3 py-1.5 text-xs last:border-b-0"
                        >
                          <span className="min-w-0 flex-1 truncate">
                            {p.name}
                            {p.name !== p.original && (
                              <span className="text-default-400">
                                {" "}
                                ← {p.original}
                              </span>
                            )}
                          </span>
                          {p.chain && (
                            <span
                              className={`shrink-0 rounded-full px-1.5 py-0.5 ${p.chain === "landing" ? "bg-warning-100 text-warning-700 dark:bg-warning-500/20" : "bg-secondary-100 text-secondary-700 dark:bg-secondary-500/20"}`}
                            >
                              {p.chain === "landing" ? "落地" : "中转"}
                            </span>
                          )}
                          <span className="shrink-0 text-default-500">
                            {p.line === "relay" ? "中转" : "直连"}
                          </span>
                        </div>
                      ))
                    )}
                  </div>
                </>
              )}
            </Section>
            {preview?.templates &&
              Object.keys(preview.templates).length > 0 &&
              (() => {
                const kinds = TEMPLATE_SLOTS.filter(
                  ([k]) => preview.templates?.[k],
                ).map(([k, l]) => [k, l] as [SubTemplateSlot, string]);
                const kind =
                  previewKind && preview.templates[previewKind]
                    ? previewKind
                    : kinds[0][0];
                const view = preview.templates[kind]!;

                return (
                  <Section
                    hint="按模板里的写法，每个策略组会收到哪些节点；空组在客户端里没有节点可选"
                    title="模板策略组"
                  >
                    {kinds.length > 1 && (
                      <Segmented
                        options={kinds}
                        value={kind}
                        onChange={setPreviewKind}
                      />
                    )}
                    <p className="text-xs text-default-500">
                      {slotLabel(kind)} · 「{view.name}」
                    </p>
                    {view.error ? (
                      <p className="rounded-lg bg-danger-50 px-3 py-2 text-xs text-danger dark:bg-danger-500/10">
                        模板出错：{view.error}
                      </p>
                    ) : (
                      <TemplateGroups
                        groups={view.groups ?? []}
                        warnings={view.warnings}
                      />
                    )}
                  </Section>
                );
              })()}
          </div>
        )}
      </ModalBody>
      <ModalFooter>
        <Button variant="light" onPress={onClose}>
          取消
        </Button>
        <Button color="primary" isLoading={saving} onPress={onSave}>
          保存
        </Button>
      </ModalFooter>
    </>
  );
}

const PAGE = "subscription";

export default function SubscriptionPage() {
  // A revisit shows the last data at once and reloads in the background.
  const [loading, setLoading] = useState(() => !pageCached(PAGE));
  const [subs, setSubs] = useCachedState<SubscriptionItem[]>(PAGE, "subs", []);
  const [regions, setRegions] = useCachedState<string[]>(PAGE, "regions", []);
  const [ruleGroups, setRuleGroups] = useCachedState<SubRuleGroup[]>(
    PAGE,
    "ruleGroups",
    [],
  );
  const [inbounds, setInbounds] = useCachedState<InboundApiItem[]>(
    PAGE,
    "inbounds",
    [],
  );
  const [sources, setSources] = useCachedState<RemoteSourceItem[]>(
    PAGE,
    "sources",
    [],
  );
  const [groups, setGroups] = useCachedState<NodeGroupItem[]>(
    PAGE,
    "groups",
    [],
  );
  const [groupDraft, setGroupDraft] = useState<NodeGroupDraft | null>(null);
  const [tab, setTab] = useState<"subs" | "groups">("subs");
  const [query, setQuery] = useState("");
  const [draft, setDraft] = useState<Draft | null>(null);
  const [saving, setSaving] = useState(false);
  const [detail, setDetail] = useState<SubscriptionItem | null>(null);
  const [detailTab, setDetailTab] = useState<"import" | "traffic">("import");
  // The subscription whose QR code is open in the detail dialog.
  const [qrFor, setQrFor] = useState<number | null>(null);

  useEffect(() => {
    if (!detail) setDetailTab("import");
  }, [detail]);
  const [confirm, setConfirm] = useState<"reset" | "delete" | null>(null);
  const [toggling, setToggling] = useState<number | null>(null);
  const [resettingTraffic, setResettingTraffic] = useState(false);
  const [templates, setTemplates] = useCachedState<SubTemplate[]>(
    PAGE,
    "templates",
    [],
  );
  const [templatesOpen, setTemplatesOpen] = useState(false);
  const loadTemplates = useCallback(async () => {
    try {
      const res = await getSubTemplates();

      if (res.code === 0) setTemplates(res.data ?? []);
    } catch {}
  }, []);
  const load = useCallback(async () => {
    try {
      const [subRes, inRes, srcRes, groupRes] = await Promise.all([
        getSubscriptions(),
        getInboundList(),
        getRemoteSources(),
        getNodeGroups(),
      ]);

      if (srcRes.code === 0) setSources(srcRes.data ?? []);
      if (groupRes.code === 0) setGroups(groupRes.data?.items ?? []);

      if (subRes.code === 0 && subRes.data) {
        setSubs(subRes.data.items ?? []);
        setRegions(subRes.data.regions ?? []);
        setRuleGroups(subRes.data.ruleGroups ?? []);
        markPageCached(PAGE);
      } else {
        toast.error(subRes.msg || "加载订阅失败");
      }
      if (inRes.code === 0) setInbounds(inRes.data ?? []);
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    load();
    void loadTemplates();
  }, [load, loadTemplates]);

  const url = (s: SubscriptionItem, format = "") =>
    `${panelOrigin()}${s.path}${format ? `?format=${format}` : ""}`;

  const showResult = (msg: string | undefined, ok: string) => {
    if (msg && msg !== "操作成功") toast(msg, { icon: "⚠️" });
    else toast.success(ok);
  };

  const save = async () => {
    if (!draft) return;
    setSaving(true);
    try {
      const res = await saveSubscription(toPayload(draft));

      if (res.code !== 0) {
        toast.error(res.msg || "保存失败");

        return;
      }
      showResult(
        res.msg,
        draft.id ? "已保存" : "已创建，复制链接导入客户端即可",
      );
      setDraft(null);
      await load();
    } finally {
      setSaving(false);
    }
  };

  const toggle = async (s: SubscriptionItem) => {
    setToggling(s.id);
    try {
      const res = await saveSubscription(
        toPayload({ ...draftFrom(s), enabled: !s.enabled }),
      );

      if (res.code !== 0) toast.error(res.msg || "操作失败");
      else showResult(res.msg, s.enabled ? "已停用" : "已启用");
      await load();
    } finally {
      setToggling(null);
    }
  };

  /** Swaps two groups by id, so it works on a search-filtered list too. */
  const moveGroup = async (a: number, b: number) => {
    const next = [...groups];
    const i = next.findIndex((g) => g.id === a);
    const j = next.findIndex((g) => g.id === b);

    if (i < 0 || j < 0) return;
    [next[i], next[j]] = [next[j], next[i]];
    setGroups(next);
    const res = await orderNodeGroups(next.map((g) => g.id));

    if (res.code !== 0) {
      toast.error(res.msg || "排序失败");
      await load();
    }
  };

  /** Swaps two subscriptions by id, so it works on a search-filtered list too. */
  const moveSub = async (a: number, b: number) => {
    const next = [...subs];
    const i = next.findIndex((s) => s.id === a);
    const j = next.findIndex((s) => s.id === b);

    if (i < 0 || j < 0) return;
    [next[i], next[j]] = [next[j], next[i]];
    setSubs(next);
    const res = await orderSubscriptions(next.map((s) => s.id));

    if (res.code !== 0) {
      toast.error(res.msg || "排序失败");
      await load();
    }
  };

  const resetTraffic = async () => {
    if (!detail) return;
    setResettingTraffic(true);
    try {
      const res = await resetSubscriptionTraffic(detail.id);

      if (res.code !== 0 || !res.data) {
        toast.error(res.msg || "操作失败");

        return;
      }
      toast.success(detail.overQuota ? "流量已重置，已恢复连接" : "流量已重置");
      setDetail(res.data);
      await load();
    } finally {
      setResettingTraffic(false);
    }
  };

  const runConfirm = async () => {
    if (!detail || !confirm) return;
    setSaving(true);
    try {
      const res =
        confirm === "reset"
          ? await resetSubscription(detail.id)
          : await deleteSubscription(detail.id);

      if (res.code !== 0) {
        toast.error(res.msg || "操作失败");

        return;
      }
      showResult(
        res.msg,
        confirm === "reset" ? "已重置，旧链接立即失效" : "已删除",
      );
      setConfirm(null);
      setDetail(null);
      await load();
    } finally {
      setSaving(false);
    }
  };

  if (loading) return <PageLoadingState message="正在加载..." />;

  const groupName = new Map(groups.map((g) => [g.id, g.name]));
  const q = query.trim().toLowerCase();
  const shownSubs = q
    ? subs.filter((s) => `${s.name} ${s.remark}`.toLowerCase().includes(q))
    : subs;
  const shownGroups = q
    ? groups.filter((g) => g.name.toLowerCase().includes(q))
    : groups;
  const sourceText = (s: SubscriptionItem) =>
    (s.groupIds ?? [])
      .map((id) => groupName.get(id))
      .filter(Boolean)
      .join("、") || "未选节点组";
  const stateChip = (s: SubscriptionItem) =>
    !s.enabled ? (
      <Chip size="sm" variant="flat">
        已停用
      </Chip>
    ) : (
      !s.active && (
        <Chip color="danger" size="sm" variant="flat">
          {s.overQuota ? "流量用完" : "已过期"}
        </Chip>
      )
    );
  const actions = (s: SubscriptionItem) => (
    <>
      <Button
        isIconOnly
        aria-label={`复制 ${s.name} 的订阅链接`}
        className="text-default-500"
        size="sm"
        title="复制订阅链接"
        variant="light"
        onPress={() => copyText(url(s), "订阅链接已复制")}
      >
        <Copy className="h-4 w-4" />
      </Button>
      {/* Off: the link stops working and its nodes are cut off. */}
      <span
        className="flex"
        title={s.enabled ? "已启用，点击停用订阅" : "已停用，点击启用订阅"}
      >
        <Switch
          aria-label={`${s.enabled ? "停用" : "启用"} ${s.name}`}
          isDisabled={toggling === s.id}
          isSelected={s.enabled}
          size="sm"
          onValueChange={() => toggle(s)}
        />
      </span>
    </>
  );
  const row = (s: SubscriptionItem, i: number) => (
    <div
      key={s.id}
      className="list-row group flex cursor-pointer items-center gap-3 border-b border-divider px-2 py-3 transition-colors hover:bg-default-50"
      role="button"
      tabIndex={0}
      onClick={() => setDetail(s)}
      onKeyDown={(e) => {
        if (e.target !== e.currentTarget) return;
        if (e.key === "Enter" || e.key === " ") {
          e.preventDefault();
          setDetail(s);
        }
      }}
    >
      <span
        className={`h-1.5 w-1.5 shrink-0 rounded-full ${s.active ? "bg-success" : s.enabled ? "bg-danger" : "bg-default-300"}`}
      />
      <div className="min-w-0 flex-1">
        <div className="flex items-center gap-1.5">
          <span className="truncate">{s.name}</span>
          {stateChip(s)}
        </div>
        <div className="truncate text-xs text-default-400">
          {s.nodeCount} 个节点 · {sourceText(s)}
          {s.expireTime ? ` · ${expiryLabel(s.expireTime)}` : ""}
          {s.remark ? ` · ${s.remark}` : ""}
        </div>
        {s.trafficLimit > 0 && (
          <div className="mt-1.5 max-w-xs">
            <UsageBar s={s} track="bg-default-100" />
          </div>
        )}
      </div>
      <div className="hidden w-40 shrink-0 text-right text-sm tabular-nums sm:block">
        {formatTraffic(s.uploadBytes + s.downloadBytes)}
        <span className="text-xs text-default-400">
          {s.trafficLimit ? ` / ${formatTraffic(s.trafficLimit)}` : ""}
        </span>
      </div>
      <div
        className="flex shrink-0 items-center gap-2"
        role="presentation"
        onClick={(e) => e.stopPropagation()}
      >
        <span className="flex items-center transition-opacity sm:opacity-0 sm:group-hover:opacity-100">
          <Button
            isIconOnly
            aria-label={`上移 ${s.name}`}
            className="text-default-500"
            isDisabled={i === 0}
            size="sm"
            title="上移"
            variant="light"
            onPress={() => void moveSub(s.id, shownSubs[i - 1].id)}
          >
            <ArrowUp className="h-4 w-4" />
          </Button>
          <Button
            isIconOnly
            aria-label={`下移 ${s.name}`}
            className="text-default-500"
            isDisabled={i === shownSubs.length - 1}
            size="sm"
            title="下移"
            variant="light"
            onPress={() => void moveSub(s.id, shownSubs[i + 1].id)}
          >
            <ArrowDown className="h-4 w-4" />
          </Button>
        </span>
        {actions(s)}
      </div>
    </div>
  );

  return (
    <AnimatedPage className="px-4 py-6 lg:px-10 lg:py-8">
      <PageHeader
        description={
          tab === "subs"
            ? "每个订阅一个链接：选节点组、分流和链式代理，客户端自动识别格式"
            : "节点组：从自建、导入的节点和订阅源里挑选，筛选、改名后给订阅用，多个订阅可以共用"
        }
        title="订阅"
      />

      <TemplatesModal
        isOpen={templatesOpen}
        subscriptions={subs}
        templates={templates}
        onChanged={loadTemplates}
        onClose={() => setTemplatesOpen(false)}
      />

      <PageTabs
        tabs={[
          { key: "subs", label: "订阅", badge: subs.length },
          { key: "groups", label: "节点组", badge: groups.length },
        ]}
        value={tab}
        onChange={setTab}
      />
      <PageToolbar
        placeholder={tab === "subs" ? "搜索订阅名称或备注" : "搜索节点组名称"}
        primary={
          tab === "subs"
            ? {
                key: "create",
                label: "新建订阅",
                onPress: () => setDraft(draftFrom(undefined, sources, groups)),
              }
            : {
                key: "create-group",
                label: "新建节点组",
                onPress: () => setGroupDraft(groupDraftFrom()),
              }
        }
        search={query}
        onSearch={setQuery}
      >
        <ToolbarButton onPress={() => setTemplatesOpen(true)}>
          <LayoutTemplate className="h-3.5 w-3.5" />
          模板
          {templates.length > 0 && (
            <span className="tabular-nums text-default-400">
              {templates.length}
            </span>
          )}
        </ToolbarButton>
      </PageToolbar>

      {tab === "groups" ? (
        <NodeGroupList
          groups={shownGroups}
          onEdit={(g) => setGroupDraft(groupDraftFrom(g))}
          onMove={(i, d) =>
            void moveGroup(shownGroups[i].id, shownGroups[i + d].id)
          }
        />
      ) : subs.length === 0 ? (
        <EmptyHint
          text={
            inbounds.length === 0
              ? "还没有节点。先在“节点”里搭建或导入，订阅里才会有内容"
              : "每个订阅一个链接，有独立凭据：可以设到期和流量上限，停用后自动断开"
          }
        />
      ) : (
        <>
          <div className="flex flex-col">
            {shownSubs.map((s, i) => row(s, i))}
          </div>
        </>
      )}

      {/* Detail: import into a client, copy links, actions. */}
      <Modal
        classNames={{ base: modalBase }}
        isOpen={Boolean(detail) && !confirm}
        placement="center"
        scrollBehavior="inside"
        size="lg"
        onOpenChange={(open) => !open && setDetail(null)}
      >
        <ModalContent>
          {(onClose) =>
            detail && (
              <>
                <ModalHeader closable className="flex flex-col gap-1">
                  <h2 className="break-all text-base font-semibold">
                    {detail.name}
                  </h2>
                  <p className="text-small font-normal text-default-500">
                    {detail.nodeCount} 个节点
                    {detail.expireTime
                      ? ` · ${expiryLabel(detail.expireTime)}`
                      : ""}
                  </p>
                </ModalHeader>
                <ModalBody className="space-y-3">
                  <div className="space-y-1.5 rounded-lg border border-divider px-3 py-2.5">
                    <div className="flex items-center justify-between gap-2">
                      <span className="text-sm tabular-nums">
                        {formatTraffic(
                          detail.uploadBytes + detail.downloadBytes,
                        )}
                        <span className="text-default-500">
                          {detail.trafficLimit
                            ? ` / ${formatTraffic(detail.trafficLimit)}`
                            : " · 不限流量"}
                        </span>
                      </span>
                      <Button
                        className="h-7 min-w-0 px-2 text-xs"
                        isDisabled={
                          detail.uploadBytes + detail.downloadBytes === 0
                        }
                        isLoading={resettingTraffic}
                        size="sm"
                        variant="flat"
                        onPress={resetTraffic}
                      >
                        重置流量
                      </Button>
                    </div>
                    {detail.trafficLimit > 0 && (
                      <UsageBar s={detail} track="bg-default-200" />
                    )}
                    <p
                      className={`text-xs ${detail.overQuota ? "text-danger" : "text-default-500"}`}
                    >
                      {detail.overQuota
                        ? "流量已用完，已从服务器上断开"
                        : [
                            `↑ ${formatTraffic(detail.uploadBytes)} ↓ ${formatTraffic(detail.downloadBytes)}`,
                            `${new Date(detail.trafficSince).toLocaleDateString(undefined, { month: "numeric", day: "numeric" })} 起`,
                            detail.resetDay
                              ? `每月 ${detail.resetDay} 号重置`
                              : "不自动重置",
                          ].join(" · ")}
                    </p>
                  </div>
                  <Segmented
                    options={[
                      ["import", "导入"],
                      ["traffic", "流量统计"],
                    ]}
                    value={detailTab}
                    onChange={setDetailTab}
                  />
                  {detailTab === "traffic" ? (
                    <SubTrafficStats id={detail.id} />
                  ) : (
                    <>
                      <div className="flex gap-2">
                        <Button
                          className="flex-1"
                          color="primary"
                          size="sm"
                          startContent={<Copy className="h-3.5 w-3.5" />}
                          onPress={() =>
                            copyText(url(detail), "订阅链接已复制")
                          }
                        >
                          复制订阅链接（自动识别客户端）
                        </Button>
                        <Button
                          isIconOnly
                          aria-expanded={qrFor === detail.id}
                          aria-label="订阅二维码"
                          size="sm"
                          title="订阅二维码"
                          variant={qrFor === detail.id ? "flat" : "bordered"}
                          onPress={() =>
                            setQrFor((id) =>
                              id === detail.id ? null : detail.id,
                            )
                          }
                        >
                          <QrCode className="h-4 w-4" />
                        </Button>
                      </div>
                      {qrFor === detail.id && (
                        <div className="flex flex-col items-center gap-2 py-2">
                          <QrImage text={url(detail)} />
                          <p className="text-xs text-default-400">
                            用客户端扫码导入（自动识别客户端格式）
                          </p>
                        </div>
                      )}
                      <SubClientGrid
                        configClients={Object.entries(
                          detail.options.templates ?? {},
                        )
                          .filter(([, id]) => id)
                          .map(([slot]) => slot)}
                        link={(format) => url(detail, format)}
                        name={detail.name}
                        rules={detail.options.rules}
                      />
                      <div className="space-y-1 text-xs leading-relaxed text-default-500">
                        <p>
                          点客户端直接导入
                          <span className="hidden sm:inline">
                            ，右侧按钮复制该客户端的链接
                          </span>
                          {detail.options.rules &&
                            "；已开分流：Shadowrocket、Loon、Quantumult X 导入完整配置，会替换 App 原来的配置"}
                        </p>
                        {detail.sharedIds.length > 0 && (
                          <p className="text-warning">
                            {detail.sharedIds.length}{" "}
                            个节点是共用凭据，停用后已导入的配置仍能连接
                          </p>
                        )}
                      </div>
                    </>
                  )}
                </ModalBody>
                <ModalFooter className="flex-wrap">
                  <Button
                    color="danger"
                    size="sm"
                    variant="light"
                    onPress={() => setConfirm("delete")}
                  >
                    删除
                  </Button>
                  <Button
                    color="warning"
                    size="sm"
                    variant="light"
                    onPress={() => setConfirm("reset")}
                  >
                    重置链接和凭据
                  </Button>
                  <Button
                    color="primary"
                    size="sm"
                    onPress={() => {
                      const s = detail;

                      onClose();
                      setDraft(draftFrom(s));
                    }}
                  >
                    编辑
                  </Button>
                </ModalFooter>
              </>
            )
          }
        </ModalContent>
      </Modal>

      <Modal
        classNames={{ base: modalBase }}
        isOpen={Boolean(draft)}
        placement="center"
        scrollBehavior="inside"
        size="2xl"
        onOpenChange={(open) => !open && setDraft(null)}
      >
        <ModalContent>
          {(onClose) =>
            draft && (
              <Editor
                draft={draft}
                groups={groups}
                ruleGroups={ruleGroups}
                saving={saving}
                setDraft={setDraft}
                sources={sources}
                templates={templates}
                onClose={onClose}
                onSave={save}
              />
            )
          }
        </ModalContent>
      </Modal>

      <Modal
        classNames={{ base: modalBase }}
        isOpen={Boolean(groupDraft)}
        placement="center"
        scrollBehavior="inside"
        size="2xl"
        onOpenChange={(open) => !open && setGroupDraft(null)}
      >
        <ModalContent>
          {(onClose) =>
            groupDraft && (
              <NodeGroupEditor
                draft={groupDraft}
                inbounds={inbounds}
                regions={regions}
                setDraft={setGroupDraft}
                sources={sources}
                usedBy={
                  groups.find((g) => g.id === groupDraft.id)?.subscriptions ??
                  []
                }
                onClose={onClose}
                onSaved={() => {
                  setGroupDraft(null);
                  void load();
                }}
              />
            )
          }
        </ModalContent>
      </Modal>

      <Modal
        classNames={{ base: modalBase }}
        isOpen={Boolean(confirm)}
        placement="center"
        onOpenChange={(open) => !open && setConfirm(null)}
      >
        <ModalContent>
          {(onClose) => (
            <>
              <ModalHeader>
                <h2
                  className={`text-lg font-bold ${confirm === "delete" ? "text-danger" : ""}`}
                >
                  {confirm === "reset" ? "重置链接" : "删除订阅"}
                </h2>
              </ModalHeader>
              <ModalBody>
                <p className="text-sm text-default-600">
                  {confirm === "reset"
                    ? "旧链接和已导入的配置会立即失效，需要用新链接重新导入。"
                    : "订阅链接立即失效，无法恢复。"}
                </p>
              </ModalBody>
              <ModalFooter>
                <Button variant="light" onPress={onClose}>
                  取消
                </Button>
                <Button
                  color={confirm === "delete" ? "danger" : "warning"}
                  isLoading={saving}
                  onPress={runConfirm}
                >
                  {confirm === "delete" ? "删除" : "重置"}
                </Button>
              </ModalFooter>
            </>
          )}
        </ModalContent>
      </Modal>
    </AnimatedPage>
  );
}

/** 不用 / 落地 / 中转 for a node or a whole source. */
function ChainRoleButtons({
  value,
  onChange,
}: {
  value: ChainRole;
  onChange: (role: ChainRole) => void;
}) {
  return (
    <div className="flex shrink-0 gap-1">
      {(
        [
          ["", "不用"],
          ["landing", "落地"],
          ["relay", "中转"],
        ] as Array<[ChainRole, string]>
      ).map(([r, label]) => (
        <button
          key={r}
          className={`rounded-lg border px-2 py-1 text-xs transition-colors ${
            value === r
              ? r === "landing"
                ? "border-warning bg-warning-50 text-warning-700 dark:bg-warning-500/20"
                : r === "relay"
                  ? "border-secondary bg-secondary-50 text-secondary-700 dark:bg-secondary-500/20"
                  : "border-default-400 bg-default-100 text-default-700"
              : "border-default-200 text-default-500 hover:border-default-400 dark:border-default-100"
          }`}
          type="button"
          onClick={() => onChange(r)}
        >
          {label}
        </button>
      ))}
    </div>
  );
}

/**
 * The panel's filter expression (as the backend parses it): /regex/, or
 * keywords separated by "|", any of them, case-insensitive. Null when
 * empty. Go's leading (?i) becomes the i flag.
 */
function nameMatcher(expr: string): ((name: string) => boolean) | null {
  const e = expr.trim();

  if (!e) return null;
  if (e.length > 2 && e.startsWith("/") && e.endsWith("/")) {
    let body = e.slice(1, -1);
    let flags = "";

    if (body.startsWith("(?i)")) {
      body = body.slice(4);
      flags = "i";
    }
    try {
      const re = new RegExp(body, flags);

      return (name) => re.test(name);
    } catch {
      return () => false;
    }
  }
  const words = e
    .split("|")
    .map((w) => w.trim().toLowerCase())
    .filter(Boolean);

  return (name) => {
    const lower = name.toLowerCase();

    return words.some((w) => lower.includes(w));
  };
}
