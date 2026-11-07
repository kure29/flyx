import type { TunnelDeletePreviewApiData } from "@/api/types";
import type { Dispatch, SetStateAction } from "react";

import { Button } from "@/shadcn-bridge/heroui/button";
import { Select, SelectItem } from "@/shadcn-bridge/heroui/select";
import {
  Modal,
  ModalContent,
  ModalHeader,
  ModalBody,
  ModalFooter,
} from "@/shadcn-bridge/heroui/modal";
import { Chip } from "@/shadcn-bridge/heroui/chip";
import { Spinner } from "@/shadcn-bridge/heroui/spinner";
import { Alert } from "@/shadcn-bridge/heroui/alert";
import { Radio, RadioGroup } from "@/shadcn-bridge/heroui/radio";
import { Tunnel, TunnelDeleteAction } from "@/pages/tunnel/types";

export interface DeleteTunnelModalProps {
  confirmDelete: () => Promise<void>;
  deleteAction: TunnelDeleteAction;
  deleteConfirmLabel:
    | "迁移规则后删除该隧道"
    | "删除规则并删除该隧道"
    | "删除该隧道";
  deleteHasForwardDependencies: boolean;
  deleteLoading: boolean;
  deleteModalOpen: boolean;
  deletePreviewForwardCount: number;
  deletePreviewLoading: boolean;
  deleteReplaceUnavailable: boolean;
  deleteReplacementTunnels: Tunnel[];
  deleteTargetTunnelId: number | null;
  handleDeleteModalOpenChange: (open: boolean) => void;
  setDeleteAction: Dispatch<SetStateAction<TunnelDeleteAction>>;
  setDeleteTargetTunnelId: Dispatch<SetStateAction<number | null>>;
  tunnelDeletePreview: TunnelDeletePreviewApiData | null;
  tunnelToDelete: Tunnel | null;
}

