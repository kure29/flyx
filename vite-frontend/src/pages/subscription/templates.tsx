import type {
  SubscriptionItem,
  SubTemplateBuilder,
  SubTemplateCategory,
  SubTemplate,
  SubTemplateDraft,
  SubTemplateGroup,
  SubTemplateKind,
  SubTemplateSlot,
  SubTemplateSummary,
} from "@/api";

import { useEffect, useRef, useState } from "react";
import toast from "react-hot-toast";
import { ChevronLeft, Plus } from "lucide-react";

import {
  checkSubTemplate,
  deleteSubTemplate,
  refreshSubTemplate,
  saveSubTemplate,
} from "@/api";
import { ListRow } from "@/components/page-kit";
import {
  TemplateBuilder,
  TemplateRulePreview,
  newTemplateBuilder,
} from "@/pages/subscription/template-builder";
import { Segmented } from "@/pages/subscription/controls";
import { Button } from "@/shadcn-bridge/heroui/button";
import { Input, Textarea } from "@/shadcn-bridge/heroui/input";
import {
  Modal,
  ModalBody,
  ModalContent,
  ModalFooter,
  ModalHeader,
} from "@/shadcn-bridge/heroui/modal";

export const TEMPLATE_KINDS: Array<[SubTemplateKind, string]> = [
  ["clash", "Mihomo"],
  ["singbox", "sing-box"],
  ["surge", "Surge"],
];

/** The clients a subscription picks templates for, and the format each takes. */
export const TEMPLATE_SLOTS: Array<[SubTemplateSlot, string, SubTemplateKind]> =
  [
    ["clash", "Mihomo", "clash"],
    ["singbox", "sing-box", "singbox"],
    ["surge", "Surge", "surge"],
    ["stash", "Stash", "clash"],
    ["shadowrocket", "Shadowrocket", "clash"],
  ];

export const slotLabel = (s: SubTemplateSlot) =>
  TEMPLATE_SLOTS.find(([key]) => key === s)?.[1] ?? s;

export const kindLabel = (k: SubTemplateKind) =>
  TEMPLATE_KINDS.find(([key]) => key === k)?.[1] ?? k;

/** How a group takes nodes, in each format's own syntax. */
const AUTO_HINT: Record<SubTemplateKind, string> = {
  clash: 'include-all: true，可加 filter: "港|HK"',
  singbox: 'outbounds 里写 "{all}"，可加 filter',
  surge: "include-all-proxies=1，可加 policy-regex-filter=港|HK",
};

const REGION_PRESETS: Array<[string, string]> = [
  ["🇭🇰 香港", "港|HK|Hong Kong"],
  ["🇯🇵 日本", "日本|JP|Japan|东京|大阪"],
  ["🇺🇸 美国", "美国|US|United States|洛杉矶|圣何塞"],
  ["🇸🇬 新加坡", "新加坡|狮城|SG|Singapore"],
  ["🇹🇼 台湾", "台湾|TW|Taiwan"],
  ["♻️ 自动选择", ""],
];

/** A url-test group taking the nodes matching keywords (all when empty). */
function groupSnippet(kind: SubTemplateKind, name: string, keywords: string) {
  switch (kind) {
    case "clash":
      return [
        `  - name: ${name}`,
        "    type: url-test",
        "    include-all: true",
        ...(keywords ? [`    filter: "(?i)${keywords}"`] : []),
        "    url: https://www.gstatic.com/generate_204",
        "    interval: 300",
        "",
      ].join("\n");
    case "singbox":
      return `    { "type": "urltest", "tag": "${name}", "outbounds": ["{all}"]${
        keywords
          ? `, "filter": [{ "action": "include", "keywords": ["(?i)${keywords}"] }]`
          : ""
      } },\n`;
    case "surge":
      return `${name} = url-test, include-all-proxies=1${
        keywords ? `, policy-regex-filter=(?i)${keywords}` : ""
      }, url=http://www.gstatic.com/generate_204, interval=300\n`;
  }
}

/**
 * Where a new group goes when the cursor isn't in the content box: the end
 * of proxy-groups (Mihomo), the top of outbounds (sing-box), the end of
 * [Proxy Group] (Surge); the end of the text when there is none.
 */
