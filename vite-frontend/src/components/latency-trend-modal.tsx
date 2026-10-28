import type { LatencyTrend } from "@/api/types";

import { useEffect, useMemo, useState } from "react";
import { Loader2 } from "lucide-react";
import {
  CartesianGrid,
  Line,
  LineChart,
  ResponsiveContainer,
  Tooltip,
  XAxis,
  YAxis,
} from "recharts";

import { useLatestRequest } from "@/hooks/use-latest-request";
import { getNodeLatencyHistory } from "@/api";
import {
  Modal,
  ModalBody,
  ModalContent,
  ModalHeader,
} from "@/shadcn-bridge/heroui/modal";

const RANGES = [
  { hours: 24, label: "24 小时" },
  { hours: 72, label: "3 天" },
  { hours: 168, label: "7 天" },
];

const COLORS = [
  "#10b981",
  "#3b82f6",
  "#f59e0b",
  "#ef4444",
  "#8b5cf6",
  "#06b6d4",
  "#ec4899",
  "#84cc16",
  "#f97316",
  "#64748b",
];

function timeLabel(t: number, hours: number) {
  const d = new Date(t);
  const hm = `${String(d.getHours()).padStart(2, "0")}:${String(
    d.getMinutes(),
  ).padStart(2, "0")}`;

  return hours > 24 ? `${d.getMonth() + 1}/${d.getDate()} ${hm}` : hm;
}

