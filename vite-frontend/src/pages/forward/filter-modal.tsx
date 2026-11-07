import type { Dispatch, SetStateAction } from "react";

import { Button } from "@/shadcn-bridge/heroui/button";
import { Input } from "@/shadcn-bridge/heroui/input";
import { Select, SelectItem } from "@/shadcn-bridge/heroui/select";
import {
  Modal,
  ModalContent,
  ModalHeader,
  ModalBody,
  ModalFooter,
} from "@/shadcn-bridge/heroui/modal";
import { Tunnel } from "@/pages/forward/types";

export interface ForwardFilterModalProps {
  isSearchModalOpen: boolean;
  resetSearchParams: () => void;
  searchParams: {
    name: string;
    userId: string;
    tunnelId: string;
    inPort: string;
    remoteAddr: string;
  };
  setIsSearchModalOpen: Dispatch<SetStateAction<boolean>>;
  setSearchParams: Dispatch<
    SetStateAction<{
      name: string;
      userId: string;
      tunnelId: string;
      inPort: string;
      remoteAddr: string;
    }>
  >;
  tunnels: Tunnel[];
}

/** Search and filter conditions for the forward list. */
export function ForwardFilterModal({
  isSearchModalOpen,
  resetSearchParams,
  searchParams,
  setIsSearchModalOpen,
  setSearchParams,
  tunnels,
}: ForwardFilterModalProps) {
  return (
    <Modal
      classNames={{
        base: "!w-[calc(100%-32px)] !mx-auto sm:!w-full overflow-hidden",
      }}
      isOpen={isSearchModalOpen}
      placement="center"
      size="md"
      onOpenChange={setIsSearchModalOpen}
    >
      <ModalContent>
        {() => (
          <>
            <ModalHeader closable className="flex flex-col gap-1">
              筛选规则
            </ModalHeader>
            <ModalBody>
              <div className="flex flex-col gap-4 py-2">
                <Select
                  label="所属隧道"
                  placeholder="选择隧道"
                  selectedKeys={[searchParams.tunnelId]}
                  variant="bordered"
                  onSelectionChange={(keys) => {
                    const key = Array.from(keys)[0] as string;

                    setSearchParams((prev) => ({
                      ...prev,
                      tunnelId: key || "all",
                    }));
                  }}
                >
                  <SelectItem key="all">全部隧道</SelectItem>
                  {tunnels.map((tunnel) => (
                    <SelectItem key={tunnel.id.toString()}>
                      {tunnel.name}
                    </SelectItem>
                  ))}
                </Select>

                <Input
                  label="入口监听端口 (精确)"
                  placeholder="请输入具体端口号"
                  type="number"
                  value={searchParams.inPort}
                  variant="bordered"
                  onChange={(e) =>
                    setSearchParams((prev) => ({
                      ...prev,
                      inPort: e.target.value,
                    }))
                  }
                />

                <Input
                  label="目标地址或端口 (模糊)"
                  placeholder="请输入目标IP、域名或端口"
                  value={searchParams.remoteAddr}
                  variant="bordered"
                  onChange={(e) =>
                    setSearchParams((prev) => ({
                      ...prev,
                      remoteAddr: e.target.value,
                    }))
                  }
                />
              </div>
            </ModalBody>
            <ModalFooter>
              <Button variant="flat" onPress={resetSearchParams}>
                重置
              </Button>
            </ModalFooter>
          </>
        )}
      </ModalContent>
    </Modal>
  );
}
