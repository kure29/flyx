import type {
  NodeGroupDraft,
  NodeGroupItem,
  NodeGroupOptions,
  NodeGroupPreview,
  RemoteSourceItem,
} from "@/api";
import type { InboundApiItem } from "@/api/types";

import { useEffect, useMemo, useState } from "react";
import toast from "react-hot-toast";
import { ArrowDown, ArrowUp } from "lucide-react";

import { deleteNodeGroup, previewNodeGroup, saveNodeGroup } from "@/api";
import { EmptyHint } from "@/components/page-kit";
import { PROTOCOL_LABELS } from "@/pages/inbound/presets";
import { Button } from "@/shadcn-bridge/heroui/button";
import { Checkbox } from "@/shadcn-bridge/heroui/checkbox";
import { Chip } from "@/shadcn-bridge/heroui/chip";
import { Input, Textarea } from "@/shadcn-bridge/heroui/input";
import {
  ModalBody,
  ModalFooter,
  ModalHeader,
} from "@/shadcn-bridge/heroui/modal";
import { RenameRules } from "@/pages/subscription/rename-rules";
import {
  ChipPicker,
  EditorTabs,
  Section,
  Segmented,
  ToggleRow,
} from "@/pages/subscription/controls";

/** Protocols that can carry per-user credentials (mirrors the backend). */
const MULTI_USER_PROTOCOLS = new Set([
  "vless",
  "vmess",
  "trojan",
  "hysteria2",
  "tuic",
  "anytls",
  "naive",
]);

export const EMPTY_GROUP_OPTIONS: NodeGroupOptions = {
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
  script: "",
};

const SCRIPT_EXAMPLE = `// 对处理后的节点再做一遍筛选、改名、排序，返回新的节点数组
// 每个节点：name 名称（可改）、original 原名、server、port、
// protocol 协议、region 地区（如 "🇭🇰 香港"）、line（"direct" 直连 / "relay" 中转）
function operator(nodes) {
  return nodes
    .filter((n) => !/过期|剩余/.test(n.name))
    .map((n) => {
      n.name = n.name.replace("IEPL", "专线");
      return n;
    })
    .sort((a, b) => a.region.localeCompare(b.region));
}
`;

/** Names the filters a group turns on, e.g. "地区 2 个、去重". */
function filterSummary(o: NodeGroupOptions): string {
  const steps: string[] = [];

  if (o.lines)
    steps.push(
      o.lines === "prefer_relay"
        ? "有中转优先显示中转"
        : o.lines === "relay"
          ? "只要中转"
          : "只要直连",
    );
  if (o.regions.length) steps.push(`地区 ${o.regions.length} 个`);
  if (o.protocols.length) steps.push(`协议 ${o.protocols.length} 个`);
  if (o.include || o.exclude) steps.push("名称");
  if (o.dedupe) steps.push("去重");

  return steps.join("、");
}

/** Names the naming steps a group turns on, e.g. "改名 2 条、国旗". */
function namingSummary(o: NodeGroupOptions): string {
  const steps: string[] = [];
  const renames = o.rename
    .split("\n")
    .filter((l) => l.trim() && !l.trim().startsWith("#")).length;

  if (renames) steps.push(`改名 ${renames} 条`);
  if (o.addFlag) steps.push("国旗");
  if (o.prefix) steps.push("前缀");
  if (o.sort === "name") steps.push("按名称排序");

  return steps.join("、");
}

/** Where a group's nodes come from, e.g. "3 个落地 + 1 个订阅源". */
export function groupSourceText(g: NodeGroupItem | NodeGroupDraft): string {
  if (g.sourceAll) return "全部节点";

  return [
    g.inboundIds.length ? `${g.inboundIds.length} 个节点` : "",
    g.sourceIds.length ? `${g.sourceIds.length} 个订阅源` : "",
  ]
    .filter(Boolean)
    .join(" + ");
}

