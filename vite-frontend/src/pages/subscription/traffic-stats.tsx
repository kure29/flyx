import { getSubscriptionTraffic, getUserSubscriptionTraffic } from "@/api";
import { TrafficHistoryChart } from "@/components/traffic-history-chart";
import { useI18n } from "@/i18n";

/**
 * A subscription's traffic by hour, day or month: totals, an up / down
 * line chart and the per-bucket list.
 */
export function SubTrafficStats({
  id,
  scope = "admin",
}: {
  id: number;
  scope?: "admin" | "user";
}) {
  const { t } = useI18n();

  return (
    <TrafficHistoryChart
      cacheKey={`${scope}-${id}`}
      fetchData={(range) =>
        scope === "user"
          ? getUserSubscriptionTraffic(id, range)
          : getSubscriptionTraffic(id, range)
      }
      footnotes={({ sinceDate }) => (
        <>
          <p className="text-xs text-default-400">
            {t(
              "统计自建节点上按用户区分的协议（VLESS、Trojan、Hysteria2 等）；导入的节点、Shadowsocks、Snell 不计入。3.3.29 起开始记录，之前的流量不在图里",
            )}
          </p>
          {sinceDate && (
            <p className="text-xs text-default-400">
              {t(
                "订阅上方的已用流量从本周期开始（{date}，创建或上次重置）累计，和这里的时间范围不同，两边的数不一定相等",
                { date: sinceDate },
              )}
            </p>
          )}
        </>
      )}
    />
  );
}
