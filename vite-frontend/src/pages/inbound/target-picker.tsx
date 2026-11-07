import type { InboundApiItem } from "@/api/types";

import { useState } from "react";
import {
  Ban,
  Check,
  ChevronLeft,
  ChevronRight,
  Search,
  Waypoints,
} from "lucide-react";

import {
  Modal,
  ModalBody,
  ModalContent,
  ModalHeader,
} from "@/shadcn-bridge/heroui/modal";

/** Where traffic goes: "direct", "block" or an inbound id. */
export type TargetKey = string;

const SEARCH_FROM = 8;

export function targetSource(i: InboundApiItem): string {
  if (!i.external) return i.nodeName || "服务器";

  return i.sourceName ? `订阅源 · ${i.sourceName}` : "导入";
}

function TargetIcon({ k }: { k: TargetKey }) {
  if (k === "block") return <Ban className="h-4 w-4 text-danger" />;
  if (k === "direct") return <Waypoints className="h-4 w-4 text-success" />;

  return null;
}

/** The current choice, shown as a tappable row. */
export function TargetButton({
  value,
  targets,
  missing,
  onPress,
}: {
  value: TargetKey;
  targets: InboundApiItem[];
  missing?: boolean;
  onPress: () => void;
}) {
  const t = targets.find((i) => String(i.id) === value);
  const title =
    value === "direct"
      ? "直连"
      : value === "block"
        ? "阻断"
        : missing
          ? "已删除的节点"
          : (t?.name ?? "选择出站节点");

  return (
    <button
      className={`flex w-full items-center gap-2.5 rounded-xl border px-3 py-2.5 text-left transition-colors active:bg-default-100 ${
        missing ? "border-danger/50" : "border-divider bg-content1"
      }`}
      type="button"
      onClick={onPress}
    >
      <TargetIcon k={value} />
      <span className="min-w-0 flex-1">
        <span
          className={`block truncate text-sm ${
            !value ? "text-default-400" : missing ? "text-danger" : ""
          }`}
        >
          {title}
        </span>
        {t && (
          <span className="block truncate text-xs text-default-400">
            {targetSource(t)}
          </span>
        )}
      </span>
      <ChevronRight className="h-4 w-4 shrink-0 text-default-400" />
    </button>
  );
}

/** A list of exits to pick from, opened over the 分流 dialog. */
export function TargetPicker({
  isOpen,
  title,
  value,
  targets,
  withBlock,
  onPick,
  onClose,
}: {
  isOpen: boolean;
  title: string;
  value: TargetKey;
  targets: InboundApiItem[];
  withBlock?: boolean;
  onPick: (k: TargetKey) => void;
  onClose: () => void;
}) {
  const [query, setQuery] = useState("");
  const q = query.trim().toLowerCase();
  const shown = q
    ? targets.filter((t) =>
        `${t.name} ${targetSource(t)}`.toLowerCase().includes(q),
      )
    : targets;
  const groups = [...new Set(shown.map(targetSource))];

  const close = () => {
    setQuery("");
    onClose();
  };

  const row = (k: TargetKey, name: string, sub?: string) => (
    <button
      key={k}
      className="flex w-full items-center gap-2.5 rounded-xl px-3 py-2.5 text-left active:bg-default-100"
      type="button"
      onClick={() => {
        onPick(k);
        close();
      }}
    >
      <TargetIcon k={k} />
      <span className="min-w-0 flex-1">
        <span className="block truncate text-sm">{name}</span>
        {sub && (
          <span className="block truncate text-xs text-default-400">{sub}</span>
        )}
      </span>
      {value === k && <Check className="h-4 w-4 shrink-0 text-primary" />}
    </button>
  );

  return (
    <Modal
      classNames={{
        base: "!w-[calc(100%-32px)] !mx-auto sm:!w-full overflow-hidden",
      }}
      isOpen={isOpen}
      placement="center"
      scrollBehavior="inside"
      size="md"
      onOpenChange={(open) => !open && close()}
    >
      <ModalContent>
        {/* The list can outgrow a phone screen: keep a way back and the
            search box above it, not inside the scrolled body. */}
        <ModalHeader className="flex items-center gap-2">
          <button
            aria-label="返回"
            className="rounded-lg p-1 text-default-500 hover:bg-default-100"
            type="button"
            onClick={close}
          >
            <ChevronLeft className="h-5 w-5" />
          </button>
          <h2 className="text-lg font-bold">{title}</h2>
        </ModalHeader>
        {targets.length >= SEARCH_FROM && (
          <label className="flex shrink-0 items-center gap-2 rounded-xl border border-divider bg-content1 px-3 py-2">
            <Search className="h-4 w-4 text-default-400" />
            <input
              className="min-w-0 flex-1 bg-transparent text-sm outline-none"
              placeholder="搜索节点"
              value={query}
              onChange={(e) => setQuery(e.target.value)}
            />
          </label>
        )}
        <ModalBody className="space-y-1 pb-4">
          {!q && (
            <div className="rounded-xl bg-content1">
              {row("direct", "直连", "从本服务器出去")}
              {withBlock && row("block", "阻断", "丢弃匹配的流量")}
            </div>
          )}
          {groups.map((g) => (
            <div key={g} className="pt-2">
              <p className="px-3 pb-1 text-xs text-default-400">{g}</p>
              <div className="rounded-xl bg-content1">
                {shown
                  .filter((t) => targetSource(t) === g)
                  .map((t) => row(String(t.id), t.name))}
              </div>
            </div>
          ))}
          {q && shown.length === 0 && (
            <p className="py-6 text-center text-sm text-default-400">
              没有匹配的节点
            </p>
          )}
        </ModalBody>
      </ModalContent>
    </Modal>
  );
}
