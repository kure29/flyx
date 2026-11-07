import type { Dispatch, SetStateAction } from "react";

import { Button } from "@/shadcn-bridge/heroui/button";
import { Input } from "@/shadcn-bridge/heroui/input";
import { Textarea } from "@/shadcn-bridge/heroui/input";
import { Select, SelectItem } from "@/shadcn-bridge/heroui/select";
import {
  Modal,
  ModalContent,
  ModalHeader,
  ModalBody,
  ModalFooter,
} from "@/shadcn-bridge/heroui/modal";
import { Accordion, AccordionItem } from "@/shadcn-bridge/heroui/accordion";
import { Tunnel, ForwardForm } from "@/pages/forward/types";

export interface ForwardFormModalProps {
  currentTunnelIpOptions: string[];
  currentTunnelPortRange: { min: number; max: number } | null;
  errors: { [key: string]: string };
  form: ForwardForm;
  getAddressCount: (addressString: string) => number;
  handleSubmit: () => Promise<void>;
  handleTunnelChange: (tunnelId: string) => void;
  isCurrentTunnelMultiEntrance: boolean;
  isEdit: boolean;
  modalOpen: boolean;
  setForm: Dispatch<SetStateAction<ForwardForm>>;
  setInIpTouched: Dispatch<SetStateAction<boolean>>;
  setModalOpen: Dispatch<SetStateAction<boolean>>;
  submitLoading: boolean;
  tunnels: Tunnel[];
}

