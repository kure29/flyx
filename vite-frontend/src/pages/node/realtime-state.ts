import type { Node } from "@/pages/node/types";
import type { NodeRealtimeMessage } from "@/pages/node/use-node-realtime";

import { buildNodeSystemInfo } from "@/pages/node/system-info";

/** Apply a websocket delivery in one list traversal, retaining unchanged rows. */
export function applyNodeRealtimeBatch(
  nodes: Node[],
  messages: NodeRealtimeMessage[],
): Node[] {
  const byNode = new Map<number, NodeRealtimeMessage[]>();

  for (const message of messages) {
    const id = Number(message.id);

    if (
      !Number.isFinite(id) ||
      !["status", "info", "metric"].includes(message.type ?? "")
    )
      continue;
    let data = message.data;

    if (message.type === "metric") {
      try {
        data = typeof data === "string" ? JSON.parse(data) : data;
      } catch {
        continue;
      }
      if (!data || typeof data !== "object") continue;
    }
    const pending = byNode.get(id) ?? [];

    pending.push({ ...message, data });
    byNode.set(id, pending);
  }
  if (byNode.size === 0) return nodes;
  let changed = false;
  const next = nodes.map((original) => {
    let node = original;

    for (const message of byNode.get(node.id) ?? []) {
      if (message.type === "status") {
        if (message.data === 1 && node.connectionStatus !== "online") {
          node = { ...node, connectionStatus: "online" };
        }
      } else if (message.type === "info") {
        const systemInfo = buildNodeSystemInfo(message.data, node.systemInfo);

        if (systemInfo)
          node = { ...node, connectionStatus: "online", systemInfo };
      } else {
        const metric = message.data as Record<string, number | undefined>;

        node = applyMetric(node, metric);
      }
    }
    if (node !== original) changed = true;

    return node;
  });

  return changed ? next : nodes;
}

function applyMetric(
  node: Node,
  metric: Record<string, number | undefined>,
): Node {
  const incomingUptime = metric.uptime ?? 0;

  return {
    ...node,
    connectionStatus: "online",
    systemInfo: {
      cpuUsage: metric.cpuUsage ?? metric.cpu_usage ?? 0,
      memoryUsage: metric.memoryUsage ?? metric.memory_usage ?? 0,
      uploadTraffic: metric.netOutBytes ?? metric.bytes_transmitted ?? 0,
      downloadTraffic: metric.netInBytes ?? metric.bytes_received ?? 0,
      uploadSpeed: metric.netOutSpeed ?? metric.net_out_speed ?? 0,
      downloadSpeed: metric.netInSpeed ?? metric.net_in_speed ?? 0,
      uptime: incomingUptime || node.systemInfo?.uptime || 0,
      diskUsage: metric.diskUsage ?? metric.disk_usage,
      load1: metric.load1,
      load5: metric.load5,
      load15: metric.load15,
      tcpConns: metric.tcpConns ?? metric.tcp_conns,
      udpConns: metric.udpConns ?? metric.udp_conns,
      netInSpeed: metric.netInSpeed ?? metric.net_in_speed,
      netOutSpeed: metric.netOutSpeed ?? metric.net_out_speed,
    },
  };
}
