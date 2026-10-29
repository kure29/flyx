import type { Dispatch, SetStateAction } from "react";

import { useEffect, useState } from "react";

import { Button } from "@/shadcn-bridge/heroui/button";
import { Checkbox } from "@/shadcn-bridge/heroui/checkbox";
import {
  Modal,
  ModalContent,
  ModalHeader,
  ModalBody,
  ModalFooter,
} from "@/shadcn-bridge/heroui/modal";
import { Node } from "@/pages/node/types";

export interface RollbackNodeModalProps {
  confirmRollback: () => Promise<void>;
  nodeToRollback: Node | null;
  rollbackModalOpen: boolean;
  setRollbackModalOpen: Dispatch<SetStateAction<boolean>>;
}

/** Confirm rolling a node's agent back to the previous version. */
export function RollbackNodeModal({
  confirmRollback,
  nodeToRollback,
  rollbackModalOpen,
  setRollbackModalOpen,
}: RollbackNodeModalProps) {
  return (
    <Modal
      backdrop="blur"
      classNames={{
        base: "!w-[calc(100%-32px)] !mx-auto sm:!w-full rounded-2xl overflow-hidden",
      }}
      isOpen={rollbackModalOpen}
      placement="center"
      scrollBehavior="inside"
      size="2xl"
      onOpenChange={setRollbackModalOpen}
    >
      <ModalContent>
        {(onClose) => (
          <>
            <ModalHeader className="flex flex-col gap-1">
              <h2 className="text-xl font-bold">确认回退</h2>
            </ModalHeader>
            <ModalBody>
              <p>
                确定要将服务器{" "}
                <strong>&quot;{nodeToRollback?.name}&quot;</strong>{" "}
                回退到上一个版本吗？
              </p>
              <p className="text-small text-default-500">
                服务器将执行版本回退并自动重启，期间会导致服务器短暂离线。
              </p>
            </ModalBody>
            <ModalFooter>
              <Button variant="light" onPress={onClose}>
                取消
              </Button>
              <Button color="secondary" onPress={confirmRollback}>
                回退
              </Button>
            </ModalFooter>
          </>
        )}
      </ModalContent>
    </Modal>
  );
}

/** Ticked whenever a delete dialog opens: deleting usually means retiring the server. */
function useUninstallChoice(open: boolean) {
  const [uninstall, setUninstall] = useState(true);

  useEffect(() => {
    if (open) setUninstall(true);
  }, [open]);

  return [uninstall, setUninstall] as const;
}

function UninstallAgentOption({
  isSelected,
  onValueChange,
}: {
  isSelected: boolean;
  onValueChange: (value: boolean) => void;
}) {
  return (
    <div className="space-y-1 rounded-xl border border-default-200 bg-default-100/60 px-3 py-2.5">
      <Checkbox isSelected={isSelected} size="sm" onValueChange={onValueChange}>
        同时卸载服务器上的 agent
      </Checkbox>
      <p className="pl-6 text-xs text-default-500">
        停止并删除 agent、内核和 nftables
        规则，恢复调优前的系统参数。服务器离线时，会在它下次连接面板时卸载。只想把服务器移到另一个面板时请取消勾选。
      </p>
    </div>
  );
}

export interface DeleteNodeModalProps {
  confirmDelete: (uninstall: boolean) => Promise<void>;
  deleteLoading: boolean;
  deleteModalOpen: boolean;
  nodeToDelete: Node | null;
  setDeleteModalOpen: Dispatch<SetStateAction<boolean>>;
}

/** Confirm deleting one node. */
export function DeleteNodeModal({
  confirmDelete,
  deleteLoading,
  deleteModalOpen,
  nodeToDelete,
  setDeleteModalOpen,
}: DeleteNodeModalProps) {
  const [uninstall, setUninstall] = useUninstallChoice(deleteModalOpen);

  return (
    <Modal
      backdrop="blur"
      classNames={{
        base: "!w-[calc(100%-32px)] !mx-auto sm:!w-full rounded-2xl overflow-hidden",
      }}
      isOpen={deleteModalOpen}
      placement="center"
      scrollBehavior="inside"
      size="2xl"
      onOpenChange={setDeleteModalOpen}
    >
      <ModalContent>
        {(onClose) => (
          <>
            <ModalHeader className="flex flex-col gap-1">
              <h2 className="text-xl font-bold">确认删除</h2>
            </ModalHeader>
            <ModalBody>
              <p>
                确定要删除服务器{" "}
                <strong>&quot;{nodeToDelete?.name}&quot;</strong> 吗？
              </p>
              <p className="text-small text-default-500">
                此操作不可恢复，请谨慎操作。
              </p>
              <UninstallAgentOption
                isSelected={uninstall}
                onValueChange={setUninstall}
              />
            </ModalBody>
            <ModalFooter>
              <Button variant="light" onPress={onClose}>
                取消
              </Button>
              <Button
                color="danger"
                isLoading={deleteLoading}
                onPress={() => void confirmDelete(uninstall)}
              >
                {deleteLoading ? "删除中..." : "删除"}
              </Button>
            </ModalFooter>
          </>
        )}
      </ModalContent>
    </Modal>
  );
}

export interface BatchDeleteNodesModalProps {
  batchDeleteModalOpen: boolean;
  batchLoading: boolean;
  handleBatchDelete: (uninstall: boolean) => Promise<void>;
  selectedIds: Set<number>;
  setBatchDeleteModalOpen: Dispatch<SetStateAction<boolean>>;
}

/** Confirm deleting the selected nodes. */
export function BatchDeleteNodesModal({
  batchDeleteModalOpen,
  batchLoading,
  handleBatchDelete,
  selectedIds,
  setBatchDeleteModalOpen,
}: BatchDeleteNodesModalProps) {
  const [uninstall, setUninstall] = useUninstallChoice(batchDeleteModalOpen);

  return (
    <Modal
      backdrop="blur"
      classNames={{
        base: "!w-[calc(100%-32px)] !mx-auto sm:!w-full rounded-2xl overflow-hidden",
      }}
      isOpen={batchDeleteModalOpen}
      placement="center"
      scrollBehavior="inside"
      size="md"
      onOpenChange={setBatchDeleteModalOpen}
    >
      <ModalContent>
        {(onClose) => (
          <>
            <ModalHeader className="flex flex-col gap-1">
              <h2 className="text-xl font-bold">确认删除</h2>
            </ModalHeader>
            <ModalBody>
              <p>
                确定要删除选中的 <strong>{selectedIds.size}</strong>{" "}
                个服务器吗？
              </p>
              <p className="text-small text-default-500">
                此操作不可恢复，请谨慎操作。
              </p>
              <UninstallAgentOption
                isSelected={uninstall}
                onValueChange={setUninstall}
              />
            </ModalBody>
            <ModalFooter>
              <Button variant="light" onPress={onClose}>
                取消
              </Button>
              <Button
                color="danger"
                isLoading={batchLoading}
                onPress={() => void handleBatchDelete(uninstall)}
              >
                {batchLoading ? "删除中..." : "删除"}
              </Button>
            </ModalFooter>
          </>
        )}
      </ModalContent>
    </Modal>
  );
}
