import type { InboundApiItem, SplitCategory } from "@/api/types";

import { useState } from "react";
import { ArrowDown, ArrowUp, ChevronDown, Plus, Trash2 } from "lucide-react";

import { TargetButton } from "./target-picker";

import { Button } from "@/shadcn-bridge/heroui/button";
import { Input } from "@/shadcn-bridge/heroui/input";

/** A 分流 rule as edited: domains and IPs are free text. */
export interface SplitRuleForm {
  categories: string[];
  domains: string;
  ips: string;
  action: "direct" | "block" | "proxy";
  /** Target inbound for "proxy". */
  outboundId: string;
}

export const emptySplitRule = (): SplitRuleForm => ({
  categories: [],
  domains: "",
  ips: "",
  action: "proxy",
  outboundId: "",
});

/** The picker key of a rule's target. */
export const ruleTarget = (r: SplitRuleForm) =>
  r.action === "proxy" ? r.outboundId : r.action;

export const ruleFromTarget = (key: string): Partial<SplitRuleForm> =>
  key === "direct" || key === "block"
    ? { action: key, outboundId: "" }
    : { action: "proxy", outboundId: key };

const chipClass = (on: boolean) =>
  `rounded-full border px-3 py-1 text-xs transition-colors ${
    on
      ? "border-primary bg-primary text-primary-foreground"
      : "border-divider bg-content1 text-default-600"
  }`;

const countEntries = (v: string) => v.split(/[\s,，]+/).filter(Boolean).length;

function matchSummary(r: SplitRuleForm, categories: SplitCategory[]): string {
  const parts = r.categories.map(
    (k) => categories.find((c) => c.key === k)?.label ?? k,
  );
  const domains = countEntries(r.domains);
  const ips = countEntries(r.ips);

  if (domains) parts.push(`${domains} 个域名`);
  if (ips) parts.push(`${ips} 个 IP`);

  return parts.join("、");
}

/**
 * 分流 rules of an inbound, tried in order before its default exit. Each
 * rule folds to one line; tapping it opens it for editing.
 */
