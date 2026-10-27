import type {
  RemoteSourceItem,
  RemoteSourceRole,
  RemoteSyncResult,
} from "@/api";

import { useEffect, useRef, useState } from "react";
import toast from "react-hot-toast";
import { FileUp, RefreshCw } from "lucide-react";

import {
  deleteRemoteSource,
  refreshRemoteSource,
  saveRemoteSource,
} from "@/api";
import { Segmented, ToggleRow } from "@/pages/subscription/controls";
import { Button } from "@/shadcn-bridge/heroui/button";
import { Input } from "@/shadcn-bridge/heroui/input";
import {
  Modal,
  ModalBody,
  ModalContent,
  ModalFooter,
  ModalHeader,
} from "@/shadcn-bridge/heroui/modal";
import { formatTraffic } from "@/utils/traffic";

const modalBase =
  "!w-[calc(100%-32px)] !mx-auto sm:!w-full rounded-2xl overflow-hidden";

export const INTERVALS: Array<[string, string]> = [
  ["0", "手动"],
  ["6", "6 小时"],
  ["12", "12 小时"],
  ["24", "每天"],
];

export const SOURCE_ROLES: Array<[RemoteSourceRole, string]> = [
  ["", "普通节点"],
  ["relay", "中转（前置）"],
  ["landing", "落地"],
];

export const ROLE_HINTS: Record<string, string> = {
  "": "节点直接放进订阅，和自建节点一样用",
  relay:
    "机场节点做前置：新建订阅时整个源作为链式代理的中转，自建节点作为落地，节点改名、增删自动跟上",
  landing: "新建订阅时整个源作为链式代理的落地（出口）",
};

/** The same limit as the panel's subscription download. */
const MAX_FILE_BYTES = 5 << 20;

/** Reads an uploaded subscription file as text; null (with a toast) when it can't be used. */
export async function readSubscriptionFile(file: File): Promise<string | null> {
  if (file.size > MAX_FILE_BYTES) {
    toast.error("文件超过 5MB");

    return null;
  }
  const text = await file.text();

  if (!text.trim()) {
    toast.error("文件是空的");

    return null;
  }

  return text;
}

interface SourceDraft {
  id?: number;
  name: string;
  url: string;
  kind: "url" | "file";
  role: RemoteSourceRole;
  intervalHours: number;
  relayOnly: boolean;
  userAgent: string;
  /** A newly chosen file (file sources). */
  content?: string;
  fileName?: string;
}

const draftOf = (s: RemoteSourceItem): SourceDraft => ({
  id: s.id,
  name: s.name,
  url: s.url,
  kind: s.kind,
  role: s.role,
  intervalHours: s.intervalHours,
  relayOnly: s.relayOnly,
  userAgent: s.userAgent ?? "",
});

function ago(ms: number): string {
  if (!ms) return "还没更新过";
  const m = Math.floor((Date.now() - ms) / 60000);

  if (m < 1) return "刚刚更新";
  if (m < 60) return `${m} 分钟前更新`;
  if (m < 60 * 24) return `${Math.floor(m / 60)} 小时前更新`;

  return `${Math.floor(m / 1440)} 天前更新`;
}

/** The provider's plan from Subscription-Userinfo, e.g. "已用 12 GB / 200 GB · 2026-12-01 到期". */
function planText(userinfo: string): string {
  const v: Record<string, number> = {};

  for (const part of userinfo.split(";")) {
    const [k, n] = part.split("=").map((x) => x.trim());

    if (k && n && !Number.isNaN(Number(n))) v[k] = Number(n);
  }
  const parts: string[] = [];
  const used = (v.upload ?? 0) + (v.download ?? 0);

  if (v.total)
    parts.push(`已用 ${formatTraffic(used)} / ${formatTraffic(v.total)}`);
  if (v.expire) {
    parts.push(`${new Date(v.expire * 1000).toLocaleDateString("zh-CN")} 到期`);
  }

  return parts.join(" · ");
}

export function syncText(r: RemoteSyncResult): string {
  const parts = [`新增 ${r.added}`, `更新 ${r.updated}`, `移除 ${r.removed}`];

  if (r.kept) parts.push(`保留 ${r.kept} 个旧节点`);

  return parts.join("，");
}