/** Delete one tunnel, moving or deleting its forwards. */
export function DeleteTunnelModal({
  confirmDelete,
  deleteAction,
  deleteConfirmLabel,
  deleteHasForwardDependencies,
  deleteLoading,
  deleteModalOpen,
  deletePreviewForwardCount,
  deletePreviewLoading,
  deleteReplaceUnavailable,
  deleteReplacementTunnels,
  deleteTargetTunnelId,
  handleDeleteModalOpenChange,
  setDeleteAction,
  setDeleteTargetTunnelId,
  tunnelDeletePreview,
  tunnelToDelete,
}: DeleteTunnelModalProps) {
  return (
    <Modal
      classNames={{
        base: "!w-[calc(100%-32px)] !mx-auto sm:!w-full overflow-hidden",
      }}
      isOpen={deleteModalOpen}
      placement="center"
      scrollBehavior="inside"
      size="2xl"
      onOpenChange={handleDeleteModalOpenChange}
    >
      <ModalContent>
        {(onClose) => (
          <>
            <ModalHeader className="flex flex-col gap-1">
              <h2 className="text-lg font-bold sm:text-xl">删除隧道</h2>
              <p className="text-xs font-normal leading-5 text-default-500 sm:text-sm">
                {tunnelDeletePreview?.tunnelName || tunnelToDelete?.name
                  ? `即将删除“${tunnelDeletePreview?.tunnelName || tunnelToDelete?.name}”，删除前会先检查是否有关联规则。`
                  : "删除前会先检查是否有关联规则。"}
              </p>
            </ModalHeader>
            <ModalBody className="space-y-3 sm:space-y-4">
              {deletePreviewLoading ? (
                <div className="flex items-center gap-3 py-5 text-sm text-default-600">
                  <Spinner size="sm" />
                  正在检查是否有规则正在使用该隧道...
                </div>
              ) : deleteHasForwardDependencies ? (
                <>
                  <Alert
                    color="warning"
                    description={`隧道 \"${tunnelDeletePreview?.tunnelName || tunnelToDelete?.name || ""}\" 当前被 ${deletePreviewForwardCount} 条规则使用。删除前需要先处理这些规则。`}
                    title="发现关联规则"
                    variant="flat"
                  />

                  {(tunnelDeletePreview?.sampleForwards?.length ?? 0) > 0 ? (
                    <div className="space-y-2">
                      <div className="flex items-center justify-between gap-3">
                        <h3 className="text-sm font-semibold text-foreground">
                          关联规则预览
                        </h3>
                        <span className="text-xs text-default-500">
                          前 {tunnelDeletePreview?.sampleForwards?.length ?? 0}{" "}
                          条
                        </span>
                      </div>
                      <div className="divide-y divide-divider border-y border-divider">
                        {tunnelDeletePreview?.sampleForwards?.map((forward) => (
                          <div key={forward.id} className="py-2">
                            <div className="flex items-center justify-between gap-3">
                              <span className="truncate text-sm font-medium text-foreground">
                                {forward.name}
                              </span>
                              <span className="shrink-0 tabular-nums text-xs text-default-500">
                                :{forward.inPort || 0}
                              </span>
                            </div>
                          </div>
                        ))}
                      </div>
                      {deletePreviewForwardCount >
                      (tunnelDeletePreview?.sampleForwards?.length ?? 0) ? (
                        <p className="text-xs text-default-500">
                          还有{" "}
                          {deletePreviewForwardCount -
                            (tunnelDeletePreview?.sampleForwards?.length ??
                              0)}{" "}
                          条规则未展开显示。
                        </p>
                      ) : null}
                    </div>
                  ) : null}

                  <RadioGroup
                    label="处理方式"
                    value={deleteAction}
                    onValueChange={(value) => {
                      const nextAction = value as TunnelDeleteAction;

                      setDeleteAction(nextAction);
                      if (nextAction !== "replace") {
                        setDeleteTargetTunnelId(null);

                        return;
                      }

                      setDeleteTargetTunnelId(
                        deleteReplacementTunnels[0]?.id ?? null,
                      );
                    }}
                  >
                    <Radio value="replace">
                      保留规则，迁移到其他隧道
                      {deleteReplaceUnavailable
                        ? "（当前无可用目标）"
                        : "（推荐）"}
                    </Radio>
                    <Radio value="delete_forwards">直接删除这些关联规则</Radio>
                  </RadioGroup>

                  {deleteReplaceUnavailable ? (
                    <Alert
                      color="warning"
                      description="当前没有其他启用中的隧道可用于承接这些规则，只能删除关联规则后再删除该隧道。"
                      variant="flat"
                    />
                  ) : null}

                  {deleteAction === "replace" && !deleteReplaceUnavailable ? (
                    <div className="space-y-2">
                      <Select
                        label="目标隧道"
                        placeholder="请选择目标隧道"
                        selectedKeys={
                          deleteTargetTunnelId
                            ? [String(deleteTargetTunnelId)]
                            : []
                        }
                        variant="bordered"
                        onSelectionChange={(keys) => {
                          const selected = Array.from(keys)[0];

                          setDeleteTargetTunnelId(
                            selected ? Number(selected) : null,
                          );
                        }}
                      >
                        {deleteReplacementTunnels.map((tunnel) => (
                          <SelectItem key={String(tunnel.id)}>
                            {tunnel.name}
                          </SelectItem>
                        ))}
                      </Select>
                      <p className="text-xs text-default-500">
                        关联规则会迁移到这里，当前要删除的隧道不会出现在可选项里。
                      </p>
                    </div>
                  ) : null}
                </>
              ) : (
                <Alert
                  color="warning"
                  description={`当前未发现关联规则。确认后将直接删除“${tunnelToDelete?.name || "该隧道"}”，此操作不可撤销。`}
                  title="可以直接删除"
                  variant="flat"
                />
              )}
            </ModalBody>
            <ModalFooter>
              <Button variant="light" onPress={onClose}>
                取消
              </Button>
              <Button
                color="danger"
                isDisabled={
                  deletePreviewLoading ||
                  (deleteHasForwardDependencies &&
                    deleteAction === "replace" &&
                    (!deleteTargetTunnelId || deleteReplaceUnavailable))
                }
                isLoading={deleteLoading}
                onPress={confirmDelete}
              >
                {deleteLoading ? "删除中..." : deleteConfirmLabel}
              </Button>
            </ModalFooter>
          </>
        )}
      </ModalContent>
    </Modal>
  );
}

