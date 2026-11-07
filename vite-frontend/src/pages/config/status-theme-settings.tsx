import type { StatusTheme } from "@/api";

import { useEffect, useMemo, useState } from "react";
import toast from "react-hot-toast";

import {
  configFields,
  configForm,
  configOverrides,
  configSections,
  configValues,
  fits,
  ThemeConfigField,
} from "./status-theme-config";

import { getStatusThemeConfig, saveStatusThemeConfig } from "@/api";
import { Button } from "@/shadcn-bridge/heroui/button";
import { Input, Textarea } from "@/shadcn-bridge/heroui/input";
import {
  Modal,
  ModalBody,
  ModalContent,
  ModalFooter,
  ModalHeader,
} from "@/shadcn-bridge/heroui/modal";
import { Select, SelectItem } from "@/shadcn-bridge/heroui/select";
import { Spinner } from "@/shadcn-bridge/heroui/spinner";
import { Switch } from "@/shadcn-bridge/heroui/switch";

/** Whether the theme declares any setting the panel can draw. */
export const hasThemeSettings = (theme: StatusTheme) =>
  configFields(configForm(theme.config)).length > 0;

const label = (f: ThemeConfigField) => f.label || f.key;

/**
 * 设置 for one theme: the form its theme.json declares. Saving stores only
 * what differs from the defaults; the page reads it when it next loads.
 */
export function StatusThemeSettings({
  theme,
  onClose,
}: {
  theme: StatusTheme;
  onClose: () => void;
}) {
  const form = useMemo(() => configForm(theme.config), [theme.config]);
  const fields = useMemo(() => configFields(form), [form]);
  const sections = useMemo(() => configSections(form), [form]);
  const [saved, setSaved] = useState<Record<string, unknown> | null>(null);
  const [values, setValues] = useState<Record<string, unknown>>({});
  const [saving, setSaving] = useState(false);

  useEffect(() => {
    void getStatusThemeConfig(theme.short).then((res) => {
      if (res.code !== 0 || !res.data) {
        toast.error(res.msg || "读取主题设置失败");
        onClose();

        return;
      }
      const v =
        res.data.values && typeof res.data.values === "object"
          ? (res.data.values as Record<string, unknown>)
          : {};

      setSaved(v);
      setValues(configValues(fields, v));
    });
  }, [theme.short, fields, onClose]);

  const set = (key: string, value: unknown) =>
    setValues((old) => ({ ...old, [key]: value }));
  // A number box holds its text while being edited; an empty one is no number.
  const typed = (f: ThemeConfigField) =>
    f.type !== "number"
      ? values[f.key]
      : values[f.key] === ""
        ? NaN
        : Number(values[f.key]);

  const submit = async (
    base: Record<string, unknown>,
    next: Record<string, unknown>,
  ) => {
    setSaving(true);
    try {
      const res = await saveStatusThemeConfig(theme.short, next);

      if (res.code === 0) {
        toast.success("主题设置已保存，状态页刷新后生效");
        setSaved(base);
        onClose();
      } else {
        toast.error(res.msg || "保存失败");
      }
    } finally {
      setSaving(false);
    }
  };

  const save = () => {
    if (!saved) return;
    const invalid = fields.find(
      (f) => f.type === "number" && !fits(f, typed(f)),
    );

    if (invalid) {
      const range =
        invalid.min !== undefined && invalid.max !== undefined
          ? `${invalid.min}–${invalid.max} 之间的`
          : invalid.min !== undefined
            ? `不小于 ${invalid.min} 的`
            : invalid.max !== undefined
              ? `不大于 ${invalid.max} 的`
              : "";

      toast.error(`「${label(invalid)}」要填${range}数字`);

      return;
    }
    const next = configOverrides(
      fields,
      saved,
      Object.fromEntries(fields.map((f) => [f.key, typed(f)])),
    );

    void submit(next, next);
  };

  // Also drops keys the theme no longer declares: the only way to remove a
  // publicly readable value an older version left.
  const reset = () => {
    if (!window.confirm(`把「${theme.name}」的设置全部恢复为默认值？`)) return;
    void submit({}, {});
  };

  const input = (f: ThemeConfigField) => {
    if (f.type === "boolean") {
      return (
        <div key={f.key} className="flex items-center justify-between gap-3">
          <div className="min-w-0">
            <p className="text-sm">{label(f)}</p>
            {f.help && <p className="text-xs text-default-400">{f.help}</p>}
          </div>
          <Switch
            aria-label={label(f)}
            isSelected={values[f.key] === true}
            size="sm"
            onValueChange={(v) => set(f.key, v)}
          />
        </div>
      );
    }

    return (
      <div key={f.key} className="space-y-1.5">
        <p className="text-sm">{label(f)}</p>
        {f.type === "text" ? (
          <Textarea
            aria-label={label(f)}
            minRows={3}
            size="sm"
            value={String(values[f.key] ?? "")}
            variant="bordered"
            onChange={(e) => set(f.key, e.target.value)}
          />
        ) : f.type === "select" ? (
          <Select
            aria-label={label(f)}
            selectedKeys={[String(values[f.key] ?? "")]}
            size="sm"
            variant="bordered"
            onSelectionChange={(keys) => {
              const key = Array.from(keys)[0];

              if (key !== undefined) set(f.key, String(key));
            }}
          >
            {f.options!.map((o) => (
              <SelectItem key={o.value}>{o.label || o.value}</SelectItem>
            ))}
          </Select>
        ) : (
          <Input
            aria-label={label(f)}
            max={f.type === "number" ? f.max : undefined}
            min={f.type === "number" ? f.min : undefined}
            size="sm"
            step={f.type === "number" ? "any" : undefined}
            type={f.type === "number" ? "number" : "text"}
            value={String(values[f.key] ?? "")}
            variant="bordered"
            onChange={(e) => set(f.key, e.target.value)}
          />
        )}
        {f.help && <p className="text-xs text-default-400">{f.help}</p>}
      </div>
    );
  };

  return (
    <Modal isOpen scrollBehavior="inside" size="lg" onClose={onClose}>
      <ModalContent>
        <ModalHeader>{theme.name} 设置</ModalHeader>
        <ModalBody>
          {!saved ? (
            <div className="flex justify-center py-8">
              <Spinner size="sm" />
            </div>
          ) : (
            <div className="flex flex-col gap-5">
              {sections.map((s) => (
                <section key={s.label} className="flex flex-col gap-3">
                  {sections.length > 1 && (
                    <p className="border-b border-divider pb-1 text-xs font-semibold text-default-500">
                      {s.label}
                    </p>
                  )}
                  {s.fields.map(input)}
                </section>
              ))}
            </div>
          )}
        </ModalBody>
        <ModalFooter className="justify-between">
          <Button
            isDisabled={!saved || saving}
            size="sm"
            variant="light"
            onPress={reset}
          >
            恢复默认
          </Button>
          <div className="flex gap-2">
            <Button size="sm" variant="bordered" onPress={onClose}>
              取消
            </Button>
            <Button
              color="primary"
              isDisabled={!saved}
              isLoading={saving}
              size="sm"
              onPress={save}
            >
              保存
            </Button>
          </div>
        </ModalFooter>
      </ModalContent>
    </Modal>
  );
}
