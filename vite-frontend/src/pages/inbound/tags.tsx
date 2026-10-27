import { useEffect, useState } from "react";
import toast from "react-hot-toast";
import { X } from "lucide-react";

import { setInboundTags } from "@/api";
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

const modalBase =
  "!w-[calc(100%-32px)] !mx-auto sm:!w-full rounded-2xl overflow-hidden";

/** Offered before the user has tags of their own. */
const PRESET_TAGS = ["出站", "落地", "中转", "备用"];

/** The same limits as the backend. */
const MAX_TAGS = 10;
const MAX_TAG_LEN = 20;

/** Every tag in use, most used first. */
export function collectTags(items: Array<{ tags?: string[] }>): string[] {
  const count = new Map<string, number>();

  for (const i of items) {
    for (const t of i.tags ?? []) count.set(t, (count.get(t) ?? 0) + 1);
  }

  return [...count.entries()]
    .sort((a, b) => b[1] - a[1] || a[0].localeCompare(b[0], "zh-CN"))
    .map(([t]) => t);
}

export function TagChips({ tags }: { tags?: string[] }) {
  return (
    <>
      {(tags ?? []).map((t) => (
        <Chip
          key={t}
          className="shrink-0 whitespace-nowrap"
          color="success"
          size="sm"
          variant="flat"
        >
          {t}
        </Chip>
      ))}
    </>
  );
}

/** Picks tags: type and press Enter (or a comma), or tap a suggestion. */
export function TagInput({
  value,
  onChange,
  suggestions,
  label = "标签",
}: {
  value: string[];
  onChange: (tags: string[]) => void;
  suggestions: string[];
  label?: string;
}) {
  const [draft, setDraft] = useState("");

  const add = (raw: string) => {
    const next = [...value];

    for (const part of raw.split(/[,，]/)) {
      const t = part.trim();

      if (!t || next.includes(t)) continue;
      if ([...t].length > MAX_TAG_LEN) {
        toast.error(`标签不超过 ${MAX_TAG_LEN} 字`);
        continue;
      }
      if (next.length >= MAX_TAGS) {
        toast.error(`最多 ${MAX_TAGS} 个标签`);
        break;
      }
      next.push(t);
    }
    onChange(next);
    setDraft("");
  };

  const offered = [...new Set([...suggestions, ...PRESET_TAGS])].filter(
    (t) => !value.includes(t),
  );

  return (
    <div className="space-y-2">
      <Input
        description="输入后按回车添加，可以加多个，比如“出站”“落地”“香港”"
        label={label}
        placeholder="新标签"
        value={draft}
        variant="bordered"
        onBlur={() => draft.trim() && add(draft)}
        onChange={(e) => {
          const v = e.target.value;

          if (/[,，]/.test(v)) add(v);
          else setDraft(v);
        }}
        onKeyDown={(e) => {
          if (e.key === "Enter" && !e.nativeEvent.isComposing) {
            e.preventDefault();
            add(draft);
          }
        }}
      />
      {value.length > 0 && (
        <div className="flex flex-wrap gap-1.5">
          {value.map((t) => (
            <span
              key={t}
              className="inline-flex items-center gap-1 rounded-full bg-success-50 py-0.5 pl-2.5 pr-1 text-xs text-success-700 dark:bg-success-900/30 dark:text-success-400"
            >
              {t}
              <button
                aria-label={`移除标签 ${t}`}
                className="rounded-full p-0.5 hover:bg-success-100 dark:hover:bg-success-900/50"
                type="button"
                onClick={() => onChange(value.filter((v) => v !== t))}
              >
                <X className="h-3 w-3" />
              </button>
            </span>
          ))}
        </div>
      )}
      {offered.length > 0 && (
        <div className="flex flex-wrap gap-1.5">
          {offered.slice(0, 12).map((t) => (
            <button
              key={t}
              className="rounded-full border border-dashed border-default-300 px-2.5 py-0.5 text-xs text-default-600 hover:border-success hover:text-success"
              type="button"
              onClick={() => add(t)}
            >
              + {t}
            </button>
          ))}
        </div>
      )}
    </div>
  );
}

/** Sets the tags of one inbound, or of several at once (replacing theirs). */
export function TagsModal({
  target,
  suggestions,
  onClose,
  onSaved,
}: {
  target: { ids: number[]; tags: string[]; title: string } | null;
  suggestions: string[];
  onClose: () => void;
  onSaved: () => void;
}) {
  const [tags, setTags] = useState<string[]>([]);
  const [saving, setSaving] = useState(false);

  useEffect(() => {
    setTags(target?.tags ?? []);
  }, [target]);

  const submit = async () => {
    if (!target) return;
    setSaving(true);
    try {
      const res = await setInboundTags(target.ids, tags);

      if (res.code !== 0) {
        toast.error(res.msg || "保存失败");

        return;
      }
      toast.success("已保存");
      onSaved();
      onClose();
    } finally {
      setSaving(false);
    }
  };

  return (
    <Modal
      backdrop="blur"
      classNames={{ base: modalBase }}
      isOpen={Boolean(target)}
      placement="center"
      onOpenChange={(open) => !open && onClose()}
    >
      <ModalContent>
        <ModalHeader className="flex flex-col gap-1">
          <h2 className="text-lg font-bold">标签</h2>
          <p className="text-small font-normal text-default-500">
            {target?.title}
            {target && target.ids.length > 1
              ? "：会替换它们原来的标签"
              : "：只用来在列表里区分和筛选，不影响订阅"}
          </p>
        </ModalHeader>
        <ModalBody>
          <TagInput suggestions={suggestions} value={tags} onChange={setTags} />
        </ModalBody>
        <ModalFooter>
          <Button variant="light" onPress={onClose}>
            取消
          </Button>
          <Button color="primary" isLoading={saving} onPress={submit}>
            保存
          </Button>
        </ModalFooter>
      </ModalContent>
    </Modal>
  );
}
