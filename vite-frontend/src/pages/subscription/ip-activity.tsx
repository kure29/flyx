import type {
  SubscriptionIPAction,
  SubscriptionIPActivity,
  SubscriptionItem,
} from "@/api";

import { useCallback, useEffect, useState } from "react";
import toast from "react-hot-toast";
import {
  Activity,
  History,
  RefreshCw,
  ShieldAlert,
  ShieldCheck,
} from "lucide-react";

import { batchSubscriptions, getSubscriptionIPActivity } from "@/api";
import { useI18n } from "@/i18n";
import { nodeRegionFlag } from "@/pages/node/region";
import { Button } from "@/shadcn-bridge/heroui/button";
import { Chip } from "@/shadcn-bridge/heroui/chip";

function formatRelativeAgo(
  ms: number,
  t: (zh: string, vars?: Record<string, string | number>) => string,
): string {
  if (!ms) return "—";
  const sec = Math.max(0, Math.floor((Date.now() - ms) / 1000));

  if (sec < 15) return t("刚刚");
  if (sec < 60) return t("{n} 秒前", { n: sec });
  const min = Math.floor(sec / 60);

  if (min < 60) return t("{n} 分钟前", { n: min });
  const hr = Math.floor(min / 60);

  return t("{n} 小时前", { n: hr });
}

function formatClock(ms: number): string {
  const d = new Date(ms);
  const pad = (n: number) => String(n).padStart(2, "0");

  return `${pad(d.getHours())}:${pad(d.getMinutes())}`;
}

function formatPullTime(ms: number): string {
  const d = new Date(ms);
  const pad = (n: number) => String(n).padStart(2, "0");

  return `${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${pad(d.getHours())}:${pad(d.getMinutes())}:${pad(d.getSeconds())}`;
}

function actionLabel(
  action: SubscriptionIPAction,
  suspendMinutes: number,
  t: (zh: string, vars?: Record<string, string | number>) => string,
): string {
  switch (action) {
    case "suspend":
      return t("超限暂停 {n} 分钟", { n: suspendMinutes || 10 });
    case "disable":
      return t("超限自动停用");
    default:
      return t("超限仅告警");
  }
}

