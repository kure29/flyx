import type { Dispatch, SetStateAction } from "react";

import { useEffect, useRef, useState } from "react";
import {
  Box,
  Check,
  Copy,
  FileCode2,
  Info,
  ShieldAlert,
  SlidersHorizontal,
  Terminal,
} from "lucide-react";

import { useI18n } from "@/i18n";
import { Button } from "@/shadcn-bridge/heroui/button";
import {
  Modal,
  ModalContent,
  ModalHeader,
  ModalBody,
  ModalFooter,
} from "@/shadcn-bridge/heroui/modal";
import { Chip } from "@/shadcn-bridge/heroui/chip";
import { Spinner } from "@/shadcn-bridge/heroui/spinner";
import { Select, SelectItem } from "@/shadcn-bridge/heroui/select";
import { Switch } from "@/shadcn-bridge/heroui/switch";
import { type ReleaseChannel } from "@/api";

export interface InstallCommands {
  script: string;
  docker: string;
  dockerPrivileged: string;
  dockerCompose: string;
  dockerComposePrivileged: string;
}

export interface InstallCommandModalProps {
  currentNodeName: string;
  handleCopyCommand: (command: string) => Promise<boolean | void>;
  installCommands: InstallCommands;
  installCommandModal: boolean;
  setInstallCommandModal: Dispatch<SetStateAction<boolean>>;
}

type InstallMode = "script" | "docker" | "docker-compose";

const INLINE_CODE =
  "rounded bg-default-200/70 px-1 py-0.5 font-mono text-[11px] text-foreground dark:bg-default-100";

