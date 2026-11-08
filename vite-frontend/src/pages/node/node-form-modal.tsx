import type { Dispatch, ReactNode, SetStateAction } from "react";

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
import { Accordion, AccordionItem } from "@/shadcn-bridge/heroui/accordion";
import { Select, SelectItem } from "@/shadcn-bridge/heroui/select";
import { nodeRegionLabel, nodeRegionOptions } from "@/pages/node/region";
import { NodeForm, TrafficCalc } from "@/pages/node/types";
import { BillingFields, type BillingValue } from "@/pages/node/billing-fields";
import { NODE_NOTIFY_EVENTS } from "@/utils/notification-events";

export interface NodeFormModalProps {
  dialogTitle: string;
  dialogVisible: boolean;
  errors: Record<string, string>;
  form: NodeForm;
  /** Groups other servers use, offered as suggestions. */
  groups: string[];
  handleSubmit: () => Promise<void>;
  protocolControlsDisabled: boolean;
  protocolControlsReason: string;
  setDialogVisible: Dispatch<SetStateAction<boolean>>;
  setForm: Dispatch<SetStateAction<NodeForm>>;
  submitLoading: boolean;
}

// Select keys can't be empty strings; these stand for "detect" / "up + down".
const REGION_AUTO = "auto";
const CALC_SUM = "sum";

const TRAFFIC_CALCS: Array<[TrafficCalc, string, string]> = [
  ["", "上下行相加", "上传和下载都算"],
  ["max", "取较大值", "上传、下载取多的一边"],
  ["up", "只算上行", "只算服务器发出的流量"],
  ["down", "只算下行", "只算服务器收到的流量"],
];

/** A titled part of the form; the first has no rule above it. */
function Section({
  title,
  hint,
  children,
}: {
  title: string;
  hint?: ReactNode;
  children: ReactNode;
}) {
  return (
    <section className="space-y-3 border-t border-divider pt-5 first:border-t-0 first:pt-0">
      <div className="flex items-baseline justify-between gap-3">
        <h3 className="text-sm font-semibold">{title}</h3>
        {hint && <p className="text-xs text-default-400">{hint}</p>}
      </div>
      {children}
    </section>
  );
}

/** A switch with its label and a line of help, in a bordered tile. */
function SwitchTile({
  label,
  help,
  isSelected,
  isDisabled,
  onChange,
}: {
  label: string;
  help: string;
  isSelected: boolean;
  isDisabled?: boolean;
  onChange: (v: boolean) => void;
}) {
  return (
    <div className="flex items-center justify-between gap-3 rounded-xl border border-divider px-3.5 py-3">
      <div className="min-w-0">
        <p className="text-sm font-medium">{label}</p>
        <p className="text-xs text-default-500">{help}</p>
      </div>
      <Switch
        aria-label={label}
        isDisabled={isDisabled}
        isSelected={isSelected}
        size="sm"
        onValueChange={onChange}
      />
    </div>
  );
}

