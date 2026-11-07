import type { Node } from "@/pages/node/types";

import { useEffect, useState } from "react";
import toast from "react-hot-toast";

import { getConfigByName, setNodeLatencyTargets } from "@/api";
import { Button } from "@/shadcn-bridge/heroui/button";
import { Checkbox } from "@/shadcn-bridge/heroui/checkbox";
import {
  Modal,
  ModalBody,
  ModalContent,
  ModalFooter,
  ModalHeader,
} from "@/shadcn-bridge/heroui/modal";
import {
  LATENCY_TARGETS_KEY,
  type LatencyTarget,
  parseLatencyTargets,
} from "@/utils/latency-targets";

/**
 * Chooses which reference points one or more nodes measure: the defaults,
 * or their own selection.
 */
export function LatencyTargetsModal({
  nodes,
  onClose,
  onSaved,
}: {
  /** Nodes being set; empty closes the dialog. */
  nodes: Node[];
  onClose: () => void;
  onSaved: () => void;
}) {
  const [pool, setPool] = useState<LatencyTarget[]>([]);
  const [custom, setCustom] = useState(false);
  const [points, setPoints] = useState<string[]>([]);
  const [saving, setSaving] = useState(false);
  const open = nodes.length > 0;
  const batch = nodes.length > 1;

  useEffect(() => {
    if (!open) return;
    getConfigByName(LATENCY_TARGETS_KEY)
      .then((res) =>
        setPool(res.code === 0 ? parseLatencyTargets(res.data?.value) : []),
      )
      .catch(() => setPool([]));
    // One node starts from its own setting; a batch from the defaults.
    const own = !batch ? nodes[0].latencyTargets : null;

    setCustom(Boolean(own));
    setPoints(own?.points ?? []);
  }, [open, nodes, batch]);

  const defaults = pool.filter((t) => t.enabled);

  const startCustom = () => {
    if (points.length === 0) {
      setPoints(defaults.map((t) => t.name));
    }
    setCustom(true);
  };

  const toggle = (name: string, on: boolean) => {
    setPoints((prev) =>
      on ? [...prev, name] : prev.filter((n) => n !== name),
    );
  };

  const save = async () => {
    setSaving(true);
    try {
      const res = await setNodeLatencyTargets({
        ids: nodes.map((n) => n.id),
        custom,
        // Keep pool order.
        points: pool.map((t) => t.name).filter((n) => points.includes(n)),
        // Pins chose what node cards showed; lists show the worst point.
        pinned: [],
      });

      if (res.code !== 0) {
        toast.error(res.msg || "保存失败");

        return;
      }
      toast.success(batch ? `已设置 ${nodes.length} 个服务器` : "已保存");
      onSaved();
      onClose();
    } catch {
      toast.error("保存失败");
    } finally {
      setSaving(false);
    }
  };

  const modeButton = (on: boolean, label: string, onPress: () => void) => (
    <button
      className={`rounded-md px-3 py-1.5 text-sm transition-colors ${
        on
          ? "bg-default-100 font-medium text-foreground"
          : "text-default-500 hover:text-foreground"
      }`}
      type="button"
      onClick={onPress}
    >
      {label}
    </button>
  );

  return (
    <Modal
      classNames={{
        base: "!w-[calc(100%-32px)] !mx-auto sm:!w-full overflow-hidden",
      }}
      isOpen={open}
      placement="center"
      scrollBehavior="inside"
      size="lg"
      onOpenChange={(v) => !v && onClose()}
    >
      <ModalContent>
        <ModalHeader className="flex flex-col gap-0.5">
          <h2 className="text-base font-semibold">
            {batch ? `为 ${nodes.length} 个服务器设置测试点` : "测试点"}
          </h2>
          <p className="truncate text-xs font-normal text-default-400">
            {nodes.map((n) => n.name).join("、")}
          </p>
        </ModalHeader>
        <ModalBody className="space-y-3">
          <div className="flex gap-1">
            {modeButton(!custom, "跟随默认", () => setCustom(false))}
            {modeButton(custom, "自定义", startCustom)}
          </div>

          {pool.length === 0 ? (
            <p className="py-4 text-center text-sm text-default-500">
              还没有测试点，请先到网站配置添加
            </p>
          ) : !custom ? (
            <div className="space-y-1.5 text-sm">
              <p className="text-xs text-default-500">
                测网站配置里打开“默认”的测试点，那边改了这里跟着变：
              </p>
              <div className="flex flex-wrap gap-1.5">
                {defaults.map((t) => (
                  <span
                    key={t.name}
                    className="rounded-md bg-default-100 px-2 py-0.5 text-xs"
                  >
                    {t.name}
                  </span>
                ))}
              </div>
            </div>
          ) : (
            <div className="space-y-2">
              <div className="flex items-center justify-between text-xs text-default-500">
                <span>已选 {points.length} 个</span>
                <span className="flex gap-3">
                  <button
                    className="text-primary"
                    type="button"
                    onClick={() => setPoints(pool.map((t) => t.name))}
                  >
                    全选
                  </button>
                  <button
                    className="text-primary"
                    type="button"
                    onClick={() => setPoints([])}
                  >
                    清空
                  </button>
                </span>
              </div>
              <div className="divide-y divide-divider rounded-lg border border-divider">
                {pool.map((t) => {
                  const on = points.includes(t.name);

                  return (
                    <div
                      key={t.name}
                      className="flex items-center gap-2 px-3 py-2"
                    >
                      <Checkbox
                        isSelected={on}
                        size="sm"
                        onValueChange={(v) => toggle(t.name, v)}
                      >
                        <span className="text-sm">{t.name}</span>
                      </Checkbox>
                      <span className="min-w-0 flex-1 truncate text-right font-mono text-[11px] text-default-400">
                        {t.host}:{t.port}
                      </span>
                    </div>
                  );
                })}
              </div>
            </div>
          )}
          {custom && pool.length > 0 && points.length === 0 && (
            <p className="text-xs text-default-500">
              一个都不选：{batch ? "这些服务器" : "这台服务器"}
              不测延迟和回程，列表上显示“不测延迟”。
            </p>
          )}
          {batch && (
            <p className="text-xs text-warning-600">
              保存后会覆盖这些服务器原来的测试点设置。
            </p>
          )}
        </ModalBody>
        <ModalFooter>
          <Button variant="light" onPress={onClose}>
            取消
          </Button>
          <Button
            color="primary"
            isDisabled={pool.length === 0}
            isLoading={saving}
            onPress={save}
          >
            保存
          </Button>
        </ModalFooter>
      </ModalContent>
    </Modal>
  );
}
