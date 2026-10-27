import type { RemoteSourceRole, RemoteSyncResult } from "@/api";
import type { InboundApiItem } from "@/api/types";

import { useEffect, useState } from "react";
import toast from "react-hot-toast";

import { importInbounds, saveRemoteSource, updateExternalInbound } from "@/api";
import {
  FilePick,
  INTERVALS,
  ROLE_HINTS,
  SOURCE_ROLES,
  readSubscriptionFile,
  syncText,
} from "@/pages/inbound/sources";
import {
  type ManualNode,
  ManualNodeFields,
  emptyManualNode,
  manualNodeLink,
} from "@/pages/inbound/manual-node";
import { TagInput } from "@/pages/inbound/tags";
import { Segmented } from "@/pages/subscription/controls";
import { Button } from "@/shadcn-bridge/heroui/button";
import { Checkbox } from "@/shadcn-bridge/heroui/checkbox";
import { Input, Textarea } from "@/shadcn-bridge/heroui/input";
import {
  Modal,
  ModalBody,
  ModalContent,
  ModalFooter,
  ModalHeader,
} from "@/shadcn-bridge/heroui/modal";
import { Select, SelectItem } from "@/shadcn-bridge/heroui/select";
import { Switch } from "@/shadcn-bridge/heroui/switch";

const modalBase =
  "!w-[calc(100%-32px)] !mx-auto sm:!w-full rounded-2xl overflow-hidden";

/** Label shown where a panel-managed inbound shows its node or kernel. */
export const EXTERNAL_LABEL = "导入的节点";

/** A lone http(s) URL: a subscription that can be kept in sync. */
function subscriptionURL(text: string): URL | null {
  const t = text.trim();

  if (!/^https?:\/\/\S+$/i.test(t)) return null;
  try {
    return new URL(t);
  } catch {
    return null;
  }
}

type ImportMode = "paste" | "manual";

const IMPORT_MODES: Array<[ImportMode, string]> = [
  ["paste", "粘贴链接 / 订阅"],
  ["manual", "手动填写"],
];

