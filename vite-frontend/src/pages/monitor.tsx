import type { MonitorNodeApiItem } from "@/api/types";

import { useEffect, useMemo, useState } from "react";
import { Navigate } from "react-router-dom";
import toast from "react-hot-toast";

import { getMonitorNodes } from "@/api";
import { MonitorView } from "@/components/lazy-charts";

type MonitorNode = {
  id: number;
  name: string;
  connectionStatus: "online" | "offline";
  version?: string;
};

/** 监控 lives under 服务器 (each server's 监控) and 中转 (质量); old links land there. */
export default function MonitorPage() {
  return <Navigate replace to="/node" />;
}

/**
 * One node's monitoring, opened from the node list: live figures, history
 * charts and its service monitors.
 */
export function NodeMonitorDetail({
  nodeId,
  onBack,
}: {
  nodeId: number;
  onBack: () => void;
}) {
  const [nodes, setNodes] = useState<MonitorNodeApiItem[]>([]);

  useEffect(() => {
    let active = true;

    (async () => {
      try {
        const res = await getMonitorNodes();

        if (!active) return;
        if (res.code === 0 && Array.isArray(res.data)) setNodes(res.data);
        else toast.error(res.msg || "加载服务器失败");
      } catch {
        if (active) toast.error("加载服务器失败");
      }
    })();

    return () => {
      active = false;
    };
  }, []);

  const nodeMap = useMemo(
    () =>
      new Map<number, MonitorNode>(
        nodes
          .filter((n) => Number(n.id) > 0)
          .map((n) => [
            Number(n.id),
            {
              id: Number(n.id),
              name: String(n.name ?? ""),
              connectionStatus: n.status === 1 ? "online" : "offline",
              version: n.version,
            },
          ]),
      ),
    [nodes],
  );

  return <MonitorView focusNodeId={nodeId} nodeMap={nodeMap} onBack={onBack} />;
}
