import type { BatchOperationFailure } from "@/api/types";

import { copyWithToast } from "@/utils/clipboard";
import { Button } from "@/shadcn-bridge/heroui/button";
import { Chip } from "@/shadcn-bridge/heroui/chip";
import {
  Modal,
  ModalBody,
  ModalContent,
  ModalFooter,
  ModalHeader,
} from "@/shadcn-bridge/heroui/modal";
import { Alert } from "@/shadcn-bridge/heroui/alert";

interface BatchActionResultModalProps {
  failures: BatchOperationFailure[];
  isOpen: boolean;
  onOpenChange: (open: boolean) => void;
  summary: string;
  title: string;
}

const getFailureTitle = (
  failure: BatchOperationFailure,
  index: number,
): string => {
  const name = typeof failure.name === "string" ? failure.name.trim() : "";

  if (name) {
    return name;
  }

  if (typeof failure.id === "number" && Number.isFinite(failure.id)) {
    return `ID ${failure.id}`;
  }

  return `失败项 ${index + 1}`;
};

const getFailureReason = (failure: BatchOperationFailure): string => {
  const reason =
    typeof failure.reason === "string" ? failure.reason.trim() : "";

  return reason || "未知错误";
};

const buildFailureCopyText = (
  title: string,
  summary: string,
  failures: BatchOperationFailure[],
): string => {
  return [
    title,
    summary,
    "",
    ...failures.map(
      (failure, index) =>
        `${index + 1}. ${getFailureTitle(failure, index)}\n${getFailureReason(failure)}`,
    ),
  ].join("\n");
};

export function BatchActionResultModal({
  failures,
  isOpen,
  onOpenChange,
  summary,
  title,
}: BatchActionResultModalProps) {
  const handleCopy = async () => {
    await copyWithToast(
      buildFailureCopyText(title, summary, failures),
      `已复制 ${failures.length} 项失败原因`,
    );
  };

  return (
    <Modal
      isOpen={isOpen}
      scrollBehavior="inside"
      size="2xl"
      onOpenChange={onOpenChange}
    >
      <ModalContent>
        {() => (
          <>
            <ModalHeader closable>{title}</ModalHeader>
            <ModalBody className="space-y-4">
              <Alert
                color="warning"
                description={summary}
                title={`共 ${failures.length} 项需要处理`}
                variant="flat"
              />
              <div className="space-y-3">
                {failures.map((failure, index) => (
                  <details
                    key={`${failure.id ?? "unknown"}-${index}`}
                    className="group rounded-xl border border-divider bg-content2/40 px-4 py-3"
                  >
                    <summary className="flex cursor-pointer list-none items-center justify-between gap-3">
                      <div className="min-w-0">
                        <p className="truncate text-sm font-medium text-foreground">
                          {getFailureTitle(failure, index)}
                        </p>
                        <p className="mt-1 text-xs text-default-500 group-open:hidden">
                          点击展开查看失败原因
                        </p>
                      </div>
                      <Chip color="danger" size="sm" variant="flat">
                        失败
                      </Chip>
                    </summary>
                    <div className="mt-3 rounded-lg bg-background/70 p-3 text-sm leading-6 text-foreground/90">
                      {getFailureReason(failure)}
                    </div>
                  </details>
                ))}
              </div>
            </ModalBody>
            <ModalFooter>
              <Button variant="light" onPress={handleCopy}>
                复制失败原因
              </Button>
            </ModalFooter>
          </>
        )}
      </ModalContent>
    </Modal>
  );
}
