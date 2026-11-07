import type {
  SubTemplateBuilder,
  SubTemplateCategory,
  SubTemplateServiceGroup,
} from "@/api";

import { useEffect, useState } from "react";
import toast from "react-hot-toast";
import { ArrowDown, ArrowUp, Plus, Trash2 } from "lucide-react";

import {
  GroupIcon,
  IconPicker,
  loadLibraryIcons,
  matchIcon,
  useIconLibraries,
} from "./icon-picker";

import { getSubTemplateCatalog } from "@/api";
import { Button } from "@/shadcn-bridge/heroui/button";
import { Input } from "@/shadcn-bridge/heroui/input";

const MAIN = "节点选择";

/**
 * A group name is committed on blur: rules follow the renamed group, and a
 * half-typed name must not take over another target's rules. Taken names
 * are refused.
 */
function GroupNameInput({
  label,
  name,
  taken,
  onCommit,
}: {
  label: string;
  name: string;
  taken: string[];
  onCommit: (name: string) => void;
}) {
  const [draft, setDraft] = useState(name);

  useEffect(() => setDraft(name), [name]);

  return (
    <Input
      aria-label={label}
      size="sm"
      value={draft}
      variant="bordered"
      onBlur={() => {
        const next = draft.trim();

        if (next && next !== name && !taken.includes(next)) onCommit(next);
        else setDraft(name);
      }}
      onChange={(e) => setDraft(e.target.value)}
    />
  );
}
const selectClass =
  "h-9 min-w-0 rounded-lg border border-divider bg-background px-2 text-sm";
const PRESETS = [
  ["minimal", "极简规则"],
  ["balanced", "均衡规则（推荐）"],
  ["full", "完整规则"],
  ["custom", "自定义"],
] as const;
const REGION_GROUPS: SubTemplateBuilder["groups"] = [
  { name: "🇭🇰 香港", type: "url-test", filter: "(?i)港|HK|Hong Kong" },
  { name: "🇯🇵 日本", type: "url-test", filter: "(?i)日本|JP|Japan|东京|大阪" },
  {
    name: "🇺🇸 美国",
    type: "url-test",
    filter: "(?i)美国|US|United States|洛杉矶|圣何塞",
  },
];
const RULE_TYPES = [
  ["domain_suffix", "域名后缀"],
  ["domain", "完整域名"],
  ["domain_keyword", "域名关键词"],
  ["ip_cidr", "IP / CIDR"],
] as const;

// File names icon libraries use for each group, tried in order.
const ICON_NAMES: Record<string, string[]> = {
  __main: ["Proxy", "节点选择", "Select"],
  __final: ["Final", "漏网之鱼", "Match"],
  private: ["Lan", "Private", "Router", "WiFi", "Home", "Direct"],
  ads: ["Advertising", "AdBlack", "AdBlock", "Ad", "Reject", "Hijacking"],
  ai: ["AI", "ChatGPT", "OpenAI", "Claude"],
  github: ["GitHub"],
  youtube: ["YouTube"],
  netflix: ["Netflix"],
  spotify: ["Spotify"],
  abema: ["AbemaTV", "Abema"],
  media: ["ForeignMedia", "Streaming", "Media", "GlobalMedia"],
  telegram: ["Telegram"],
  google: ["Google_Search", "Google"],
  microsoft: ["Microsoft"],
  apple: ["Apple"],
  bilibili: ["bilibili", "BiliBili"],
  games: ["Game", "Games", "Steam"],
  pixiv: ["Pixiv"],
  scholar: ["Scholar", "Education", "Study"],
  cn: ["China", "CN", "Domestic", "DomesticMedia"],
  noncn: ["Global", "Foreign", "World"],
};
const REGION_ICON_NAMES: Array<[RegExp, string[]]> = [
  [/港|HK|Hong ?Kong/i, ["Hong_Kong", "HK", "HongKong"]],
  [/台|TW|Taiwan/i, ["Taiwan", "TW"]],
  [/日本|JP|Japan/i, ["Japan", "JP"]],
  [/新加坡|狮城|SG|Singapore/i, ["Singapore", "SG"]],
  [/韩国|KR|Korea/i, ["Korea", "KR", "South_Korea"]],
  [/美国|US|United States/, ["United_States", "US", "USA"]],
];

