import { useState } from "react";
import toast from "react-hot-toast";

import { testNotify } from "@/api";
import { NOTIFY_EVENTS } from "@/utils/notification-events";
import { Button } from "@/shadcn-bridge/heroui/button";
import { Checkbox } from "@/shadcn-bridge/heroui/checkbox";
import { Input } from "@/shadcn-bridge/heroui/input";
import { Switch } from "@/shadcn-bridge/heroui/switch";
import { Field, SubHeading } from "@/pages/config/section";

const ALL_EVENT_KEYS = NOTIFY_EVENTS.map((e) => e.key) as string[];

interface NotifySettingsProps {
  configs: Record<string, string>;
  originalConfigs: Record<string, string>;
  onChange: (key: string, value: string) => void;
}

const selectedEvents = (configs: Record<string, string>) => {
  const rawEvents = (configs.notify_events ?? "").trim();

  // Empty means every event, so new installs get all of them.
  return rawEvents
    ? rawEvents
        .split(",")
        .map((e) => e.trim())
        .filter(Boolean)
    : ALL_EVENT_KEYS;
};

/** One line for the collapsed section, e.g. "已启用 · Telegram · 6 个事件". */
export function notifySummary(configs: Record<string, string>): string {
  if (configs.notify_enabled !== "true") return "未启用";
  const channels: string[] = [];

  if (configs.notify_telegram_bot_token && configs.notify_telegram_chat_id)
    channels.push("Telegram");
  if (configs.notify_webhook_url) channels.push("Webhook");
  const count = selectedEvents(configs).filter((e) =>
    ALL_EVENT_KEYS.includes(e),
  ).length;

  return [
    "已启用",
    channels.length ? channels.join("、") : "未填推送渠道",
    `${count} 个事件`,
  ].join(" · ");
}