export function groupDraftFrom(g?: NodeGroupItem): NodeGroupDraft {
  if (!g) {
    return {
      name: "",
      sourceAll: true,
      inboundIds: [],
      sourceIds: [],
      options: { ...EMPTY_GROUP_OPTIONS },
    };
  }

  return {
    id: g.id,
    name: g.name,
    sourceAll: g.sourceAll,
    inboundIds: g.inboundIds,
    sourceIds: g.sourceIds,
    options: { ...EMPTY_GROUP_OPTIONS, ...g.options },
  };
}

/** The node groups, in the order subscriptions list them by default. */
export function NodeGroupList({
  groups,
  onEdit,
  onMove,
}: {
  groups: NodeGroupItem[];
  onEdit: (g: NodeGroupItem) => void;
  onMove: (index: number, delta: -1 | 1) => void;
}) {
  if (groups.length === 0) {
    return (
      <EmptyHint text="节点组把自建、导入的节点和订阅源筛选、改名后存成一份列表，多个订阅可以共用；新建订阅前先建一个" />
    );
  }

  return (
    <div className="flex flex-col">
      {groups.map((g, i) => (
        <div
          key={g.id}
          className="group flex cursor-pointer items-center gap-3 border-b border-divider px-2 py-3 transition-colors hover:bg-default-50"
          role="button"
          tabIndex={0}
          onClick={() => onEdit(g)}
          onKeyDown={(e) => {
            if (e.target !== e.currentTarget) return;
            if (e.key === "Enter" || e.key === " ") {
              e.preventDefault();
              onEdit(g);
            }
          }}
        >
          <div className="min-w-0 flex-1">
            <div className="flex items-center gap-1.5">
              <span className="truncate">{g.name}</span>
              {g.subscriptions.length === 0 && (
                <Chip size="sm" variant="flat">
                  未使用
                </Chip>
              )}
            </div>
            <div className="truncate text-xs text-default-400">
              {g.nodeCount} 个节点 · {groupSourceText(g)}
              {[filterSummary(g.options), namingSummary(g.options)]
                .filter(Boolean)
                .map((x) => ` · ${x}`)
                .join("")}
              {g.options.script ? " · 脚本" : ""}
            </div>
            {g.subscriptions.length > 0 && (
              <div className="mt-1 truncate text-xs text-default-400">
                订阅：{g.subscriptions.map((s) => s.name).join("、")}
              </div>
            )}
          </div>
          <div
            className="flex shrink-0 items-center gap-1 transition-opacity sm:opacity-0 sm:group-hover:opacity-100"
            role="presentation"
            onClick={(e) => e.stopPropagation()}
          >
            <Button
              isIconOnly
              aria-label={`上移 ${g.name}`}
              isDisabled={i === 0}
              size="sm"
              variant="light"
              onPress={() => onMove(i, -1)}
            >
              <ArrowUp className="h-4 w-4" />
            </Button>
            <Button
              isIconOnly
              aria-label={`下移 ${g.name}`}
              isDisabled={i === groups.length - 1}
              size="sm"
              variant="light"
              onPress={() => onMove(i, 1)}
            >
              <ArrowDown className="h-4 w-4" />
            </Button>
          </div>
        </div>
      ))}
    </div>
  );
}

type GroupPage = "source" | "process" | "preview";