export function SubIPActivity({
  sub,
  onUnsuspended,
}: {
  sub: SubscriptionItem;
  onUnsuspended?: () => void;
}) {
  const { t } = useI18n();
  const [data, setData] = useState<SubscriptionIPActivity | null>(null);
  const [loading, setLoading] = useState(true);
  const [unsuspending, setUnsuspending] = useState(false);

  const fetchActivity = useCallback(
    async (silent = false) => {
      if (!silent) setLoading(true);
      try {
        const res = await getSubscriptionIPActivity(sub.id);

        if (res.code === 0 && res.data) {
          setData(res.data);
        } else if (!silent) {
          toast.error(res.msg || t("加载 IP 记录失败"));
        }
      } catch {
        if (!silent) toast.error(t("加载 IP 记录失败"));
      } finally {
        setLoading(false);
      }
    },
    [sub.id, t],
  );

  useEffect(() => {
    void fetchActivity();
  }, [fetchActivity]);

  const handleUnsuspend = async () => {
    setUnsuspending(true);
    try {
      const res = await batchSubscriptions({
        ids: [sub.id],
        action: "unsuspend",
      });

      if (res.code !== 0) {
        toast.error(res.msg || t("恢复失败"));

        return;
      }
      toast.success(t("已解除 IP 超限暂停"));
      await fetchActivity(true);
      onUnsuspended?.();
    } catch {
      toast.error(t("恢复失败"));
    } finally {
      setUnsuspending(false);
    }
  };

  const onlineIps = data?.onlineIps ?? [];
  const pullLogs = data?.pullLogs ?? [];
  const maxOnlineIps = data?.maxOnlineIps ?? sub.maxOnlineIps ?? 0;
  const maxPullIps = data?.maxPullIps ?? sub.maxPullIps ?? 0;
  const ipAction = data?.ipAction ?? sub.ipAction ?? "alert";
  const ipSuspendMinutes = data?.ipSuspendMinutes ?? sub.ipSuspendMinutes ?? 10;
  const suspendedUntil = data?.ipSuspendedUntil ?? sub.ipSuspendedUntil ?? 0;
  const isSuspended = suspendedUntil > Date.now();
  const pullIpCount24h = data?.pullIpCount24h ?? sub.pullIpCount24h ?? 0;
  const overOnline = maxOnlineIps > 0 && onlineIps.length > maxOnlineIps;
  const overPull = maxPullIps > 0 && pullIpCount24h >= maxPullIps;

  return (
    <div className="space-y-3">
      {isSuspended && (
        <div className="flex flex-wrap items-center justify-between gap-2 rounded-lg border border-warning-300/70 bg-warning-50/70 px-3 py-2.5 text-xs dark:border-warning-500/30 dark:bg-warning-500/10">
          <div className="flex items-center gap-2 text-warning-700 dark:text-warning-400">
            <ShieldAlert className="h-4 w-4 shrink-0" />
            <span>
              {t("在线 IP 超限暂停中，预计 {time} 自动恢复", {
                time: formatClock(suspendedUntil),
              })}
            </span>
          </div>
          <Button
            className="h-7 min-w-0 px-2.5 text-xs"
            color="warning"
            isLoading={unsuspending}
            size="sm"
            variant="flat"
            onPress={() => void handleUnsuspend()}
          >
            {t("立即恢复")}
          </Button>
        </div>
      )}

      {/* Compact 2-column summary strip */}
      <div className="grid grid-cols-2 gap-px overflow-hidden rounded-lg border border-divider bg-divider">
        <div className="bg-background px-3 py-2.5">
          <div className="flex items-center justify-between gap-1 text-[11px] text-default-400">
            <span>{t("当前在线 IP")}</span>
            {maxOnlineIps > 0 && (
              <span className="truncate text-[10px]">
                {actionLabel(ipAction, ipSuspendMinutes, t)}
              </span>
            )}
          </div>
          <div className="mt-1 flex items-baseline gap-1 tabular-nums">
            <span
              className={`text-lg font-semibold tracking-tight ${
                overOnline
                  ? "text-rose-600 dark:text-rose-400"
                  : "text-foreground"
              }`}
            >
              {onlineIps.length}
            </span>
            <span className="text-xs text-default-400">
              / {maxOnlineIps > 0 ? maxOnlineIps : t("不限")}
            </span>
          </div>
        </div>

        <div className="bg-background px-3 py-2.5">
          <div className="text-[11px] text-default-400">
            {t("24 小时拉取 IP")}
          </div>
          <div className="mt-1 flex items-baseline gap-1 tabular-nums">
            <span
              className={`text-lg font-semibold tracking-tight ${
                overPull
                  ? "text-amber-600 dark:text-amber-400"
                  : "text-foreground"
              }`}
            >
              {pullIpCount24h}
            </span>
            <span className="text-xs text-default-400">
              / {maxPullIps > 0 ? maxPullIps : t("不限")}
            </span>
          </div>
        </div>
      </div>

      {/* Live online IPs */}
      <div className="space-y-1.5">
        <div className="flex items-center justify-between gap-2">
          <div className="flex items-center gap-1.5 text-xs font-medium text-foreground">
            <Activity className="h-3.5 w-3.5 text-emerald-500" />
            <span>{t("活跃连接 IP")}</span>
            <span className="text-[11px] font-normal text-default-400">
              ({t("最近 2 分钟")})
            </span>
          </div>
          <Button
            className="h-7 min-w-0 px-2 text-xs text-default-500"
            isDisabled={loading}
            size="sm"
            startContent={
              <RefreshCw
                className={`h-3 w-3 ${loading ? "animate-spin" : ""}`}
              />
            }
            variant="light"
            onPress={() => void fetchActivity()}
          >
            {t("刷新")}
          </Button>
        </div>

        {loading && !data ? (
          <div className="rounded-lg border border-divider py-6 text-center text-xs text-default-400">
            {t("正在加载...")}
          </div>
        ) : onlineIps.length === 0 ? (
          <div className="rounded-lg border border-divider bg-default-50/40 px-3 py-4 text-center text-xs text-default-400 dark:bg-default-100/15">
            <p>{t("暂无在线 IP")}</p>
            <p className="mt-1 text-[11px] text-default-400/80">
              {maxOnlineIps > 0
                ? t(
                    "需节点 flyx-agent 升级至最新版本；中转场景请启用 PROXY Protocol",
                  )
                : t(
                    "Xray 节点默认统计；sing-box 节点设置在线 IP 上限后自动开启追踪",
                  )}
            </p>
          </div>
        ) : (
          <div className="divide-y divide-divider/70 overflow-hidden rounded-lg border border-divider">
            {onlineIps.map((item) => {
              const flag = item.country ? nodeRegionFlag(item.country) : "";

              return (
                <div
                  key={item.ip}
                  className="flex items-center justify-between gap-2 bg-background px-3 py-2 text-xs"
                >
                  <div className="min-w-0 flex-1 space-y-1">
                    <div className="flex items-center gap-1.5">
                      {flag && (
                        <span className="text-sm leading-none">{flag}</span>
                      )}
                      <span className="truncate font-mono font-medium text-foreground">
                        {item.ip}
                      </span>
                      {item.country && (
                        <span className="rounded bg-default-100 px-1 py-0.2 text-[10px] text-default-500 dark:bg-default-100/60">
                          {item.country}
                        </span>
                      )}
                    </div>
                    <div className="flex flex-wrap items-center gap-1">
                      {item.nodeNames.map((name, idx) => (
                        <span
                          key={`${item.nodeIds[idx] ?? idx}-${name}`}
                          className="inline-flex items-center rounded border border-divider/80 bg-default-50 px-1.5 py-0.5 text-[10px] text-default-600 dark:bg-default-100/30 dark:text-default-400"
                        >
                          {name}
                        </span>
                      ))}
                    </div>
                  </div>
                  <span className="shrink-0 tabular-nums text-[11px] text-default-400">
                    {formatRelativeAgo(item.lastSeen, t)}
                  </span>
                </div>
              );
            })}
          </div>
        )}
      </div>

      {/* Subscription pull history */}
      <div className="space-y-1.5">
        <div className="flex items-center justify-between gap-2">
          <div className="flex items-center gap-1.5 text-xs font-medium text-foreground">
            <History className="h-3.5 w-3.5 text-default-400" />
            <span>{t("订阅拉取记录")}</span>
            {pullLogs.length > 0 && (
              <span className="text-[11px] font-normal tabular-nums text-default-400">
                ({pullLogs.length})
              </span>
            )}
          </div>
          {maxPullIps > 0 && (
            <span className="inline-flex items-center gap-1 text-[11px] text-default-400">
              <ShieldCheck className="h-3 w-3 text-emerald-500" />
              {t("24 小时限 {n} 个 IP", { n: maxPullIps })}
            </span>
          )}
        </div>

        {loading && !data ? (
          <div className="rounded-lg border border-divider py-6 text-center text-xs text-default-400">
            {t("正在加载...")}
          </div>
        ) : pullLogs.length === 0 ? (
          <div className="rounded-lg border border-divider bg-default-50/40 px-3 py-4 text-center text-xs text-default-400 dark:bg-default-100/15">
            {t("暂无订阅拉取记录")}
          </div>
        ) : (
          <div className="max-h-64 divide-y divide-divider/70 overflow-y-auto rounded-lg border border-divider">
            {pullLogs.map((row) => {
              const flag = row.country ? nodeRegionFlag(row.country) : "";

              return (
                <div key={row.id} className="bg-background px-3 py-2 text-xs">
                  <div className="flex items-center justify-between gap-2">
                    <div className="flex min-w-0 items-center gap-1.5">
                      {flag && (
                        <span className="shrink-0 text-sm leading-none">
                          {flag}
                        </span>
                      )}
                      <span className="truncate font-mono font-medium text-foreground">
                        {row.ip || "—"}
                      </span>
                      {row.format && (
                        <span className="shrink-0 rounded bg-default-100 px-1.5 py-0.5 text-[10px] text-default-600 dark:bg-default-100/60 dark:text-default-400">
                          {row.format}
                        </span>
                      )}
                      {row.blocked && (
                        <Chip
                          className="h-4 px-1 text-[10px]"
                          color="danger"
                          size="sm"
                          variant="flat"
                        >
                          {t("已拦截")}
                        </Chip>
                      )}
                    </div>
                    <span className="shrink-0 tabular-nums text-[11px] text-default-400">
                      {formatPullTime(row.createdTime)}
                    </span>
                  </div>
                  {row.userAgent && (
                    <p
                      className="mt-0.5 truncate text-[11px] text-default-400"
                      title={row.userAgent}
                    >
                      {row.userAgent}
                    </p>
                  )}
                </div>
              );
            })}
          </div>
        )}
      </div>
    </div>
  );
}
