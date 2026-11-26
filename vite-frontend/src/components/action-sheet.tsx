import {
  Modal,
  ModalBody,
  ModalContent,
  ModalHeader,
} from "@/shadcn-bridge/heroui/modal";

const modalBase = "!w-[calc(100%-32px)] !mx-auto sm:!w-full overflow-hidden";

export interface SheetAction {
  key: string;
  label: string;
  /** Only "danger" changes how the action looks (red, listed last). */
  color?: "primary" | "secondary" | "success" | "warning" | "danger";
  disabled?: boolean;
  onPress: () => void;
}

/** A small menu of actions for one item, opened by tapping it. */
export function ActionSheet({
  isOpen,
  title,
  subtitle,
  children,
  actions,
  onClose,
}: {
  isOpen: boolean;
  title: React.ReactNode;
  subtitle?: React.ReactNode;
  children?: React.ReactNode;
  actions: SheetAction[];
  onClose: () => void;
}) {
  // Destructive actions go last so they are not hit by accident.
  const ordered = [
    ...actions.filter((a) => a.color !== "danger"),
    ...actions.filter((a) => a.color === "danger"),
  ];

  const gridCols =
    ordered.length <= 1
      ? "grid-cols-1"
      : ordered.length === 2
        ? "grid-cols-2"
        : ordered.length === 3
          ? "grid-cols-3"
          : "grid-cols-2 sm:grid-cols-4";

  return (
    <Modal
      classNames={{ base: modalBase }}
      isOpen={isOpen}
      placement="center"
      size="md"
      onOpenChange={(open) => !open && onClose()}
    >
      <ModalContent>
        <ModalHeader className="flex flex-col gap-0.5">
          <h2 className="break-all text-base font-semibold">{title}</h2>
          {subtitle && (
            <div className="text-xs font-normal text-default-400">
              {subtitle}
            </div>
          )}
        </ModalHeader>
        <ModalBody className="pb-0">
          {children}
          <div
            className={`grid gap-2 border-t border-divider pt-4 ${gridCols}`}
          >
            {ordered.map((a) => (
              <button
                key={a.key}
                className={`h-9 min-w-0 truncate rounded-lg border border-divider px-2.5 text-sm font-medium transition-all duration-150 active:scale-[0.97] disabled:pointer-events-none disabled:opacity-40 ${
                  a.color === "danger"
                    ? "text-danger hover:border-danger/40 hover:bg-danger-50 dark:hover:bg-danger-900/20"
                    : "text-foreground hover:border-default-400 hover:bg-default-100/70 dark:hover:bg-default-100/50"
                }`}
                disabled={a.disabled}
                title={a.label}
                type="button"
                onClick={() => {
                  onClose();
                  a.onPress();
                }}
              >
                {a.label}
              </button>
            ))}
          </div>
        </ModalBody>
      </ModalContent>
    </Modal>
  );
}