export interface BatchDeleteTunnelsModalProps {
  batchDeleteAction: TunnelDeleteAction;
  batchDeleteConfirmLabel: string;
  batchDeleteDependentItems: TunnelDeletePreviewApiData[];
  batchDeleteDependentTunnelCount: number;
  batchDeleteDirectDeleteTunnelCount: number;
  batchDeleteHasForwardDependencies: boolean;
  batchDeleteModalOpen: boolean;
  batchDeletePreviewLoading: boolean;
  batchDeleteReplaceUnavailable: boolean;
  batchDeleteReplacementTunnels: Tunnel[];
  batchDeleteTargetTunnelId: number | null;
  batchDeleteTotalForwardCount: number;
  batchLoading: boolean;
  handleBatchDelete: () => Promise<void>;
  handleBatchDeleteModalOpenChange: (open: boolean) => void;
  selectedTunnelIdList: number[];
  setBatchDeleteAction: Dispatch<SetStateAction<TunnelDeleteAction>>;
  setBatchDeleteTargetTunnelId: Dispatch<SetStateAction<number | null>>;
}

/** Delete the selected tunnels, moving or deleting their forwards. */
export function BatchDeleteTunnelsModal({
  batchDeleteAction,
  batchDeleteConfirmLabel,
  batchDeleteDependentItems,
  batchDeleteDependentTunnelCount,
  batchDeleteDirectDeleteTunnelCount,
  batchDeleteHasForwardDependencies,
  batchDeleteModalOpen,
  batchDeletePreviewLoading,
  batchDeleteReplaceUnavailable,
  batchDeleteReplacementTunnels,
  batchDeleteTargetTunnelId,
  batchDeleteTotalForwardCount,
  batchLoading,
  handleBatchDelete,
  handleBatchDeleteModalOpenChange,
  selectedTunnelIdList,
  setBatchDeleteAction,
  setBatchDeleteTargetTunnelId,
}: BatchDeleteTunnelsModalProps) {
  return (
    <Modal
      classNames={{
        base: "!w-[calc(100%-32px)] !mx-auto sm:!w-full overflow-hidden",
      }}
      isOpen={batchDeleteModalOpen}
      onOpenChange={handleBatchDeleteModalOpenChange}
    >
      <ModalContent>
        {(onClose) => (
          <>
            <ModalHeader className="flex flex-col gap-1">
              <h2 className="text-lg font-bold sm:text-xl">批量删除隧道</h2>
              <p className="text-xs font-normal leading-5 text-default-500 sm:text-sm">
                即将删除这 {selectedTunnelIdList.length}{" "}
                条隧道，删除前会先检查是否有关联规则。
              </p>
            </ModalHeader>
            <ModalBody className="space-y-3 sm:space-y-4">
              {batchDeletePreviewLoading ? (
                <div className="flex items-center gap-3 py-5 text-sm text-default-600">
                  <Spinner size="sm" />
                  正在检查选中隧道是否有关联规则...
                </div>
              ) : batchDeleteHasForwardDependencies ? (
                <>
                  <Alert
                    color="warning"
                    description={`已选 ${selectedTunnelIdList.length} 条隧道，其中 ${batchDeleteDependentTunnelCount} 条仍被规则使用，共 ${batchDeleteTotalForwardCount} 条规则待处理。${batchDeleteDirectDeleteTunnelCount > 0 ? `其余 ${batchDeleteDirectDeleteTunnelCount} 条会直接删除。` : ""}`}
                    title="发现关联规则"
                    variant="flat"
                  />

                  <div className="max-h-64 divide-y divide-divider overflow-y-auto scrollbar-hide border-y border-divider sm:max-h-72">
                    {batchDeleteDependentItems.map((item) => (
                      <div key={item.tunnelId} className="py-2.5">
                        <div className="flex items-center justify-between gap-3">
                          <div className="min-w-0">
                            <p className="truncate text-sm font-medium text-foreground">
                              {item.tunnelName}
                            </p>
                            <p className="mt-1 text-xs text-default-500">
                              {item.forwardCount} 条规则依赖
                            </p>
                          </div>
                          <Chip color="warning" size="sm" variant="flat">
                            有关联
                          </Chip>
                        </div>

                        {item.sampleForwards.length > 0 ? (
                          <div className="mt-3 space-y-2">
                            {item.sampleForwards.map((forward) => (
                              <div
                                key={forward.id}
                                className="rounded-md bg-content1/70 px-2.5 py-2 sm:px-3"
                              >
                                <div className="flex items-center justify-between gap-3">
                                  <span className="truncate text-xs font-medium text-foreground">
                                    {forward.name}
                                  </span>
                                  <span className="shrink-0 tabular-nums text-[11px] text-default-500">
                                    :{forward.inPort || 0}
                                  </span>
                                </div>
                              </div>
                            ))}
                            {item.forwardCount > item.sampleForwards.length ? (
                              <p className="text-[11px] text-default-500">
                                还有{" "}
                                {item.forwardCount - item.sampleForwards.length}{" "}
                                条规则未展开显示。
                              </p>
                            ) : null}
                          </div>
                        ) : null}
                      </div>
                    ))}
                  </div>

                  <RadioGroup
                    label="处理方式"
                    value={batchDeleteAction}
                    onValueChange={(value) => {
                      const nextAction = value as TunnelDeleteAction;

                      setBatchDeleteAction(nextAction);
                      if (nextAction !== "replace") {
                        setBatchDeleteTargetTunnelId(null);

                        return;
                      }

                      setBatchDeleteTargetTunnelId(
                        batchDeleteReplacementTunnels[0]?.id ?? null,
                      );
                    }}
                  >
                    <Radio value="replace">
                      保留规则，统一迁移到其他隧道
                      {batchDeleteReplaceUnavailable
                        ? "（当前无可用目标）"
                        : "（推荐）"}
                    </Radio>
                    <Radio value="delete_forwards">直接删除这些关联规则</Radio>
                  </RadioGroup>

                  {batchDeleteReplaceUnavailable ? (
                    <Alert
                      color="warning"
                      description="当前没有可承接这些规则的启用隧道，只能删除关联规则后再删除所选隧道。"
                      variant="flat"
                    />
                  ) : null}

                  {batchDeleteAction === "replace" &&
                  !batchDeleteReplaceUnavailable ? (
                    <div className="space-y-2">
                      <Select
                        label="目标隧道"
                        placeholder="请选择目标隧道"
                        selectedKeys={
                          batchDeleteTargetTunnelId
                            ? [String(batchDeleteTargetTunnelId)]
                            : []
                        }
                        variant="bordered"
                        onSelectionChange={(keys) => {
                          const selected = Array.from(keys)[0];

                          setBatchDeleteTargetTunnelId(
                            selected ? Number(selected) : null,
                          );
                        }}
                      >
                        {batchDeleteReplacementTunnels.map((tunnel) => (
                          <SelectItem key={String(tunnel.id)}>
                            {tunnel.name}
                          </SelectItem>
                        ))}
                      </Select>
                      <p className="text-xs text-default-500">
                        所有关联规则都会迁移到这里，删除列表中的隧道不会出现在可选项里。
                      </p>
                    </div>
                  ) : null}
                </>
              ) : (
                <Alert
                  color="warning"
                  description={`已选 ${selectedTunnelIdList.length} 条隧道，当前未发现关联规则。确认后将直接删除这些隧道，此操作不可撤销。`}
                  title="可以直接删除"
                  variant="flat"
                />
              )}
            </ModalBody>
            <ModalFooter>
              <Button variant="light" onPress={onClose}>
                取消
              </Button>
              <Button
                color="danger"
                isDisabled={
                  batchDeletePreviewLoading ||
                  (batchDeleteHasForwardDependencies &&
                    batchDeleteAction === "replace" &&
                    (!batchDeleteTargetTunnelId ||
                      batchDeleteReplaceUnavailable))
                }
                isLoading={batchLoading}
                onPress={handleBatchDelete}
              >
                {batchLoading ? "删除中..." : batchDeleteConfirmLabel}
              </Button>
            </ModalFooter>
          </>
        )}
      </ModalContent>
    </Modal>
  );
}
