import type { MonitorNodeApiItem } from "@/api/types";

import { useCallback, useEffect, useMemo, useState, useRef } from "react";
import { Navigate } from "react-router-dom";
import toast from "react-hot-toast";

import { useVisiblePolling } from "@/hooks/use-visible-polling";
import { AnimatedPage } from "@/components/animated-page";
import { PageHeader, PageTabs, PageToolbar } from "@/components/page-kit";
import { useViewMode } from "@/components/view-mode";
import { getMonitorNodes } from "@/api";
import { MonitorView, TunnelMonitorView } from "@/components/lazy-charts";
import { useNodeRealtime } from "@/pages/node/use-node-realtime";
import { isAdmin } from "@/utils/auth";

type MonitorNode = {
  id: number;
  name: string;
  connectionStatus: "online" | "offline";
  version?: string;
};

type MonitorTab = "nodes" | "tunnels";

const formatBytesPerSecond = (bytesPerSecond: number): string => {
  if (!Number.isFinite(bytesPerSecond) || bytesPerSecond <= 0) return "0 B/s";

  const k = 1024;
  const sizes = ["B/s", "KB/s", "MB/s", "GB/s", "TB/s"];
  const i = Math.floor(Math.log(bytesPerSecond) / Math.log(k));

  return `${parseFloat((bytesPerSecond / Math.pow(k, i)).toFixed(2))} ${sizes[i]}`;
};

/**
 * 监控: admins have it under 节点 (each node's 监控) and 中转 (质量); users
 * allowed to see monitoring get this overview.
 */
export default function MonitorPage() {
  return isAdmin() ? <Navigate replace to="/node" /> : <MonitorOverview />;
}

