import type { Dispatch, SetStateAction } from "react";

import { Button } from "@/shadcn-bridge/heroui/button";
import {
  Modal,
  ModalContent,
  ModalHeader,
  ModalBody,
  ModalFooter,
} from "@/shadcn-bridge/heroui/modal";
import { Select, SelectItem } from "@/shadcn-bridge/heroui/select";
import { NodeFilterMode } from "@/pages/node/types";

export interface NodeFilterModalProps {
  isFilterModalOpen: boolean;
  nodeExpiryStats: {
    expired: number;
    expiringSoon: number;
    withExpiry: number;
  };
  nodeFilterMode: NodeFilterMode;
  resetNodeFilterMode: () => void;
  setIsFilterModalOpen: Dispatch<SetStateAction<boolean>>;
  setNodeFilterMode: (value: SetStateAction<NodeFilterMode>) => void;
}

/** Filter conditions for the node list. */
export function NodeFilterModal({
  isFilterModalOpen,
  nodeExpiryStats,
  nodeFilterMode,
  resetNodeFilterMode,
  setIsFilterModalOpen,
  setNodeFilterMode,
}: NodeFilterModalProps) {
  return (
    <Modal
      classNames={{
        base: "!w-[calc(100%-32px)] !mx-auto sm:!w-full overflow-hidden",
      }}
      isOpen={isFilterModalOpen}
      placement="center"
      size="md"
      onOpenChange={setIsFilterModalOpen}
    >
      <ModalContent>
        {() => (
          <>
            <ModalHeader closable className="flex flex-col gap-1">
              筛选条件
            </ModalHeader>
            <ModalBody>
              <div className="flex flex-col gap-4 py-2">
                <div className="flex flex-col gap-2">
                  <p className="text-sm font-medium">按到期状态筛选</p>
                  <Select
                    aria-label="按到期状态筛选"
                    className="w-full"
                    selectedKeys={[nodeFilterMode]}
                    variant="bordered"
                    onSelectionChange={(keys) => {
                      const selected = Array.from(keys)[0] as
                        | NodeFilterMode
                        | undefined;

                      setNodeFilterMode(selected || "all");
                    }}
                  >
                    <SelectItem key="all">全部服务器</SelectItem>
                    <SelectItem key="expiringSoon">
                      7天内续费 ({nodeExpiryStats.expiringSoon})
                    </SelectItem>
                    <SelectItem key="expired">
                      已逾期 ({nodeExpiryStats.expired})
                    </SelectItem>
                    <SelectItem key="withExpiry">
                      已启用续费提醒 ({nodeExpiryStats.withExpiry})
                    </SelectItem>
                  </Select>
                </div>
              </div>
            </ModalBody>
            <ModalFooter>
              <Button
                color="default"
                variant="flat"
                onPress={resetNodeFilterMode}
              >
                重置
              </Button>
            </ModalFooter>
          </>
        )}
      </ModalContent>
    </Modal>
  );
}
