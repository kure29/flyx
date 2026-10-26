import type { Dispatch, SetStateAction } from "react";

import { Button } from "@/shadcn-bridge/heroui/button";
import { Input } from "@/shadcn-bridge/heroui/input";
import { Textarea } from "@/shadcn-bridge/heroui/input";
import {
  Modal,
  ModalContent,
  ModalHeader,
  ModalBody,
  ModalFooter,
} from "@/shadcn-bridge/heroui/modal";
import { Switch } from "@/shadcn-bridge/heroui/switch";
import { Alert } from "@/shadcn-bridge/heroui/alert";
import { Accordion, AccordionItem } from "@/shadcn-bridge/heroui/accordion";
import { Select, SelectItem } from "@/shadcn-bridge/heroui/select";
import { type NodeRenewalCycle } from "@/pages/node/renewal";
import { NodeForm } from "@/pages/node/types";
import { NODE_NOTIFY_EVENTS } from "@/utils/notification-events";

export interface NodeFormModalProps {
  dialogTitle: string;
  dialogVisible: boolean;
  errors: Record<string, string>;
  form: NodeForm;
  handleSubmit: () => Promise<void>;
  protocolControlsDisabled: boolean;
  protocolControlsReason: string;
  setDialogVisible: Dispatch<SetStateAction<boolean>>;
  setForm: Dispatch<SetStateAction<NodeForm>>;
  submitLoading: boolean;
}