export function groupInsertAt(kind: SubTemplateKind, text: string): number {
  const lines = text.split("\n");
  const offset = (line: number) =>
    lines.slice(0, line).reduce((n, l) => n + l.length + 1, 0);
  const endOf = (start: number, isNext: (l: string) => boolean) => {
    let end = lines.length;

    for (let i = start + 1; i < lines.length; i++) {
      if (isNext(lines[i])) {
        end = i;
        break;
      }
    }
    // Before the blank and comment lines that lead into the next part.
    while (end > start + 1 && /^\s*(#.*)?$/.test(lines[end - 1])) end--;

    return Math.min(offset(end), text.length);
  };

  switch (kind) {
    case "clash": {
      const i = lines.findIndex((l) => /^proxy-groups\s*:/.test(l));

      if (i >= 0) return endOf(i, (l) => /^[^\s#-]/.test(l));
      break;
    }
    case "singbox": {
      const i = lines.findIndex((l) => /"outbounds"\s*:\s*\[\s*$/.test(l));

      if (i >= 0) return offset(i + 1);
      break;
    }
    case "surge": {
      const i = lines.findIndex((l) => /^\s*\[proxy group\]\s*$/i.test(l));

      if (i >= 0) return endOf(i, (l) => /^\s*\[[^\]]+\]\s*$/.test(l));
      break;
    }
  }

  return text.length;
}

/** A minimal working template to start from. */
const EXAMPLES: Record<SubTemplateKind, string> = {
  clash: `mixed-port: 7890
mode: rule
proxies: [] # 面板填入订阅里的节点
proxy-groups:
  - name: 🚀 节点选择
    type: select
    proxies: [♻️ 自动选择, 🇭🇰 香港, 🇯🇵 日本, DIRECT]
${groupSnippet("clash", "♻️ 自动选择", "")}${groupSnippet("clash", "🇭🇰 香港", "港|HK|Hong Kong")}${groupSnippet("clash", "🇯🇵 日本", "日本|JP|Japan")}rules:
  - GEOIP,LAN,DIRECT,no-resolve
  - GEOIP,CN,DIRECT
  - MATCH,🚀 节点选择
`,
  singbox: `{
  "dns": { "servers": [{ "tag": "local", "type": "local" }] },
  "inbounds": [{ "type": "mixed", "tag": "mixed-in", "listen": "127.0.0.1", "listen_port": 2080 }],
  "outbounds": [
    { "type": "selector", "tag": "🚀 节点选择", "outbounds": ["♻️ 自动选择", "🇭🇰 香港", "{all}"] },
${groupSnippet("singbox", "♻️ 自动选择", "")}${groupSnippet("singbox", "🇭🇰 香港", "港|HK|Hong Kong")}    { "type": "direct", "tag": "direct" }
  ],
  "route": {
    "rules": [{ "action": "sniff" }, { "ip_is_private": true, "outbound": "direct" }],
    "final": "🚀 节点选择",
    "auto_detect_interface": true,
    "default_domain_resolver": "local"
  }
}
`,
  surge: `[General]
loglevel = notify

[Proxy]

[Proxy Group]
🚀 节点选择 = select, ♻️ 自动选择, 🇭🇰 香港, DIRECT
${groupSnippet("surge", "♻️ 自动选择", "")}${groupSnippet("surge", "🇭🇰 香港", "港|HK|Hong Kong")}
[Rule]
GEOIP,CN,DIRECT
FINAL,🚀 节点选择
`,
};

/**
 * A template's policy groups with the nodes each gets: empty groups that
 * take nodes are flagged, filters the panel can't preview are noted.
 */
export function TemplateGroups({
  groups,
  warnings,
}: {
  groups: SubTemplateGroup[];
  warnings?: string[];
}) {
  return (
    <div className="space-y-2">
      {warnings?.map((w) => (
        <p
          key={w}
          className="rounded-xl bg-warning-50 px-3 py-2 text-xs text-warning-700 dark:bg-warning-500/10"
        >
          {w}
        </p>
      ))}
      <div className="max-h-80 overflow-y-auto rounded-xl border border-divider">
        {groups.map((g) => {
          const empty = g.auto && g.nodes !== null && g.nodes.length === 0;

          return (
            <div
              key={g.name}
              className="flex items-center gap-2 border-b border-divider px-3 py-1.5 text-xs last:border-b-0"
            >
              <span className="w-32 shrink-0 truncate font-medium sm:w-40">
                {g.name}
              </span>
              <span className="w-16 shrink-0 text-default-400">{g.type}</span>
              <span className="min-w-0 flex-1 truncate text-default-500">
                {g.auto ? (
                  g.filterError ? (
                    <span title={g.filterError}>由客户端按过滤条件收节点</span>
                  ) : (
                    <>
                      <span
                        className={`mr-1.5 rounded-full px-1.5 py-0.5 ${
                          empty
                            ? "bg-warning-100 text-warning-700 dark:bg-warning-500/20"
                            : "bg-primary-50 text-primary dark:bg-primary-500/20"
                        }`}
                      >
                        {empty ? "0 · 空组" : `${g.nodes?.length ?? 0} 个节点`}
                      </span>
                      {g.nodes?.join("、")}
                    </>
                  )
                ) : (
                  g.proxies.join("、") || "—"
                )}
              </span>
            </div>
          );
        })}
      </div>
    </div>
  );
}

type Check = {
  serviceGroups: import("@/api").SubTemplateServiceGroup[];
  builder?: SubTemplateBuilder;
  summary: SubTemplateSummary;
  autoGroups: number;
  sample: string;
  nodes: number;
};

const blank = (kind: SubTemplateKind = "clash"): SubTemplateDraft => ({
  name: "",
  kind,
  content: "",
  sourceUrl: "",
  builder: newTemplateBuilder(),
});

function Editor({
  initial,
  saved,
  subscriptions,
  onBack,
  onSaved,
  onDeleted,
}: {
  initial: SubTemplateDraft;
  /** The stored template being edited (undefined for a new one). */
  saved?: SubTemplate;
  subscriptions: SubscriptionItem[];
  onBack: () => void;
  onSaved: (t: SubTemplate) => void;
  onDeleted: (id: number) => void;
}) {
  const [draft, setDraft] = useState(initial);
  const [catalog, setCatalog] = useState<SubTemplateCategory[]>([]);
  const checkSequence = useRef(0);
  const [mode, setMode] = useState<"builder" | "paste" | "url">(
    initial.builder ? "builder" : initial.sourceUrl ? "url" : "paste",
  );
  const fromUrl = mode === "url";
  const building = mode === "builder";
  const [sampleId, setSampleId] = useState(subscriptions[0]?.id ?? 0);
  const [check, setCheck] = useState<Check | null>(null);
  const [checking, setChecking] = useState(false);
  const [saving, setSaving] = useState(false);
  const [confirmDelete, setConfirmDelete] = useState(false);
  // The cursor in the content box, once the user has put it there.
  const cursor = useRef<number | null>(null);
  const set = (patch: Partial<SubTemplateDraft>) => {
    checkSequence.current++;
    setChecking(false);
    setDraft((d) => ({ ...d, ...patch }));
    setCheck(null);
  };

  const runCheck = async () => {
    const sequence = ++checkSequence.current;

    setChecking(true);
    try {
      const res = await checkSubTemplate({
        kind: draft.kind,
        content: fromUrl ? "" : draft.content,
        sourceUrl: fromUrl ? draft.sourceUrl : "",
        builder: building ? draft.builder : undefined,
        subscriptionId: sampleId || undefined,
      });

      if (sequence !== checkSequence.current) return;
      if (res.code !== 0 || !res.data) {
        toast.error(res.msg || "检查失败");
        setCheck(null);

        return;
      }
      setCheck({ ...res.data, builder: building ? draft.builder : undefined });
      if (fromUrl || building)
        setDraft((d) => ({ ...d, content: res.data!.content }));
    } catch {
      if (sequence === checkSequence.current) toast.error("检查失败");
    } finally {
      if (sequence === checkSequence.current) setChecking(false);
    }
  };

  const save = async () => {
    setSaving(true);
    try {
      const res = await saveSubTemplate({
        ...draft,
        sourceUrl: fromUrl ? draft.sourceUrl.trim() : "",
        builder: building ? draft.builder : undefined,
      });

      if (res.code !== 0 || !res.data) {
        toast.error(res.msg || "保存失败");

        return;
      }
      toast.success("已保存");
      onSaved(res.data);
    } catch {
      toast.error("保存失败");
    } finally {
      setSaving(false);
    }
  };

  const changeMode = async (next: "builder" | "paste" | "url") => {
    checkSequence.current++;
    if (building && next !== "builder") {
      setChecking(true);
      try {
        const res = await checkSubTemplate({
          kind: draft.kind,
          content: "",
          sourceUrl: "",
          builder: draft.builder,
        });

        if (res.code !== 0 || !res.data) {
          toast.error(res.msg || "生成配置失败，请检查规则");

          return;
        }
        setDraft((d) => ({ ...d, content: res.data!.content }));
      } catch {
        toast.error("生成配置失败");

        return;
      } finally {
        setChecking(false);
      }
    }
    if (next === "builder" && !draft.builder)
      setDraft((d) => ({ ...d, builder: newTemplateBuilder() }));
    setCheck(null);
    setMode(next);
  };

  const refresh = async () => {
    if (!saved) return;
    setChecking(true);
    try {
      const res = await refreshSubTemplate(saved.id);

      if (res.code !== 0 || !res.data) {
        toast.error(res.msg || "更新失败");

        return;
      }
      toast.success("已更新");
      setDraft((d) => ({ ...d, content: res.data!.content }));
      onSaved(res.data);
    } finally {
      setChecking(false);
    }
  };

  const remove = async () => {
    if (!saved) return;
    const res = await deleteSubTemplate(saved.id);

    if (res.code !== 0) {
      toast.error(res.msg || "删除失败");
      setConfirmDelete(false);

      return;
    }
    toast.success("已删除");
    onDeleted(saved.id);
  };

  // Inserts a group snippet at the cursor, or where groups belong.
  const insertGroup = (name: string, keywords: string) => {
    const snippet = groupSnippet(draft.kind, name, keywords);
    const at = Math.min(
      cursor.current ?? groupInsertAt(draft.kind, draft.content),
      draft.content.length,
    );
    const before = draft.content.slice(0, at);
    const lead = before === "" || before.endsWith("\n") ? "" : "\n";
    const after = draft.content.slice(at);
    const trail =
      after === "" || after.startsWith("\n") || snippet.endsWith("\n")
        ? ""
        : "\n";

    set({
      content: before + lead + snippet + trail + after,
      builder: undefined,
    });
    cursor.current = null;
    toast.success(`已插入「${name}」，记得在选择组里引用它`);
  };
  const keepCursor = (e: React.SyntheticEvent<HTMLTextAreaElement>) => {
    cursor.current = e.currentTarget.selectionStart;
  };

  const autoGroups = check?.summary.groups.filter((g) => g.auto) ?? [];

  return (
    <>
      <ModalHeader className="flex items-center gap-2">
        <button
          aria-label="返回"
          className="rounded-lg p-1 text-default-500 hover:bg-default-100"
          type="button"
          onClick={onBack}
        >
          <ChevronLeft className="h-5 w-5" />
        </button>
        <h2 className="text-lg font-bold">{saved ? "编辑模板" : "新建模板"}</h2>
      </ModalHeader>
      <ModalBody className="space-y-3">
        <div className="grid gap-3 sm:grid-cols-2">
          <Input
            label="名称"
            placeholder="例如 我的分流 / ACL4SSR 精简版"
            value={draft.name}
            variant="bordered"
            onChange={(e) => set({ name: e.target.value })}
          />
          <div className="space-y-1.5">
            <p className="text-sm font-medium">格式</p>
            <Segmented
              options={TEMPLATE_KINDS}
              value={draft.kind}
              onChange={(kind) => set({ kind })}
            />
          </div>
        </div>
        {(!building || draft.kind === "singbox") && (
          <p className="text-xs text-default-500">
            {building
              ? "生成的 sing-box 模板适用于 1.12 及以上版本"
              : `节点进策略组的写法：${AUTO_HINT[draft.kind]}`}
          </p>
        )}
        <div className="space-y-1.5">
          <p className="text-sm font-medium">编辑方式</p>
          <Segmented
            options={[
              ["builder", "选择规则"],
              ["paste", "源码编辑"],
              ["url", "从网址获取"],
            ]}
            value={mode}
            onChange={(v) => {
              void changeMode(v);
            }}
          />
        </div>

        {building && draft.builder ? (
          <div className="space-y-3">
            {initial.content && !initial.builder && (
              <p className="text-xs text-default-500">
                原有源码不会自动转换为规则选项，保存时将按下面的选择重新生成配置。
              </p>
            )}
            <TemplateBuilder
              value={draft.builder}
              onCatalog={setCatalog}
              onChange={(builder) => set({ builder })}
            />
          </div>
        ) : fromUrl ? (
          <div className="space-y-2">
            <Input
              placeholder="https://raw.githubusercontent.com/.../config.yaml"
              value={draft.sourceUrl}
              variant="bordered"
              onChange={(e) => set({ sourceUrl: e.target.value })}
            />
            <p className="text-xs text-default-500">
              面板每天下载一次，下载失败继续用上次的内容
              {saved?.lastError && (
                <span className="text-danger">
                  ；上次更新失败：{saved.lastError}
                </span>
              )}
            </p>
            {draft.content && (
              <>
                <Textarea
                  readOnly
                  classNames={{ input: "font-mono text-xs" }}
                  minRows={10}
                  value={draft.content}
                  variant="bordered"
                  wrap="off"
                />
                <div className="flex flex-wrap gap-2">
                  {saved && (
                    <Button
                      isLoading={checking}
                      size="sm"
                      variant="flat"
                      onPress={refresh}
                    >
                      立即更新
                    </Button>
                  )}
                  <Button
                    size="sm"
                    variant="flat"
                    onPress={() => {
                      // A pasted copy: editable, no longer follows the URL.
                      setDraft({
                        name: `${draft.name || "模板"} 副本`,
                        kind: draft.kind,
                        content: draft.content,
                        sourceUrl: "",
                      });
                      setMode("paste");
                      toast("已复制为本地模板，保存后生效", { icon: "📋" });
                    }}
                  >
                    复制为本地模板
                  </Button>
                </div>
              </>
            )}
          </div>
        ) : (
          <div className="space-y-2">
            <div className="flex flex-wrap items-center gap-1.5">
              <span className="text-xs text-default-500">插入分组：</span>
              {REGION_PRESETS.map(([name, kw]) => (
                <button
                  key={name}
                  className="rounded-lg bg-default-100 px-2 py-1 text-xs text-default-700 hover:bg-default-200"
                  title={kw ? `收名字含 ${kw} 的节点` : "收全部节点"}
                  type="button"
                  onClick={() => insertGroup(name, kw)}
                >
                  {name}
                </button>
              ))}
              {!draft.content && (
                <button
                  className="ml-auto text-xs text-primary"
                  type="button"
                  onClick={() => {
                    set({ content: EXAMPLES[draft.kind], builder: undefined });
                    cursor.current = null;
                  }}
                >
                  用示例开始
                </button>
              )}
            </div>
            <Textarea
              classNames={{ input: "font-mono text-xs" }}
              id="tpl-content"
              minRows={14}
              placeholder={`粘贴你的 ${kindLabel(draft.kind)} 配置；节点列表留空，面板会填入订阅里的节点`}
              spellCheck={false}
              value={draft.content}
              variant="bordered"
              wrap="off"
              onChange={(e) => {
                set({ content: e.target.value, builder: undefined });
                keepCursor(e);
              }}
              onClick={keepCursor}
              onKeyUp={keepCursor}
            />
          </div>
        )}

        <div className="space-y-2 rounded-xl bg-default-50 p-3 dark:bg-default-100/40">
          <div className="flex flex-wrap items-center gap-2">
            <span className="text-xs text-default-500">用订阅预演：</span>
            <select
              aria-label="用哪个订阅预演"
              className="h-8 rounded-lg border border-divider bg-background px-2 text-xs"
              value={sampleId}
              onChange={(e) => {
                setSampleId(Number(e.target.value));
                set({});
              }}
            >
              <option value={0}>全部节点</option>
              {subscriptions.map((s) => (
                <option key={s.id} value={s.id}>
                  {s.name}
                </option>
              ))}
            </select>
            <Button
              className="ml-auto"
              isDisabled={
                building
                  ? !draft.builder
                  : fromUrl
                    ? !draft.sourceUrl.trim()
                    : !draft.content
              }
              isLoading={checking}
              size="sm"
              variant="flat"
              onPress={runCheck}
            >
              {building ? "生成并预览" : fromUrl ? "获取并检查" : "检查"}
            </Button>
          </div>
          {check ? (
            <>
              {check.builder ? (
                <>
                  <p className="text-xs">
                    以下规则按顺序匹配，未命中时走兜底。共生成{" "}
                    {check.summary.rules} 条客户端规则
                    {check.summary.providers > 0
                      ? `、${check.summary.providers} 个规则集`
                      : ""}
                    。
                  </p>
                  <TemplateRulePreview
                    catalog={catalog}
                    serviceGroups={check.serviceGroups}
                    value={check.builder}
                  />
                </>
              ) : (
                <p className="text-xs">
                  识别到 <b>{check.summary.groups.length}</b> 个策略组，其中{" "}
                  <b>{check.autoGroups}</b> 个自动收节点；规则{" "}
                  {check.summary.rules} 条
                  {check.summary.providers > 0 &&
                    `，规则集 ${check.summary.providers} 个`}
                  。按「{check.sample}」的 {check.nodes} 个节点预演：
                </p>
              )}
              {building && (
                <details className="rounded-lg border border-divider p-2">
                  <summary className="cursor-pointer text-xs">
                    查看生成的配置
                  </summary>
                  <Textarea
                    readOnly
                    classNames={{ input: "font-mono text-xs" }}
                    minRows={10}
                    value={draft.content}
                    variant="bordered"
                    wrap="off"
                  />
                </details>
              )}
              {check.builder ? (
                <details className="space-y-2 rounded-lg border border-divider p-2">
                  <summary className="cursor-pointer text-xs">
                    代理节点详情 · {check.sample}（{check.nodes} 个节点）
                  </summary>
                  <p className="text-xs text-default-500">
                    规则选择“代理”时，由节点选择或自动测速决定使用哪个节点。这里不改变规则的匹配顺序。
                  </p>
                  <TemplateGroups
                    groups={check.summary.groups}
                    warnings={check.summary.warnings}
                  />
                </details>
              ) : (
                <TemplateGroups
                  groups={check.summary.groups}
                  warnings={check.summary.warnings}
                />
              )}
              {autoGroups.some((g) => g.filterError) && (
                <p className="text-xs text-default-400">
                  标“由客户端按过滤条件收节点”的组用了面板预览不了的正则写法，客户端照常生效
                </p>
              )}
            </>
          ) : (
            <p className="text-xs text-default-400">
              {building
                ? "生成配置并查看规则的分流去向，可展开代理节点详情。"
                : "检查格式，并看看每个策略组能收到哪些节点"}
            </p>
          )}
        </div>
      </ModalBody>
      <ModalFooter>
        {saved &&
          (confirmDelete ? (
            <>
              <span className="mr-auto self-center text-xs text-danger">
                删除这个模板？
              </span>
              <Button variant="light" onPress={() => setConfirmDelete(false)}>
                取消
              </Button>
              <Button color="danger" onPress={remove}>
                删除
              </Button>
            </>
          ) : (
            <Button
              className="mr-auto"
              color="danger"
              variant="light"
              onPress={() => setConfirmDelete(true)}
            >
              删除
            </Button>
          ))}
        {!confirmDelete && (
          <>
            <Button variant="light" onPress={onBack}>
              取消
            </Button>
            <Button
              color="primary"
              isDisabled={
                checking ||
                (building &&
                  draft.builder?.preset !== "custom" &&
                  !draft.builder?.rules.length)
              }
              isLoading={saving}
              onPress={save}
            >
              保存
            </Button>
          </>
        )}
      </ModalFooter>
    </>
  );
}

function ago(ms: number) {
  const min = Math.floor((Date.now() - ms) / 60000);

  if (min < 1) return "刚刚";
  if (min < 60) return `${min} 分钟前`;
  if (min < 1440) return `${Math.floor(min / 60)} 小时前`;

  return `${Math.floor(min / 1440)} 天前`;
}

/** 订阅 → 模板: the template list, and the editor in the same dialog. */
export function TemplatesModal({
  isOpen,
  templates,
  subscriptions,
  onClose,
  onChanged,
}: {
  isOpen: boolean;
  templates: SubTemplate[];
  subscriptions: SubscriptionItem[];
  onClose: () => void;
  /** Reload the templates (and subscriptions). */
  onChanged: () => void;
}) {
  const [editing, setEditing] = useState<{
    draft: SubTemplateDraft;
    saved?: SubTemplate;
  } | null>(null);

  useEffect(() => {
    if (!isOpen) setEditing(null);
  }, [isOpen]);

  return (
    <Modal
      classNames={{
        base: "!w-[calc(100%-32px)] !mx-auto sm:!w-full overflow-hidden",
      }}
      isOpen={isOpen}
      placement="center"
      scrollBehavior="inside"
      size="2xl"
      onOpenChange={(v) => !v && onClose()}
    >
      <ModalContent>
        {() =>
          editing ? (
            <Editor
              initial={editing.draft}
              saved={editing.saved}
              subscriptions={subscriptions}
              onBack={() => setEditing(null)}
              onDeleted={() => {
                setEditing(null);
                onChanged();
              }}
              onSaved={() => {
                setEditing(null);
                onChanged();
              }}
            />
          ) : (
            <>
              <ModalHeader closable className="flex flex-col gap-0.5">
                <h2 className="text-lg font-bold">订阅模板</h2>
                <p className="text-xs font-normal text-default-500">
                  用自己的配置当订阅骨架：规则、策略组、DNS
                  按模板来，面板只负责把节点放进去
                </p>
              </ModalHeader>
              <ModalBody className="space-y-2">
                {templates.length === 0 ? (
                  <div className="space-y-2 py-6 text-center text-sm text-default-500">
                    <p>还没有模板</p>
                    <p className="text-xs">
                      可以选择规则快速创建模板，也支持源码编辑和网址导入；在订阅的「规则」页里选用
                    </p>
                  </div>
                ) : (
                  templates.map((t) => (
                    <ListRow
                      key={t.id}
                      badges={
                        <span className="shrink-0 rounded-full bg-default-100 px-2 py-0.5 text-xs text-default-600">
                          {kindLabel(t.kind)}
                        </span>
                      }
                      meta={[
                        t.sourceUrl
                          ? `网址 · ${t.lastError ? "上次更新失败" : `${ago(t.lastFetch)}更新`}`
                          : t.builder
                            ? "规则编辑器"
                            : "源码",
                        `${t.groups} 个策略组`,
                        t.usedBy > 0 ? `${t.usedBy} 个订阅在用` : "未使用",
                      ].join(" · ")}
                      status={t.lastError ? "warn" : undefined}
                      title={t.name}
                      onPress={() =>
                        setEditing({
                          draft: {
                            id: t.id,
                            name: t.name,
                            kind: t.kind,
                            content: t.content,
                            sourceUrl: t.sourceUrl,
                            builder: t.builder,
                          },
                          saved: t,
                        })
                      }
                    />
                  ))
                )}
              </ModalBody>
              <ModalFooter>
                <Button
                  color="primary"
                  startContent={<Plus className="h-4 w-4" />}
                  onPress={() => setEditing({ draft: blank() })}
                >
                  新建模板
                </Button>
              </ModalFooter>
            </>
          )
        }
      </ModalContent>
    </Modal>
  );
}
