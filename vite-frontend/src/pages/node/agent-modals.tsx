import type { Dispatch, SetStateAction } from "react";

import { useEffect, useState } from "react";

import { Button } from "@/shadcn-bridge/heroui/button";
import { Textarea } from "@/shadcn-bridge/heroui/input";
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
import { Tab, Tabs } from "@/shadcn-bridge/heroui/tabs";
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
  handleCopyCommand: (command: string) => Promise<void>;
  installCommands: InstallCommands;
  installCommandModal: boolean;
  setInstallCommandModal: Dispatch<SetStateAction<boolean>>;
}

type InstallMode = "script" | "docker" | "docker-compose";

/** The commands that install the agent on a node: install.sh, Docker or Compose. */
export function InstallCommandModal({
  currentNodeName,
  handleCopyCommand,
  installCommands,
  installCommandModal,
  setInstallCommandModal,
}: InstallCommandModalProps) {
  const [mode, setMode] = useState<InstallMode>("script");
  const [privileged, setPrivileged] = useState(false);

  useEffect(() => {
    if (installCommandModal) {
      setMode("script");
      setPrivileged(false);
    }
  }, [installCommandModal]);

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
  const privilegedControl = (
    <div className="flex items-center justify-between gap-4 rounded-xl border border-default-200 bg-default-100/60 px-3 py-2.5">
      <div className="min-w-0">
        <p className="text-sm font-medium">服务器调优（--privileged）</p>
        <p className="text-xs text-default-500">
          开启后追加 --privileged，可执行内核参数调优，但容器拥有宿主机高权限。
        </p>
      </div>
      <Switch
        aria-label="服务器调优"
        isSelected={privileged}
        onValueChange={setPrivileged}
      />
    </div>
  );
  const commandBox = (
    <div className="relative">
      <Textarea
        readOnly
        className="font-mono text-sm"
        classNames={{
          input: "font-mono text-sm",
        }}
        maxRows={16}
        minRows={7}
        value={command}
        variant="bordered"
      />
      <Button
        className="absolute top-2 right-2"
        color="primary"
        size="sm"
        variant="flat"
        onPress={() => handleCopyCommand(command)}
      >
        复制
      </Button>
    </div>
  );

  return (
    <Modal
      backdrop="blur"
      classNames={{
        base: "!w-[calc(100%-32px)] !mx-auto sm:!w-full rounded-2xl overflow-hidden",
      }}
      isOpen={installCommandModal}
      placement="center"
      scrollBehavior="inside"
      size="2xl"
      onClose={() => setInstallCommandModal(false)}
    >
      <ModalContent>
        <ModalHeader closable>安装命令 - {currentNodeName}</ModalHeader>
        <ModalBody>
          <div className="space-y-4">
            {installCommands.docker || installCommands.dockerCompose ? (
              <Tabs
                aria-label="安装方式"
                selectedKey={mode}
                onSelectionChange={(key) => setMode(key as InstallMode)}
              >
                <Tab key="script" title="一键脚本">
                  <div className="space-y-3 pt-2">
                    <p className="text-sm text-default-600">
                      在服务器上以 root 执行，安装为 systemd / OpenRC 服务：
                    </p>
                    {commandBox}
                  </div>
                </Tab>
                <Tab key="docker" title="Docker">
                  <div className="space-y-3 pt-2">
                    <p className="text-sm text-default-600">
                      在已安装 Docker 的服务器上执行：
                    </p>
                    {privilegedControl}
                    {commandBox}
                    <ul className="list-disc space-y-1 pl-5 text-xs text-default-500">
                      <li>
                        使用 host 网络，数据保存在宿主机
                        /etc/flyx-agent（旧安装沿用原目录）； 不要和一键脚本装的
                        agent 同时运行。
                      </li>
                      <li>
                        面板一键升级会替换数据目录里的程序并自动重启容器；
                        拉取新镜像后重建容器也会升级。
                      </li>
                      <li>
                        NET_ADMIN 用于 nftables
                        流量统计和白名单；开启服务器调优会额外加入
                        --privileged。
                      </li>
                    </ul>
                  </div>
                </Tab>
                <Tab key="docker-compose" title="Docker Compose">
                  <div className="space-y-3 pt-2">
                    <p className="text-sm text-default-600">
                      在已安装 Docker Compose 的服务器上执行，会写入
                      /opt/docker/flyx-agent/docker-compose.yaml：
                    </p>
                    {privilegedControl}
                    {commandBox}
                    <ul className="list-disc space-y-1 pl-5 text-xs text-default-500">
                      <li>
                        自动检查并创建 /opt/docker/flyx-agent，配置写入后设置
                        600 权限；启动前会检查并移除已存在的 flyx-agent 容器。
                      </li>
                      <li>
                        使用 host 网络，数据保存在宿主机 /etc/flyx-agent；
                        检测到旧版 flux_agent 容器时会停止并提示先迁移。
                      </li>
                      <li>
                        Compose 文件包含节点密钥；开启服务器调优会写入
                        privileged: true。
                      </li>
                    </ul>
                  </div>
                </Tab>
              </Tabs>
            ) : (
              <>
                <p className="text-sm text-default-600">
                  请复制以下安装命令到服务器上执行：
                </p>
                {commandBox}
              </>
            )}
            <div className="text-xs text-default-500">
              💡 提示：如果复制按钮失效，请手动选择上方文本进行复制
            </div>
          </div>
        </ModalBody>
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
  return (
    <Modal
      backdrop="blur"
      classNames={{
        base: "!w-[calc(100%-32px)] !mx-auto sm:!w-full rounded-2xl overflow-hidden",
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
              <h2 className="text-xl font-bold">
                {upgradeTarget === "batch"
                  ? `批量升级 (${selectedIds.size} 个服务器)`
                  : "升级服务器"}
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
                    label="选择版本"
                    placeholder="留空则使用最新版本"
                    selectedKeys={selectedVersion ? [selectedVersion] : []}
                    onSelectionChange={(keys) => {
                      const selected = Array.from(keys)[0] as string;

                      setSelectedVersion(selected || "");
                    }}
                  >
                    {releases.map((r) => (
                      <SelectItem key={r.version} textValue={r.version}>
                        <div className="flex justify-between items-center">
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
                                测试
                              </Chip>
                            )}
                          </span>
                        </div>
                      </SelectItem>
                    ))}
                  </Select>
                  <p className="text-sm text-default-500">
                    {selectedVersion
                      ? `将升级到版本 ${selectedVersion}`
                      : "未选择版本，将自动使用最新版"}
                  </p>
                </div>
              )}
            </ModalBody>
            <ModalFooter>
              <Button variant="light" onPress={onClose}>
                取消
              </Button>
              <Button
                color="warning"
                isDisabled={releasesLoading}
                onPress={handleConfirmUpgrade}
              >
                升级
              </Button>
            </ModalFooter>
          </>
        )}
      </ModalContent>
    </Modal>
  );
}