export function SplitRulesEditor({
  rules,
  onChange,
  categories,
  targets,
  onChooseTarget,
}: {
  rules: SplitRuleForm[];
  onChange: (rules: SplitRuleForm[]) => void;
  categories: SplitCategory[];
  /** Inbounds traffic can be sent through. */
  targets: InboundApiItem[];
  /** Open the target picker for rule i. */
  onChooseTarget: (i: number) => void;
}) {
  // The rule being edited; a new rule opens right away.
  const [open, setOpen] = useState<number | null>(null);
  const [customOpen, setCustomOpen] = useState<Set<number>>(new Set());
  const update = (i: number, patch: Partial<SplitRuleForm>) =>
    onChange(rules.map((r, j) => (j === i ? { ...r, ...patch } : r)));
  const move = (i: number, d: number) => {
    const next = [...rules];

    [next[i], next[i + d]] = [next[i + d], next[i]];
    onChange(next);
    setOpen(i + d);
  };
  const groups = [...new Set(categories.map((c) => c.group))];

  return (
    <div className="space-y-2">
      {rules.length === 0 && (
        <div className="rounded-xl border border-dashed border-divider px-4 py-6 text-center text-sm text-default-500">
          还没有分流规则，所有流量走默认去向
        </div>
      )}
      {rules.map((r, i) => {
        const target = ruleTarget(r);
        const missing =
          r.action === "proxy" &&
          r.outboundId !== "" &&
          !targets.some((t) => String(t.id) === r.outboundId);
        const match = matchSummary(r, categories);
        const targetName =
          r.action === "direct"
            ? "直连"
            : r.action === "block"
              ? "阻断"
              : missing
                ? "已删除的节点"
                : (targets.find((t) => String(t.id) === r.outboundId)?.name ??
                  "未选择去向");
        const isOpen = open === i;

        return (
          <div
            key={i}
            className={`overflow-hidden rounded-xl border bg-content1 ${
              isOpen ? "border-primary/40" : "border-divider"
            }`}
          >
            <button
              aria-expanded={isOpen}
              className="flex w-full items-center gap-3 px-3 py-2.5 text-left"
              type="button"
              onClick={() => setOpen(isOpen ? null : i)}
            >
              <span className="flex h-6 w-6 shrink-0 items-center justify-center rounded-full bg-primary/10 text-xs font-semibold text-primary">
                {i + 1}
              </span>
              <span className="min-w-0 flex-1">
                <span
                  className={`block truncate text-sm ${
                    match ? "font-medium" : "text-default-400"
                  }`}
                >
                  {match || "未选择匹配内容"}
                </span>
                <span
                  className={`block truncate text-xs ${
                    r.action === "block" || missing || !target
                      ? "text-danger"
                      : "text-default-500"
                  }`}
                >
                  → {targetName}
                </span>
              </span>
              <ChevronDown
                className={`h-4 w-4 shrink-0 text-default-400 transition-transform ${
                  isOpen ? "rotate-180" : ""
                }`}
              />
            </button>
            {isOpen && (
              <div className="space-y-3 border-t border-divider px-3 pb-3 pt-3">
                {groups.map((g) => (
                  <div key={g} className="space-y-1.5">
                    <p className="text-[11px] text-default-400">{g}</p>
                    <div className="flex flex-wrap gap-1.5">
                      {categories
                        .filter((c) => c.group === g)
                        .map((c) => {
                          const on = r.categories.includes(c.key);

                          return (
                            <button
                              key={c.key}
                              aria-pressed={on}
                              className={chipClass(on)}
                              type="button"
                              onClick={() =>
                                update(i, {
                                  categories: on
                                    ? r.categories.filter((k) => k !== c.key)
                                    : [...r.categories, c.key],
                                })
                              }
                            >
                              {c.label}
                            </button>
                          );
                        })}
                    </div>
                  </div>
                ))}
                {r.domains || r.ips || customOpen.has(i) ? (
                  <div className="grid grid-cols-1 gap-2 sm:grid-cols-2">
                    <Input
                      label="自定义域名"
                      placeholder="example.com，含子域名，逗号分隔"
                      size="sm"
                      value={r.domains}
                      variant="bordered"
                      onChange={(e) => update(i, { domains: e.target.value })}
                    />
                    <Input
                      label="自定义 IP"
                      placeholder="1.2.3.4、1.2.3.0/24，逗号分隔"
                      size="sm"
                      value={r.ips}
                      variant="bordered"
                      onChange={(e) => update(i, { ips: e.target.value })}
                    />
                  </div>
                ) : (
                  <button
                    className="text-xs text-primary"
                    type="button"
                    onClick={() =>
                      setCustomOpen((prev) => new Set(prev).add(i))
                    }
                  >
                    + 自定义域名 / IP
                  </button>
                )}
                <div className="space-y-1.5">
                  <p className="text-[11px] text-default-400">去向</p>
                  <TargetButton
                    missing={missing}
                    targets={targets}
                    value={target}
                    onPress={() => onChooseTarget(i)}
                  />
                  {missing && (
                    <p className="text-xs text-danger">
                      所选节点已被删除，匹配的流量会被丢弃，请重新选择
                    </p>
                  )}
                </div>
                <div className="flex items-center justify-end gap-1 pt-1">
                  <Button
                    isIconOnly
                    aria-label="上移"
                    isDisabled={i === 0}
                    size="sm"
                    variant="light"
                    onPress={() => move(i, -1)}
                  >
                    <ArrowUp className="h-3.5 w-3.5" />
                  </Button>
                  <Button
                    isIconOnly
                    aria-label="下移"
                    isDisabled={i === rules.length - 1}
                    size="sm"
                    variant="light"
                    onPress={() => move(i, 1)}
                  >
                    <ArrowDown className="h-3.5 w-3.5" />
                  </Button>
                  <Button
                    color="danger"
                    size="sm"
                    startContent={<Trash2 className="h-3.5 w-3.5" />}
                    variant="light"
                    onPress={() => {
                      onChange(rules.filter((_, j) => j !== i));
                      setOpen(null);
                    }}
                  >
                    删除
                  </Button>
                </div>
              </div>
            )}
          </div>
        );
      })}
      <button
        className="flex w-full items-center justify-center gap-1.5 rounded-xl border border-dashed border-primary/40 py-2.5 text-sm text-primary active:bg-primary/5"
        type="button"
        onClick={() => {
          onChange([...rules, emptySplitRule()]);
          setOpen(rules.length);
        }}
      >
        <Plus className="h-4 w-4" />
        添加分流规则
      </button>
    </div>
  );
}
