export interface NodeSystemInfo {
  cpuUsage: number;
  memoryUsage: number;
  uploadTraffic: number;
  downloadTraffic: number;
  uploadSpeed: number;
  downloadSpeed: number;
  uptime: number;
  diskUsage?: number;
  load1?: number;
  load5?: number;
  load15?: number;
  tcpConns?: number;
  udpConns?: number;
  netInSpeed?: number;
  netOutSpeed?: number;
  /** Capacity; 0 from agents that do not report it. */
  cpuCores?: number;
  memUsed?: number;
  memTotal?: number;
  diskUsed?: number;
  diskTotal?: number;
}

type RawSystemInfo = Record<string, string | number | undefined>;

const toInteger = (value: string | number | undefined): number => {
  return Number.parseInt(String(value ?? 0), 10) || 0;
};

const toFloat = (value: string | number | undefined): number => {
  return Number.parseFloat(String(value ?? 0)) || 0;
};

const parseRawSystemInfo = (messageData: unknown): RawSystemInfo | null => {
  if (typeof messageData === "string") {
    try {
      const parsed = JSON.parse(messageData);

      if (parsed && typeof parsed === "object") {
        return parsed as RawSystemInfo;
      }

      return null;
    } catch {
      return null;
    }
  }

  if (messageData && typeof messageData === "object") {
    return messageData as RawSystemInfo;
  }

  return null;
};

/**
 * Heuristic to verify the parsed object actually contains system-info fields.
 * Prevents non-metric messages (command responses, etc.) from being mistakenly
 * treated as system info, which would reset all values to 0 and cause UI flicker.
 */
const SYSTEM_INFO_KEYS = [
  "uptime",
  "cpu_usage",
  "memory_usage",
  "disk_usage",
  "bytes_received",
  "bytes_transmitted",
  "net_in_speed",
  "net_out_speed",
  "tcp_conns",
  "udp_conns",
  "load1",
  "load5",
  "load15",
] as const;

const looksLikeSystemInfo = (raw: RawSystemInfo): boolean => {
  let matched = 0;

  for (const key of SYSTEM_INFO_KEYS) {
    if (key in raw && raw[key] !== undefined) {
      matched++;
      if (matched >= 3) {
        return true;
      }
    }
  }

  return false;
};

export const buildNodeSystemInfo = (
  messageData: unknown,
  previous: NodeSystemInfo | null | undefined,
): NodeSystemInfo | null => {
  const raw = parseRawSystemInfo(messageData);

  if (!raw || !looksLikeSystemInfo(raw)) {
    return null;
  }

  const uploadTraffic = toInteger(raw.bytes_transmitted);
  const downloadTraffic = toInteger(raw.bytes_received);
  const uptime = toInteger(raw.uptime);

  let uploadSpeed = 0;
  let downloadSpeed = 0;

  if (previous && previous.uptime) {
    const timeDiff = uptime - previous.uptime;

    if (timeDiff > 0 && timeDiff <= 10) {
      const uploadDiff = uploadTraffic - previous.uploadTraffic;
      const downloadDiff = downloadTraffic - previous.downloadTraffic;

      if (uploadTraffic >= previous.uploadTraffic && uploadDiff >= 0) {
        uploadSpeed = uploadDiff / timeDiff;
      }

      if (downloadTraffic >= previous.downloadTraffic && downloadDiff >= 0) {
        downloadSpeed = downloadDiff / timeDiff;
      }
    }
  }

  return {
    cpuUsage: toFloat(raw.cpu_usage),
    memoryUsage: toFloat(raw.memory_usage),
    uploadTraffic,
    downloadTraffic,
    uploadSpeed,
    downloadSpeed,
    uptime,
    diskUsage: toFloat(raw.disk_usage),
    load1: toFloat(raw.load1),
    load5: toFloat(raw.load5),
    load15: toFloat(raw.load15),
    tcpConns: toInteger(raw.tcp_conns),
    udpConns: toInteger(raw.udp_conns),
    netInSpeed: toInteger(raw.net_in_speed),
    netOutSpeed: toInteger(raw.net_out_speed),
    cpuCores: toInteger(raw.cpu_cores),
    memUsed: toFloat(raw.mem_used),
    memTotal: toFloat(raw.mem_total),
    diskUsed: toFloat(raw.disk_used),
    diskTotal: toFloat(raw.disk_total),
  };
};