export function ImportModal({
  isOpen,
  ssMethods,
  tagSuggestions,
  onClose,
  onImported,
  onSourceAdded,
}: {
  isOpen: boolean;
  ssMethods: string[];
  tagSuggestions: string[];
  onClose: () => void;
  onImported: () => void;
  /** A subscription URL was saved as a synced source. */
  onSourceAdded: () => void;
}) {
  const [mode, setMode] = useState<ImportMode>("paste");
  const [manual, setManual] = useState<ManualNode>(emptyManualNode);
  const [tags, setTags] = useState<string[]>([]);
  const [links, setLinks] = useState("");
  const [relayOnly, setRelayOnly] = useState(false);
  const [errors, setErrors] = useState<string[]>([]);
  const [saving, setSaving] = useState(false);
  const [sync, setSync] = useState(true);
  const [sourceName, setSourceName] = useState("");
  const [intervalHours, setIntervalHours] = useState("12");
  const [role, setRole] = useState<RemoteSourceRole>("");
  // The uploaded file the text came from, if any.
  const [fileName, setFileName] = useState("");

  useEffect(() => {
    if (isOpen) {
      setMode("paste");
      setManual(emptyManualNode());
      setTags([]);
      setLinks("");
      setRole("");
      setFileName("");
      setRelayOnly(false);
      setErrors([]);
      setSync(true);
      setSourceName("");
      setIntervalHours("12");
    }
  }, [isOpen]);

  const manualMode = mode === "manual";
  const subURL = manualMode ? null : subscriptionURL(links);
  const fromFile = !manualMode && Boolean(fileName) && !subURL;
  const syncing = (Boolean(subURL) || fromFile) && sync;

  const addSource = async () => {
    if (!subURL && !fromFile) return;
    setSaving(true);
    try {
      const res = await saveRemoteSource(
        subURL
          ? {
              name: sourceName.trim() || subURL.hostname,
              url: subURL.toString(),
              intervalHours: Number(intervalHours),
              relayOnly,
              role,
            }
          : {
              name: sourceName.trim() || fileName.replace(/\.[^.]+$/, ""),
              url: "",
              kind: "file",
              content: links,
              intervalHours: 0,
              relayOnly,
              role,
            },
      );
      const result = (res.data as { result?: RemoteSyncResult } | null)?.result;

      // A source whose first fetch failed is still saved.
      if (res.code !== 0 && !res.data) {
        toast.error(res.msg || "添加失败");

        return;
      }
      if (res.code !== 0)
        toast(res.msg || "已保存，但更新失败", { icon: "⚠️" });
      else toast.success(result ? `已添加：${syncText(result)}` : "已添加");
      onSourceAdded();
      onClose();
    } finally {
      setSaving(false);
    }
  };

  const submit = async () => {
    if (syncing) {
      await addSource();

      return;
    }
    let text = links;

    if (manualMode) {
      const built = manualNodeLink(manual);

      if (!built.link) {
        setErrors([]);
        toast.error(built.error ?? "请检查填写的内容");

        return;
      }
      text = built.link;
    }
    setSaving(true);
    try {
      const res = await importInbounds(text, relayOnly, tags);
      const failed = res.data?.errors ?? [];

      setErrors(failed);
      if (res.code !== 0) {
        if (failed.length === 0) toast.error(res.msg || "导入失败");

        return;
      }
      const created = res.data?.created.length ?? 0;

      onImported();
      if (failed.length === 0) {
        toast.success(manualMode ? "已添加节点" : `已导入 ${created} 个节点`);
        onClose();
      } else {
        toast(`已导入 ${created} 个，${failed.length} 个失败`, { icon: "⚠️" });
      }
    } finally {
      setSaving(false);
    }
  };

  return (
    <Modal
      backdrop="blur"
      classNames={{ base: modalBase }}
      isOpen={isOpen}
      placement="center"
      scrollBehavior="inside"
      size="lg"
      onOpenChange={(open) => !open && onClose()}
    >
      <ModalContent>
        <ModalHeader className="flex flex-col gap-1">
          <h2 className="text-lg font-bold">导入节点</h2>
          <p className="text-small font-normal text-default-500">
            导入别处搭好的节点或机场订阅，用来对接中转和加入订阅。面板不会部署或修改这些节点。
          </p>
        </ModalHeader>
        <ModalBody>
          <Segmented
            options={IMPORT_MODES}
            value={mode}
            onChange={(m) => {
              setMode(m);
              setErrors([]);
            }}
          />
          {manualMode ? (
            <ManualNodeFields
              ssMethods={ssMethods}
              value={manual}
              onChange={setManual}
            />
          ) : (
            <>
              <FilePick
                fileName={fileName}
                onFile={async (file) => {
                  const text = await readSubscriptionFile(file);

                  if (text !== null) {
                    setLinks(text);
                    setFileName(file.name);
                  }
                }}
              />
              <Textarea
                description="支持：分享链接（ss / vmess / vless / trojan / hysteria2 / tuic / anytls / socks5，每行一个）、base64 订阅、Clash / Mihomo 配置、sing-box 配置、Surge / Loon 节点行，或直接填订阅地址（https://...）"
                label="节点或订阅"
                minRows={5}
                placeholder={"vless://...\nss://...\n或 https://订阅地址"}
                value={links}
                variant="bordered"
                onChange={(e) => setLinks(e.target.value)}
              />
              {(subURL || fromFile) && (
                <div className="space-y-3 rounded-xl border border-divider p-3">
                  <div className="flex items-start gap-2">
                    <Switch
                      isSelected={sync}
                      size="sm"
                      onValueChange={setSync}
                    />
                    <div>
                      <p className="text-sm">
                        {subURL ? "定时同步" : "存为订阅源"}
                      </p>
                      <p className="text-xs text-default-500">
                        {!sync
                          ? "只导入一次，之后不再更新"
                          : subURL
                            ? "存为订阅源：按时重新拉取，节点增删改自动跟上"
                            : "以后重新上传文件，节点增删改自动跟上"}
                      </p>
                    </div>
                  </div>
                  {sync && (
                    <>
                      <Input
                        label="名称"
                        placeholder={
                          subURL
                            ? subURL.hostname
                            : fileName.replace(/\.[^.]+$/, "")
                        }
                        size="sm"
                        value={sourceName}
                        variant="bordered"
                        onChange={(e) => setSourceName(e.target.value)}
                      />
                      {subURL && (
                        <Segmented
                          options={INTERVALS}
                          value={intervalHours}
                          onChange={setIntervalHours}
                        />
                      )}
                      <div className="space-y-1.5">
                        <p className="text-sm">用途</p>
                        <Segmented
                          options={SOURCE_ROLES}
                          value={role}
                          onChange={(v) => setRole(v as RemoteSourceRole)}
                        />
                        <p className="text-xs text-default-500">
                          {ROLE_HINTS[role]}
                        </p>
                      </div>
                    </>
                  )}
                </div>
              )}
            </>
          )}
          {!syncing && (
            <TagInput
              label="标签（可选）"
              suggestions={tagSuggestions}
              value={tags}
              onChange={setTags}
            />
          )}
          <Checkbox isSelected={relayOnly} onValueChange={setRelayOnly}>
            <span className="text-sm">
              只通过我的转发访问（订阅里不放直连，只放中转线路）
            </span>
          </Checkbox>
          {errors.length > 0 && (
            <ul className="space-y-1 rounded-xl bg-danger-50 p-3 text-xs text-danger">
              {errors.map((e) => (
                <li key={e} className="break-all">
                  {e}
                </li>
              ))}
            </ul>
          )}
        </ModalBody>
        <ModalFooter>
          <Button variant="light" onPress={onClose}>
            取消
          </Button>
          <Button
            color="primary"
            isDisabled={!manualMode && !links.trim()}
            isLoading={saving}
            onPress={submit}
          >
            {syncing
              ? fromFile
                ? "添加订阅源"
                : "添加并同步"
              : manualMode
                ? "添加"
                : "导入"}
          </Button>
        </ModalFooter>
      </ModalContent>
    </Modal>
  );
}

