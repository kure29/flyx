/**
 * A renewal (payment) cycle: "month", "quarter", "year", or "<n>m" / "<n>y"
 * (1-120 months); "" is none.
 */
export type NodeRenewalCycle = string;

export interface NodeRenewalSnapshot {
  cycle: NodeRenewalCycle;
  anchorTime?: number;
  nextDueTime?: number;
  diffDays?: number;
  state: "unset" | "expired" | "dueSoon" | "scheduled";
  label: string;
}

const addMonths = (timestamp: number, months: number): number => {
  const date = new Date(timestamp);
  const next = new Date(date);

  next.setMonth(next.getMonth() + months);

  return next.getTime();
};

/** Months in a cycle, 0 when it is not one. */
export const cycleToMonths = (cycle?: NodeRenewalCycle): number => {
  switch (cycle) {
    case "month":
      return 1;
    case "quarter":
      return 3;
    case "year":
      return 12;
  }
  const m = (cycle ?? "").match(/^(\d{1,3})([my])$/);

  if (!m) return 0;
  const months = Number(m[1]) * (m[2] === "y" ? 12 : 1);

  return months >= 1 && months <= 120 ? months : 0;
};

/** The cycle stored for n months (or years), as the backend keeps it. */
export const cycleFrom = (count: number, unit: "m" | "y"): NodeRenewalCycle => {
  const months = Math.round(count) * (unit === "y" ? 12 : 1);

  if (!(months >= 1 && months <= 120)) return "";
  if (months === 1) return "month";
  if (months === 3) return "quarter";
  if (months === 12) return "year";

  return months % 12 === 0 ? `${months / 12}y` : `${months}m`;
};

/** A cycle as count and unit for the form: "year" → 1 年, "6m" → 6 月. */
export const cycleParts = (
  cycle?: NodeRenewalCycle,
): { count: number; unit: "m" | "y" } | null => {
  const months = cycleToMonths(cycle);

  if (!months) return null;

  return months % 12 === 0
    ? { count: months / 12, unit: "y" }
    : { count: months, unit: "m" };
};

export const getNodeRenewalCycleLabel = (cycle?: string): string => {
  const p = cycleParts(cycle);

  if (!p) return "未设置";
  if (p.count === 1) return p.unit === "y" ? "年付" : "月付";
  if (p.unit === "m" && p.count === 3) return "季付";

  return `每 ${p.count} ${p.unit === "y" ? "年" : "个月"}`;
};

export const getNodeRenewalSnapshot = (
  anchorTime?: number,
  cycle?: string,
  warningDays = 7,
): NodeRenewalSnapshot => {
  const normalizedCycle = cycleToMonths(cycle) ? (cycle ?? "") : "";

  if (!anchorTime || anchorTime <= 0 || !normalizedCycle) {
    return {
      cycle: normalizedCycle,
      anchorTime: anchorTime && anchorTime > 0 ? anchorTime : undefined,
      state: "unset",
      label: "未设置续费周期",
    };
  }

  const intervalMonths = cycleToMonths(normalizedCycle);
  let nextDueTime = anchorTime;

  while (nextDueTime < Date.now()) {
    const advanced = addMonths(nextDueTime, intervalMonths);

    if (advanced === nextDueTime) {
      break;
    }
    nextDueTime = advanced;
  }

  const diffDays = Math.ceil(
    (nextDueTime - Date.now()) / (1000 * 60 * 60 * 24),
  );

  if (diffDays <= 0) {
    return {
      cycle: normalizedCycle,
      anchorTime,
      nextDueTime,
      diffDays,
      state: "expired",
      label: "今天到期",
    };
  }

  if (diffDays <= warningDays) {
    return {
      cycle: normalizedCycle,
      anchorTime,
      nextDueTime,
      diffDays,
      state: "dueSoon",
      label: diffDays === 1 ? "明天续费" : `${diffDays}天后续费`,
    };
  }

  return {
    cycle: normalizedCycle,
    anchorTime,
    nextDueTime,
    diffDays,
    state: "scheduled",
    label: `${diffDays}天后续费`,
  };
};

export const formatNodeRenewalTime = (timestamp?: number): string => {
  if (!timestamp || timestamp <= 0) {
    return "未设置";
  }

  return new Date(timestamp).toLocaleString();
};
