import { useEffect, useState } from "react";

import { getNodeList } from "@/api";
import { Checkbox } from "@/shadcn-bridge/heroui/checkbox";
import { Input } from "@/shadcn-bridge/heroui/input";
import { Switch } from "@/shadcn-bridge/heroui/switch";
import { Field, SubHeading } from "@/pages/config/section";
import { StatusThemeManager } from "@/pages/config/status-theme-manager";

interface StatusPageSettingsProps {
  configs: Record<string, string>;
  originalConfigs: Record<string, string>;
  onChange: (key: string, value: string) => void;
}

const HIDDEN_KEY = "status_page_hidden_nodes";

const hiddenIds = (configs: Record<string, string>) =>
  new Set(
    (configs[HIDDEN_KEY] ?? "")
      .split(",")
      .map((s) => Number(s.trim()))
      .filter((n) => Number.isInteger(n) && n > 0),
  );

const statusPageUrl = () => `${window.location.origin}/status/`;

/** One line for the collapsed section, e.g. "已开启 · 隐藏 2 台". */
export function statusPageSummary(configs: Record<string, string>): string {
  if (configs.status_page_enabled !== "true") return "未开启";
  const hidden = hiddenIds(configs).size;

  return [
    "已开启",
    configs.status_page_show_traffic === "false" ? "不显示流量" : "",
    hidden ? `隐藏 ${hidden} 台` : "",
  ]
    .filter(Boolean)
    .join(" · ");
}

/** The public status page's settings, saved with the page's other configs. */
export function StatusPageSettings({
  configs,
  originalConfigs,
  onChange,
}: StatusPageSettingsProps) {
  const [nodes, setNodes] = useState<Array<{ id: number; name: string }>>([]);
  const enabled = configs.status_page_enabled === "true";
  const hidden = hiddenIds(configs);

  useEffect(() => {
    getNodeList()
      .then((res) => {
        if (res.code === 0 && Array.isArray(res.data)) {
          setNodes(res.data.map((n) => ({ id: n.id, name: n.name })));
        }
      })
      .catch(() => {});
  }, []);

  const changedClass = (key: string) =>
    configs[key] !== originalConfigs[key]
      ? "border-warning-300 data-[hover=true]:border-warning-400"
      : "";

  const toggle = (key: string, label: string, help: string, on: boolean) => (
    <Field inline help={help} label={label}>
      <Switch
        aria-label={label}
        isSelected={on}
        size="sm"
        onValueChange={(v) => onChange(key, v ? "true" : "false")}
      />
    </Field>
  );

  const setHidden = (id: number, hide: boolean) => {
    const next = new Set(hidden);

    if (hide) next.add(id);
    else next.delete(id);
    onChange(HIDDEN_KEY, [...next].sort((a, b) => a - b).join(","));
  };

  return (
    <>
      {toggle(
        "status_page_enabled",
        "开启状态页",
        "任何人不用登录就能看到服务器是否在线、可用率、负载和流量；不显示 IP、端口、用户和协议",
        enabled,
      )}
      {enabled && (
        <p className="text-xs text-default-500">
          地址：
          <a
            className="break-all text-primary hover:underline"
            href="/status/"
            rel="noreferrer"
            target="_blank"
          >
            {statusPageUrl()}
          </a>
          ，登录页也会出现入口。
        </p>
      )}

      <Field help="留空使用应用名称" label="标题">
        <Input
          classNames={{
            input: "text-sm",
            inputWrapper: changedClass("status_page_title"),
          }}
          placeholder={configs.app_name || "FLYX"}
          size="sm"
          value={configs.status_page_title ?? ""}
          variant="bordered"
          onChange={(e) => onChange("status_page_title", e.target.value)}
        />
      </Field>

      {toggle(
        "status_page_show_traffic",
        "显示网速和流量",
        "实时上下行速度与本月流量",
        configs.status_page_show_traffic !== "false",
      )}
      {toggle(
        "status_page_show_host",
        "显示系统信息",
        "系统、架构、虚拟化方式和 CPU 型号（不显示内核版本）；需要 agent 3.9.0 及以上",
        configs.status_page_show_host === "true",
      )}
      {toggle(
        "status_page_show_expiry",
        "显示到期日",
        "服务器设了到期时间时显示日期（不显示价格）",
        configs.status_page_show_expiry === "true",
      )}

      <SubHeading>主题</SubHeading>
      <StatusThemeManager />

      <SubHeading>显示哪些服务器</SubHeading>
      {nodes.length === 0 ? (
        <p className="text-xs text-default-500">还没有服务器</p>
      ) : (
        <div className="grid grid-cols-1 gap-2 sm:grid-cols-2">
          {nodes.map((n) => (
            <Checkbox
              key={n.id}
              isSelected={!hidden.has(n.id)}
              onValueChange={(on) => setHidden(n.id, !on)}
            >
              <span className="text-sm">{n.name}</span>
            </Checkbox>
          ))}
        </div>
      )}
    </>
  );
}