interface EditForm {
  name: string;
  host: string;
  port: string;
  method: string;
  password: string;
  link: string;
  remark: string;
  relayOnly: boolean;
}

export function ExternalEditModal({
  item,
  ssMethods,
  onClose,
  onSaved,
}: {
  item: InboundApiItem | null;
  ssMethods: string[];
  onClose: () => void;
  onSaved: () => void;
}) {
  const [form, setForm] = useState<EditForm | null>(null);
  const [saving, setSaving] = useState(false);

  useEffect(() => {
    setForm(
      item
        ? {
            name: item.name,
            host: item.listen,
            port: String(item.port),
            method: item.settings.method ?? "",
            password: item.settings.password ?? "",
            link: "",
            remark: item.remark,
            relayOnly: item.relayOnly,
          }
        : null,
    );
  }, [item]);

  // Nodes imported before other protocols were supported keep plain ss
  // fields; newer ones store the full proxy and are replaced by a link.
  const legacy = Boolean(item && !item.importedSummary);

  const set = (patch: Partial<EditForm>) =>
    setForm((p) => (p ? { ...p, ...patch } : p));

  const submit = async () => {
    if (!item || !form) return;
    setSaving(true);
    try {
      const res = await updateExternalInbound({
        id: item.id,
        name: form.name,
        host: form.host.trim(),
        port: Number(form.port),
        ...(legacy
          ? { method: form.method, password: form.password }
          : { link: form.link.trim() }),
        remark: form.remark,
        relayOnly: form.relayOnly,
      });

      if (res.code !== 0) {
        toast.error(res.msg || "保存失败");

        return;
      }
      if (res.msg && res.msg !== "操作成功") {
        toast(res.msg, { icon: "⚠️" });
      } else {
        toast.success("已保存");
      }
      onSaved();
      onClose();
    } finally {
      setSaving(false);
    }
  };

  const methods =
    form?.method && !ssMethods.includes(form.method)
      ? [...ssMethods, form.method]
      : ssMethods;

  return (
    <Modal
      backdrop="blur"
      classNames={{ base: modalBase }}
      isOpen={Boolean(item)}
      placement="center"
      scrollBehavior="inside"
      size="lg"
      onOpenChange={(open) => !open && onClose()}
    >
      <ModalContent>
        <ModalHeader className="flex flex-col gap-1">
          <h2 className="text-lg font-bold">编辑导入的节点</h2>
          <p className="text-small font-normal text-default-500">
            只修改面板里的记录；原服务器上的配置变了才需要在这里同步
          </p>
        </ModalHeader>
        <ModalBody>
          {form && (
            <div className="space-y-3">
              <Input
                label="名称"
                value={form.name}
                variant="bordered"
                onChange={(e) => set({ name: e.target.value })}
              />
              <div className="grid grid-cols-3 gap-3">
                <div className="col-span-2">
                  <Input
                    label="地址"
                    value={form.host}
                    variant="bordered"
                    onChange={(e) => set({ host: e.target.value })}
                  />
                </div>
                <Input
                  label="端口"
                  type="number"
                  value={form.port}
                  variant="bordered"
                  onChange={(e) => set({ port: e.target.value })}
                />
              </div>
              {legacy ? (
                <>
                  <Select
                    label="加密方式"
                    selectedKeys={form.method ? [form.method] : []}
                    onSelectionChange={(keys) =>
                      set({ method: String(Array.from(keys)[0] ?? "") })
                    }
                  >
                    {methods.map((m) => (
                      <SelectItem key={m} textValue={m}>
                        {m}
                      </SelectItem>
                    ))}
                  </Select>
                  <Input
                    label="密码"
                    value={form.password}
                    variant="bordered"
                    onChange={(e) => set({ password: e.target.value })}
                  />
                </>
              ) : (
                <Textarea
                  description={`当前：${item?.importedSummary ?? ""}。原节点的密码、密钥等参数变了时，粘贴新的分享链接或节点行替换；留空保持不变`}
                  label="用新链接替换参数（可选）"
                  minRows={2}
                  placeholder="vless://... / ss://..."
                  value={form.link}
                  variant="bordered"
                  onChange={(e) => set({ link: e.target.value })}
                />
              )}
              <Input
                label="备注"
                value={form.remark}
                variant="bordered"
                onChange={(e) => set({ remark: e.target.value })}
              />
              <Checkbox
                isSelected={form.relayOnly}
                onValueChange={(v) => set({ relayOnly: v })}
              >
                <span className="text-sm">
                  只通过我的转发访问（订阅里不放直连，只放中转线路）
                </span>
              </Checkbox>
            </div>
          )}
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