/** Telegram / Webhook notification settings, saved with the page's other configs. */
export function NotifySettings({
  configs,
  originalConfigs,
  onChange,
}: NotifySettingsProps) {
  const [testing, setTesting] = useState(false);
  const enabled = configs.notify_enabled === "true";
  const selected = selectedEvents(configs);

  const changedClass = (key: string) =>
    configs[key] !== originalConfigs[key]
      ? "border-warning-300 data-[hover=true]:border-warning-400"
      : "";

  const toggleEvent = (key: string, on: boolean) => {
    const next = on
      ? [...new Set([...selected, key])]
      : selected.filter((e) => e !== key);

    onChange(
      "notify_events",
      next.length === ALL_EVENT_KEYS.length ? "" : next.join(",") || "none",
    );
  };

  const runTest = async () => {
    setTesting(true);
    try {
      const res = await testNotify({
        telegramToken: configs.notify_telegram_bot_token ?? "",
        telegramChatId: configs.notify_telegram_chat_id ?? "",
        telegramApiBase: configs.notify_telegram_api_base ?? "",
        webhookUrl: configs.notify_webhook_url ?? "",
      });

      if (res.code === 0) {
        toast.success("测试通知已发送，请查收");
      } else {
        toast.error(res.msg || "发送失败");
      }
    } catch {
      toast.error("发送失败");
    } finally {
      setTesting(false);
    }
  };

  const input = (key: string, placeholder: string, type = "text") => (
    <Input
      classNames={{ input: "text-sm", inputWrapper: changedClass(key) }}
      placeholder={placeholder}
      size="sm"
      type={type}
      value={configs[key] ?? ""}
      variant="bordered"
      onChange={(e) => onChange(key, e.target.value.trim())}
    />
  );

  return (
    <>
      <Field
        inline
        help="服务器掉线、节点连不上、延迟异常、回程线路变化、内核出错、快到期时推送"
        label="启用通知"
      >
        <Switch
          aria-label="启用通知"
          isSelected={enabled}
          size="sm"
          onValueChange={(on) =>
            onChange("notify_enabled", on ? "true" : "false")
          }
        />
      </Field>

      <SubHeading>Telegram</SubHeading>
      <Field
        help="找 @BotFather 创建机器人拿到令牌"
        label="机器人令牌（Bot Token）"
      >
        {input("notify_telegram_bot_token", "123456:ABC-DEF…", "password")}
      </Field>
      <Field
        help="先给机器人发一条消息，再用 @userinfobot 查；群组 ID 以 -100 开头"
        label="会话 ID（Chat ID）"
      >
        {input("notify_telegram_chat_id", "例如 123456789")}
      </Field>
      <Field
        help="面板服务器连不上 Telegram 时填反代地址；留空用官方地址"
        label="API 地址（可选）"
      >
        {input("notify_telegram_api_base", "https://api.telegram.org")}
      </Field>

      <SubHeading>Webhook</SubHeading>
      <Field
        help='以 POST JSON 发送 {"event","title","message","text","time","app"}，可对接企业微信、钉钉、Bark 等转发服务'
        label="Webhook 地址"
      >
        {input("notify_webhook_url", "https://example.com/hook")}
      </Field>

      <SubHeading>通知哪些事件（全局默认）</SubHeading>
      <p className="text-xs text-default-500">
        可在「服务器 → 编辑 → 通知设置」为单台服务器单独开启或关闭事件。
      </p>
      <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
        {NOTIFY_EVENTS.map((e) => (
          <Checkbox
            key={e.key}
            isSelected={selected.includes(e.key)}
            onValueChange={(on) => toggleEvent(e.key, on)}
          >
            <div className="flex flex-col">
              <span className="text-sm">{e.label}</span>
              <span className="text-xs text-default-500">{e.description}</span>
            </div>
          </Checkbox>
        ))}
      </div>

      <SubHeading>延迟 / 丢包阈值</SubHeading>
      <p className="text-xs text-default-500">
        全局及单台服务器开启的延迟 / 丢包通知均使用以下阈值。
      </p>
      <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
        <Field help="1–100，默认 20" label="丢包超过">
          <Input
            aria-label="丢包阈值"
            classNames={{
              input: "text-sm",
              inputWrapper: changedClass("notify_latency_loss_pct"),
            }}
            endContent="%"
            max={100}
            min={1}
            placeholder="20"
            size="sm"
            type="number"
            value={configs.notify_latency_loss_pct ?? ""}
            variant="bordered"
            onChange={(e) =>
              onChange("notify_latency_loss_pct", e.target.value)
            }
          />
        </Field>
        <Field help="留空不按延迟提醒，只看丢包" label="延迟超过（可选）">
          <Input
            aria-label="延迟阈值"
            classNames={{
              input: "text-sm",
              inputWrapper: changedClass("notify_latency_ms"),
            }}
            endContent="ms"
            min={0}
            placeholder="不限"
            size="sm"
            type="number"
            value={configs.notify_latency_ms ?? ""}
            variant="bordered"
            onChange={(e) => onChange("notify_latency_ms", e.target.value)}
          />
        </Field>
      </div>

      {selected.includes("daily_report") && (
        <Field
          help="按面板服务器的时区（安装脚本默认 Asia/Shanghai），0–23 点"
          label="每日汇总发送时间"
        >
          <Input
            aria-label="每日汇总发送时间"
            classNames={{
              input: "text-sm",
              inputWrapper: changedClass("notify_daily_hour"),
            }}
            endContent="点"
            max={23}
            min={0}
            placeholder="9"
            size="sm"
            type="number"
            value={configs.notify_daily_hour ?? ""}
            variant="bordered"
            onChange={(e) => onChange("notify_daily_hour", e.target.value)}
          />
        </Field>
      )}

      <div className="flex items-center justify-between gap-3 border-t border-divider pt-3">
        <span className="text-xs text-default-500">
          用当前填写的内容发送，不用先保存
        </span>
        <Button
          className="shrink-0"
          isLoading={testing}
          size="sm"
          variant="flat"
          onPress={runTest}
        >
          发送测试
        </Button>
      </div>
    </>
  );
}