function MonitorOverview() {
  const [nodes, setNodes] = useState<MonitorNodeApiItem[]>([]);
  const [nodesLoading, setNodesLoading] = useState(false);
  const [nodesError, setNodesError] = useState<string | null>(null);
  const [activeTab, setActiveTab] = useState<MonitorTab>("nodes");
  const [viewMode, setViewMode] = useViewMode("monitor");
  const card = viewMode === "card";

  const [realtimeNodeStatus, setRealtimeNodeStatus] = useState<
    Record<number, "online" | "offline">
  >({});
  const [realtimeNodeMetrics, setRealtimeNodeMetrics] = useState<
    Record<number, any>
  >({});

  const handleRealtimeMessage = useCallback((message: any) => {
    const nodeId = Number(message?.id ?? 0);

    if (!nodeId || Number.isNaN(nodeId)) return;

    const type = String(message?.type ?? "");
    const payload = message?.data;

    if (type === "status") {
      const status = Number(payload);

      setRealtimeNodeStatus((prev) => ({
        ...prev,
        [nodeId]: status === 1 ? "online" : "offline",
      }));

      return;
    }

    if (type === "metric") {
      let raw = payload;

      if (typeof raw === "string") {
        try {
          raw = JSON.parse(raw);
        } catch {
          return;
        }
      }
      if (!raw || typeof raw !== "object") return;
      const metric = raw as any;

      setRealtimeNodeMetrics((prev) => ({
        ...prev,
        [nodeId]: {
          cpuUsage: Number(metric.cpuUsage ?? metric.cpu_usage ?? 0),
          netInSpeed: Number(metric.netInSpeed ?? metric.net_in_speed ?? 0),
          netOutSpeed: Number(metric.netOutSpeed ?? metric.net_out_speed ?? 0),
          tcpConns: Number(metric.tcpConns ?? metric.tcp_conns ?? 0),
          udpConns: Number(metric.udpConns ?? metric.udp_conns ?? 0),
        },
      }));
    }
  }, []);

  useNodeRealtime({
    onMessage: handleRealtimeMessage,
    enabled: true,
  });

  const loadNodesPending = useRef(0);

  const loadNodes = useCallback(async (options?: { silent?: boolean }) => {
    const silent = options?.silent ?? false;

    if (silent && loadNodesPending.current > 0) return;
    loadNodesPending.current += 1;

    if (!silent) setNodesLoading(true);
    try {
      const response = await getMonitorNodes();

      if (response.code === 0 && Array.isArray(response.data)) {
        setNodesError(null);
        setNodes(response.data);

        return;
      }

      if (response.code === 403) {
        setNodes([]);
        setNodesError(response.msg || "暂无监控权限，请联系管理员授权");

        return;
      }

      if (!silent) toast.error(response.msg || "加载服务器失败");
    } catch {
      if (!silent) toast.error("加载服务器失败");
    } finally {
      loadNodesPending.current -= 1;
      if (!silent) setNodesLoading(false);
    }
  }, []);

  useEffect(() => {
    void loadNodes();
  }, [loadNodes]);

  useVisiblePolling(
    useCallback(() => loadNodes({ silent: true }), [loadNodes]),
    30_000,
  );

  const nodeMap = useMemo(() => {
    const list: MonitorNode[] = nodes
      .filter((n) => Number(n.id) > 0)
      .map((n) => ({
        id: Number(n.id),
        name: String(n.name ?? ""),
        connectionStatus:
          realtimeNodeStatus[Number(n.id)] ||
          (n.status === 1 ? "online" : "offline"),
        version: n.version,
      }));

    return new Map<number, MonitorNode>(list.map((n) => [n.id, n]));
  }, [nodes, realtimeNodeStatus]);

  const aggregateMetrics = useMemo(() => {
    let totalCpu = 0;
    let totalConns = 0;
    let totalNetIn = 0;
    let totalNetOut = 0;
    let onlineCount = 0;

    nodeMap.forEach((node) => {
      if (node.connectionStatus === "online") {
        onlineCount++;
        const metric = realtimeNodeMetrics[node.id];

        if (metric) {
          totalCpu += metric.cpuUsage;
          totalConns += metric.tcpConns + metric.udpConns;
          totalNetIn += metric.netInSpeed;
          totalNetOut += metric.netOutSpeed;
        }
      }
    });

    const avgCpu = onlineCount > 0 ? totalCpu / onlineCount : 0;

    return {
      avgCpu,
      totalConns,
      totalBandwidth: totalNetIn + totalNetOut,
    };
  }, [nodeMap, realtimeNodeMetrics]);

  const stats = [
    {
      key: "load",
      label: "系统负载",
      value: `${aggregateMetrics.avgCpu.toFixed(1)}%`,
    },
    {
      key: "conns",
      label: "活动连接",
      value: String(aggregateMetrics.totalConns),
    },
    {
      key: "bandwidth",
      label: "带宽",
      value: formatBytesPerSecond(aggregateMetrics.totalBandwidth),
    },
  ];

  return (
    <AnimatedPage className="px-3 lg:px-6 py-6">
      <PageHeader description="服务器资源与隧道质量的实时状态" title="监控" />

      <div className="mb-4 grid grid-cols-3 gap-2 sm:max-w-2xl">
        {stats.map((s) => (
          <div
            key={s.key}
            className="min-w-0 rounded-xl border border-divider bg-content1 px-3 py-2"
          >
            <div className="truncate text-xs text-default-500">{s.label}</div>
            <div className="truncate font-mono text-sm font-semibold sm:text-base">
              {s.value}
            </div>
          </div>
        ))}
      </div>

      <PageTabs
        tabs={[
          { key: "nodes", label: "服务器" },
          { key: "tunnels", label: "隧道" },
        ]}
        value={activeTab}
        onChange={setActiveTab}
      />
      <PageToolbar view={{ value: viewMode, onChange: setViewMode }} />

      {nodesError && activeTab === "nodes" && (
        <div className="mb-3 rounded-xl border border-divider bg-content1 px-3 py-2 text-sm text-default-600">
          {nodesError}
        </div>
      )}

      {activeTab === "nodes" ? (
        <MonitorView
          card={card}
          nodeMap={nodeMap}
          refreshing={nodesLoading}
          onRefresh={() => void loadNodes()}
        />
      ) : (
        <TunnelMonitorView card={card} />
      )}
    </AnimatedPage>
  );
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