function groupIconNames(g: SubTemplateBuilder["groups"][number]) {
  const region = REGION_ICON_NAMES.find(([re]) => re.test(g.name));

  return [
    ...(region?.[1] ?? []),
    ...(g.type === "url-test" ? ["Auto", "Speedtest"] : ["Static"]),
  ];
}

export const newTemplateBuilder = (): SubTemplateBuilder => ({
  version: 1,
  serviceGroups: true,
  preset: "balanced",
  groups: [{ name: "自动选择", type: "url-test", filter: "" }],
  rules: [],
  final: MAIN,
});

export function TemplateBuilder({
  value,
  onChange,
  onCatalog,
}: {
  value: SubTemplateBuilder;
  onChange: (value: SubTemplateBuilder) => void;
  onCatalog?: (catalog: SubTemplateCategory[]) => void;
}) {
  const [catalog, setCatalog] = useState<SubTemplateCategory[]>([]);
  const [loadError, setLoadError] = useState(false);
  const [retry, setRetry] = useState(0);
  const { libraries, setLibraries, defaults } = useIconLibraries();
  const [library, setLibrary] = useState("");
  const [applying, setApplying] = useState(false);
  // The icon being picked: a category key, "__main", "__final" or "group:<i>".
  const [picking, setPicking] = useState<string | null>(null);
  const [custom, setCustom] = useState<SubTemplateBuilder["rules"][number]>({
    type: "domain_suffix",
    value: "",
    target: MAIN,
  });

  useEffect(() => {
    let active = true;

    getSubTemplateCatalog()
      .then((res) => {
        if (!active) return;
        if (res.code === 0 && res.data) {
          setCatalog(res.data);
          onCatalog?.(res.data);
          setLoadError(false);
        } else setLoadError(true);
      })
      .catch(() => {
        if (active) setLoadError(true);
      });

    return () => {
      active = false;
    };
  }, [retry, onCatalog]);

  useEffect(() => {
    if (catalog.length && value.preset !== "custom" && !value.rules.length) {
      onChange({
        ...value,
        rules: catalog
          .filter((c) => c.presets.includes(value.preset))
          .map((c) => ({ type: "category", value: c.key, target: c.target })),
      });
    }
  }, [catalog, value, onChange]);

  const selected = new Set(
    value.rules.filter((r) => r.type === "category").map((r) => r.value),
  );
  const targets = [
    MAIN,
    ...value.groups.map((g) => g.name),
    "DIRECT",
    "REJECT",
  ];
  const targetOptions = (reject = true) =>
    targets
      .filter((t) => reject || t !== "REJECT")
      .map((t, i) => (
        <option key={`${i}-${t}`} value={t}>
          {t === "DIRECT" ? "直连" : t === "REJECT" ? "拦截" : t}
        </option>
      ));
  const updateRules = (rules: SubTemplateBuilder["rules"]) =>
    onChange({ ...value, preset: "custom", rules });
  const preset = (p: SubTemplateBuilder["preset"]) => {
    onChange({
      ...value,
      preset: p,
      rules:
        p === "custom"
          ? value.rules
          : [
              ...value.rules.filter((r) => r.type !== "category"),
              ...catalog
                .filter((c) => c.presets.includes(p))
                .map((c) => ({
                  type: "category" as const,
                  value: c.key,
                  target: c.target,
                })),
            ],
    });
  };
  const toggle = (c: SubTemplateCategory) => {
    if (selected.has(c.key)) {
      updateRules(
        value.rules.filter((r) => r.type !== "category" || r.value !== c.key),
      );

      return;
    }
    const rules = [...value.rules];
    const rank = catalog.findIndex((x) => x.key === c.key);
    const at = rules.findIndex(
      (r) =>
        r.type === "category" &&
        catalog.findIndex((x) => x.key === r.value) > rank,
    );

    rules.splice(at < 0 ? rules.length : at, 0, {
      type: "category",
      value: c.key,
      target: c.target,
    });
    updateRules(rules);
  };
  const move = (i: number, by: number) => {
    const rules = [...value.rules];

    [rules[i], rules[i + by]] = [rules[i + by], rules[i]];
    updateRules(rules);
  };
  const editGroup = (
    i: number,
    patch: Partial<SubTemplateBuilder["groups"][number]>,
  ) => {
    const old = value.groups[i].name;

    onChange({
      ...value,
      groups: value.groups.map((g, j) => (j === i ? { ...g, ...patch } : g)),
      rules: value.rules.map((r) =>
        r.target === old && patch.name !== undefined
          ? { ...r, target: patch.name }
          : r,
      ),
      final:
        value.final === old && patch.name !== undefined
          ? patch.name
          : value.final,
    });
  };

  const activeLibrary = library || libraries[0]?.url || "";
  const serviceGroups = value.serviceGroups !== false;
  // Every group the profile gets, in profile order. Category and 兜底
  // groups only exist with independent service groups.
  const iconSlots = [
    { key: "__main", name: MAIN, fallback: defaults.__main },
    ...value.groups.map((g, i) => ({
      key: `group:${i}`,
      name: g.name,
      fallback: g.type === "url-test" ? defaults.__auto : undefined,
    })),
    ...(serviceGroups
      ? [
          ...catalog
            .filter((c) => selected.has(c.key))
            .map((c) => ({ key: c.key, name: c.name, fallback: c.icon })),
          { key: "__final", name: "漏网之鱼", fallback: defaults.__final },
        ]
      : []),
  ];
  const iconOf = (key: string) =>
    key.startsWith("group:")
      ? (value.groups[Number(key.slice(6))]?.icon ?? "")
      : (value.icons?.[key] ?? "");
  const setIcons = (
    patch: Record<string, string>,
    groupIcons?: Record<number, string>,
  ) => {
    const icons = { ...value.icons, ...patch };

    for (const k of Object.keys(icons)) if (!icons[k]) delete icons[k];
    onChange({
      ...value,
      icons,
      groups: groupIcons
        ? value.groups.map((g, i) =>
            i in groupIcons ? { ...g, icon: groupIcons[i] || undefined } : g,
          )
        : value.groups,
    });
  };
  const setIcon = (key: string, url: string) =>
    key.startsWith("group:")
      ? setIcons({}, { [Number(key.slice(6))]: url })
      : setIcons({ [key]: url });
  const applyLibrary = async () => {
    setApplying(true);
    try {
      const icons = await loadLibraryIcons(activeLibrary);
      const patch: Record<string, string> = {};
      const groupIcons: Record<number, string> = {};
      let matched = 0;

      for (const slot of iconSlots) {
        if (slot.key.startsWith("group:")) {
          const i = Number(slot.key.slice(6));
          const url = matchIcon(icons, groupIconNames(value.groups[i]));

          if (url) {
            // The default stays the default, not a copy of it.
            groupIcons[i] = url === slot.fallback ? "" : url;
            matched++;
          }
        } else {
          const url = matchIcon(icons, ICON_NAMES[slot.key] ?? [slot.name]);

          if (url) {
            patch[slot.key] = url === slot.fallback ? "" : url;
            matched++;
          }
        }
      }
      setIcons(patch, groupIcons);
      toast.success(
        `已套用 ${matched} 个图标${matched < iconSlots.length ? `，${iconSlots.length - matched} 个没匹配到，保持原样` : ""}`,
      );
    } catch (e) {
      toast.error((e as Error).message);
    } finally {
      setApplying(false);
    }
  };
  const pickingSlot = iconSlots.find((s) => s.key === picking);

  return (
    <div className="space-y-4">
      <div className="space-y-2">
        <label className="text-sm font-medium" htmlFor="template-rule-preset">
          规则选择
        </label>
        <select
          className={`${selectClass} w-full`}
          disabled={!catalog.length}
          id="template-rule-preset"
          value={value.preset}
          onChange={(e) =>
            preset(e.target.value as SubTemplateBuilder["preset"])
          }
        >
          {PRESETS.map(([key, label]) => (
            <option key={key} value={key}>
              {label}
            </option>
          ))}
        </select>
        <p className="text-xs text-default-500">
          选择预设后仍可手动调整；自定义规则会保留。
        </p>
      </div>
      <div className="rounded-xl border border-divider p-3">
        <div className="mb-3 flex items-center justify-between gap-2">
          <span className="text-sm font-medium">
            已选择 {selected.size} 个类别
          </span>
          <div className="flex gap-3 text-xs">
            <button
              className="text-primary"
              disabled={!catalog.length}
              type="button"
              onClick={() => preset("full")}
            >
              全选
            </button>
            <button
              className="text-default-500"
              type="button"
              onClick={() =>
                updateRules(value.rules.filter((r) => r.type !== "category"))
              }
            >
              清空分类
            </button>
          </div>
        </div>
        {loadError ? (
          <div className="text-sm text-danger">
            规则库加载失败{" "}
            <button
              className="underline"
              type="button"
              onClick={() => setRetry((n) => n + 1)}
            >
              重试
            </button>
          </div>
        ) : !catalog.length ? (
          <p className="text-sm text-default-500">正在加载规则库…</p>
        ) : (
          <div className="grid grid-cols-2 gap-x-3 gap-y-1.5 sm:grid-cols-3">
            {catalog.map((c) => (
              <label
                key={c.key}
                className={`flex cursor-pointer items-center gap-2 rounded-lg px-2 py-2 text-sm transition-colors ${selected.has(c.key) ? "bg-primary/5" : "hover:bg-default-50"}`}
              >
                <input
                  checked={selected.has(c.key)}
                  className="h-4 w-4 shrink-0 accent-primary"
                  type="checkbox"
                  onChange={() => toggle(c)}
                />
                <GroupIcon src={value.icons?.[c.key] || c.icon} />
                <span>{c.name}</span>
              </label>
            ))}
          </div>
        )}
      </div>

      <label className="flex items-start gap-2 rounded-xl border border-divider p-3 text-sm">
        <input
          checked={value.serviceGroups !== false}
          className="mt-0.5 accent-primary"
          type="checkbox"
          onChange={(e) =>
            onChange({ ...value, serviceGroups: e.target.checked })
          }
        />
        <span>
          按服务生成独立策略组
          <span className="mt-1 block text-xs text-default-500">
            每个已选类别和兜底各有一个选择组，可在客户端单独选节点。规则去向作为该组的默认选择；sing-box
            的拦截规则直接拒绝。
          </span>
        </span>
      </label>
      <details className="rounded-xl border border-divider p-3">
        <summary className="cursor-pointer text-sm font-medium">
          规则去向与顺序 · {value.rules.length} 条
        </summary>
        <p className="my-3 text-xs text-default-500">
          从上往下匹配，第一条命中后生效。可把具体规则放在
          Google、微软、国内服务等大类前面。启用独立策略组时，类别的去向为组内默认选择。
        </p>
        <div className="space-y-2">
          {value.rules.map((r, i) => (
            <div
              key={`${r.type}-${i}`}
              className="flex flex-wrap items-center gap-2 rounded-lg bg-default-50 p-2"
            >
              <span className="min-w-0 flex-1 break-all text-xs">
                <span className="mr-2 text-default-400">{i + 1}.</span>
                {r.type === "category"
                  ? (catalog.find((c) => c.key === r.value)?.name ?? r.value)
                  : `${RULE_TYPES.find(([k]) => k === r.type)?.[1]} · ${r.value}`}
              </span>
              <select
                aria-label={`第 ${i + 1} 条规则去向`}
                className={`${selectClass} max-w-36`}
                value={r.target}
                onChange={(e) =>
                  updateRules(
                    value.rules.map((x, j) =>
                      j === i ? { ...x, target: e.target.value } : x,
                    ),
                  )
                }
              >
                {targetOptions()}
              </select>
              <button
                aria-label={`上移第 ${i + 1} 条规则`}
                className="p-1 text-default-500 disabled:opacity-25"
                disabled={i === 0}
                type="button"
                onClick={() => move(i, -1)}
              >
                <ArrowUp className="h-4 w-4" />
              </button>
              <button
                aria-label={`下移第 ${i + 1} 条规则`}
                className="p-1 text-default-500 disabled:opacity-25"
                disabled={i === value.rules.length - 1}
                type="button"
                onClick={() => move(i, 1)}
              >
                <ArrowDown className="h-4 w-4" />
              </button>
              <button
                aria-label={`删除第 ${i + 1} 条规则`}
                className="p-1 text-danger"
                type="button"
                onClick={() =>
                  updateRules(value.rules.filter((_, j) => j !== i))
                }
              >
                <Trash2 className="h-4 w-4" />
              </button>
            </div>
          ))}
          {!value.rules.length && (
            <p className="text-xs text-default-500">
              尚未选择规则，将只使用兜底去向。
            </p>
          )}
        </div>
      </details>

      <details className="rounded-xl border border-divider p-3">
        <summary className="cursor-pointer text-sm font-medium">
          添加自定义规则
        </summary>
        <div className="mt-3 grid grid-cols-1 gap-2 sm:grid-cols-2">
          <select
            aria-label="自定义规则类型"
            className={selectClass}
            value={custom.type}
            onChange={(e) =>
              setCustom({
                ...custom,
                type: e.target.value as typeof custom.type,
              })
            }
          >
            {RULE_TYPES.map(([key, label]) => (
              <option key={key} value={key}>
                {label}
              </option>
            ))}
          </select>
          <select
            aria-label="自定义规则去向"
            className={selectClass}
            value={targets.includes(custom.target) ? custom.target : MAIN}
            onChange={(e) => setCustom({ ...custom, target: e.target.value })}
          >
            {targetOptions()}
          </select>
          <Input
            aria-label="自定义规则内容"
            className="sm:col-span-2"
            placeholder={
              custom.type === "ip_cidr"
                ? "192.0.2.0/24 或 2001:db8::/32"
                : "example.com"
            }
            value={custom.value}
            variant="bordered"
            onChange={(e) => setCustom({ ...custom, value: e.target.value })}
          />
          <p className="self-center text-xs text-default-500">
            新规则放在最前面，保存时校验域名与 CIDR。
          </p>
          <Button
            isDisabled={!custom.value.trim()}
            size="sm"
            startContent={<Plus className="h-4 w-4" />}
            variant="flat"
            onPress={() => {
              updateRules([
                {
                  ...custom,
                  value: custom.value.trim(),
                  target: targets.includes(custom.target)
                    ? custom.target
                    : MAIN,
                },
                ...value.rules,
              ]);
              setCustom({ ...custom, value: "" });
            }}
          >
            添加规则
          </Button>
        </div>
      </details>

      <details className="rounded-xl border border-divider p-3">
        <summary className="cursor-pointer text-sm font-medium">
          节点分组 · {value.groups.length} 个
        </summary>
        <p className="my-3 text-xs text-default-500">
          节点选择包含所有分组和节点。下面的分组按节点名称过滤；过滤留空即包含所有节点。
        </p>
        <div className="mb-3 flex flex-wrap gap-2">
          {REGION_GROUPS.map((g) => (
            <Button
              key={g.name}
              isDisabled={
                value.groups.length >= 20 ||
                value.groups.some((x) => x.name === g.name)
              }
              size="sm"
              variant="flat"
              onPress={() =>
                onChange({ ...value, groups: [...value.groups, { ...g }] })
              }
            >
              添加{g.name}
            </Button>
          ))}
        </div>
        <div className="space-y-3">
          {value.groups.map((g, i) => (
            <div key={i} className="space-y-2 rounded-lg bg-default-50 p-2">
              <div className="flex items-center gap-2">
                <GroupNameInput
                  label={`策略组 ${i + 1} 名称`}
                  name={g.name}
                  taken={[
                    MAIN,
                    ...value.groups
                      .filter((_, j) => j !== i)
                      .map((o) => o.name),
                  ]}
                  onCommit={(name) => editGroup(i, { name })}
                />
                <select
                  aria-label={`策略组 ${i + 1} 类型`}
                  className={selectClass}
                  value={g.type}
                  onChange={(e) =>
                    editGroup(i, { type: e.target.value as typeof g.type })
                  }
                >
                  <option value="url-test">自动测速</option>
                  <option value="select">手动选择</option>
                </select>
                <button
                  aria-label={`删除策略组 ${i + 1}`}
                  className="p-1 text-danger"
                  type="button"
                  onClick={() =>
                    onChange({
                      ...value,
                      groups: value.groups.filter((_, j) => i !== j),
                      rules: value.rules.map((r) =>
                        r.target === g.name ? { ...r, target: MAIN } : r,
                      ),
                      final: value.final === g.name ? MAIN : value.final,
                    })
                  }
                >
                  <Trash2 className="h-4 w-4" />
                </button>
              </div>
              <Input
                aria-label={`策略组 ${i + 1} 节点过滤`}
                placeholder="节点名称正则，例如 (?i)港|HK"
                size="sm"
                value={g.filter}
                variant="bordered"
                onChange={(e) => editGroup(i, { filter: e.target.value })}
              />
            </div>
          ))}
          <Button
            isDisabled={value.groups.length >= 20}
            size="sm"
            variant="flat"
            onPress={() => {
              let i = value.groups.length + 1;

              while (value.groups.some((g) => g.name === `分组 ${i}`)) i++;
              onChange({
                ...value,
                groups: [
                  ...value.groups,
                  { name: `分组 ${i}`, type: "url-test", filter: "" },
                ],
              });
            }}
          >
            添加策略组
          </Button>
        </div>
      </details>

      <details className="rounded-xl border border-divider p-3">
        <summary className="cursor-pointer text-sm font-medium">
          策略组图标 · {iconSlots.length} 个
        </summary>
        <p className="my-3 text-xs text-default-500">
          Mihomo / Stash 写入 icon，Surge 写入 icon-url；sing-box
          不支持策略组图标。
          {!serviceGroups &&
            "关闭独立策略组时，只有节点选择和节点分组是策略组。"}
        </p>
        <div className="mb-3 flex flex-wrap items-center gap-2">
          <select
            aria-label="图标库"
            className={`${selectClass} min-w-40 flex-1`}
            disabled={!libraries.length}
            value={activeLibrary}
            onChange={(e) => setLibrary(e.target.value)}
          >
            {libraries.map((l) => (
              <option key={l.url} value={l.url}>
                {l.name}
                {l.builtin ? "（内置）" : ""}
              </option>
            ))}
          </select>
          <Button
            isDisabled={!activeLibrary}
            isLoading={applying}
            size="sm"
            variant="flat"
            onPress={applyLibrary}
          >
            一键套用
          </Button>
          <Button
            isDisabled={
              !Object.keys(value.icons ?? {}).length &&
              !value.groups.some((g) => g.icon)
            }
            size="sm"
            variant="light"
            onPress={() =>
              onChange({
                ...value,
                icons: undefined,
                groups: value.groups.map((g) => ({ ...g, icon: undefined })),
              })
            }
          >
            全部恢复默认
          </Button>
        </div>
        <div className="grid grid-cols-1 gap-1 sm:grid-cols-2">
          {iconSlots.map((slot) => {
            const icon = iconOf(slot.key);

            return (
              <button
                key={slot.key}
                className="flex items-center gap-2 rounded-lg px-2 py-1.5 text-left text-sm hover:bg-default-100"
                type="button"
                onClick={() => setPicking(slot.key)}
              >
                <GroupIcon size="h-7 w-7" src={icon || slot.fallback} />
                <span className="min-w-0 flex-1 truncate">{slot.name}</span>
                <span className="shrink-0 text-xs text-default-400">
                  {icon ? "自定义" : slot.fallback ? "默认" : "无图标"}
                </span>
              </button>
            );
          })}
        </div>
      </details>
      <IconPicker
        fallback={pickingSlot?.fallback}
        isOpen={Boolean(pickingSlot)}
        libraries={libraries}
        library={activeLibrary}
        title={pickingSlot?.name ?? ""}
        value={picking ? iconOf(picking) : ""}
        onClose={() => setPicking(null)}
        onLibraries={setLibraries}
        onLibrary={setLibrary}
        onPick={(url) => picking && setIcon(picking, url)}
      />

      <div className="flex items-center justify-between gap-3">
        <label className="text-sm font-medium" htmlFor="template-rule-final">
          未匹配流量（兜底）
        </label>
        <select
          className={`${selectClass} max-w-52`}
          id="template-rule-final"
          value={value.final}
          onChange={(e) => onChange({ ...value, final: e.target.value })}
        >
          {targetOptions(false)}
        </select>
      </div>
      <p className="text-xs text-default-500">
        规则集由客户端按天更新，面板无需增加后台服务。生成后可在下方预览配置。
      </p>
    </div>
  );
}