/** Create or edit a node. */
export function NodeFormModal({
  dialogTitle,
  dialogVisible,
  errors,
  form,
  handleSubmit,
  protocolControlsDisabled,
  protocolControlsReason,
  setDialogVisible,
  setForm,
  submitLoading,
}: NodeFormModalProps) {
  return (
    <Modal
      backdrop="blur"
      classNames={{
        base: "!w-[calc(100%-32px)] !mx-auto sm:!w-full rounded-2xl overflow-hidden",
      }}
      isOpen={dialogVisible}
      placement="center"
      scrollBehavior="inside"
      size="2xl"
      onClose={() => setDialogVisible(false)}
    >
      <ModalContent>
        <ModalHeader>{dialogTitle}</ModalHeader>
        <ModalBody>
          <div className="space-y-4">
            <Input
              errorMessage={errors.name}
              isInvalid={!!errors.name}
              label="服务器名称"
              placeholder="请输入服务器名称"
              value={form.name}
              variant="bordered"
              onChange={(e) =>
                setForm((prev) => ({ ...prev, name: e.target.value }))
              }
            />

            <Textarea
              description="可记录供应商、用途、续费说明等补充信息"
              label="备注"
              maxRows={4}
              minRows={3}
              placeholder="例如: 搬瓦工年付，2026-12 续费，日本中转"
              value={form.remark}
              variant="bordered"
              onChange={(e) =>
                setForm((prev) => ({ ...prev, remark: e.target.value }))
              }
            />

            <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
              <Select
                label="续费周期"
                placeholder="选择续费周期"
                selectedKeys={form.renewalCycle ? [form.renewalCycle] : []}
                variant="bordered"
                onSelectionChange={(keys) => {
                  const selected = Array.from(keys)[0] as
                    | NodeRenewalCycle
                    | undefined;

                  setForm((prev) => ({
                    ...prev,
                    renewalCycle: selected || "",
                  }));
                }}
              >
                <SelectItem key="month" textValue="month">
                  月付
                </SelectItem>
                <SelectItem key="quarter" textValue="quarter">
                  季付
                </SelectItem>
                <SelectItem key="year" textValue="year">
                  年付
                </SelectItem>
              </Select>
            </div>

            <Input
              description="填写最近一次续费时间或周期起始时间，系统会按月/季/年自动推算下次续费"
              errorMessage={errors.expiryTime}
              isInvalid={!!errors.expiryTime}
              label="续费基准时间"
              max="9999-12-31"
              type="date"
              value={
                form.expiryTime > 0
                  ? new Date(form.expiryTime).toISOString().slice(0, 10)
                  : ""
              }
              variant="bordered"
              onChange={(e) =>
                setForm((prev) => ({
                  ...prev,
                  expiryTime: e.target.value
                    ? new Date(e.target.value).getTime()
                    : 0,
                }))
              }
            />

            <Alert
              color="primary"
              description="例如选择月付并填写 2026-03-01，系统会自动按每月同日推算下次续费时间。"
              variant="flat"
            />

            <Input
              description="可选：不带协议、不带端口。建议在 IPv4 和 IPv6 都未填写时使用。至少填写一个 IPv4/IPv6 域名"
              errorMessage={errors.serverHost}
              isInvalid={!!errors.serverHost}
              label="域名/主机名"
              placeholder="例如: test.example.com"
              value={form.serverHost}
              variant="bordered"
              onChange={(e) =>
                setForm((prev) => ({ ...prev, serverHost: e.target.value }))
              }
            />

            <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
              <Input
                description="填写一个 IPv4 地址"
                errorMessage={errors.serverIpV4}
                isInvalid={!!errors.serverIpV4}
                label="IPv4 地址"
                placeholder="例如: 192.168.1.100"
                value={form.serverIpV4}
                variant="bordered"
                onChange={(e) =>
                  setForm((prev) => ({ ...prev, serverIpV4: e.target.value }))
                }
              />

              <Input
                description="填写一个 IPv6 地址"
                errorMessage={errors.serverIpV6}
                isInvalid={!!errors.serverIpV6}
                label="IPv6 地址"
                placeholder="例如: 2001:db8::10"
                value={form.serverIpV6}
                variant="bordered"
                onChange={(e) =>
                  setForm((prev) => ({ ...prev, serverIpV6: e.target.value }))
                }
              />
            </div>

            <Input
              classNames={{
                input: "font-mono",
              }}
              description="支持单个端口(80)、多个端口(80,443)或端口范围(10000-65535)，多个可用逗号分隔"
              errorMessage={errors.port}
              isInvalid={!!errors.port}
              label="可用端口"
              placeholder="例如: 80,443,10000-65535"
              value={form.port}
              variant="bordered"
              onChange={(e) =>
                setForm((prev) => ({ ...prev, port: e.target.value }))
              }
            />

            <Select
              label="转发模式"
              selectedKeys={[form.forwardMode]}
              variant="bordered"
              onSelectionChange={(keys) => {
                const selected = Array.from(keys)[0] as
                  | NodeForm["forwardMode"]
                  | undefined;

                setForm((prev) => ({
                  ...prev,
                  forwardMode: selected || "agent",
                }));
              }}
            >
              <SelectItem key="agent" textValue="agent">
                agent 模式
              </SelectItem>
              <SelectItem key="nftables" textValue="nftables">
                nftables 模式
              </SelectItem>
            </Select>

            {form.forwardMode === "nftables" ? (
              <Alert
                color="warning"
                description="nftables 模式仅支持纯转发，不支持隧道、流量控制和 agent 安装，规则将由面板通过 SSH 下发。"
                variant="flat"
              />
            ) : (
              <Alert
                color="primary"
                description="agent 模式会继续使用服务器 agent 执行转发与管理操作。"
                variant="flat"
              />
            )}

            {/* 高级配置 */}
            <Accordion className="px-0" variant="light">
              <AccordionItem
                key="notifications"
                aria-label="通知设置"
                className="[&_[data-slot=accordion-trigger]]:no-underline [&_[data-slot=accordion-trigger]]:hover:no-underline"
                title={
                  <span className="text-small text-default-500 font-medium">
                    通知设置
                    <span className="ml-2 text-xs font-normal">
                      {Object.keys(form.notifyEvents).length
                        ? `已自定义 ${Object.keys(form.notifyEvents).length} 项`
                        : "跟随全局"}
                    </span>
                  </span>
                }
              >
                <div className="space-y-3 pb-3">
                  <p className="text-xs text-default-500">
                    仅对这台服务器生效，使用全局推送渠道。需先在「设置 →
                    通知」启用通知总开关；跟随全局时使用全局事件选择。
                  </p>
                  <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
                    {NODE_NOTIFY_EVENTS.map((event) => (
                      <Select
                        key={event.key}
                        description={
                          event.key === "latency_bad"
                            ? `${event.description}；关闭后仍保留检测和图表`
                            : event.description
                        }
                        label={event.label}
                        selectedKeys={[
                          form.notifyEvents[event.key] === undefined
                            ? "inherit"
                            : form.notifyEvents[event.key]
                              ? "on"
                              : "off",
                        ]}
                        size="sm"
                        variant="bordered"
                        onSelectionChange={(keys) => {
                          const selected = Array.from(keys)[0];

                          setForm((prev) => {
                            const notifyEvents = { ...prev.notifyEvents };

                            if (selected === "on" || selected === "off") {
                              notifyEvents[event.key] = selected === "on";
                            } else {
                              delete notifyEvents[event.key];
                            }

                            return { ...prev, notifyEvents };
                          });
                        }}
                      >
                        <SelectItem key="inherit">跟随全局</SelectItem>
                        <SelectItem key="on">开启</SelectItem>
                        <SelectItem key="off">关闭</SelectItem>
                      </Select>
                    ))}
                  </div>
                </div>
              </AccordionItem>
              <AccordionItem
                key="advanced"
                aria-label="高级配置"
                className="border-b-0 [&_[data-slot=accordion-trigger]]:no-underline [&_[data-slot=accordion-trigger]]:hover:no-underline"
                title={
                  <span className="text-small text-default-500 font-medium">
                    高级配置
                  </span>
                }
              >
                <div className="space-y-4 pb-2">
                  {form.forwardMode === "nftables" ? (
                    <div className="space-y-4 rounded-xl border border-warning-200 bg-warning-50/60 p-4 dark:border-warning-500/30 dark:bg-warning-950/20">
                      <div>
                        <div className="text-sm font-medium text-warning-700 dark:text-warning-300">
                          SSH 配置
                        </div>
                        <div className="text-xs text-default-500">
                          面板会通过 SSH 下发 nftables 规则，不会安装 agent。
                        </div>
                      </div>

                      <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
                        <Input
                          errorMessage={errors.sshHost}
                          isInvalid={!!errors.sshHost}
                          label="SSH 主机"
                          placeholder="例如: 192.0.2.10"
                          value={form.sshHost}
                          variant="bordered"
                          onChange={(e) =>
                            setForm((prev) => ({
                              ...prev,
                              sshHost: e.target.value,
                            }))
                          }
                        />

                        <Input
                          errorMessage={errors.sshPort}
                          isInvalid={!!errors.sshPort}
                          label="SSH 端口"
                          placeholder="22"
                          value={form.sshPort}
                          variant="bordered"
                          onChange={(e) =>
                            setForm((prev) => ({
                              ...prev,
                              sshPort: e.target.value,
                            }))
                          }
                        />
                      </div>

                      <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
                        <Input
                          errorMessage={errors.sshUsername}
                          isInvalid={!!errors.sshUsername}
                          label="SSH 用户名"
                          placeholder="例如: root"
                          value={form.sshUsername}
                          variant="bordered"
                          onChange={(e) =>
                            setForm((prev) => ({
                              ...prev,
                              sshUsername: e.target.value,
                            }))
                          }
                        />

                        <Select
                          label="SSH 认证方式"
                          selectedKeys={[form.sshAuthType]}
                          variant="bordered"
                          onSelectionChange={(keys) => {
                            const selected = Array.from(keys)[0] as
                              | "password"
                              | "private_key"
                              | undefined;

                            setForm((prev) => ({
                              ...prev,
                              sshAuthType: selected || "private_key",
                            }));
                          }}
                        >
                          <SelectItem key="private_key" textValue="private_key">
                            私钥
                          </SelectItem>
                          <SelectItem key="password" textValue="password">
                            密码
                          </SelectItem>
                        </Select>
                      </div>

                      {form.sshAuthType === "password" ? (
                        <Input
                          errorMessage={errors.sshPassword}
                          isInvalid={!!errors.sshPassword}
                          label="SSH 密码"
                          placeholder="请输入 SSH 密码"
                          type="password"
                          value={form.sshPassword}
                          variant="bordered"
                          onChange={(e) =>
                            setForm((prev) => ({
                              ...prev,
                              sshPassword: e.target.value,
                            }))
                          }
                        />
                      ) : (
                        <Textarea
                          errorMessage={errors.sshPrivateKey}
                          isInvalid={!!errors.sshPrivateKey}
                          label="SSH 私钥"
                          minRows={6}
                          placeholder="粘贴 PEM 格式私钥内容"
                          value={form.sshPrivateKey}
                          variant="bordered"
                          onChange={(e) =>
                            setForm((prev) => ({
                              ...prev,
                              sshPrivateKey: e.target.value,
                            }))
                          }
                        />
                      )}

                      <Input
                        label="SSH 私钥密码短语"
                        placeholder="可选"
                        type="password"
                        value={form.sshPassphrase}
                        variant="bordered"
                        onChange={(e) =>
                          setForm((prev) => ({
                            ...prev,
                            sshPassphrase: e.target.value,
                          }))
                        }
                      />

                      <Select
                        label="sudo 提权方式"
                        selectedKeys={[form.sshSudoMode]}
                        variant="bordered"
                        onSelectionChange={(keys) => {
                          const selected = Array.from(keys)[0] as
                            | "none"
                            | "sudo"
                            | "sudo_su"
                            | undefined;

                          setForm((prev) => ({
                            ...prev,
                            sshSudoMode: selected || "none",
                          }));
                        }}
                      >
                        <SelectItem key="none" textValue="none">
                          无需提权
                        </SelectItem>
                        <SelectItem key="sudo" textValue="sudo">
                          sudo
                        </SelectItem>
                        <SelectItem key="sudo_su" textValue="sudo_su">
                          sudo su
                        </SelectItem>
                      </Select>
                    </div>
                  ) : (
                    <Alert
                      color="primary"
                      description="agent 模式下无需填写 SSH 配置；服务器安装 agent 后会自行接管转发。"
                      variant="flat"
                    />
                  )}

                  <Input
                    description="用于多IP服务器指定使用那个IP请求远程地址，不懂的默认为空就行"
                    errorMessage={errors.interfaceName}
                    isInvalid={!!errors.interfaceName}
                    label="出口网卡名或IP"
                    placeholder="请输入出口网卡名或IP"
                    value={form.interfaceName}
                    variant="bordered"
                    onChange={(e) =>
                      setForm((prev) => ({
                        ...prev,
                        interfaceName: e.target.value,
                      }))
                    }
                  />

                  <Input
                    description="多IP服务器可填写额外IP地址，逗号分隔"
                    label="额外IP地址"
                    placeholder="例如: 192.168.1.100, 10.0.0.5"
                    value={form.extraIPs}
                    variant="bordered"
                    onChange={(e) =>
                      setForm((prev) => ({
                        ...prev,
                        extraIPs: e.target.value,
                      }))
                    }
                  />

                  <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
                    <Input
                      errorMessage={errors.tcpListenAddr}
                      isInvalid={!!errors.tcpListenAddr}
                      label="TCP监听地址"
                      placeholder="请输入TCP监听地址"
                      startContent={
                        <div className="pointer-events-none flex items-center">
                          <span className="text-default-400 text-small">
                            TCP
                          </span>
                        </div>
                      }
                      value={form.tcpListenAddr}
                      variant="bordered"
                      onChange={(e) =>
                        setForm((prev) => ({
                          ...prev,
                          tcpListenAddr: e.target.value,
                        }))
                      }
                    />

                    <Input
                      errorMessage={errors.udpListenAddr}
                      isInvalid={!!errors.udpListenAddr}
                      label="UDP监听地址"
                      placeholder="请输入UDP监听地址"
                      startContent={
                        <div className="pointer-events-none flex items-center">
                          <span className="text-default-400 text-small">
                            UDP
                          </span>
                        </div>
                      }
                      value={form.udpListenAddr}
                      variant="bordered"
                      onChange={(e) =>
                        setForm((prev) => ({
                          ...prev,
                          udpListenAddr: e.target.value,
                        }))
                      }
                    />
                  </div>
                  {/* 屏蔽协议 */}
                  <div>
                    <div className="text-sm font-medium text-default-700 mb-2">
                      屏蔽协议
                    </div>
                    <div className="text-xs text-default-500 mb-2">
                      开启开关以屏蔽对应协议
                    </div>
                    {protocolControlsDisabled && (
                      <Alert
                        className="mb-2"
                        color="warning"
                        description={protocolControlsReason}
                        variant="flat"
                      />
                    )}
                    <div
                      className={`grid grid-cols-1 sm:grid-cols-3 gap-3 bg-content1/30 dark:bg-content1/20 p-3 rounded-md border border-divider ${protocolControlsDisabled ? "opacity-70" : ""}`}
                    >
                      {/* HTTP tile */}
                      <div className="px-3 py-3 rounded-lg bg-content1/55 dark:bg-content1/35 border border-divider hover:border-primary-200 dark:hover:border-primary-500/30 transition-colors">
                        <div className="flex items-center gap-2 mb-2">
                          <svg
                            aria-hidden="true"
                            className="w-4 h-4 text-default-500"
                            fill="none"
                            stroke="currentColor"
                            strokeLinecap="round"
                            strokeLinejoin="round"
                            strokeWidth="2"
                            viewBox="0 0 24 24"
                          >
                            <rect height="16" rx="2" width="20" x="2" y="4" />
                            <path d="M2 10h20" />
                          </svg>
                          <div className="text-sm font-medium text-default-700">
                            HTTP
                          </div>
                        </div>
                        <div className="flex items-center justify-between">
                          <div className="text-xs text-default-500">
                            禁用/启用
                          </div>
                          <Switch
                            isDisabled={protocolControlsDisabled}
                            isSelected={form.http === 1}
                            size="sm"
                            onValueChange={(v) =>
                              setForm((prev) => ({
                                ...prev,
                                http: v ? 1 : 0,
                              }))
                            }
                          />
                        </div>
                        <div className="mt-1 text-xs text-default-400">
                          {form.http === 1 ? "已开启" : "已关闭"}
                        </div>
                      </div>

                      {/* TLS tile */}
                      <div className="px-3 py-3 rounded-lg bg-content1/55 dark:bg-content1/35 border border-divider hover:border-primary-200 dark:hover:border-primary-500/30 transition-colors">
                        <div className="flex items-center gap-2 mb-2">
                          <svg
                            aria-hidden="true"
                            className="w-4 h-4 text-default-500"
                            fill="none"
                            stroke="currentColor"
                            strokeLinecap="round"
                            strokeLinejoin="round"
                            strokeWidth="2"
                            viewBox="0 0 24 24"
                          >
                            <path d="M6 10V7a6 6 0 1 1 12 0v3" />
                            <rect height="10" rx="2" width="16" x="4" y="10" />
                          </svg>
                          <div className="text-sm font-medium text-default-700">
                            TLS
                          </div>
                        </div>
                        <div className="flex items-center justify-between">
                          <div className="text-xs text-default-500">
                            禁用/启用
                          </div>
                          <Switch
                            isDisabled={protocolControlsDisabled}
                            isSelected={form.tls === 1}
                            size="sm"
                            onValueChange={(v) =>
                              setForm((prev) => ({ ...prev, tls: v ? 1 : 0 }))
                            }
                          />
                        </div>
                        <div className="mt-1 text-xs text-default-400">
                          {form.tls === 1 ? "已开启" : "已关闭"}
                        </div>
                      </div>

                      {/* SOCKS tile */}
                      <div className="px-3 py-3 rounded-lg bg-content1/55 dark:bg-content1/35 border border-divider hover:border-primary-200 dark:hover:border-primary-500/30 transition-colors">
                        <div className="flex items-center gap-2 mb-2">
                          <svg
                            aria-hidden="true"
                            className="w-4 h-4 text-default-500"
                            fill="none"
                            stroke="currentColor"
                            strokeLinecap="round"
                            strokeLinejoin="round"
                            strokeWidth="2"
                            viewBox="0 0 24 24"
                          >
                            <path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4" />
                            <polyline points="7 10 12 15 17 10" />
                            <line x1="12" x2="12" y1="15" y2="3" />
                          </svg>
                          <div className="text-sm font-medium text-default-700">
                            SOCKS
                          </div>
                        </div>
                        <div className="flex items-center justify-between">
                          <div className="text-xs text-default-500">
                            禁用/启用
                          </div>
                          <Switch
                            isDisabled={protocolControlsDisabled}
                            isSelected={form.socks === 1}
                            size="sm"
                            onValueChange={(v) =>
                              setForm((prev) => ({
                                ...prev,
                                socks: v ? 1 : 0,
                              }))
                            }
                          />
                        </div>
                        <div className="mt-1 text-xs text-default-400">
                          {form.socks === 1 ? "已开启" : "已关闭"}
                        </div>
                      </div>
                    </div>
                  </div>

                  <Alert
                    color="danger"
                    description="请不要在出口服务器执行屏蔽协议，否则可能影响转发；屏蔽协议仅需在入口服务器执行。"
                    variant="flat"
                  />
                </div>
              </AccordionItem>
            </Accordion>

            <Alert
              className="mt-4"
              color="primary"
              description="服务器ip地址是你要添加的入口/出口的ip地址，不是面板的ip地址。"
              variant="flat"
            />
          </div>
        </ModalBody>
        <ModalFooter>
          <Button variant="light" onPress={() => setDialogVisible(false)}>
            取消
          </Button>
          <Button
            color="primary"
            isLoading={submitLoading}
            onPress={handleSubmit}
          >
            {submitLoading ? "提交中..." : form.id ? "保存" : "创建"}
          </Button>
        </ModalFooter>
      </ModalContent>
    </Modal>
  );
}
