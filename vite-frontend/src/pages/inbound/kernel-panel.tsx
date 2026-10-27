import type {
  KernelApiItem,
  KernelCapabilitiesApiData,
  KernelStateApiData,
  NodeApiItem,
} from "@/api/types";

import { useEffect, useState } from "react";
import toast from "react-hot-toast";
import { Plus } from "lucide-react";

import { groupItems } from "./group";
import { eligibleNodes } from "./inbound-form-modal";
import { KERNEL_STATUS } from "./presets";

import { TONE } from "@/components/latency-cell";
import { ActionSheet } from "@/components/action-sheet";
import {
  controlKernel,
  getKernelLogs,
  getKernelVersions,
  installKernel,
} from "@/api";
import { Button } from "@/shadcn-bridge/heroui/button";
import { Card, CardBody } from "@/shadcn-bridge/heroui/card";
import { Chip } from "@/shadcn-bridge/heroui/chip";
import { Input } from "@/shadcn-bridge/heroui/input";
import {
  Modal,
  ModalBody,
  ModalContent,
  ModalFooter,
  ModalHeader,
} from "@/shadcn-bridge/heroui/modal";
import { Select, SelectItem } from "@/shadcn-bridge/heroui/select";

const modalBase =
  "!w-[calc(100%-32px)] !mx-auto sm:!w-full rounded-2xl overflow-hidden";

export interface InstallRequest {
  nodeId: number;
  kernelType: string;
}

interface Props {
  caps: KernelCapabilitiesApiData | null;
  kernels: KernelApiItem[];
  nodes: NodeApiItem[];
  installRequest: InstallRequest | null;
  onInstallRequestHandled: () => void;
  onChanged: () => void;
}

/** The latency tiles' status colours are too dark on a dark page. */
const DARK_LABEL: Record<string, string> = {
  success: "dark:text-emerald-400",
  warning: "dark:text-amber-400",
  danger: "dark:text-rose-400",
  default: "dark:text-default-500",
};

function shortVersion(v: string): string {
  const m = v.match(/\d+\.\d+\.\d+/);

  return m ? m[0] : v;
}