/** Create or edit a node. */
export function NodeFormModal({
  dialogTitle,
  dialogVisible,
  errors,
  form,
  groups,
  handleSubmit,
  protocolControlsDisabled,
  protocolControlsReason,
  setDialogVisible,
  setForm,
  submitLoading,
}: NodeFormModalProps) {
  const set = <K extends keyof NodeForm>(key: K, value: NodeForm[K]) =>
    setForm((prev) => ({ ...prev, [key]: value }));
  const regionOptionLabel = (code: string) =>
    code !== REGION_AUTO
      ? nodeRegionLabel(code)
      : form.regionDetected
        ? `自动（${form.regionDetected}）`
        : "自动识别";
  const offlineNotify = form.notifyEvents.node_offline !== false;
  const groupSuggestions = groups.filter((g) => g && g !== form.group.trim());
  const billing: BillingValue = {
    price: form.price,
    currency: form.currency,
    renewalCycle: form.renewalCycle,
    expiryTime: form.expiryTime,
  };

  return (
    <Modal
      classNames={{
        base: "!w-[calc(100%-32px)] !mx-auto sm:!w-full overflow-hidden",
      }}
      isOpen={dialogVisible}
      placement="center"
      scrollBehavior="inside"
      size="4xl"
      onClose={() => setDialogVisible(false)}
    >
      <ModalContent>
        <ModalHeader>{dialogTitle}</ModalHeader>
        <ModalBody>
          <div className="space-y-5">
            <Section title="基本">
              <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
                <Input
                  errorMessage={errors.name}
                  isInvalid={!!errors.name}
                  label="名称"
                  placeholder="例如：香港 HKT"
                  value={form.name}
                  variant="bordered"
                  onChange={(e) => set("name", e.target.value)}
                />
                <div className="space-y-1.5">
                  <Input
                    description="公开页按它分组，留空为未分组"
                    errorMessage={errors.group}
                    isInvalid={!!errors.group}
                    label="分组"
                    maxLength={50}
                    placeholder="未分组"
                    value={form.group}
                    variant="bordered"
                    onChange={(e) => set("group", e.target.value)}
                  />
                  {groupSuggestions.length > 0 && (
                    <div className="flex flex-wrap gap-1.5">
                      {groupSuggestions.slice(0, 8).map((g) => (
                        <button
                          key={g}
                          className="rounded-md border border-dashed border-divider px-2 py-0.5 text-xs text-default-600 transition-colors hover:bg-default-100"
                          type="button"
                          onClick={() => set("group", g)}
                        >
                          {g}
                        </button>
                      ))}
                    </div>
                  )}
                </div>
                <Input
                  description="公开页可见"
                  errorMessage={errors.publicNote}
                  isInvalid={!!errors.publicNote}
                  label="公开备注"
                  maxLength={200}
                  placeholder="例如：CN2 GIA 三网直连"
                  value={form.publicNote}
                  variant="bordered"
                  onChange={(e) => set("publicNote", e.target.value)}
                />
                <Textarea
                  // One line like the fields beside it; grows as typed.
                  className="field-sizing-content min-h-9 resize-none"
                  description="仅管理员可见"
                  label="私有备注"
                  maxRows={4}
                  minRows={1}
                  placeholder="例如：搬瓦工年付，日本中转"
                  value={form.remark}
                  variant="bordered"
                  onChange={(e) => set("remark", e.target.value)}
                />
                <SwitchTile
                  help="关闭后只在管理后台可见"
                  isSelected={form.statusVisible}
                  label="公开显示"
                  onChange={(v) => set("statusVisible", v)}
                />
                <SwitchTile
                  help="掉线、恢复时各推一条"
                  isSelected={offlineNotify}
                  label="离线通知"
                  onChange={(v) =>
                    setForm((prev) => ({
                      ...prev,
                      notifyEvents: { ...prev.notifyEvents, node_offline: v },
                    }))
                  }
                />
              </div>
            </Section>

            <Section title="价格与到期">
              <BillingFields
                errors={errors}
                value={billing}
                onChange={(next) => setForm((prev) => ({ ...prev, ...next }))}
              />
            </Section>

            <Section title="流量">
              <div className="grid grid-cols-1 gap-4 sm:grid-cols-3">
                <Input
                  description="留空或 0 不限"
                  errorMessage={errors.trafficQuotaGb}
                  inputMode="numeric"
                  isInvalid={!!errors.trafficQuotaGb}
                  label="每月额度（GB）"
                  placeholder="不限"
                  value={form.trafficQuotaGb}
                  variant="bordered"
                  onChange={(e) =>
                    set("trafficQuotaGb", e.target.value.replace(/[^\d]/g, ""))
                  }
                />
                <Select
                  description={
                    TRAFFIC_CALCS.find(([k]) => k === form.trafficCalc)?.[2]
                  }
                  label="计算方式"
                  selectedKeys={[form.trafficCalc || CALC_SUM]}
                  variant="bordered"
                  onSelectionChange={(keys) => {
                    const k = Array.from(keys)[0] as string | undefined;

                    set(
                      "trafficCalc",
                      !k || k === CALC_SUM ? "" : (k as TrafficCalc),
                    );
                  }}
                >
                  {TRAFFIC_CALCS.map(([k, label]) => (
                    <SelectItem key={k || CALC_SUM} textValue={label}>
                      {label}
                    </SelectItem>
                  ))}
                </Select>
                <Input
                  description="1–31，月份没有这天时按月底"
                  errorMessage={errors.trafficResetDay}
                  inputMode="numeric"
                  isInvalid={!!errors.trafficResetDay}
                  label="每月重置日"
                  placeholder="1"
                  value={form.trafficResetDay}
                  variant="bordered"
                  onChange={(e) =>
                    set("trafficResetDay", e.target.value.replace(/[^\d]/g, ""))
                  }
                />
              </div>
              {form.id && (
                <details className="group rounded-xl border border-divider px-3.5 py-2.5 [&_summary::-webkit-details-marker]:hidden">
                  <summary className="flex cursor-pointer list-none items-center gap-1.5 text-sm">
                    <span className="text-default-400 transition-transform group-open:rotate-90">
                      ▸
                    </span>
                    流量校正
                    <span className="text-xs text-default-400">
                      统计和服务商对不上时，填服务商显示的本期用量
                    </span>
                  </summary>
                  <div className="mt-3 grid grid-cols-1 gap-4 sm:grid-cols-2">
                    <Input
                      errorMessage={errors.correctUpGb}
                      inputMode="decimal"
                      isInvalid={!!errors.correctUpGb}
                      label="本期上行（GB）"
                      placeholder="留空不改"
                      value={form.correctUpGb}
                      variant="bordered"
                      onChange={(e) =>
                        set(
                          "correctUpGb",
                          e.target.value.replace(/[^\d.]/g, ""),
                        )
                      }
                    />
                    <Input
                      errorMessage={errors.correctDownGb}
                      inputMode="decimal"
                      isInvalid={!!errors.correctDownGb}
                      label="本期下行（GB）"
                      placeholder="留空不改"
                      value={form.correctDownGb}
                      variant="bordered"
                      onChange={(e) =>
                        set(
                          "correctDownGb",
                          e.target.value.replace(/[^\d.]/g, ""),
                        )
                      }
                    />
                  </div>
                  <p className="mt-2 text-xs text-default-400">
                    保存后本期流量改为这里的数值，之后照常累加；只影响本期，到下个重置日自动失效
                  </p>
                </details>
              )}
            </Section>

            <Section
              hint="至少填一个；是这台服务器自己的地址，不是面板的"
              title="地址与地区"
            >
              <div className="grid grid-cols-1 gap-4 sm:grid-cols-[minmax(0,1fr)_minmax(0,1fr)_10rem]">
                <Input
                  errorMessage={errors.serverIpV4}
                  isInvalid={!!errors.serverIpV4}
                  label="IPv4"
                  placeholder="203.0.113.10"
                  value={form.serverIpV4}
                  variant="bordered"
                  onChange={(e) => set("serverIpV4", e.target.value)}
                />
                <Input
                  errorMessage={errors.serverIpV6}
                  isInvalid={!!errors.serverIpV6}
                  label="IPv6"
                  placeholder="2001:db8::10"
                  value={form.serverIpV6}
                  variant="bordered"
                  onChange={(e) => set("serverIpV6", e.target.value)}
                />
                <Select
                  label="国家/地区"
                  selectedKeys={[form.region || REGION_AUTO]}
                  variant="bordered"
                  onSelectionChange={(keys) => {
                    const selected = Array.from(keys)[0] as string | undefined;

                    set(
                      "region",
                      !selected || selected === REGION_AUTO ? "" : selected,
                    );
                  }}
                >
                  {[
                    REGION_AUTO,
                    ...nodeRegionOptions(form.region, form.regionDetected),
                  ].map((code) => (
                    <SelectItem key={code} textValue={regionOptionLabel(code)}>
                      {regionOptionLabel(code)}
                    </SelectItem>
                  ))}
                </Select>
              </div>
              <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
                <Input
                  description="没有固定 IP 时填，不带协议和端口"
                  errorMessage={errors.serverHost}
                  isInvalid={!!errors.serverHost}
                  label="域名"
                  placeholder="node.example.com"
                  value={form.serverHost}
                  variant="bordered"
                  onChange={(e) => set("serverHost", e.target.value)}
                />
                <Input
                  classNames={{ input: "font-mono" }}
                  description="单个、多个或范围，逗号分隔"
                  errorMessage={errors.port}
                  isInvalid={!!errors.port}
                  label="可用端口"
                  placeholder="80,443,10000-65535"
                  value={form.port}
                  variant="bordered"
                  onChange={(e) => set("port", e.target.value)}
                />
              </div>
            </Section>

            <Section title="转发方式">
              <div
                aria-label="转发方式"
                className="grid grid-cols-2 gap-2"
                role="radiogroup"
              >
                {(
                  [
                    ["agent", "agent", "服务器装 agent，支持全部功能（推荐）"],
                    ["nftables", "nftables", "面板经 SSH 下发规则，只能纯转发"],
                  ] as const
                ).map(([key, label, help]) => {
                  const on = form.forwardMode === key;

                  return (
                    <button
                      key={key}
                      aria-checked={on}
                      className={`rounded-xl border px-3.5 py-2.5 text-left transition-colors ${
                        on
                          ? "border-foreground bg-default-50"
                          : "border-divider hover:bg-default-50"
                      }`}
                      role="radio"
                      type="button"
                      onClick={() => set("forwardMode", key)}
                    >
                      <p className="text-sm font-medium">{label}</p>
                      <p className="text-xs text-default-500">{help}</p>
                    </button>
                  );
                })}
              </div>
              {form.forwardMode === "nftables" && (
                <div className="space-y-4 rounded-xl border border-warning-200 bg-warning-50/40 p-3.5 dark:border-warning-900/40 dark:bg-warning-950/20">
                  <p className="text-xs text-default-600">
                    面板通过 SSH 下发 nftables 规则，不安装
                    agent；不支持隧道、流量控制和节点内核
                  </p>
                  <div className="grid grid-cols-1 gap-4 sm:grid-cols-[minmax(0,1fr)_7rem]">
                    <Input
                      errorMessage={errors.sshHost}
                      isInvalid={!!errors.sshHost}
                      label="SSH 主机"
                      placeholder="192.0.2.10"
                      value={form.sshHost}
                      variant="bordered"
                      onChange={(e) => set("sshHost", e.target.value)}
                    />
                    <Input
                      errorMessage={errors.sshPort}
                      isInvalid={!!errors.sshPort}
                      label="端口"
                      placeholder="22"
                      value={form.sshPort}
                      variant="bordered"
                      onChange={(e) => set("sshPort", e.target.value)}
                    />
                  </div>
                  <div className="grid grid-cols-1 gap-4 sm:grid-cols-3">
                    <Input
                      errorMessage={errors.sshUsername}
                      isInvalid={!!errors.sshUsername}
                      label="用户名"
                      placeholder="root"
                      value={form.sshUsername}
                      variant="bordered"
                      onChange={(e) => set("sshUsername", e.target.value)}
                    />
                    <Select
                      label="认证方式"
                      selectedKeys={[form.sshAuthType]}
                      variant="bordered"
                      onSelectionChange={(keys) =>
                        set(
                          "sshAuthType",
                          (Array.from(keys)[0] as NodeForm["sshAuthType"]) ||
                            "private_key",
                        )
                      }
                    >
                      <SelectItem key="private_key" textValue="私钥">
                        私钥
                      </SelectItem>
                      <SelectItem key="password" textValue="密码">
                        密码
                      </SelectItem>
                    </Select>
                    <Select
                      label="提权方式"
                      selectedKeys={[form.sshSudoMode]}
                      variant="bordered"
                      onSelectionChange={(keys) =>
                        set(
                          "sshSudoMode",
                          (Array.from(keys)[0] as NodeForm["sshSudoMode"]) ||
                            "none",
                        )
                      }
                    >
                      <SelectItem key="none" textValue="无需提权">
                        无需提权
                      </SelectItem>
                      <SelectItem key="sudo" textValue="sudo">
                        sudo
                      </SelectItem>
                      <SelectItem key="sudo_su" textValue="sudo su">
                        sudo su
                      </SelectItem>
                    </Select>
                  </div>
                  {form.sshAuthType === "password" ? (
                    <Input
                      errorMessage={errors.sshPassword}
                      isInvalid={!!errors.sshPassword}
                      label="SSH 密码"
                      type="password"
                      value={form.sshPassword}
                      variant="bordered"
                      onChange={(e) => set("sshPassword", e.target.value)}
                    />
                  ) : (
                    <>
                      <Textarea
                        errorMessage={errors.sshPrivateKey}
                        isInvalid={!!errors.sshPrivateKey}
                        label="SSH 私钥"
                        minRows={4}
                        placeholder="粘贴 PEM 格式私钥"
                        value={form.sshPrivateKey}
                        variant="bordered"
                        onChange={(e) => set("sshPrivateKey", e.target.value)}
                      />
                      <Input
                        label="私钥密码短语"
                        placeholder="可选"
                        type="password"
                        value={form.sshPassphrase}
                        variant="bordered"
                        onChange={(e) => set("sshPassphrase", e.target.value)}
                      />
                    </>
                  )}
                </div>
              )}
            </Section>

            <Accordion className="px-0" variant="light">
              <AccordionItem
                key="notifications"
                aria-label="通知设置"
                className="[&_[data-slot=accordion-trigger]]:no-underline [&_[data-slot=accordion-trigger]]:hover:no-underline"
                title={
                  <span className="text-sm font-semibold">
                    通知
                    <span className="ml-2 text-xs font-normal text-default-400">
                      {Object.keys(form.notifyEvents).length
                        ? `已自定义 ${Object.keys(form.notifyEvents).length} 项`
                        : "跟随全局"}
                    </span>
                  </span>
                }
              >
                <div className="space-y-3 pb-3">
                  <p className="text-xs text-default-500">
                    只对这台服务器生效，使用全局推送渠道；需先在「设置 →
                    通知」打开通知
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
                aria-label="高级"
                className="border-b-0 [&_[data-slot=accordion-trigger]]:no-underline [&_[data-slot=accordion-trigger]]:hover:no-underline"
                title={
                  <span className="text-sm font-semibold">
                    高级
                    <span className="ml-2 text-xs font-normal text-default-400">
                      出口网卡、额外 IP、监听地址、屏蔽协议
                    </span>
                  </span>
                }
              >
                <div className="space-y-4 pb-2">
                  <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
                    <Input
                      description="多 IP 服务器指定用哪个出去，不懂留空"
                      errorMessage={errors.interfaceName}
                      isInvalid={!!errors.interfaceName}
                      label="出口网卡名或 IP"
                      value={form.interfaceName}
                      variant="bordered"
                      onChange={(e) => set("interfaceName", e.target.value)}
                    />
                    <Input
                      description="多 IP 服务器的其他 IP，逗号分隔"
                      label="额外 IP"
                      placeholder="192.0.2.11, 192.0.2.12"
                      value={form.extraIPs}
                      variant="bordered"
                      onChange={(e) => set("extraIPs", e.target.value)}
                    />
                    <Input
                      errorMessage={errors.tcpListenAddr}
                      isInvalid={!!errors.tcpListenAddr}
                      label="TCP 监听地址"
                      value={form.tcpListenAddr}
                      variant="bordered"
                      onChange={(e) => set("tcpListenAddr", e.target.value)}
                    />
                    <Input
                      errorMessage={errors.udpListenAddr}
                      isInvalid={!!errors.udpListenAddr}
                      label="UDP 监听地址"
                      value={form.udpListenAddr}
                      variant="bordered"
                      onChange={(e) => set("udpListenAddr", e.target.value)}
                    />
                  </div>
                  <div className="space-y-2">
                    <p className="text-sm font-medium">屏蔽协议</p>
                    <p className="text-xs text-default-500">
                      {protocolControlsDisabled
                        ? protocolControlsReason
                        : "打开即屏蔽该协议；只在入口服务器上开，出口服务器开了会影响转发"}
                    </p>
                    <div className="grid grid-cols-3 gap-2">
                      {(
                        [
                          ["http", "HTTP"],
                          ["tls", "TLS"],
                          ["socks", "SOCKS"],
                        ] as const
                      ).map(([key, label]) => (
                        <div
                          key={key}
                          className={`flex items-center justify-between rounded-xl border border-divider px-3 py-2 ${
                            protocolControlsDisabled ? "opacity-60" : ""
                          }`}
                        >
                          <span className="text-sm">{label}</span>
                          <Switch
                            aria-label={`屏蔽 ${label}`}
                            isDisabled={protocolControlsDisabled}
                            isSelected={form[key] === 1}
                            size="sm"
                            onValueChange={(v) => set(key, v ? 1 : 0)}
                          />
                        </div>
                      ))}
                    </div>
                  </div>
                </div>
              </AccordionItem>
            </Accordion>
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