/** A node's latency to each reference point over time, one line per point. */
export function LatencyTrendModal({
  nodeId,
  nodeName,
  onClose,
}: {
  nodeId: number | null;
  nodeName?: string;
  onClose: () => void;
}) {
  const [hours, setHours] = useState(24);
  const [trend, setTrend] = useState<LatencyTrend | null>(null);
  const [loading, setLoading] = useState(false);

  const beginRequest = useLatestRequest(`${nodeId}:${hours}`);

  useEffect(() => {
    setTrend(null);
    if (nodeId === null) return;
    const isCurrent = beginRequest();

    setLoading(true);
    getNodeLatencyHistory(nodeId, hours)
      .then((res) => {
        if (isCurrent()) setTrend(res.code === 0 ? (res.data ?? null) : null);
      })
      .catch(() => {
        if (isCurrent()) setTrend(null);
      })
      .finally(() => {
        if (isCurrent()) setLoading(false);
      });
  }, [nodeId, hours, beginRequest]);

  useEffect(() => {
    if (nodeId === null) setHours(24);
  }, [nodeId]);

  // One row per time with each point's latency and loss.
  const rows = useMemo(() => {
    const byTime = new Map<number, Record<string, number | null>>();

    for (const p of trend?.points ?? []) {
      for (const s of p.samples) {
        const row = byTime.get(s.t) ?? { t: s.t };

        row[p.name] = s.ms;
        row[`${p.name}\u0000loss`] = s.lossPct;
        byTime.set(s.t, row);
      }
    }

    return [...byTime.values()].sort((a, b) => Number(a.t) - Number(b.t));
  }, [trend]);

  const points = trend?.points ?? [];

  return (
    <Modal
      backdrop="blur"
      classNames={{
        base: "!w-[calc(100%-32px)] !mx-auto sm:!w-full rounded-2xl overflow-hidden",
      }}
      isOpen={nodeId !== null}
      placement="center"
      scrollBehavior="inside"
      size="2xl"
      onOpenChange={(v) => !v && onClose()}
    >
      <ModalContent>
        <ModalHeader closable className="flex flex-col gap-0.5">
          <h2 className="text-lg font-bold">延迟趋势</h2>
          <p className="text-xs font-normal text-default-500">
            {nodeName ? `${nodeName} → 各测试点` : "服务器 → 各测试点"}
            ，每 3 分钟一次
          </p>
        </ModalHeader>
        <ModalBody className="space-y-3">
          <div className="flex gap-1 self-start rounded-lg bg-default-100 p-1">
            {RANGES.map((r) => (
              <button
                key={r.hours}
                className={`rounded-md px-3 py-1 text-xs transition-colors ${
                  hours === r.hours
                    ? "bg-content1 font-medium shadow-sm"
                    : "text-default-500 hover:text-default-700"
                }`}
                type="button"
                onClick={() => setHours(r.hours)}
              >
                {r.label}
              </button>
            ))}
          </div>

          {loading && !trend ? (
            <div className="flex h-52 items-center sm:h-64 justify-center">
              <Loader2 className="h-5 w-5 animate-spin text-default-400" />
            </div>
          ) : rows.length === 0 ? (
            <p className="flex h-52 items-center sm:h-64 justify-center text-sm text-default-500">
              这段时间还没有记录
            </p>
          ) : (
            <div className="h-52 sm:h-64">
              <ResponsiveContainer height="100%" width="100%">
                <LineChart data={rows}>
                  <CartesianGrid strokeDasharray="3 3" />
                  <XAxis
                    dataKey="t"
                    fontSize={11}
                    minTickGap={32}
                    tickFormatter={(t: number) => timeLabel(t, hours)}
                  />
                  <YAxis
                    fontSize={11}
                    tickFormatter={(v: number) => `${Math.round(v)}ms`}
                    width={52}
                  />
                  <Tooltip
                    contentStyle={{
                      backgroundColor: "rgba(0,0,0,0.8)",
                      border: "none",
                      borderRadius: "8px",
                      fontSize: 12,
                    }}
                    formatter={(
                      value: unknown,
                      name: unknown,
                      item: { payload?: unknown },
                    ) => {
                      const loss = Number(
                        (item?.payload as Record<string, unknown>)?.[
                          `${String(name)}\u0000loss`
                        ] ?? 0,
                      );

                      return [
                        value === null || value === undefined
                          ? "超时"
                          : `${Number(value).toFixed(0)}ms${
                              loss > 0 ? ` · 丢包 ${loss}%` : ""
                            }`,
                        String(name),
                      ];
                    }}
                    itemStyle={{ padding: 0 }}
                    labelFormatter={(t: unknown) =>
                      timeLabel(Number(t), hours > 24 ? 48 : 24)
                    }
                    labelStyle={{ color: "#fff" }}
                  />
                  {points.map((p, i) => (
                    <Line
                      key={p.name}
                      connectNulls={false}
                      dataKey={p.name}
                      dot={false}
                      isAnimationActive={false}
                      name={p.name}
                      stroke={COLORS[i % COLORS.length]}
                      strokeWidth={1.5}
                      type="monotone"
                    />
                  ))}
                </LineChart>
              </ResponsiveContainer>
            </div>
          )}

          {points.length > 0 && (
            <div className="overflow-x-auto">
              <table className="w-full text-xs">
                <thead className="text-default-500">
                  <tr className="border-b border-divider">
                    <th className="py-1.5 text-left font-normal">测试点</th>
                    <th className="py-1.5 text-right font-normal">平均</th>
                    <th className="py-1.5 text-right font-normal">最高</th>
                    <th className="py-1.5 text-right font-normal">平均丢包</th>
                  </tr>
                </thead>
                <tbody className="font-mono">
                  {points.map((p, i) => (
                    <tr
                      key={p.name}
                      className="border-b border-divider last:border-0"
                    >
                      <td className="py-1.5 font-sans">
                        <span
                          className="mr-2 inline-block h-2 w-2 rounded-full"
                          style={{ background: COLORS[i % COLORS.length] }}
                        />
                        {p.name}
                      </td>
                      <td className="py-1.5 text-right">
                        {p.avgMs ? `${Math.round(p.avgMs)} ms` : "-"}
                      </td>
                      <td className="py-1.5 text-right">
                        {p.maxMs ? `${p.maxMs} ms` : "-"}
                      </td>
                      <td
                        className={`py-1.5 text-right ${p.lossPct >= 5 ? "text-rose-600" : ""}`}
                      >
                        {p.lossPct}%
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
        </ModalBody>
      </ModalContent>
    </Modal>
  );
}