/** Ordered routing is the primary preview; groups only choose proxy nodes. */
export function TemplateRulePreview({
  value,
  catalog,
  serviceGroups,
}: {
  value: SubTemplateBuilder;
  catalog: SubTemplateCategory[];
  serviceGroups: SubTemplateServiceGroup[];
}) {
  const target = (to: string) => (
    <span
      className={`shrink-0 rounded-full px-2 py-1 text-xs ${to === "DIRECT" ? "bg-success/10 text-success" : to === "REJECT" ? "bg-danger/10 text-danger" : "bg-primary/10 text-primary"}`}
    >
      {to === "DIRECT" ? "直连" : to === "REJECT" ? "拦截" : `代理 · ${to}`}
    </span>
  );

  const routeTarget = (key: string, fallback: string) => {
    const group = serviceGroups.find((g) => g.key === key);

    return group ? (
      <span className="text-right text-xs">
        <span className="block text-primary">服务组 · {group.name}</span>
        <span className="text-default-500">
          默认：
          {group.target === "DIRECT"
            ? "直连"
            : group.target === "REJECT"
              ? "拦截"
              : group.target}
        </span>
      </span>
    ) : (
      target(fallback)
    );
  };

  return (
    <div className="overflow-hidden rounded-xl border border-divider">
      <div className="flex justify-between gap-2 bg-default-100/60 px-3 py-2 text-xs font-medium">
        <span>分流规则（从上往下匹配）</span>
        <span>去向</span>
      </div>
      {value.rules.map((r, i) => {
        const category =
          r.type === "category"
            ? catalog.find((c) => c.key === r.value)
            : undefined;

        return (
          <div
            key={`${i}-${r.type}`}
            className="flex items-center gap-2 border-t border-divider px-3 py-2"
          >
            <span className="w-5 shrink-0 text-xs text-default-400">
              {i + 1}
            </span>
            <div className="min-w-0 flex-1 text-xs">
              {category ? (
                <span className="flex items-center gap-1.5">
                  <GroupIcon
                    src={value.icons?.[category.key] || category.icon}
                  />
                  {category.name}
                </span>
              ) : (
                <>
                  <span className="text-default-500">
                    {RULE_TYPES.find(([k]) => k === r.type)?.[1] ?? r.type}{" "}
                    ·{" "}
                  </span>
                  <span className="break-all">{r.value}</span>
                </>
              )}
            </div>
            <span aria-hidden className="text-default-400">
              →
            </span>
            {routeTarget(r.type === "category" ? r.value : "", r.target)}
          </div>
        );
      })}
      <div className="flex items-center gap-2 border-t border-divider bg-default-100/40 px-3 py-2">
        <span className="min-w-0 flex-1 text-xs">
          <b>兜底</b> · 未命中以上规则的流量
        </span>
        <span aria-hidden className="text-default-400">
          →
        </span>
        {routeTarget("__final", value.final)}
      </div>
    </div>
  );
}