export function KernelPanel({
  caps,
  kernels,
  nodes,
  installRequest,
  onInstallRequestHandled,
  onChanged,
}: Props) {
  const [sheetFor, setSheetFor] = useState<number | null>(null);
  const [releases, setReleases] = useState<{
    type: string;
    items: { version: string; prerelease: boolean }[];
    error: string;
  } | null>(null);
  const [customVersion, setCustomVersion] = useState(false);
  const [installOpen, setInstallOpen] = useState(false);
  const [installForm, setInstallForm] = useState({
    nodeId: "",
    kernelType: "xray",
    version: "latest",
  });
  const [installing, setInstalling] = useState(false);
  const [busy, setBusy] = useState<string | null>(null);
  const [logsFor, setLogsFor] = useState<KernelApiItem | null>(null);
  const [logs, setLogs] = useState<KernelStateApiData | null>(null);
  const [logsLoading, setLogsLoading] = useState(false);
  const [removeTarget, setRemoveTarget] = useState<KernelApiItem | null>(null);
  const [removeFailed, setRemoveFailed] = useState(false);

  useEffect(() => {
    if (!installRequest) return;
    setInstallForm({
      nodeId: String(installRequest.nodeId),
      kernelType: installRequest.kernelType,
      version: "latest",
    });
    setInstallOpen(true);
    onInstallRequestHandled();
  }, [installRequest, onInstallRequestHandled]);

  const kernelName = (type: string) => {
    const info = caps?.kernels.find((k) => k.type === type);

    return info?.relay ? `${info.name}（转发引擎）` : (info?.name ?? type);
  };

  const openInstall = (nodeId?: number, kernelType = "xray") => {
    setInstallForm({
      nodeId: nodeId ? String(nodeId) : "",
      kernelType,
      version:
        caps?.kernels.find((k) => k.type === kernelType)?.versions?.[0] ??
        "latest",
    });
    setInstallOpen(true);
  };

  const doInstall = async () => {
    if (!installForm.nodeId) {
      toast.error("请选择服务器");

      return;
    }
    setInstalling(true);
    try {
      const res = await installKernel({
        nodeId: Number(installForm.nodeId),
        kernelType: installForm.kernelType,
        version: installForm.version.trim() || "latest",
      });

      if (res.code === 0) {
        toast.success("内核已安装并启动");
        setInstallOpen(false);
      } else {
        toast.error(res.msg || "安装失败", { duration: 8000 });
      }
      onChanged();
    } finally {
      setInstalling(false);
    }
  };

  const doControl = async (
    k: KernelApiItem,
    action: "start" | "stop" | "restart",
  ) => {
    setBusy(`${k.id}-${action}`);
    try {
      const res = await controlKernel(k.id, action);

      if (res.code === 0) {
        toast.success("操作成功");
      } else {
        toast.error(res.msg || "操作失败");
      }
      onChanged();
    } finally {
      setBusy(null);
    }
  };

  const openLogs = async (k: KernelApiItem) => {
    setLogsFor(k);
    setLogs(null);
    setLogsLoading(true);
    try {
      const res = await getKernelLogs(k.id);

      if (res.code === 0) {
        setLogs(res.data);
      } else {
        toast.error(res.msg || "获取日志失败");
      }
    } finally {
      setLogsLoading(false);
    }
  };

  const doRemove = async (force: boolean) => {
    if (!removeTarget) return;
    setBusy(`${removeTarget.id}-remove`);
    try {
      const res = await controlKernel(removeTarget.id, "remove", force);

      if (res.code === 0) {
        toast.success("已卸载");
        setRemoveTarget(null);
        onChanged();
      } else {
        setRemoveFailed(true);
        toast.error(res.msg || "卸载失败");
      }
    } finally {
      setBusy(null);
    }
  };

  const installNodeOptions = eligibleNodes(nodes);
  const versionListType =
    installOpen &&
    !caps?.kernels.find((k) => k.type === installForm.kernelType)?.versions
      ?.length
      ? installForm.kernelType
      : "";

  useEffect(() => {
    if (!versionListType || releases?.type === versionListType) return;
    setCustomVersion(false);
    getKernelVersions(versionListType)
      .then((res) =>
        setReleases({
          type: versionListType,
          items: res.code === 0 ? res.data || [] : [],
          error: res.code === 0 ? "" : res.msg || "获取版本列表失败",
        }),
      )
      .catch(() =>
        setReleases({
          type: versionListType,
          items: [],
          error: "获取版本列表失败",
        }),
      );
  }, [versionListType]);

  const releaseItems =
    releases?.type === installForm.kernelType ? releases.items : [];
  const pinnedVersions =
    caps?.kernels.find((k) => k.type === installForm.kernelType)?.versions ??
    [];

  const nodeGroups = groupItems(
    kernels,
    (k) => String(k.nodeId),
    (_key, k) => k.nodeName || `服务器 ${k.nodeId}`,
  );
  const sheetKernel = kernels.find((k) => k.id === sheetFor) ?? null;

  // One tile per kernel, tinted by its status like the latency tiles.
  const renderKernel = (k: KernelApiItem) => {
    const st = KERNEL_STATUS[k.status] ?? {
      label: k.status,
      color: "default" as const,
    };
    const [pill, dot] = TONE[st.color] ?? TONE.default;

    return (
      <button
        key={k.id}
        className={`flex min-w-0 flex-col items-start overflow-hidden rounded-lg px-2 py-1.5 text-left transition-opacity hover:opacity-80 ${pill}`}
        type="button"
        onClick={() => setSheetFor(k.id)}
      >
        <span
          className={`flex w-full min-w-0 items-center gap-1 text-[11px] ${DARK_LABEL[st.color] ?? ""}`}
        >
          <span className={`h-1.5 w-1.5 shrink-0 rounded-full ${dot}`} />
          <span className="truncate">
            {!k.synced && !k.lastError && k.status !== "offline"
              ? "待同步"
              : st.label}
          </span>
        </span>
        <span className="w-full truncate text-sm font-semibold text-foreground">
          {kernelName(k.kernelType)}
        </span>
        <span className="w-full truncate font-mono text-[11px] text-default-500">
          {k.installedVersion ? shortVersion(k.installedVersion) : k.version}
        </span>
        <span className="w-full truncate text-[11px] text-default-500">
          {k.inboundCount} 个入站
        </span>
      </button>
    );
  };

  return (
    <div>
      <div className="flex items-center justify-between mb-3 gap-2">
        <p className="text-xs text-default-500">
          点击内核可启停、重启、查看日志或卸载
        </p>
        <Button
          color="primary"
          size="sm"
          variant="flat"
          onPress={() => openInstall()}
        >
          安装内核
        </Button>
      </div>

      {kernels.length === 0 ? (
        <Card className="shadow-sm bg-default-50/50">
          <CardBody className="text-center py-10 text-sm text-default-500">
            还没有在任何服务器上安装内核。点击“安装内核”，先在服务器上安装
            Xray，再到“节点”里创建入站。
          </CardBody>
        </Card>
      ) : (
        <div className="grid grid-cols-1 md:grid-cols-2 xl:grid-cols-3 gap-3 items-start">
          {nodeGroups.map((g) => {
            const online = g.items[0]?.nodeOnline;

            return (
              <Card key={g.key} className="shadow-sm overflow-hidden">
                <div className="flex items-center gap-2 pb-0 pl-3 pr-1.5 pt-2">
                  <span
                    className={`h-2 w-2 shrink-0 rounded-full ${
                      online ? "bg-success" : "bg-default-300"
                    }`}
                  />
                  <span className="min-w-0 flex-1 truncate text-sm font-semibold">
                    {g.title}
                  </span>
                  {!online && (
                    <Chip size="sm" variant="flat">
                      离线
                    </Chip>
                  )}
                  <Button
                    className="h-7 min-w-0 gap-1 px-2 text-xs text-primary dark:text-primary"
                    size="sm"
                    startContent={<Plus className="h-3.5 w-3.5" />}
                    variant="light"
                    onPress={() => openInstall(g.items[0].nodeId)}
                  >
                    内核
                  </Button>
                </div>
                <div className="grid grid-cols-3 gap-2 p-3">
                  {g.items.map(renderKernel)}
                </div>
                {g.items.some((k) => k.lastError) && (
                  <div className="space-y-1 px-3 pb-3">
                    {g.items
                      .filter((k) => k.lastError)
                      .map((k) => (
                        <p
                          key={k.id}
                          className="line-clamp-2 break-all text-xs text-danger"
                        >
                          {kernelName(k.kernelType)}：{k.lastError}
                        </p>
                      ))}
                  </div>
                )}
              </Card>
            );
          })}
        </div>
      )}

      <ActionSheet
        actions={
          sheetKernel
            ? [
                sheetKernel.enabled
                  ? {
                      key: "stop",
                      label: "停止",
                      onPress: () => doControl(sheetKernel, "stop"),
                    }
                  : {
                      key: "start",
                      label: "启动",
                      color: "success" as const,
                      onPress: () => doControl(sheetKernel, "start"),
                    },
                {
                  key: "restart",
                  label: "重启",
                  disabled: !sheetKernel.enabled,
                  onPress: () => doControl(sheetKernel, "restart"),
                },
                {
                  key: "logs",
                  label: "日志",
                  onPress: () => openLogs(sheetKernel),
                },
                // realm's version follows the panel.
                ...(caps?.kernels.find((k) => k.type === sheetKernel.kernelType)
                  ?.relay
                  ? []
                  : [
                      {
                        key: "upgrade",
                        label: "换版本",
                        onPress: () =>
                          openInstall(
                            sheetKernel.nodeId,
                            sheetKernel.kernelType,
                          ),
                      },
                    ]),
                {
                  key: "remove",
                  label: "卸载",
                  color: "danger" as const,
                  onPress: () => {
                    setRemoveFailed(false);
                    setRemoveTarget(sheetKernel);
                  },
                },
              ]
            : []
        }
        isOpen={Boolean(sheetKernel)}
        subtitle={
          sheetKernel &&
          `${sheetKernel.nodeName || `服务器 ${sheetKernel.nodeId}`} · ${
            sheetKernel.installedVersion
              ? shortVersion(sheetKernel.installedVersion)
              : sheetKernel.version
          } · ${sheetKernel.inboundCount} 个入站`
        }
        title={sheetKernel ? kernelName(sheetKernel.kernelType) : ""}
        onClose={() => setSheetFor(null)}
      >
        {sheetKernel?.lastError && (
          <p className="text-xs text-danger break-all">
            {sheetKernel.lastError}
          </p>
        )}
      </ActionSheet>

      <Modal
        backdrop="blur"
        classNames={{ base: modalBase }}
        isOpen={installOpen}
        placement="center"
        onOpenChange={setInstallOpen}
      >
        <ModalContent>
          {(onClose) => (
            <>
              <ModalHeader className="flex flex-col gap-1">
                <h2 className="text-xl font-bold">安装内核</h2>
                <p className="text-small text-default-500">
                  服务器会从官方 Release 下载并校验
                  SHA256，首次安装可能需要一两分钟
                </p>
              </ModalHeader>
              <ModalBody>
                <div className="space-y-3">
                  <Select
                    label="服务器"
                    placeholder="选择服务器"
                    selectedKeys={
                      installForm.nodeId ? [installForm.nodeId] : []
                    }
                    onSelectionChange={(keys) =>
                      setInstallForm((p) => ({
                        ...p,
                        nodeId: String(Array.from(keys)[0] ?? ""),
                      }))
                    }
                  >
                    {installNodeOptions.map((n) => (
                      <SelectItem
                        key={String(n.id)}
                        textValue={
                          n.status === 1 ? n.name : `${n.name}（离线）`
                        }
                      >
                        {n.status === 1 ? n.name : `${n.name}（离线）`}
                      </SelectItem>
                    ))}
                  </Select>
                  <Select
                    disabledKeys={(caps?.kernels ?? [])
                      .filter((k) => !k.implemented)
                      .map((k) => k.type)}
                    label="内核"
                    selectedKeys={[installForm.kernelType]}
                    onSelectionChange={(keys) =>
                      setInstallForm((p) => ({
                        ...p,
                        kernelType: String(Array.from(keys)[0] ?? "xray"),
                        version:
                          caps?.kernels.find(
                            (k) => k.type === String(Array.from(keys)[0]),
                          )?.versions?.[0] ?? "latest",
                      }))
                    }
                  >
                    {(caps?.kernels ?? [])
                      .filter((k) => !k.relay)
                      .map((k) => (
                        <SelectItem
                          key={k.type}
                          textValue={
                            k.implemented ? k.name : `${k.name}（即将支持）`
                          }
                        >
                          {k.implemented ? k.name : `${k.name}（即将支持）`}
                        </SelectItem>
                      ))}
                  </Select>
                  {pinnedVersions.length > 0 ? (
                    <Select
                      description="官方渠道不提供校验文件，仅开放已核对 SHA256 的版本"
                      label="版本"
                      selectedKeys={[
                        pinnedVersions.includes(installForm.version)
                          ? installForm.version
                          : pinnedVersions[0],
                      ]}
                      onSelectionChange={(keys) =>
                        setInstallForm((p) => ({
                          ...p,
                          version: String(Array.from(keys)[0] ?? ""),
                        }))
                      }
                    >
                      {pinnedVersions.map((v) => (
                        <SelectItem key={v} textValue={v}>
                          {v}
                          {installForm.kernelType === "snell" &&
                            v.startsWith("6.") && (
                              <span className="ml-2 text-xs text-warning">
                                测试版 · 只接受 v6 客户端 · 无 armv7
                              </span>
                            )}
                        </SelectItem>
                      ))}
                    </Select>
                  ) : (
                    <>
                      {releaseItems.length > 0 && (
                        <Select
                          description="最新版出问题时可以选旧版本；预发布版不建议使用"
                          label="版本"
                          selectedKeys={[
                            customVersion
                              ? "custom"
                              : installForm.version === "latest" ||
                                  !installForm.version
                                ? "latest"
                                : installForm.version,
                          ]}
                          onSelectionChange={(keys) => {
                            const v = String(Array.from(keys)[0] ?? "latest");

                            setCustomVersion(v === "custom");
                            if (v !== "custom") {
                              setInstallForm((p) => ({ ...p, version: v }));
                            }
                          }}
                        >
                          {[
                            <SelectItem
                              key="latest"
                              textValue="最新版（latest）"
                            >
                              最新版（latest）
                            </SelectItem>,
                            ...releaseItems.map((r) => (
                              <SelectItem
                                key={r.version}
                                textValue={
                                  r.prerelease
                                    ? `${r.version}（预发布）`
                                    : r.version
                                }
                              >
                                {r.prerelease
                                  ? `${r.version}（预发布）`
                                  : r.version}
                              </SelectItem>
                            )),
                            <SelectItem key="custom" textValue="手动输入…">
                              手动输入…
                            </SelectItem>,
                          ]}
                        </Select>
                      )}
                      {(releaseItems.length === 0 || customVersion) && (
                        <Input
                          description={
                            releases?.error && releaseItems.length === 0
                              ? `${releases.error}，可手动填写版本号，latest 表示最新版`
                              : "latest 表示最新版，也可填写具体版本号"
                          }
                          label={
                            releaseItems.length > 0 ? "自定义版本" : "版本"
                          }
                          value={installForm.version}
                          variant="bordered"
                          onChange={(e) =>
                            setInstallForm((p) => ({
                              ...p,
                              version: e.target.value,
                            }))
                          }
                        />
                      )}
                    </>
                  )}
                </div>
              </ModalBody>
              <ModalFooter>
                <Button variant="light" onPress={onClose}>
                  取消
                </Button>
                <Button
                  color="primary"
                  isLoading={installing}
                  onPress={doInstall}
                >
                  安装
                </Button>
              </ModalFooter>
            </>
          )}
        </ModalContent>
      </Modal>

      <Modal
        backdrop="blur"
        classNames={{ base: modalBase }}
        isOpen={Boolean(logsFor)}
        placement="center"
        scrollBehavior="inside"
        size="4xl"
        onOpenChange={(open) => !open && setLogsFor(null)}
      >
        <ModalContent>
          {() => (
            <>
              <ModalHeader closable className="flex flex-col gap-1">
                <h2 className="text-lg font-bold">
                  {logsFor?.nodeName} · {kernelName(logsFor?.kernelType ?? "")}{" "}
                  日志
                </h2>
                {logs && (
                  <p className="text-small text-default-500">
                    {logs.running ? `运行中 PID ${logs.pid}` : "未运行"}
                    {logs.restarts > 0
                      ? ` · 已自动重启 ${logs.restarts} 次`
                      : ""}
                  </p>
                )}
              </ModalHeader>
              <ModalBody>
                <pre className="text-xs whitespace-pre-wrap break-all bg-default-100 rounded-xl p-3 max-h-[60vh] overflow-auto">
                  {logsLoading
                    ? "加载中..."
                    : (logs?.logs ?? []).join("\n") || "暂无日志"}
                </pre>
              </ModalBody>
              <ModalFooter>
                <Button
                  variant="flat"
                  onPress={() => logsFor && openLogs(logsFor)}
                >
                  刷新
                </Button>
              </ModalFooter>
            </>
          )}
        </ModalContent>
      </Modal>

      <Modal
        backdrop="blur"
        classNames={{ base: modalBase }}
        isOpen={Boolean(removeTarget)}
        placement="center"
        onOpenChange={(open) => !open && setRemoveTarget(null)}
      >
        <ModalContent>
          {(onClose) => (
            <>
              <ModalHeader>
                <h2 className="text-lg font-bold text-danger">卸载内核</h2>
              </ModalHeader>
              <ModalBody>
                <p className="text-default-600">
                  确定从{" "}
                  <span className="font-semibold text-foreground">
                    {removeTarget?.nodeName}
                  </span>{" "}
                  卸载 {kernelName(removeTarget?.kernelType ?? "")} 吗？
                </p>
                <p className="text-small text-danger">
                  该内核下的 {removeTarget?.inboundCount ?? 0}{" "}
                  个入站会一起删除，且无法恢复。
                </p>
                {removeFailed && (
                  <p className="text-small text-warning">
                    服务器卸载失败（可能离线）。可以强制删除面板记录，服务器上线后会自动清理。
                  </p>
                )}
              </ModalBody>
              <ModalFooter>
                <Button variant="light" onPress={onClose}>
                  取消
                </Button>
                {removeFailed && (
                  <Button
                    color="warning"
                    isLoading={busy === `${removeTarget?.id}-remove`}
                    onPress={() => doRemove(true)}
                  >
                    强制删除
                  </Button>
                )}
                <Button
                  color="danger"
                  isLoading={busy === `${removeTarget?.id}-remove`}
                  onPress={() => doRemove(false)}
                >
                  卸载
                </Button>
              </ModalFooter>
            </>
          )}
        </ModalContent>
      </Modal>
    </div>
  );
}