/** The commands that install the agent on a node: install.sh, Docker or Compose. */
export function InstallCommandModal({
  currentNodeName,
  handleCopyCommand,
  installCommands,
  installCommandModal,
  setInstallCommandModal,
}: InstallCommandModalProps) {
  const { t } = useI18n();
  const [mode, setMode] = useState<InstallMode>("script");
  const [privileged, setPrivileged] = useState(false);
  const [copied, setCopied] = useState(false);
  const copiedTimerRef = useRef<number | null>(null);

  useEffect(() => {
    if (installCommandModal) {
      setMode("script");
      setPrivileged(false);
      setCopied(false);
    }
  }, [installCommandModal]);

  useEffect(() => {
    setCopied(false);
  }, [mode, privileged]);

  useEffect(() => {
    return () => {
      if (copiedTimerRef.current !== null) {
        window.clearTimeout(copiedTimerRef.current);
      }
    };
  }, []);

  const hasDockerModes = Boolean(
    installCommands.docker || installCommands.dockerCompose,
  );
  const command =
    mode === "script"
      ? installCommands.script
      : mode === "docker"
        ? privileged
          ? installCommands.dockerPrivileged
          : installCommands.docker
        : privileged
          ? installCommands.dockerComposePrivileged
          : installCommands.dockerCompose;

  const onCopy = async () => {
    const ok = await handleCopyCommand(command);

    if (ok !== false) {
      setCopied(true);
      if (copiedTimerRef.current !== null) {
        window.clearTimeout(copiedTimerRef.current);
      }
      copiedTimerRef.current = window.setTimeout(() => setCopied(false), 2000);
    }
  };

  const modes: Array<{
    key: InstallMode;
    label: string;
    icon: typeof Terminal;
  }> = [
    { key: "script", label: t("一键脚本"), icon: Terminal },
    { key: "docker", label: "Docker", icon: Box },
    { key: "docker-compose", label: "Docker Compose", icon: FileCode2 },
  ];

  const shellLabel =
    mode === "script"
      ? "bash · root"
      : mode === "docker"
        ? "docker run"
        : "docker-compose.yaml";

  return (
    <Modal
      classNames={{
        base: "!w-[calc(100%-32px)] !mx-auto sm:!w-full overflow-hidden",
      }}
      isOpen={installCommandModal}
      placement="center"
      scrollBehavior="inside"
      size="lg"
      onClose={() => setInstallCommandModal(false)}
    >
      <ModalContent>
        <ModalHeader closable>
          <div className="flex items-center gap-3">
            <span className="flex h-9 w-9 shrink-0 items-center justify-center rounded-xl bg-default-100 text-foreground dark:bg-default-100/70">
              <Terminal className="h-4 w-4" strokeWidth={1.75} />
            </span>
            <div className="min-w-0">
              <div className="text-base font-semibold leading-tight text-foreground">
                {t("安装命令")}
              </div>
              <div className="mt-0.5 truncate text-xs font-normal text-default-500">
                {currentNodeName}
              </div>
            </div>
          </div>
        </ModalHeader>

        <ModalBody className="space-y-3.5 pt-1">
          {hasDockerModes && (
            <div
              aria-label={t("安装方式")}
              className="grid grid-cols-3 gap-1 rounded-xl bg-default-100/80 p-1 dark:bg-default-100/40"
              role="tablist"
            >
              {modes.map((item) => {
                const active = mode === item.key;
                const Icon = item.icon;

                return (
                  <button
                    key={item.key}
                    aria-selected={active}
                    className={`flex items-center justify-center gap-1.5 rounded-lg px-2.5 py-2 text-xs font-medium transition-all duration-150 sm:text-sm ${
                      active
                        ? "bg-background text-foreground shadow-xs"
                        : "text-default-500 hover:text-foreground"
                    }`}
                    role="tab"
                    type="button"
                    onClick={() => setMode(item.key)}
                  >
                    <Icon className="h-3.5 w-3.5 shrink-0" strokeWidth={1.75} />
                    <span className="truncate">{item.label}</span>
                  </button>
                );
              })}
            </div>
          )}

          <p className="text-xs leading-relaxed text-default-500 sm:text-[13px]">
            {mode === "script" ? (
              hasDockerModes ? (
                <>
                  {t("在服务器上以")} <code className={INLINE_CODE}>root</code>{" "}
                  {t("执行，安装为")}{" "}
                  <code className={INLINE_CODE}>systemd / OpenRC</code>{" "}
                  {t("服务")}
                </>
              ) : (
                t("请复制以下安装命令到服务器上执行")
              )
            ) : mode === "docker" ? (
              t("在已安装 Docker 的服务器上执行，自动拉取镜像并启动容器")
            ) : (
              <>
                {t("在已安装 Docker Compose 的服务器上执行，配置写入")}{" "}
                <code className={`${INLINE_CODE} inline-block`}>
                  /opt/docker/flyx-agent/docker-compose.yaml
                </code>
              </>
            )}
          </p>

          {mode !== "script" && (
            <div
              className={`flex items-center justify-between gap-3 rounded-xl border px-3.5 py-2.5 transition-colors ${
                privileged
                  ? "border-warning/40 bg-warning/5 dark:bg-warning/10"
                  : "border-divider/80 bg-default-50/70 dark:bg-default-100/25"
              }`}
            >
              <div className="flex min-w-0 items-start gap-2.5">
                <span
                  className={`mt-0.5 flex h-7 w-7 shrink-0 items-center justify-center rounded-lg ${
                    privileged
                      ? "bg-warning/15 text-warning"
                      : "bg-default-200/60 text-default-500 dark:bg-default-100"
                  }`}
                >
                  {privileged ? (
                    <ShieldAlert className="h-3.5 w-3.5" strokeWidth={1.75} />
                  ) : (
                    <SlidersHorizontal
                      className="h-3.5 w-3.5"
                      strokeWidth={1.75}
                    />
                  )}
                </span>
                <div className="min-w-0">
                  <div className="flex flex-wrap items-center gap-1.5">
                    <span className="text-xs font-medium text-foreground sm:text-sm">
                      {t("服务器调优")}
                    </span>
                    <code className={INLINE_CODE}>--privileged</code>
                    {privileged && (
                      <span className="rounded-full bg-warning/15 px-1.5 py-0.5 text-[10px] font-medium text-warning">
                        {t("高权限")}
                      </span>
                    )}
                  </div>
                  <p className="mt-0.5 text-xs leading-relaxed text-default-500">
                    {t("开启后可执行内核参数调优，但容器将拥有宿主机高权限。")}
                  </p>
                </div>
              </div>
              <Switch
                aria-label={t("服务器调优")}
                isSelected={privileged}
                size="sm"
                onValueChange={setPrivileged}
              />
            </div>
          )}

          {/* Terminal code container */}
          <div className="overflow-hidden rounded-xl border border-zinc-800/90 bg-zinc-950 text-zinc-100 shadow-xs">
            <div className="flex items-center justify-between gap-2 border-b border-white/10 bg-white/[0.04] px-3.5 py-2">
              <div className="flex min-w-0 items-center gap-2">
                <span className="font-mono text-xs text-emerald-400">$</span>
                <span className="truncate font-mono text-[11px] text-zinc-400">
                  {shellLabel}
                </span>
                {mode !== "script" && privileged && (
                  <span className="rounded bg-amber-500/15 px-1.5 py-0.5 font-mono text-[10px] text-amber-300">
                    privileged
                  </span>
                )}
              </div>
              <button
                className={`inline-flex shrink-0 items-center gap-1.5 rounded-md px-2.5 py-1 text-xs font-medium transition-colors ${
                  copied
                    ? "bg-emerald-500/20 text-emerald-300"
                    : "bg-white/10 text-zinc-200 hover:bg-white/15 hover:text-white"
                }`}
                type="button"
                onClick={onCopy}
              >
                {copied ? (
                  <>
                    <Check className="h-3.5 w-3.5" />
                    <span>{t("已复制")}</span>
                  </>
                ) : (
                  <>
                    <Copy className="h-3.5 w-3.5" />
                    <span>{t("复制")}</span>
                  </>
                )}
              </button>
            </div>
            <div
              className={`overflow-auto px-4 py-3.5 desktop-scrollbar ${
                mode === "script" ? "max-h-[240px]" : "max-h-[195px]"
              }`}
            >
              <pre
                className={`m-0 select-all font-mono text-[12.5px] leading-relaxed text-zinc-100 ${
                  mode === "script"
                    ? "whitespace-pre-wrap break-all"
                    : "whitespace-pre-wrap break-words"
                }`}
              >
                <code>{command}</code>
              </pre>
            </div>
          </div>

          {mode === "docker" && (
            <div className="flex gap-2.5 rounded-xl border border-divider/60 bg-default-50/70 px-3.5 py-2.5 text-xs leading-relaxed text-default-500 dark:bg-default-100/20">
              <Info className="mt-0.5 h-3.5 w-3.5 shrink-0 text-default-400" />
              <ul className="list-disc space-y-1 pl-3.5">
                <li>
                  {t("使用 host 网络，数据保存在宿主机")}{" "}
                  <code className={INLINE_CODE}>/etc/flyx-agent</code>{" "}
                  {t(
                    "（旧安装沿用原目录）；请勿与一键脚本安装的 agent 同时运行。",
                  )}
                </li>
                <li>
                  {t(
                    "面板一键升级会替换数据目录里的程序并自动重启容器；拉取新镜像后重建容器同样可完成升级。",
                  )}
                </li>
                <li>
                  <code className={INLINE_CODE}>NET_ADMIN</code>{" "}
                  {t(
                    "用于 nftables 流量统计与白名单；开启服务器调优会额外追加",
                  )}{" "}
                  <code className={INLINE_CODE}>--privileged</code>
                  {t("。")}
                </li>
              </ul>
            </div>
          )}

          {mode === "docker-compose" && (
            <div className="flex gap-2.5 rounded-xl border border-divider/60 bg-default-50/70 px-3.5 py-2.5 text-xs leading-relaxed text-default-500 dark:bg-default-100/20">
              <Info className="mt-0.5 h-3.5 w-3.5 shrink-0 text-default-400" />
              <ul className="list-disc space-y-1 pl-3.5">
                <li>
                  {t("自动创建")}{" "}
                  <code className={INLINE_CODE}>/opt/docker/flyx-agent</code>{" "}
                  {t(
                    "并设置 600 权限；启动前会自动替换已存在的 flyx-agent 容器。",
                  )}
                </li>
                <li>
                  {t("使用 host 网络，数据保存在宿主机")}{" "}
                  <code className={INLINE_CODE}>/etc/flyx-agent</code>
                  {t(
                    "；检测到本项目旧版 flux_agent 容器时会停止并提示先迁移，不影响其他面板的容器。",
                  )}
                </li>
                <li>
                  {t("Compose 文件包含节点密钥；开启服务器调优会写入")}{" "}
                  <code className={INLINE_CODE}>privileged: true</code>
                  {t("。")}
                </li>
              </ul>
            </div>
          )}
        </ModalBody>

        <ModalFooter className="items-center justify-between gap-2 pt-2">
          <span className="hidden text-[11px] text-default-400 sm:inline">
            {t("点击代码块可全选，支持手动复制")}
          </span>
          <div className="ml-auto flex items-center gap-2">
            <Button
              size="sm"
              variant="light"
              onPress={() => setInstallCommandModal(false)}
            >
              {t("关闭")}
            </Button>
            <Button
              className="gap-1.5"
              color="primary"
              size="sm"
              startContent={
                copied ? (
                  <Check className="h-3.5 w-3.5" />
                ) : (
                  <Copy className="h-3.5 w-3.5" />
                )
              }
              onPress={onCopy}
            >
              {copied ? t("已复制") : t("复制命令")}
            </Button>
          </div>
        </ModalFooter>
      </ModalContent>
    </Modal>
  );
}

