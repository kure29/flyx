import type { Dispatch, SetStateAction } from "react";

import { Button } from "@/shadcn-bridge/heroui/button";
import { Input, Textarea } from "@/shadcn-bridge/heroui/input";
import { Select, SelectItem } from "@/shadcn-bridge/heroui/select";
import {
  Modal,
  ModalContent,
  ModalHeader,
  ModalBody,
  ModalFooter,
} from "@/shadcn-bridge/heroui/modal";
import { Chip } from "@/shadcn-bridge/heroui/chip";
import { Divider } from "@/shadcn-bridge/heroui/divider";
import { Accordion, AccordionItem } from "@/shadcn-bridge/heroui/accordion";
import { getTunnelForwardMode } from "@/pages/tunnel/helpers";
import { ChainTunnel, Node, TunnelForm } from "@/pages/tunnel/types";

export interface TunnelFormModalProps {
  errors: { [key: string]: string };
  form: TunnelForm;
  getChainGroups: () => ChainTunnel[][];
  getCommonIpOptions: (nodeIds: number[]) => string[];
  getSelectedChainNodeIds: () => number[];
  handleSubmit: () => Promise<void>;
  handleTypeChange: (type: number) => void;
  isEdit: boolean;
  mergeOrderedNodes: (
    currentNodes: ChainTunnel[],
    selectedNodeIds: number[],
    buildDefault: (nodeId: number) => ChainTunnel,
  ) => ChainTunnel[];
  modalOpen: boolean;
  nodes: Node[];
  removeChainNode: (groupIndex: number) => void;
  setForm: Dispatch<SetStateAction<TunnelForm>>;
  setModalOpen: Dispatch<SetStateAction<boolean>>;
  submitLoading: boolean;
  syncChainGroupNodes: (groupIndex: number, selectedNodeIds: number[]) => void;
  toSelectedNodeIds: (keys: Iterable<unknown>) => number[];
  updateChainConnectIp: (groupIndex: number, connectIp: string) => void;
  updateChainProtocol: (groupIndex: number, protocol: string) => void;
  updateChainStrategy: (groupIndex: number, strategy: string) => void;
}