/** Create or edit a forward. */
export function ForwardFormModal({
  currentTunnelIpOptions,
  currentTunnelPortRange,
  errors,
  form,
  getAddressCount,
  handleSubmit,
  handleTunnelChange,
  isCurrentTunnelMultiEntrance,
  isEdit,
  modalOpen,
  setForm,
  setInIpTouched,
  setModalOpen,
  submitLoading,
  tunnels,
}: ForwardFormModalProps) {
  return (
    <Modal
      classNames={{
        base: "!w-[calc(100%-32px)] !mx-auto sm:!w-full overflow-hidden",
      }}
      isOpen={modalOpen}
      placement="center"
      scrollBehavior="inside"
      size="2xl"
      onOpenChange={setModalOpen}
    >
      <ModalContent>
        {(onClose) => (
          <>
            <ModalHeader className="flex flex-col gap-1">
              <h2 className="text-xl font-bold">
                {isEdit ? "编辑规则" : "新增规则"}
              </h2>
              <p className="text-small text-default-500">
                {isEdit ? "修改现有规则配置的信息" : "创建新的规则配置"}
              </p>
            </ModalHeader>
            <ModalBody>
              <div className="space-y-4 pb-4">
                <Input
                  errorMessage={errors.name}
                  isInvalid={!!errors.name}
                  label="规则名称"
                  placeholder="请输入规则名称"
                  value={form.name}
                  variant="bordered"
                  onChange={(e) =>
                    setForm((prev) => ({ ...prev, name: e.target.value }))
                  }
                />

                <Select
                  description={
                    isEdit
                      ? "更改隧道将释放原端口并在新隧道分配端口"
                      : undefined
                  }
                  errorMessage={errors.tunnelId}
                  isInvalid={!!errors.tunnelId}
                  label="选择隧道"
                  placeholder="请选择关联的隧道"
                  selectedKeys={form.tunnelId ? [form.tunnelId.toString()] : []}
                  variant="bordered"
                  onSelectionChange={(keys) => {
                    const selectedKey = Array.from(keys)[0] as string;

                    if (selectedKey) {
                      handleTunnelChange(selectedKey);
                    }
                  }}
                >
                  {tunnels.map((tunnel) => (
                    <SelectItem key={tunnel.id}>{tunnel.name}</SelectItem>
                  ))}
                </Select>

                <Input
                  description={
                    currentTunnelPortRange
                      ? `指定入口端口，留空自动分配 (允许范围: ${currentTunnelPortRange.min}-${currentTunnelPortRange.max})`
                      : "指定入口端口，留空则从服务器可用端口中自动分配"
                  }
                  errorMessage={errors.inPort}
                  isInvalid={!!errors.inPort}
                  label="入口端口"
                  placeholder="留空则自动分配可用端口"
                  type="number"
                  value={form.inPort !== null ? form.inPort.toString() : ""}
                  variant="bordered"
                  onChange={(e) => {
                    const value = e.target.value;

                    setForm((prev) => ({
                      ...prev,
                      inPort: value ? parseInt(value) : null,
                    }));
                  }}
                />

                <Select
                  description={
                    isCurrentTunnelMultiEntrance
                      ? "多入口隧道不支持自定义监听IP，使用各服务器默认IP"
                      : "从入口服务器IP中选择，留空使用默认"
                  }
                  isDisabled={
                    !form.tunnelId ||
                    currentTunnelIpOptions.length === 0 ||
                    isCurrentTunnelMultiEntrance
                  }
                  label="监听IP"
                  placeholder={
                    isCurrentTunnelMultiEntrance
                      ? "多入口隧道使用服务器默认IP"
                      : form.tunnelId
                        ? currentTunnelIpOptions.length > 0
                          ? "选择入口监听IP"
                          : "当前隧道入口服务器暂无可选IP"
                        : "请先选择隧道"
                  }
                  selectedKeys={[form.inIp || "__default__"]}
                  variant="bordered"
                  onSelectionChange={(keys) => {
                    const selectedKey = Array.from(keys)[0] as string;

                    setInIpTouched(true);

                    setForm((prev) => ({
                      ...prev,
                      inIp: selectedKey === "__default__" ? "" : selectedKey,
                    }));
                  }}
                >
                  <SelectItem key="__default__">默认入口IP</SelectItem>
                  {currentTunnelIpOptions.map((ip) => (
                    <SelectItem key={ip}>{ip}</SelectItem>
                  ))}
                </Select>

                <Textarea
                  description="格式: IP:端口 或 域名:端口，支持多个地址（每行一个）"
                  errorMessage={errors.remoteAddr}
                  isInvalid={!!errors.remoteAddr}
                  label="落地地址"
                  maxRows={6}
                  minRows={3}
                  placeholder="请输入落地地址，多个地址用换行分隔，例如:&#10;192.168.1.100:10000&#10;[2001:db8::10]:10086&#10;test.example.com:10010"
                  value={form.remoteAddr}
                  variant="bordered"
                  onChange={(e) =>
                    setForm((prev) => ({
                      ...prev,
                      remoteAddr: e.target.value,
                    }))
                  }
                />

                {getAddressCount(form.remoteAddr) > 1 && (
                  <Select
                    description="多个目标地址的负载均衡策略"
                    label="负载策略"
                    placeholder="请选择负载均衡策略"
                    selectedKeys={[form.strategy]}
                    variant="bordered"
                    onSelectionChange={(keys) => {
                      const selectedKey = Array.from(keys)[0] as string;

                      setForm((prev) => ({ ...prev, strategy: selectedKey }));
                    }}
                  >
                    <SelectItem key="fifo">主备模式 - 自上而下</SelectItem>
                    <SelectItem key="round">轮询模式 - 依次轮换</SelectItem>
                    <SelectItem key="rand">随机模式 - 随机选择</SelectItem>
                    <SelectItem key="hash">哈希模式 - IP哈希</SelectItem>
                  </Select>
                )}
                <Accordion className="px-0" variant="light">
                  <AccordionItem
                    key="advanced"
                    aria-label="高级设置"
                    className="border-b-0 [&_[data-slot=accordion-trigger]]:no-underline [&_[data-slot=accordion-trigger]]:hover:no-underline"
                    title={
                      <span className="text-small text-default-500 font-medium">
                        高级设置
                      </span>
                    }
                  >
                    <div className="space-y-4 pb-2">
                      <div className="grid grid-cols-1 gap-4 md:grid-cols-2">
                        <Select
                          description="入口监听接收 PROXY protocol，用于读取上游传入的真实客户端 IP。"
                          label="Proxy Protocol 接收"
                          placeholder="禁用"
                          selectedKeys={[
                            String(form.proxyProtocolReceive || 0),
                          ]}
                          variant="bordered"
                          onSelectionChange={(keys) => {
                            const selectedKey = Array.from(keys)[0] as string;

                            setForm((prev) => ({
                              ...prev,
                              proxyProtocolReceive: Number(selectedKey),
                            }));
                          }}
                        >
                          <SelectItem key="0">禁用</SelectItem>
                          <SelectItem key="1">Version 1</SelectItem>
                          <SelectItem key="2">Version 2</SelectItem>
                        </Select>
                        <Select
                          description="连接目标地址时发送 PROXY protocol，用于向下游透传客户端真实 IP。"
                          label="Proxy Protocol 发送"
                          placeholder="禁用"
                          selectedKeys={[String(form.proxyProtocolSend || 0)]}
                          variant="bordered"
                          onSelectionChange={(keys) => {
                            const selectedKey = Array.from(keys)[0] as string;
                            const proxyProtocolSend = Number(selectedKey);

                            setForm((prev) => ({
                              ...prev,
                              proxyProtocol: proxyProtocolSend,
                              proxyProtocolSend,
                            }));
                          }}
                        >
                          <SelectItem key="0">禁用</SelectItem>
                          <SelectItem key="1">Version 1</SelectItem>
                          <SelectItem key="2">Version 2</SelectItem>
                        </Select>
                      </div>
                    </div>
                  </AccordionItem>
                </Accordion>
              </div>
            </ModalBody>
            <ModalFooter>
              <Button variant="light" onPress={onClose}>
                取消
              </Button>
              <Button
                color="primary"
                isLoading={submitLoading}
                onPress={handleSubmit}
              >
                {isEdit ? "保存修改" : "创建规则"}
              </Button>
            </ModalFooter>
          </>
        )}
      </ModalContent>
    </Modal>
  );
}