export interface UpgradeNodeModalProps {
  handleConfirmUpgrade: () => Promise<void>;
  releases: {
    version: string;
    name: string;
    publishedAt: string;
    prerelease: boolean;
    channel: ReleaseChannel;
  }[];
  releasesLoading: boolean;
  selectedIds: Set<number>;
  selectedVersion: string;
  setSelectedVersion: Dispatch<SetStateAction<string>>;
  setUpgradeModalOpen: Dispatch<SetStateAction<boolean>>;
  upgradeModalOpen: boolean;
  upgradeTarget: "single" | "batch";
}

/** Pick a version and upgrade the agent on one or more nodes. */
export function UpgradeNodeModal({
  handleConfirmUpgrade,
  releases,
  releasesLoading,
  selectedIds,
  selectedVersion,
  setSelectedVersion,
  setUpgradeModalOpen,
  upgradeModalOpen,
  upgradeTarget,
}: UpgradeNodeModalProps) {
  const { t } = useI18n();

  return (
    <Modal
      classNames={{
        base: "!w-[calc(100%-32px)] !mx-auto sm:!w-full overflow-hidden",
      }}
      isOpen={upgradeModalOpen}
      placement="center"
      scrollBehavior="inside"
      size="md"
      onOpenChange={setUpgradeModalOpen}
    >
      <ModalContent>
        {(onClose) => (
          <>
            <ModalHeader className="flex flex-col gap-1">
              <h2 className="text-lg font-semibold">
                {upgradeTarget === "batch"
                  ? t("批量升级 ({count} 个服务器)", {
                      count: selectedIds.size,
                    })
                  : t("升级服务器")}
              </h2>
            </ModalHeader>
            <ModalBody>
              {releasesLoading ? (
                <div className="flex justify-center py-8">
                  <Spinner size="lg" />
                </div>
              ) : (
                <div className="space-y-4">
                  <Select
                    label={t("选择版本")}
                    placeholder={t("留空则使用最新版本")}
                    selectedKeys={selectedVersion ? [selectedVersion] : []}
                    onSelectionChange={(keys) => {
                      const selected = Array.from(keys)[0] as string;

                      setSelectedVersion(selected || "");
                    }}
                  >
                    {releases.map((r) => (
                      <SelectItem key={r.version} textValue={r.version}>
                        <div className="flex items-center justify-between">
                          <span>{r.version}</span>
                          <span className="text-xs text-default-400">
                            {r.publishedAt
                              ? new Date(r.publishedAt).toLocaleDateString()
                              : ""}
                            {r.channel === "dev" && (
                              <Chip
                                className="ml-1 shrink-0 whitespace-nowrap"
                                color="warning"
                                size="sm"
                                variant="flat"
                              >
                                {t("测试")}
                              </Chip>
                            )}
                          </span>
                        </div>
                      </SelectItem>
                    ))}
                  </Select>
                  <p className="text-sm text-default-500">
                    {selectedVersion
                      ? t("将升级到版本 {version}", {
                          version: selectedVersion,
                        })
                      : t("未选择版本，将自动使用最新版")}
                  </p>
                </div>
              )}
            </ModalBody>
            <ModalFooter>
              <Button variant="light" onPress={onClose}>
                {t("取消")}
              </Button>
              <Button
                color="warning"
                isDisabled={releasesLoading}
                onPress={handleConfirmUpgrade}
              >
                {t("升级")}
              </Button>
            </ModalFooter>
          </>
        )}
      </ModalContent>
    </Modal>
  );
}