/** Edits a node group: its sources, filters, naming, script; previews it. */
export function NodeGroupEditor({
  draft,
  setDraft,
  inbounds,
  sources,
  regions,
  usedBy,
  onSaved,
  onClose,
}: {
  draft: NodeGroupDraft;
  setDraft: (d: NodeGroupDraft) => void;
  inbounds: InboundApiItem[];
  sources: RemoteSourceItem[];
  regions: string[];
  /** Subscriptions listing this group (an existing one). */
  usedBy: Array<{ id: number; name: string }>;
  onSaved: () => void;
  onClose: () => void;
}) {
  const [page, setPage] = useState<GroupPage>("source");
  const [preview, setPreview] = useState<NodeGroupPreview | null>(null);
  const [previewing, setPreviewing] = useState(false);
  const [saving, setSaving] = useState(false);
  const [confirmDelete, setConfirmDelete] = useState(false);
  const o = draft.options;
  const set = (patch: Partial<NodeGroupDraft>) => {
    setDraft({ ...draft, ...patch });
    setPreview(null);
  };
  const setOpts = (patch: Partial<NodeGroupOptions>) =>
    set({ options: { ...o, ...patch } });

  const protocols = useMemo(
    () =>
      [...new Set(inbounds.map((i) => i.protocol))].map(
        (p) => [p, PROTOCOL_LABELS[p] ?? p] as [string, string],
      ),
    [inbounds],
  );
  const selfBuilt = inbounds.filter((i) => !i.external);
  // Nodes of a remote source are picked as the whole source.
  const imported = inbounds.filter((i) => i.external && !i.sourceId);
  const sharedPicked = inbounds.filter(
    (i) =>
      (draft.sourceAll
        ? i.enabled
        : draft.inboundIds.includes(i.id) ||
          draft.sourceIds.includes(i.sourceId ?? 0)) &&
      (i.external || !MULTI_USER_PROTOCOLS.has(i.protocol)),
  ).length;

  const runPreview = async () => {
    setPreviewing(true);
    try {
      const res = await previewNodeGroup(draft);

      if (res.code !== 0) {
        toast.error(res.msg || "预览失败");

        return;
      }
      setPreview(res.data);
    } catch {
      toast.error("预览失败");
    } finally {
      setPreviewing(false);
    }
  };

  useEffect(() => {
    if (page === "preview" && !preview && !previewing) void runPreview();
  }, [page, preview]);

  const save = async () => {
    setSaving(true);
    try {
      const res = await saveNodeGroup(draft);

      if (res.code !== 0) {
        toast.error(res.msg || "保存失败");

        return;
      }
      if (res.msg && res.msg !== "操作成功") toast(res.msg, { icon: "⚠️" });
      else toast.success(draft.id ? "已保存" : "已创建");
      onSaved();
    } catch {
      toast.error("保存失败");
    } finally {
      setSaving(false);
    }
  };

  const remove = async () => {
    if (!draft.id) return;
    setSaving(true);
    try {
      const res = await deleteNodeGroup(draft.id);

      if (res.code !== 0) {
        toast.error(res.msg || "删除失败");

        return;
      }
      toast.success("已删除");
      onSaved();
    } catch {
      toast.error("删除失败");
    } finally {
      setSaving(false);
      setConfirmDelete(false);
    }
  };

  const pickList = (title: string, list: InboundApiItem[]) =>
    list.length > 0 && (
      <div className="space-y-1">
        <p className="text-xs text-default-500">{title}</p>
        {list.map((i) => (
          <Checkbox
            key={i.id}
            className="w-full"
            isSelected={draft.inboundIds.includes(i.id)}
            onValueChange={(on) =>
              set({
                inboundIds: on
                  ? [...draft.inboundIds, i.id]
                  : draft.inboundIds.filter((x) => x !== i.id),
              })
            }
          >
            <span className="text-sm">{i.name}</span>
            {!i.enabled && (
              <span className="ml-1 text-xs text-default-400">已停用</span>
            )}
            {(i.external || !MULTI_USER_PROTOCOLS.has(i.protocol)) && (
              <span className="ml-1 text-xs text-warning">共用凭据</span>
            )}
          </Checkbox>
        ))}
      </div>
    );

  return (
    <>
      <ModalHeader>
        <h2 className="text-lg font-bold">
          {draft.id ? "编辑节点组" : "新建节点组"}
        </h2>
      </ModalHeader>
      <ModalBody className="min-h-[min(65vh,34rem)] space-y-4">
        <div className="sticky top-0 z-10">
          <EditorTabs
            tabs={[
              { key: "source", label: "来源" },
              {
                key: "process",
                label: "处理",
                badge:
                  filterSummary(o) || namingSummary(o) || o.script ? "开" : "",
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

        {page === "source" && (
          <div className="space-y-4">
            <Section title="基本">
              <Input
                label="名称"
                placeholder="例如 香港专线 / 全部自建"
                value={draft.name}
                variant="bordered"
                onChange={(e) => set({ name: e.target.value })}
              />
              {usedBy.length > 0 && (
                <p className="text-xs text-default-500">
                  订阅{usedBy.map((s) => `「${s.name}」`).join("、")}
                  在用，改动对它们立即生效
                </p>
              )}
            </Section>
            <Section hint="中转线路会跟着它的节点一起出现" title="来源">
              <Segmented
                options={[
                  ["all", "全部节点"],
                  ["pick", "指定节点"],
                ]}
                value={draft.sourceAll ? "all" : "pick"}
                onChange={(v) => set({ sourceAll: v === "all" })}
              />
              {draft.sourceAll ? (
                <p className="text-xs text-default-500">
                  包括以后新增的节点和订阅源；只想要一部分可以在“处理”里筛选
                </p>
              ) : (
                <div className="max-h-56 space-y-3 overflow-y-auto rounded-lg border border-divider p-2">
                  {pickList("自建", selfBuilt)}
                  {pickList("导入", imported)}
                  {sources.length > 0 && (
                    <div className="space-y-1">
                      <p className="text-xs text-default-500">
                        订阅源（整个源，更新后的节点自动跟上）
                      </p>
                      {sources.map((src) => (
                        <Checkbox
                          key={src.id}
                          className="w-full"
                          isSelected={draft.sourceIds.includes(src.id)}
                          onValueChange={(on) =>
                            set({
                              sourceIds: on
                                ? [...draft.sourceIds, src.id]
                                : draft.sourceIds.filter((x) => x !== src.id),
                            })
                          }
                        >
                          <span className="text-sm">{src.name}</span>
                          <span className="ml-1 text-xs text-default-400">
                            {src.nodeCount} 个节点
                          </span>
                        </Checkbox>
                      ))}
                    </div>
                  )}
                </div>
              )}
              {sharedPicked > 0 && (
                <p className="text-xs text-warning">
                  其中 {sharedPicked} 个是导入的节点或 Shadowsocks /
                  Snell，没法单独分配凭据：对方拿到共用的密码，订阅停用后已导入的配置仍能连接。
                </p>
              )}
            </Section>
          </div>
        )}

        {page === "process" && (
          <div className="space-y-4">
            <Section
              collapsible
              defaultOpen={false}
              hint="只保留需要的节点；筛完剩几个可以在“预览”里看"
              summary={filterSummary(o) || "未设置"}
              title="筛选"
            >
              <div className="space-y-1.5">
                <p className="text-xs font-medium">线路</p>
                <Segmented
                  columns={2}
                  options={[
                    ["", "全部"],
                    ["relay", "只要中转"],
                    ["direct", "只要直连"],
                    ["prefer_relay", "有中转优先显示中转"],
                  ]}
                  value={o.lines}
                  onChange={(lines) => setOpts({ lines })}
                />
                {o.lines === "prefer_relay" && (
                  <p className="text-xs text-default-500">
                    同一节点有启用的中转时隐藏原直连，保留全部中转线路；没有中转则保留直连。按中转配置判断，不随测速结果变化，其他筛选继续生效。
                  </p>
                )}
              </div>
              <div className="space-y-1.5">
                <p className="text-xs font-medium">地区</p>
                <ChipPicker
                  emptyText="全部地区"
                  options={regions.map((r) => [r, r])}
                  selected={o.regions}
                  onChange={(regions) => setOpts({ regions })}
                />
              </div>
              {protocols.length > 1 && (
                <div className="space-y-1.5">
                  <p className="text-xs font-medium">协议</p>
                  <ChipPicker
                    emptyText="全部协议"
                    options={protocols}
                    selected={o.protocols}
                    onChange={(protocols) => setOpts({ protocols })}
                  />
                </div>
              )}
              <div className="grid grid-cols-1 gap-2 sm:grid-cols-2">
                <Input
                  description="多个关键词用 | 分隔，或 /正则/"
                  label="名称包含"
                  placeholder="例如 IEPL|专线"
                  value={o.include}
                  variant="bordered"
                  onChange={(e) => setOpts({ include: e.target.value })}
                />
                <Input
                  label="名称排除"
                  placeholder="例如 /过期|剩余/"
                  value={o.exclude}
                  variant="bordered"
                  onChange={(e) => setOpts({ exclude: e.target.value })}
                />
              </div>
              <ToggleRow
                hint="地址、端口、协议都相同的只保留第一个"
                id="group-dedupe"
                label="去重"
                value={o.dedupe}
                onChange={(dedupe) => setOpts({ dedupe })}
              />
            </Section>
            <Section
              collapsible
              defaultOpen={false}
              hint="按顺序执行：改名 → 国旗 → 前缀 → 排序"
              summary={namingSummary(o) || "未设置"}
              title="命名"
            >
              <div className="space-y-1.5">
                <p className="text-xs font-medium">改名规则</p>
                <p className="text-xs text-default-500">
                  从上到下依次替换，替换为留空就是删除匹配的部分，可用 $1
                  引用分组
                </p>
                <RenameRules
                  value={o.rename}
                  onChange={(rename) => setOpts({ rename })}
                />
              </div>
              <ToggleRow
                hint="按名称识别地区，在前面加上国旗"
                id="group-flag"
                label="自动加国旗"
                value={o.addFlag}
                onChange={(addFlag) => setOpts({ addFlag })}
              />
              <Input
                label="名称前缀"
                placeholder="例如 [家宽] "
                value={o.prefix}
                variant="bordered"
                onChange={(e) => setOpts({ prefix: e.target.value })}
              />
              <div className="space-y-1.5">
                <p className="text-xs font-medium">排序</p>
                <Segmented
                  options={[
                    ["", "面板顺序"],
                    ["name", "按名称"],
                  ]}
                  value={o.sort}
                  onChange={(sort) => setOpts({ sort })}
                />
              </div>
            </Section>
            <Section
              collapsible
              defaultOpen={false}
              hint="在上面的筛选、改名、排序之后运行，可以写任意 JS 来筛选、改名、排序；只取回节点顺序和名称"
              summary={o.script ? "已设置" : "未设置"}
              title="JS 脚本"
            >
              <Textarea
                classNames={{ input: "font-mono text-xs" }}
                minRows={6}
                placeholder={"function operator(nodes) {\n  return nodes;\n}"}
                value={o.script}
                variant="bordered"
                onChange={(e) => setOpts({ script: e.target.value })}
              />
              <div className="flex flex-wrap gap-2">
                {!o.script && (
                  <Button
                    size="sm"
                    variant="flat"
                    onPress={() => setOpts({ script: SCRIPT_EXAMPLE })}
                  >
                    插入示例
                  </Button>
                )}
                {o.script && (
                  <Button
                    color="primary"
                    size="sm"
                    variant="flat"
                    onPress={() => setPage("preview")}
                  >
                    试运行并预览
                  </Button>
                )}
              </div>
            </Section>
          </div>
        )}

        {page === "preview" && (
          <Section title="预览">
            {previewing && !preview && (
              <p className="py-6 text-center text-xs text-default-500">
                正在生成…
              </p>
            )}
            {preview?.scriptError && (
              <p className="rounded-lg bg-danger-50 px-3 py-2 text-xs text-danger dark:bg-danger-500/10">
                {preview.scriptError}；节点组会先不经过脚本输出
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
        )}
      </ModalBody>
      <ModalFooter className="flex-wrap">
        {draft.id &&
          (confirmDelete ? (
            <>
              <span className="mr-auto self-center text-xs text-danger">
                删除这个节点组？
              </span>
              <Button variant="light" onPress={() => setConfirmDelete(false)}>
                取消
              </Button>
              <Button color="danger" isLoading={saving} onPress={remove}>
                删除
              </Button>
            </>
          ) : (
            <Button
              className="mr-auto"
              color="danger"
              isDisabled={usedBy.length > 0}
              title={usedBy.length > 0 ? "有订阅在用，先在订阅里取消选择" : ""}
              variant="light"
              onPress={() => setConfirmDelete(true)}
            >
              删除
            </Button>
          ))}
        {!confirmDelete && (
          <>
            <Button variant="light" onPress={onClose}>
              取消
            </Button>
            <Button color="primary" isLoading={saving} onPress={save}>
              保存
            </Button>
          </>
        )}
      </ModalFooter>
    </>
  );
}
