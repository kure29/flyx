import type { RelayLatency } from "@/api/types";

import { LatencyCell, TONE, pointTone } from "@/components/latency-cell";

/** Name of the only point when no reference points are set (backend). */
const PATH_POINT = "中转路径";

/** The legs a relay line takes from its entry node to the landing. */
function RelayPath({ latency }: { latency: RelayLatency }) {
  const pathOnly = latency.points?.[0]?.name === PATH_POINT;

  if (latency.hops.length === 0 && !latency.error) return null;

  return (
    <div className="space-y-1 border-t border-divider px-2 py-2 text-xs">
      <div className="text-default-500">
        {pathOnly
          ? "入口到节点的各段延迟"
          : `= ${latency.entry ?? "入口"} 到测试点 + 下面各段`}
      </div>
      {latency.hops.map((h, i) => (
        <div
          key={i}
          className="flex items-center justify-between gap-2 font-mono"
        >
          <span className="truncate font-sans">
            {h.from} → {h.to}
          </span>
          <span
            className={`shrink-0 ${TONE[pointTone({ name: "", ...h })][0].split(" ")[1]}`}
          >
            {h.ok ? `${h.latencyMs} ms` : h.error || "不通"}
            {h.ok && (h.lossPct ?? 0) > 0 && (
              <span className="ml-1 text-default-400">丢 {h.lossPct}%</span>
            )}
          </span>
        </div>
      ))}
      {latency.hops.length === 0 && latency.error && (
        <div className="text-rose-600">{latency.error}</div>
      )}
    </div>
  );
}

/**
 * A relay line's latency: through the line to each reference point
 * (estimated as the entry node's latency plus every leg), with the legs
 * in the details.
 */
export function RelayLatencyCell({
  latency,
  probing,
  onProbe,
}: {
  latency?: RelayLatency;
  probing?: boolean;
  onProbe: () => void;
}) {
  const pathOnly = latency?.points?.[0]?.name === PATH_POINT;

  return (
    <LatencyCell
      footer={latency && <RelayPath latency={latency} />}
      probe={latency}
      probing={probing}
      title={pathOnly ? "中转路径延迟" : "经中转到各测试点（估算）"}
      onProbe={onProbe}
    />
  );
}