/** Remote subscriptions whose nodes are kept as imported landings. */
export function RemoteSourcesSection({
  sources,
  onChanged,
}: {
  sources: RemoteSourceItem[];
  onChanged: () => Promise<void> | void;
}) {
  const [draft, setDraft] = useState<SourceDraft | null>(null);
  const [saving, setSaving] = useState(false);
  const [refreshing, setRefreshing] = useState<number | null>(null);
  const [deleting, setDeleting] = useState(false);
  // Deleting asks first, in the footer: with the nodes or keeping them.
  const [confirmDelete, setConfirmDelete] = useState(false);

  useEffect(() => setConfirmDelete(false), [draft?.id]);
  // A file source being updated from the list's upload button.
  const reupload = useRef<RemoteSourceItem | null>(null);
  const reuploadInput = useRef<HTMLInputElement>(null);

  const report = (res: {
    code: number;
    msg: string;
    data?: { result?: RemoteSyncResult } | unknown;
  }) => {
    const result = (res.data as { result?: RemoteSyncResult } | null)?.result;

    if (res.code !== 0) toast.error(res.msg || "更新失败");
    else if (result) toast.success(`已更新：${syncText(result)}`);
    else toast.success("已保存");
  };

  const refresh = async (s: RemoteSourceItem) => {
    setRefreshing(s.id);
    try {
      report(await refreshRemoteSource(s.id));
      await onChanged();
    } finally {
      setRefreshing(null);
    }
  };

  const uploadAgain = async (s: RemoteSourceItem, file: File) => {
    const content = await readSubscriptionFile(file);

    if (content === null) return;
    setRefreshing(s.id);
    try {
      report(await saveRemoteSource({ ...draftOf(s), content }));
      await onChanged();
    } finally {
      setRefreshing(null);
    }
  };

  const save = async () => {
    if (!draft) return;
    if (draft.kind === "file" && !draft.id && !draft.content) {
      toast.error("请选择订阅文件");

      return;
    }
    setSaving(true);
    try {
      const data = {
        id: draft.id,
        name: draft.name,
        url: draft.url,
        kind: draft.kind,
        role: draft.role,
        intervalHours: draft.intervalHours,
        relayOnly: draft.relayOnly,
        userAgent: draft.kind === "url" ? draft.userAgent.trim() : "",
        content: draft.kind === "file" ? draft.content : undefined,
      };
      const res = await saveRemoteSource(data);

      report(res);
      // A source whose first fetch failed is still saved; keep the editor
      // open only when nothing was stored.
      if (res.code === 0 || res.data) {
        setDraft(null);
        await onChanged();
      }
    } finally {
      setSaving(false);
    }
  };

  const remove = async (keepNodes: boolean) => {
    if (!draft?.id) return;
    setDeleting(true);
    try {
      const res = await deleteRemoteSource(draft.id, keepNodes);

      if (res.code !== 0) {
        toast.error(res.msg || "删除失败");

        return;
      }
      toast.success(
        keepNodes ? "已删除订阅源，节点保留" : "已删除订阅源和它的节点",
      );
      setDraft(null);
      await onChanged();
    } finally {
      setDeleting(false);
    }
  };

  const editing = sources.find((s) => s.id === draft?.id);

  return (
    <section className="space-y-2">
      <div className="flex items-center justify-between">
        <h2 className="text-sm font-semibold text-default-600">订阅源</h2>
        <Button
          size="sm"
          variant="light"
          onPress={() =>
            setDraft({
              name: "",
              url: "",
              kind: "url",
              role: "",
              intervalHours: 12,
              relayOnly: false,
              userAgent: "",
            })
          }
        >
          添加
        </Button>
      </div>
      {sources.length === 0 ? (
        <p className="rounded-xl border border-dashed border-divider px-3 py-4 text-center text-xs text-default-500">
          填机场或别人的订阅链接，或上传订阅文件：节点出现在“入站”里，可以给自建节点做前置中转，也能和自己的节点一起放进订阅。在“导入”里粘贴订阅地址并打开“定时同步”也会存到这里
        </p>
      ) : (
        <div className="grid grid-cols-1 gap-2 2xl:grid-cols-2">
          {sources.map((s) => {
            const plan = planText(s.userinfo);

            return (
              <div
                key={s.id}
                className="flex cursor-pointer items-center gap-3 rounded-xl border border-divider bg-content1 px-3 py-2.5 shadow-sm transition-colors hover:bg-default-100"
                role="button"
                tabIndex={0}
                onClick={() => setDraft(draftOf(s))}
                onKeyDown={(e) => {
                  if (e.target !== e.currentTarget) return;
                  if (e.key === "Enter" || e.key === " ") {
                    e.preventDefault();
                    (e.currentTarget as HTMLElement).click();
                  }
                }}
              >
                <span
                  className={`h-2 w-2 shrink-0 rounded-full ${
                    s.lastError
                      ? "bg-danger"
                      : s.lastFetch
                        ? "bg-success"
                        : "bg-default-300"
                  }`}
                />
                <div className="min-w-0 flex-1">
                  <div className="flex min-w-0 items-center gap-1.5">
                    <span className="truncate font-medium">{s.name}</span>
                    {s.role && (
                      <span
                        className={`shrink-0 rounded-full px-1.5 py-0.5 text-[10px] ${s.role === "relay" ? "bg-secondary-100 text-secondary-700 dark:bg-secondary-500/20" : "bg-warning-100 text-warning-700 dark:bg-warning-500/20"}`}
                      >
                        {s.role === "relay" ? "中转" : "落地"}
                      </span>
                    )}
                  </div>
                  <div className="truncate text-xs text-default-500">
                    {s.nodeCount} 个节点 ·{" "}
                    {s.kind === "file"
                      ? s.lastFetch
                        ? ago(s.lastFetch).replace("更新", "上传")
                        : "还没上传过"
                      : ago(s.lastFetch)}
                    {s.kind === "file"
                      ? " · 文件"
                      : s.intervalHours
                        ? ` · 每 ${s.intervalHours} 小时`
                        : " · 手动更新"}
                  </div>
                  {s.lastError ? (
                    <div className="truncate text-xs text-danger">
                      {s.lastError}
                    </div>
                  ) : (
                    plan && (
                      <div className="truncate text-xs text-default-400">
                        {plan}
                      </div>
                    )
                  )}
                </div>
                <div
                  className="shrink-0"
                  role="presentation"
                  onClick={(e) => e.stopPropagation()}
                >
                  <Button
                    isIconOnly
                    aria-label={
                      s.kind === "file"
                        ? `重新上传 ${s.name}`
                        : `更新 ${s.name}`
                    }
                    isLoading={refreshing === s.id}
                    size="sm"
                    variant="flat"
                    onPress={() => {
                      if (s.kind !== "file") {
                        void refresh(s);

                        return;
                      }
                      reupload.current = s;
                      reuploadInput.current?.click();
                    }}
                  >
                    {s.kind === "file" ? (
                      <FileUp className="h-4 w-4" />
                    ) : (
                      <RefreshCw className="h-4 w-4" />
                    )}
                  </Button>
                </div>
              </div>
            );
          })}
        </div>
      )}
      <input
        ref={reuploadInput}
        className="hidden"
        type="file"
        onChange={(e) => {
          const file = e.target.files?.[0];
          const s = reupload.current;

          e.target.value = "";
          if (file && s) void uploadAgain(s, file);
        }}
      />

      <Modal
        backdrop="blur"
        classNames={{ base: modalBase }}
        isOpen={Boolean(draft)}
        placement="center"
        scrollBehavior="inside"
        onOpenChange={(open) => !open && setDraft(null)}
      >
        <ModalContent>
          {(onClose) =>
            draft && (
              <>
                <ModalHeader>
                  <h2 className="text-lg font-bold">
                    {draft.id ? "编辑订阅源" : "添加订阅源"}
                  </h2>
                </ModalHeader>
                <ModalBody className="space-y-4">
                  <Input
                    label="名称"
                    placeholder="例如 某某机场"
                    value={draft.name}
                    variant="bordered"
                    onChange={(e) =>
                      setDraft({ ...draft, name: e.target.value })
                    }
                  />
                  <Segmented
                    options={[
                      ["url", "订阅地址"],
                      ["file", "上传文件"],
                    ]}
                    value={draft.kind}
                    onChange={(kind) =>
                      setDraft({ ...draft, kind: kind as SourceDraft["kind"] })
                    }
                  />
                  {draft.kind === "url" ? (
                    <Input
                      label="订阅地址"
                      placeholder="https://..."
                      value={draft.url}
                      variant="bordered"
                      onChange={(e) =>
                        setDraft({ ...draft, url: e.target.value })
                      }
                    />
                  ) : (
                    <FilePick
                      fileName={draft.fileName}
                      hint={
                        draft.id && editing?.kind === "file"
                          ? "不选文件就保留现在的节点；选了文件就按新文件更新"
                          : "订阅文件：Base64 订阅、分享链接、Clash / Mihomo、sing-box、Surge / Loon 节点，最大 5MB"
                      }
                      onFile={async (file) => {
                        const content = await readSubscriptionFile(file);

                        if (content !== null) {
                          setDraft({
                            ...draft,
                            content,
                            fileName: file.name,
                            name:
                              draft.name || file.name.replace(/\.[^.]+$/, ""),
                          });
                        }
                      }}
                    />
                  )}
                  {draft.kind === "url" && (
                    <div className="space-y-1.5">
                      <p className="text-sm font-medium">自动更新</p>
                      <Segmented
                        options={INTERVALS}
                        value={String(draft.intervalHours)}
                        onChange={(v) =>
                          setDraft({ ...draft, intervalHours: Number(v) })
                        }
                      />
                    </div>
                  )}
                  {draft.kind === "url" && (
                    <Input
                      description="拉取订阅时发送的 UA。有的机场按 UA 决定返回的格式或节点，拿不到节点或缺节点时可以换成客户端的 UA，比如 mihomo/1.19.0、Shadowrocket/2070；留空用默认的 clash.meta/1.19.0"
                      label="User-Agent（可选）"
                      placeholder="clash.meta/1.19.0"
                      value={draft.userAgent}
                      variant="bordered"
                      onChange={(e) =>
                        setDraft({ ...draft, userAgent: e.target.value })
                      }
                    />
                  )}
                  <p className="text-xs text-default-500">
                    {draft.kind === "url" ? "更新" : "重新上传"}
                    时按名称对上已有节点：改了地址就跟着改（中转也跟着改），没了就移除；挂着中转的会保留
                  </p>
                  <div className="space-y-1.5">
                    <p className="text-sm font-medium">用途</p>
                    <Segmented
                      options={SOURCE_ROLES}
                      value={draft.role}
                      onChange={(role) =>
                        setDraft({
                          ...draft,
                          role: role as RemoteSourceRole,
                        })
                      }
                    />
                    <p className="text-xs text-default-500">
                      {ROLE_HINTS[draft.role]}
                    </p>
                  </div>
                  <ToggleRow
                    hint="新加进来的节点不直接放进订阅，只通过你自己机器上的转发（中转线路）连接"
                    id="source-relay-only"
                    label="只通过我的转发访问"
                    value={draft.relayOnly}
                    onChange={(relayOnly) => setDraft({ ...draft, relayOnly })}
                  />
                  {editing && (
                    <p className="text-xs text-default-500">
                      当前 {editing.nodeCount} 个节点 · {ago(editing.lastFetch)}
                    </p>
                  )}
                </ModalBody>
                <ModalFooter className="flex-wrap">
                  {confirmDelete ? (
                    <>
                      <p className="w-full text-sm text-danger">
                        删除这个订阅源？它导入的节点可以保留，也可以一起删除
                      </p>
                      <Button
                        variant="light"
                        onPress={() => setConfirmDelete(false)}
                      >
                        取消
                      </Button>
                      <Button
                        isDisabled={deleting}
                        variant="flat"
                        onPress={() => remove(true)}
                      >
                        保留节点
                      </Button>
                      <Button
                        color="danger"
                        isLoading={deleting}
                        onPress={() => remove(false)}
                      >
                        一起删除
                      </Button>
                    </>
                  ) : (
                    <>
                      {draft.id && (
                        <Button
                          className="mr-auto"
                          color="danger"
                          variant="light"
                          onPress={() => setConfirmDelete(true)}
                        >
                          删除
                        </Button>
                      )}
                      <Button variant="light" onPress={onClose}>
                        取消
                      </Button>
                      <Button color="primary" isLoading={saving} onPress={save}>
                        {draft.id
                          ? "保存"
                          : draft.kind === "file"
                            ? "添加"
                            : "添加并更新"}
                      </Button>
                    </>
                  )}
                </ModalFooter>
              </>
            )
          }
        </ModalContent>
      </Modal>
    </section>
  );
}

/** A file picker row: the chosen file's name, or a prompt. */
export function FilePick({
  fileName,
  hint,
  onFile,
}: {
  fileName?: string;
  hint?: string;
  onFile: (file: File) => void;
}) {
  const input = useRef<HTMLInputElement>(null);

  return (
    <div className="space-y-1">
      <button
        className="flex w-full items-center gap-2 rounded-xl border border-dashed border-divider px-3 py-3 text-left text-sm hover:bg-default-100"
        type="button"
        onClick={() => input.current?.click()}
      >
        <FileUp className="h-4 w-4 shrink-0 text-default-500" />
        <span
          className={`min-w-0 flex-1 truncate ${fileName ? "" : "text-default-500"}`}
        >
          {fileName || "选择订阅文件"}
        </span>
        {fileName && (
          <span className="shrink-0 text-xs text-primary">更换</span>
        )}
      </button>
      {hint && <p className="text-xs text-default-500">{hint}</p>}
      <input
        ref={input}
        className="hidden"
        type="file"
        onChange={(e) => {
          const file = e.target.files?.[0];

          e.target.value = "";
          if (file) onFile(file);
        }}
      />
    </div>
  );
}