/** Create or edit a tunnel: entry nodes, forwarding chain and exit. */
export function TunnelFormModal({
  errors,
  form,
  getChainGroups,
  getCommonIpOptions,
  getSelectedChainNodeIds,
  handleSubmit,
  handleTypeChange,
  isEdit,
  mergeOrderedNodes,
  modalOpen,
  nodes,
  removeChainNode,
  setForm,
  setModalOpen,
  submitLoading,
  syncChainGroupNodes,
  toSelectedNodeIds,
  updateChainConnectIp,
  updateChainProtocol,
  updateChainStrategy,
}: TunnelFormModalProps) {
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
                {isEdit ? "编辑隧道" : "新增隧道"}
              </h2>
              <p className="text-small text-default-500">
                {isEdit
                  ? "修改服务器配置会中断现有连接，隧道类型不可修改"
                  : "创建新的隧道配置"}
              </p>
            </ModalHeader>
            <ModalBody>
              <div className="space-y-4">
                <Input
                  errorMessage={errors.name}
                  isInvalid={!!errors.name}
                  label="隧道名称"
                  placeholder="请输入隧道名称"
                  value={form.name}
                  variant="bordered"
                  onChange={(e) =>
                    setForm((prev) => ({ ...prev, name: e.target.value }))
                  }
                />

                <Select
                  description={isEdit ? "编辑时无法修改隧道类型" : undefined}
                  errorMessage={errors.type}
                  isDisabled={isEdit}
                  isInvalid={!!errors.type}
                  label="隧道类型"
                  placeholder="请选择隧道类型"
                  selectedKeys={[form.type.toString()]}
                  variant="bordered"
                  onSelectionChange={(keys) => {
                    const selectedKey = Array.from(keys)[0] as string;

                    if (selectedKey) {
                      handleTypeChange(parseInt(selectedKey));
                    }
                  }}
                >
                  <SelectItem key="1">端口转发</SelectItem>
                  <SelectItem key="2">隧道转发</SelectItem>
                </Select>

                {form.type === 1 &&
                  (getTunnelForwardMode(form.inNodeId, nodes) === "nftables" ? (
                    <p className="text-small text-default-500">
                      入口是 nftables 服务器，由 nftables
                      转发（在服务器的转发模式里设置）
                    </p>
                  ) : (
                    <Select
                      description={
                        form.engine === "realm"
                          ? "入口服务器自动安装 realm 运行这条隧道的转发；不支持限速和连接数限制，服务器 agent 需 3.3.26 及以上"
                          : "服务器 agent 直接转发，支持限速和连接数限制"
                      }
                      label="转发引擎"
                      selectedKeys={[
                        form.engine === "realm" ? "realm" : "agent",
                      ]}
                      variant="bordered"
                      onSelectionChange={(keys) => {
                        const selectedKey = Array.from(keys)[0] as string;

                        if (selectedKey) {
                          setForm((prev) => ({
                            ...prev,
                            engine: selectedKey === "realm" ? "realm" : "",
                          }));
                        }
                      }}
                    >
                      <SelectItem key="agent">
                        面板转发（默认，功能最全）
                      </SelectItem>
                      <SelectItem key="realm">
                        realm（占用低，不支持限速）
                      </SelectItem>
                    </Select>
                  ))}

                <Divider />
                <h3 className="text-lg font-semibold">入口配置</h3>

                <div className="space-y-2">
                  <Select
                    disabledKeys={[
                      ...nodes
                        .filter(
                          (node) =>
                            node.status !== 1 &&
                            !(
                              isEdit &&
                              form.inNodeId.some((ct) => ct.nodeId === node.id)
                            ),
                        )
                        .map((node) => node.id.toString()),
                      ...(form.outNodeId || []).map((ct) =>
                        ct.nodeId.toString(),
                      ),
                      ...getSelectedChainNodeIds().map((id) => id.toString()),
                    ]}
                    errorMessage={errors.inNodeId}
                    isInvalid={!!errors.inNodeId}
                    label="入口服务器"
                    placeholder="请选择入口服务器（可多选）"
                    selectedKeys={form.inNodeId.map((ct) =>
                      ct.nodeId.toString(),
                    )}
                    selectionMode="multiple"
                    variant="bordered"
                    onSelectionChange={(keys) => {
                      const selectedIds = toSelectedNodeIds(keys);

                      setForm((prev) => ({
                        ...prev,
                        inNodeId: mergeOrderedNodes(
                          prev.inNodeId,
                          selectedIds,
                          (nodeId) => ({ nodeId, chainType: 1 }),
                        ),
                      }));
                    }}
                  >
                    {nodes.map((node) => (
                      <SelectItem key={node.id} textValue={`${node.name}`}>
                        <div className="flex items-center justify-between">
                          <span>{node.name}</span>
                          <div className="flex items-center gap-2">
                            <Chip
                              color={node.status === 1 ? "success" : "default"}
                              size="sm"
                              variant="flat"
                            >
                              {node.status === 1 ? "在线" : "离线"}
                            </Chip>
                            {form.outNodeId &&
                              form.outNodeId.some(
                                (ct) => ct.nodeId === node.id,
                              ) && (
                                <Chip color="danger" size="sm" variant="flat">
                                  已选为出口
                                </Chip>
                              )}
                            {getSelectedChainNodeIds().includes(node.id) && (
                              <Chip color="primary" size="sm" variant="flat">
                                已选为转发链
                              </Chip>
                            )}
                          </div>
                        </div>
                      </SelectItem>
                    ))}
                  </Select>
                </div>

                {/* 隧道转发时显示转发链配置 */}
                {form.type === 2 && (
                  <>
                    <Divider />
                    <div className="flex items-center justify-between">
                      <h3 className="text-lg font-semibold">转发链配置</h3>
                      <Button
                        color="primary"
                        size="sm"
                        startContent={
                          <svg
                            aria-hidden="true"
                            className="w-4 h-4"
                            fill="none"
                            stroke="currentColor"
                            viewBox="0 0 24 24"
                          >
                            <path
                              d="M12 4v16m8-8H4"
                              strokeLinecap="round"
                              strokeLinejoin="round"
                              strokeWidth={2}
                            />
                          </svg>
                        }
                        variant="flat"
                        onPress={() => {
                          // 添加新的一跳（一个空组，或包含占位节点）
                          setForm((prev) => ({
                            ...prev,
                            chainNodes: [
                              ...(prev.chainNodes || []),
                              [
                                {
                                  nodeId: -1,
                                  chainType: 2,
                                  protocol: "tls",
                                  strategy: "round",
                                },
                              ],
                            ],
                          }));
                        }}
                      >
                        添加一跳
                      </Button>
                    </div>

                    {getChainGroups().length > 0 && (
                      <div className="space-y-3">
                        {getChainGroups().map((groupNodes, groupIndex) => {
                          const protocol =
                            groupNodes.length > 0
                              ? groupNodes[0].protocol || "tls"
                              : "tls";
                          const strategy =
                            groupNodes.length > 0
                              ? groupNodes[0].strategy || "round"
                              : "round";
                          const groupSelectedNodeIds = groupNodes
                            .filter((ct) => ct.nodeId !== -1)
                            .map((ct) => ct.nodeId);
                          const groupIpOptions =
                            getCommonIpOptions(groupSelectedNodeIds);
                          const isMultiNodeGroup =
                            groupSelectedNodeIds.length > 1;
                          const selectedGroupConnectIp =
                            groupNodes.length > 0
                              ? groupNodes[0].connectIp || ""
                              : "";

                          return (
                            <div
                              key={groupIndex}
                              className="border border-default-200 rounded-lg p-3"
                            >
                              <div className="flex items-center justify-between mb-2">
                                <span className="text-sm font-medium text-default-600">
                                  第{groupIndex + 1}跳
                                </span>
                                <Button
                                  color="danger"
                                  size="sm"
                                  variant="light"
                                  onPress={() => removeChainNode(groupIndex)}
                                >
                                  移除
                                </Button>
                              </div>

                              <div className="grid grid-cols-1 md:grid-cols-4 gap-2">
                                {/* 节点选择 - 移动端100%，桌面端50% */}
                                <div className="col-span-1 md:col-span-2">
                                  <Select
                                    classNames={{
                                      label: "text-xs",
                                      value: "text-sm",
                                    }}
                                    disabledKeys={[
                                      ...nodes
                                        .filter(
                                          (node) =>
                                            node.status !== 1 &&
                                            !(
                                              isEdit &&
                                              groupNodes.some(
                                                (ct) =>
                                                  ct.nodeId === node.id &&
                                                  ct.nodeId !== -1,
                                              )
                                            ),
                                        )
                                        .map((node) => node.id.toString()),
                                      ...form.inNodeId.map((ct) =>
                                        ct.nodeId.toString(),
                                      ),
                                      ...(form.outNodeId || []).map((ct) =>
                                        ct.nodeId.toString(),
                                      ),
                                      // 排除其他跳数已选的节点
                                      ...(form.chainNodes || [])
                                        .flatMap((group, idx) =>
                                          idx !== groupIndex
                                            ? group.map((ct) => ct.nodeId)
                                            : [],
                                        )
                                        .filter((id) => id !== -1)
                                        .map((id) => id.toString()),
                                    ]}
                                    dropdownPlacement="top"
                                    label="服务器"
                                    placeholder="选择服务器（可多选）"
                                    selectedKeys={groupNodes
                                      .filter((ct) => ct.nodeId !== -1)
                                      .map((ct) => ct.nodeId.toString())}
                                    selectionMode="multiple"
                                    size="sm"
                                    variant="bordered"
                                    onSelectionChange={(keys) => {
                                      syncChainGroupNodes(
                                        groupIndex,
                                        toSelectedNodeIds(keys),
                                      );
                                    }}
                                  >
                                    {nodes.map((node) => (
                                      <SelectItem
                                        key={node.id}
                                        textValue={`${node.name}`}
                                      >
                                        <div className="flex items-center justify-between">
                                          <span className="text-sm">
                                            {node.name}
                                          </span>
                                          <div className="flex items-center gap-2">
                                            <Chip
                                              color={
                                                node.status === 1
                                                  ? "success"
                                                  : "default"
                                              }
                                              size="sm"
                                              variant="flat"
                                            >
                                              {node.status === 1
                                                ? "在线"
                                                : "离线"}
                                            </Chip>
                                            {form.inNodeId.some(
                                              (ct) => ct.nodeId === node.id,
                                            ) && (
                                              <Chip
                                                color="warning"
                                                size="sm"
                                                variant="flat"
                                              >
                                                已选为入口
                                              </Chip>
                                            )}
                                            {form.outNodeId &&
                                              form.outNodeId.some(
                                                (ct) => ct.nodeId === node.id,
                                              ) && (
                                                <Chip
                                                  color="danger"
                                                  size="sm"
                                                  variant="flat"
                                                >
                                                  已选为出口
                                                </Chip>
                                              )}
                                            {/* 显示是否在其他跳数中被选择 */}
                                            {(form.chainNodes || []).some(
                                              (group, idx) =>
                                                idx !== groupIndex &&
                                                group.some(
                                                  (ct) =>
                                                    ct.nodeId === node.id &&
                                                    ct.nodeId !== -1,
                                                ),
                                            ) && (
                                              <Chip
                                                color="primary"
                                                size="sm"
                                                variant="flat"
                                              >
                                                已选为其他跳
                                              </Chip>
                                            )}
                                          </div>
                                        </div>
                                      </SelectItem>
                                    ))}
                                  </Select>
                                </div>

                                {/* 协议选择 - 25% */}
                                <Select
                                  classNames={{
                                    label: "text-xs",
                                    value: "text-sm",
                                  }}
                                  label="协议"
                                  placeholder="选择协议"
                                  selectedKeys={[protocol]}
                                  size="sm"
                                  variant="bordered"
                                  onSelectionChange={(keys) => {
                                    const selectedKey = Array.from(
                                      keys,
                                    )[0] as string;

                                    if (selectedKey) {
                                      updateChainProtocol(
                                        groupIndex,
                                        selectedKey,
                                      );
                                    }
                                  }}
                                >
                                  <SelectItem key="tls">TLS</SelectItem>
                                  <SelectItem key="wss">WSS</SelectItem>
                                  <SelectItem key="tcp">TCP</SelectItem>
                                  <SelectItem key="mtls">MTLS</SelectItem>
                                  <SelectItem key="mwss">MWSS</SelectItem>
                                  <SelectItem key="mtcp">MTCP</SelectItem>
                                  <SelectItem key="kcp">KCP</SelectItem>
                                </Select>

                                {/* 负载策略 - 25% */}
                                <Select
                                  classNames={{
                                    label: "text-xs",
                                    value: "text-sm",
                                  }}
                                  label="负载策略"
                                  placeholder="选择策略"
                                  selectedKeys={[strategy]}
                                  size="sm"
                                  variant="bordered"
                                  onSelectionChange={(keys) => {
                                    const selectedKey = Array.from(
                                      keys,
                                    )[0] as string;

                                    if (selectedKey) {
                                      updateChainStrategy(
                                        groupIndex,
                                        selectedKey,
                                      );
                                    }
                                  }}
                                >
                                  <SelectItem key="fifo">主备</SelectItem>
                                  <SelectItem key="round">轮询</SelectItem>
                                  <SelectItem key="rand">随机</SelectItem>
                                </Select>
                              </div>

                              {/* 连接IP - 转发链节点 */}
                              <Select
                                classNames={{
                                  label: "text-xs",
                                  value: "text-sm",
                                }}
                                description={
                                  isMultiNodeGroup
                                    ? "多服务器跳不支持设置自定义连接IP，使用各服务器默认IP"
                                    : "按当前跳所选服务器的共有IP进行选择，留空使用默认"
                                }
                                isDisabled={
                                  groupSelectedNodeIds.length === 0 ||
                                  groupIpOptions.length === 0 ||
                                  isMultiNodeGroup
                                }
                                label="连接IP"
                                placeholder={
                                  isMultiNodeGroup
                                    ? "多服务器跳使用服务器默认IP"
                                    : groupSelectedNodeIds.length === 0
                                      ? "请先选择服务器"
                                      : groupIpOptions.length > 0
                                        ? "选择连接IP"
                                        : "所选服务器无共同可选IP"
                                }
                                selectedKeys={[
                                  selectedGroupConnectIp || "__default__",
                                ]}
                                size="sm"
                                variant="bordered"
                                onSelectionChange={(keys) => {
                                  const selectedKey = Array.from(
                                    keys,
                                  )[0] as string;

                                  updateChainConnectIp(
                                    groupIndex,
                                    selectedKey === "__default__"
                                      ? ""
                                      : selectedKey,
                                  );
                                }}
                              >
                                <SelectItem key="__default__">
                                  默认连接IP
                                </SelectItem>
                                {groupIpOptions.map((ip) => (
                                  <SelectItem key={ip}>{ip}</SelectItem>
                                ))}
                              </Select>
                            </div>
                          );
                        })}
                      </div>
                    )}

                    {getChainGroups().length === 0 && (
                      <div className="text-center py-8 bg-default-50 dark:bg-default-100/50 rounded border border-dashed border-default-300">
                        <p className="text-sm text-default-500">
                          还没有添加转发链，点击上方&quot;添加一跳&quot;按钮开始添加
                        </p>
                      </div>
                    )}
                  </>
                )}

                {/* 隧道转发时显示出口配置 */}
                {form.type === 2 && (
                  <>
                    <Divider />
                    <h3 className="text-lg font-semibold">出口配置</h3>

                    {(() => {
                      const selectedOutNodeIds = (form.outNodeId || [])
                        .filter((ct) => ct.nodeId !== -1)
                        .map((ct) => ct.nodeId);
                      const isMultiExit = selectedOutNodeIds.length > 1;
                      const commonOutIpOptions =
                        getCommonIpOptions(selectedOutNodeIds);

                      return (
                        <>
                          <div className="grid grid-cols-1 md:grid-cols-4 gap-2">
                            {/* 节点选择 - 移动端100%，桌面端50% */}
                            <div className="col-span-1 md:col-span-2">
                              <Select
                                classNames={{
                                  label: "text-xs",
                                  value: "text-sm",
                                }}
                                disabledKeys={[
                                  ...nodes
                                    .filter(
                                      (node) =>
                                        node.status !== 1 &&
                                        !(
                                          isEdit &&
                                          (form.outNodeId || []).some(
                                            (ct) =>
                                              ct.nodeId === node.id &&
                                              ct.nodeId !== -1,
                                          )
                                        ),
                                    )
                                    .map((node) => node.id.toString()),
                                  ...form.inNodeId.map((ct) =>
                                    ct.nodeId.toString(),
                                  ),
                                  ...getSelectedChainNodeIds().map((id) =>
                                    id.toString(),
                                  ),
                                ]}
                                dropdownPlacement="top"
                                errorMessage={errors.outNodeId}
                                isInvalid={!!errors.outNodeId}
                                label="服务器"
                                placeholder="请选择出口服务器（可多选）"
                                selectedKeys={
                                  form.outNodeId
                                    ? form.outNodeId
                                        .filter((ct) => ct.nodeId !== -1)
                                        .map((ct) => ct.nodeId.toString())
                                    : []
                                }
                                selectionMode="multiple"
                                variant="bordered"
                                onSelectionChange={(keys) => {
                                  const selectedIds = toSelectedNodeIds(keys);

                                  setForm((prev) => {
                                    const currentOutNodes =
                                      prev.outNodeId || [];
                                    const protocol =
                                      currentOutNodes[0]?.protocol || "tls";
                                    const strategy =
                                      currentOutNodes[0]?.strategy || "round";
                                    const realNodes = currentOutNodes.filter(
                                      (ct) => ct.nodeId !== -1,
                                    );

                                    return {
                                      ...prev,
                                      outNodeId: mergeOrderedNodes(
                                        realNodes,
                                        selectedIds,
                                        (nodeId) => ({
                                          nodeId,
                                          chainType: 3,
                                          protocol,
                                          strategy,
                                        }),
                                      ),
                                    };
                                  });
                                }}
                              >
                                {nodes.map((node) => (
                                  <SelectItem
                                    key={node.id}
                                    textValue={`${node.name}`}
                                  >
                                    <div className="flex items-center justify-between">
                                      <span>{node.name}</span>
                                      <div className="flex items-center gap-2">
                                        <Chip
                                          color={
                                            node.status === 1
                                              ? "success"
                                              : "default"
                                          }
                                          size="sm"
                                          variant="flat"
                                        >
                                          {node.status === 1 ? "在线" : "离线"}
                                        </Chip>
                                        {form.inNodeId.some(
                                          (ct) => ct.nodeId === node.id,
                                        ) && (
                                          <Chip
                                            color="warning"
                                            size="sm"
                                            variant="flat"
                                          >
                                            已选为入口
                                          </Chip>
                                        )}
                                        {getSelectedChainNodeIds().includes(
                                          node.id,
                                        ) && (
                                          <Chip
                                            color="primary"
                                            size="sm"
                                            variant="flat"
                                          >
                                            已选为转发链
                                          </Chip>
                                        )}
                                      </div>
                                    </div>
                                  </SelectItem>
                                ))}
                              </Select>
                            </div>

                            {/* 协议选择 - 25% */}
                            <Select
                              classNames={{
                                label: "text-xs",
                                value: "text-sm",
                              }}
                              errorMessage={errors.protocol}
                              isInvalid={!!errors.protocol}
                              label="协议"
                              placeholder="选择协议"
                              selectedKeys={[
                                (() => {
                                  if (
                                    !form.outNodeId ||
                                    form.outNodeId.length === 0
                                  )
                                    return "tls";

                                  return form.outNodeId[0].protocol || "tls";
                                })(),
                              ]}
                              variant="bordered"
                              onSelectionChange={(keys) => {
                                const selectedKey = Array.from(
                                  keys,
                                )[0] as string;

                                if (selectedKey) {
                                  setForm((prev) => {
                                    const currentOutNodes =
                                      prev.outNodeId || [];
                                    const currentStrategy =
                                      currentOutNodes.length > 0
                                        ? currentOutNodes[0].strategy || "round"
                                        : "round";

                                    if (currentOutNodes.length === 0) {
                                      // 如果还没有出口节点，创建一个占位节点保存设置
                                      return {
                                        ...prev,
                                        outNodeId: [
                                          {
                                            nodeId: -1,
                                            chainType: 3,
                                            protocol: selectedKey,
                                            strategy: currentStrategy,
                                          },
                                        ],
                                      };
                                    }

                                    // 更新所有出口节点的协议
                                    return {
                                      ...prev,
                                      outNodeId: currentOutNodes.map((ct) => ({
                                        ...ct,
                                        protocol: selectedKey,
                                      })),
                                    };
                                  });
                                }
                              }}
                            >
                              <SelectItem key="tls">TLS</SelectItem>
                              <SelectItem key="wss">WSS</SelectItem>
                              <SelectItem key="tcp">TCP</SelectItem>
                              <SelectItem key="mtls">MTLS</SelectItem>
                              <SelectItem key="mwss">MWSS</SelectItem>
                              <SelectItem key="mtcp">MTCP</SelectItem>
                              <SelectItem key="kcp">KCP</SelectItem>
                            </Select>

                            {/* 负载策略 - 25% */}
                            <Select
                              classNames={{
                                label: "text-xs",
                                value: "text-sm",
                              }}
                              label="负载策略"
                              placeholder="选择策略"
                              selectedKeys={[
                                (() => {
                                  if (
                                    !form.outNodeId ||
                                    form.outNodeId.length === 0
                                  )
                                    return "round";

                                  return form.outNodeId[0].strategy || "round";
                                })(),
                              ]}
                              variant="bordered"
                              onSelectionChange={(keys) => {
                                const selectedKey = Array.from(
                                  keys,
                                )[0] as string;

                                if (selectedKey) {
                                  setForm((prev) => {
                                    const currentOutNodes =
                                      prev.outNodeId || [];
                                    const currentProtocol =
                                      currentOutNodes.length > 0
                                        ? currentOutNodes[0].protocol || "tls"
                                        : "tls";

                                    if (currentOutNodes.length === 0) {
                                      return {
                                        ...prev,
                                        outNodeId: [
                                          {
                                            nodeId: -1,
                                            chainType: 3,
                                            protocol: currentProtocol,
                                            strategy: selectedKey,
                                          },
                                        ],
                                      };
                                    }

                                    return {
                                      ...prev,
                                      outNodeId: currentOutNodes.map((ct) => ({
                                        ...ct,
                                        strategy: selectedKey,
                                      })),
                                    };
                                  });
                                }
                              }}
                            >
                              <SelectItem key="fifo">主备</SelectItem>
                              <SelectItem key="round">轮询</SelectItem>
                              <SelectItem key="rand">随机</SelectItem>
                              <SelectItem key="best">最优</SelectItem>
                            </Select>
                          </div>

                          {/* 连接IP - 出口节点 */}
                          <Select
                            classNames={{
                              label: "text-xs",
                              value: "text-sm",
                            }}
                            description={
                              isMultiExit
                                ? "多出口隧道不支持设置自定义连接IP，使用各服务器默认IP"
                                : "按出口服务器共同可用IP选择，留空使用默认"
                            }
                            isDisabled={
                              selectedOutNodeIds.length === 0 ||
                              commonOutIpOptions.length === 0 ||
                              isMultiExit
                            }
                            label="连接IP"
                            placeholder={
                              isMultiExit
                                ? "多出口隧道使用服务器默认IP"
                                : selectedOutNodeIds.length === 0
                                  ? "请先选择出口服务器"
                                  : commonOutIpOptions.length > 0
                                    ? "选择连接IP"
                                    : "所选服务器无共同可选IP"
                            }
                            selectedKeys={[
                              form.outNodeId && form.outNodeId.length > 0
                                ? form.outNodeId[0].connectIp || "__default__"
                                : "__default__",
                            ]}
                            size="sm"
                            variant="bordered"
                            onSelectionChange={(keys) => {
                              const selectedKey = Array.from(keys)[0] as string;
                              const value =
                                selectedKey === "__default__"
                                  ? ""
                                  : selectedKey;

                              setForm((prev) => {
                                const currentOutNodes = prev.outNodeId || [];

                                if (currentOutNodes.length === 0) {
                                  return {
                                    ...prev,
                                    outNodeId: [
                                      {
                                        nodeId: -1,
                                        chainType: 3,
                                        protocol: "tls",
                                        strategy: "round",
                                        connectIp: value,
                                      },
                                    ],
                                  };
                                }

                                return {
                                  ...prev,
                                  outNodeId: currentOutNodes.map((ct) => ({
                                    ...ct,
                                    connectIp: value,
                                  })),
                                };
                              });
                            }}
                          >
                            <SelectItem key="__default__">
                              默认连接IP
                            </SelectItem>
                            {commonOutIpOptions.map((ip) => (
                              <SelectItem key={ip}>{ip}</SelectItem>
                            ))}
                          </Select>
                        </>
                      );
                    })()}
                  </>
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
                      <Textarea
                        description="入口IP由系统自动从入口服务器采集，无需手动填写。支持多个IP，每行一个地址，留空则使用入口服务器IP"
                        errorMessage={errors.inIp}
                        isInvalid={!!errors.inIp}
                        label="入口IP"
                        maxRows={5}
                        minRows={3}
                        placeholder="一行一个IP地址或域名，例如:&#10;192.168.1.100&#10;example.com"
                        value={form.inIp}
                        variant="bordered"
                        onChange={(e) =>
                          setForm((prev) => ({
                            ...prev,
                            inIp: e.target.value,
                          }))
                        }
                      />

                      {form.type === 2 && (
                        <Select
                          description="当服务器同时拥有IPv4和IPv6地址时，选择隧道连接使用的地址类型"
                          label="隧道连接地址偏好"
                          placeholder="自动选择"
                          selectedKeys={[form.ipPreference || ""]}
                          variant="bordered"
                          onSelectionChange={(keys) => {
                            const selectedKey = Array.from(keys)[0] as string;

                            setForm((prev) => ({
                              ...prev,
                              ipPreference: selectedKey || "",
                            }));
                          }}
                        >
                          <SelectItem key="v4">优先IPv4</SelectItem>
                          <SelectItem key="v6">优先IPv6</SelectItem>
                        </Select>
                      )}

                      <div>
                        <div className="text-sm font-medium">质量检测目标</div>
                        <p className="text-xs text-default-500 mt-0.5">
                          用于实时隧道质量检测、诊断目标和 best
                          最优出口评分，留空使用 www.bing.com:443
                        </p>
                      </div>
                      <div className="grid grid-cols-1 md:grid-cols-[1fr_140px] gap-3">
                        <Input
                          errorMessage={errors.probeTargetHost}
                          isInvalid={!!errors.probeTargetHost}
                          label="Host"
                          placeholder="www.bing.com"
                          value={form.probeTargetHost || ""}
                          variant="bordered"
                          onChange={(e) =>
                            setForm((prev) => ({
                              ...prev,
                              probeTargetHost: e.target.value,
                            }))
                          }
                        />
                        <Input
                          errorMessage={errors.probeTargetPort}
                          isInvalid={!!errors.probeTargetPort}
                          label="Port"
                          max={65535}
                          min={1}
                          placeholder="443"
                          type="number"
                          value={
                            form.probeTargetPort
                              ? String(form.probeTargetPort)
                              : ""
                          }
                          variant="bordered"
                          onChange={(e) =>
                            setForm((prev) => ({
                              ...prev,
                              probeTargetPort: e.target.value
                                ? Number(e.target.value)
                                : 0,
                            }))
                          }
                        />
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
                {submitLoading
                  ? isEdit
                    ? "更新中..."
                    : "创建中..."
                  : isEdit
                    ? "更新"
                    : "创建"}
              </Button>
            </ModalFooter>
          </>
        )}
      </ModalContent>
    </Modal>
  );
}
