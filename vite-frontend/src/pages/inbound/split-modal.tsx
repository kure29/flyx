import type { InboundApiItem, SplitCategory } from "@/api/types";
import type { SplitRuleForm } from "./split-rules";

import { useState } from "react";
import toast from "react-hot-toast";

import { splitFormFromItem, splitPayload } from "./presets";
import { DROP_NOTICE, savedDespiteDrop } from "./save-recovery";
import { ruleFromTarget, ruleTarget, SplitRulesEditor } from "./split-rules";
import { TargetButton, TargetPicker } from "./target-picker";
import { OutboundIPSelect } from "./outbound-ip-select";

import { updateInboundSplit } from "@/api";
import { Button } from "@/shadcn-bridge/heroui/button";
import {
  Modal,
  ModalBody,
  ModalContent,
  ModalFooter,
  ModalHeader,
} from "@/shadcn-bridge/heroui/modal";

/** 出站: where an inbound's traffic leaves — IP version, default exit and 分流 rules. */
export function SplitModal({
  item,
  inbounds,
  categories,
  onClose,
  onSaved,
}: {
  item: InboundApiItem | null;
  inbounds: InboundApiItem[];
  categories: SplitCategory[];
  onClose: () => void;
  onSaved: () => void;
}) {
  return (
    <Modal
      classNames={{
        base: "!w-[calc(100%-32px)] !mx-auto sm:!w-full overflow-hidden",
      }}
      isOpen={Boolean(item)}
      placement="center"
      scrollBehavior="inside"
      size="2xl"
      onOpenChange={(open) => !open && onClose()}
    >
      <ModalContent>
        {item && (
          <SplitForm
            key={item.id}
            categories={categories}
            inbounds={inbounds}
            item={item}
            onClose={onClose}
            onSaved={onSaved}
          />
        )}
      </ModalContent>
    </Modal>
  );
}

function SplitForm({
  item,
  inbounds,
  categories,
  onClose,
  onSaved,
}: {
  item: InboundApiItem;
  inbounds: InboundApiItem[];
  categories: SplitCategory[];
  onClose: () => void;
  onSaved: () => void;
}) {
  const [outboundId, setOutboundId] = useState(
    item.outboundId ? String(item.outboundId) : "",
  );
  const [rules, setRules] = useState<SplitRuleForm[]>(() =>
    splitFormFromItem(item),
  );
  const [saving, setSaving] = useState(false);
  const [outboundIpStrategy, setOutboundIpStrategy] = useState(
    item.outboundIpStrategy || "auto",
  );
  // Which picker is open: the default exit or rule i.
  const [picking, setPicking] = useState<"default" | number | null>(null);

  // Any other inbound can carry this one's traffic; the backend rejects
  // loops and protocols the kernel cannot dial.
  const targets = inbounds.filter((i) => i.id !== item.id);
  const outboundMissing =
    outboundId !== "" && !targets.some((i) => String(i.id) === outboundId);

  const submit = async () => {
    if (rules.some((r) => r.action === "proxy" && !r.outboundId)) {
      toast.error("请为每条分流规则选择去向");

      return;
    }
    setSaving(true);
    try {
      const res = await updateInboundSplit({
        id: item.id,
        outboundId: Number(outboundId) || 0,
        splitRules: splitPayload(rules) ?? [],
        outboundIpStrategy,
      });

      if (res.code !== 0) {
        if (await savedDespiteDrop(res, item.id, item.updatedTime)) {
          toast.success(DROP_NOTICE, { duration: 6000 });
          onClose();
          onSaved();

          return;
        }
        toast.error(res.msg || "保存失败");

        return;
      }
      if (res.msg && res.msg !== "操作成功") {
        toast(res.msg, { icon: "⚠️", duration: 6000 });
      } else {
        toast.success("已保存并下发");
      }
      onClose();
      onSaved();
    } finally {
      setSaving(false);
    }
  };

  return (
    <>
      <ModalHeader className="flex flex-col gap-1">
        <h2 className="text-lg font-bold">出站</h2>
        <p className="truncate text-small font-normal text-default-500">
          {item.name}
        </p>
      </ModalHeader>
      <ModalBody className="space-y-5">
        <OutboundIPSelect
          kernelType={item.kernelType}
          value={outboundIpStrategy}
          onChange={setOutboundIpStrategy}
        />
        <section className="space-y-2">
          <p className="text-sm font-medium">默认去向</p>
          <TargetButton
            missing={outboundMissing}
            targets={targets}
            value={outboundId || "direct"}
            onPress={() => setPicking("default")}
          />
          <p
            className={`text-xs ${outboundMissing ? "text-danger" : "text-default-400"}`}
          >
            {outboundMissing
              ? "原出站节点已被删除，该入站的流量会被丢弃，请重新选择"
              : "没命中规则的流量从这里出去；选节点即链式代理"}
          </p>
        </section>
        <section className="space-y-2">
          <div className="flex items-baseline justify-between gap-2">
            <p className="text-sm font-medium">
              分流规则
              {rules.length > 0 && (
                <span className="ml-1.5 text-default-400">{rules.length}</span>
              )}
            </p>
            <p className="text-xs text-default-400">从上到下，先命中先生效</p>
          </div>
          <SplitRulesEditor
            categories={categories}
            rules={rules}
            targets={targets}
            onChange={setRules}
            onChooseTarget={setPicking}
          />
        </section>
        <TargetPicker
          isOpen={picking !== null}
          targets={targets}
          title={picking === "default" ? "默认去向" : "去向"}
          value={
            picking === "default"
              ? outboundId || "direct"
              : picking !== null
                ? ruleTarget(rules[picking])
                : ""
          }
          withBlock={picking !== "default"}
          onClose={() => setPicking(null)}
          onPick={(key) => {
            if (picking === "default") {
              setOutboundId(key === "direct" ? "" : key);
            } else if (picking !== null) {
              setRules(
                rules.map((r, j) =>
                  j === picking ? { ...r, ...ruleFromTarget(key) } : r,
                ),
              );
            }
          }}
        />
      </ModalBody>
      <ModalFooter>
        <Button variant="light" onPress={onClose}>
          取消
        </Button>
        <Button color="primary" isLoading={saving} onPress={submit}>
          保存并下发
        </Button>
      </ModalFooter>
    </>
  );
}
