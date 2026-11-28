import { t } from "@/i18n";

export type TrafficUnit = "MB" | "GB" | "TB" | "PB";

export const MIB = 1024 * 1024;
export const GIB = 1024 * MIB;

export const TRAFFIC_UNIT_MIB: Record<TrafficUnit, number> = {
  MB: 1,
  GB: 1024,
  TB: 1024 ** 2,
  PB: 1024 ** 3,
};

const BYTE_UNITS = ["B", "KB", "MB", "GB", "TB", "PB"];
const SHORT_BYTE_UNITS = ["B", "K", "M", "G", "T", "P"];

export function formatTraffic(bytes: number): string {
  if (!Number.isFinite(bytes) || bytes <= 0) return "0 B";

  let value = bytes;
  let unit = 0;

  while (value >= 1024 && unit < BYTE_UNITS.length - 1) {
    value /= 1024;
    unit++;
  }

  return `${unit === 0 ? Math.floor(value) : value.toFixed(2)} ${BYTE_UNITS[unit]}`;
}

/** Compact sizes that fit KPI tiles and breakdown rows: 3.3 GB, 267 MB. */
export function formatTrafficCompact(bytes: number): string {
  if (!Number.isFinite(bytes) || bytes <= 0) return "0 B";

  let value = bytes;
  let unit = 0;

  while (value >= 1024 && unit < BYTE_UNITS.length - 1) {
    value /= 1024;
    unit++;
  }

  return `${value >= 100 || unit === 0 ? Math.round(value) : value.toFixed(1)} ${BYTE_UNITS[unit]}`;
}

/** Dense inline sizes for speed/row readouts: 3.3G, 267M. */
export function formatTrafficShort(bytes: number): string {
  if (!Number.isFinite(bytes) || bytes <= 0) return "0B";

  let value = bytes;
  let unit = 0;

  while (value >= 1024 && unit < SHORT_BYTE_UNITS.length - 1) {
    value /= 1024;
    unit++;
  }

  return `${value >= 100 || unit === 0 ? Math.round(value) : value.toFixed(1)}${SHORT_BYTE_UNITS[unit]}`;
}

/** Short chart Y-axis tick labels: 1.2G, 340M, 0. */
export function formatTrafficAxis(bytes: number): string {
  if (!Number.isFinite(bytes) || bytes <= 0) return "0";

  let value = bytes;
  let unit = 0;

  while (value >= 1024 && unit < SHORT_BYTE_UNITS.length - 1) {
    value /= 1024;
    unit++;
  }

  return `${value >= 10 || unit === 0 ? Math.round(value) : value.toFixed(1)}${SHORT_BYTE_UNITS[unit]}`;
}

export function flowLimitMiB(flowGB: number, flowMiB?: number): number {
  return flowMiB && flowMiB > 0 ? flowMiB : flowGB * 1024;
}

export function flowLimitBytes(flowGB: number, flowMiB?: number): number {
  return flowLimitMiB(flowGB, flowMiB) * MIB;
}

export function formatFlowLimit(flowGB: number, flowMiB?: number): string {
  if (flowGB === 99999 && !flowMiB) return t("无限制");

  return formatTraffic(flowLimitBytes(flowGB, flowMiB));
}

export function preferredTrafficUnit(mib: number): TrafficUnit {
  if (mib <= 0) return "GB";
  if (mib > 0 && mib % TRAFFIC_UNIT_MIB.PB === 0) return "PB";
  if (mib > 0 && mib % TRAFFIC_UNIT_MIB.TB === 0) return "TB";
  if (mib > 0 && mib % TRAFFIC_UNIT_MIB.GB === 0) return "GB";

  return "MB";
}

export function parseTrafficInput(
  value: string,
  unit: TrafficUnit,
): number | null {
  const amount = Number(value);
  const mib = amount * TRAFFIC_UNIT_MIB[unit];

  if (
    !value.trim() ||
    !Number.isFinite(amount) ||
    amount <= 0 ||
    !Number.isSafeInteger(mib)
  ) {
    return null;
  }

  // Backend stores bytes as int64. Keep the converted value within that range.
  if (mib > 8_796_093_022_207) return null;

  return mib;
}
