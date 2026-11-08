import type { Node } from "@/pages/node/types";

import { useEffect, useState } from "react";
import toast from "react-hot-toast";

import { updateNodeBilling } from "@/api";
import { Button } from "@/shadcn-bridge/heroui/button";
import {
  Modal,
  ModalBody,
  ModalContent,
  ModalFooter,
  ModalHeader,
} from "@/shadcn-bridge/heroui/modal";
import { BillingFields, type BillingValue } from "@/pages/node/billing-fields";

const fromNode = (node: Node | null): BillingValue => ({
  price: node?.price ? String(node.price) : "",
  currency: node?.currency || "",
  renewalCycle: node?.renewalCycle || "",
  expiryTime: node?.expiryTime || 0,
});

/** The quick 价格与到期 dialog: just the price and renewal of a server. */
export function NodeBillingModal({
  node,
  onClose,
  onSaved,
}: {
  node: Node | null;
  onClose: () => void;
  onSaved: (id: number, value: Partial<Node>) => void;
}) {
  const [value, setValue] = useState<BillingValue>(() => fromNode(node));
  const [error, setError] = useState("");
  const [saving, setSaving] = useState(false);

  useEffect(() => {
    setValue(fromNode(node));
    setError("");
  }, [node]);

  const save = async () => {
    if (!node) return;
    if (Boolean(value.renewalCycle) !== value.expiryTime > 0) {
      setError("付款周期和到期时间要一起填");

      return;
    }
    setSaving(true);
    try {
      const payload = {
        id: node.id,
        price: Number(value.price) || 0,
        currency: value.currency,
        renewalCycle: value.renewalCycle,
        expiryTime: value.expiryTime,
      };
      const res = await updateNodeBilling(payload);

      if (res.code !== 0) {
        toast.error(res.msg || "保存失败");

        return;
      }
      toast.success("已保存");
      onSaved(node.id, payload);
      onClose();
    } finally {
      setSaving(false);
    }
  };

  return (
    <Modal
      classNames={{
        base: "!w-[calc(100%-32px)] !mx-auto sm:!w-full overflow-hidden",
      }}
      isOpen={node !== null}
      placement="center"
      size="lg"
      onClose={onClose}
    >
      <ModalContent>
        <ModalHeader className="truncate">{node?.name}</ModalHeader>
        <ModalBody>
          <BillingFields
            errors={error ? { expiryTime: error } : undefined}
            value={value}
            onChange={(next) => {
              setError("");
              setValue((prev) => ({ ...prev, ...next }));
            }}
          />
        </ModalBody>
        <ModalFooter>
          <Button variant="light" onPress={onClose}>
            取消
          </Button>
          <Button color="primary" isLoading={saving} onPress={save}>
            保存
          </Button>
        </ModalFooter>
      </ModalContent>
    </Modal>
  );
}
