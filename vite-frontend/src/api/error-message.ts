import type { BatchOperationFailure } from "@/api/types";

import axios from "axios";

interface ErrorPayload {
  msg?: string;
  message?: string;
}

interface BatchFailurePayload {
  id?: number;
  name?: string;
  reason?: string;
  msg?: string;
  message?: string;
}

interface BatchResultPayload {
  failures?: unknown[];
}

const MAX_BATCH_FAILURES_IN_TOAST = 3;

/** The request got no answer: the connection dropped or never came up. */
export const NETWORK_LOST = "网络连接中断";

export const isUnauthorizedError = (error: unknown): boolean => {
  return axios.isAxiosError(error) && error.response?.status === 401;
};

export const extractApiErrorMessage = (
  error: unknown,
  fallback = "网络请求失败",
): string => {
  if (axios.isAxiosError(error)) {
    if (!error.response && error.code === "ERR_NETWORK") {
      return NETWORK_LOST;
    }
    const payload = error.response?.data as ErrorPayload | undefined;

    return payload?.msg || payload?.message || error.message || fallback;
  }

  if (error instanceof Error && error.message) {
    return error.message;
  }

  return fallback;
};

const normalizeBatchFailure = (
  failure: unknown,
): BatchOperationFailure | null => {
  if (typeof failure === "string") {
    const reason = failure.trim();

    return reason ? { reason } : null;
  }

  const payload = (failure ?? {}) as BatchFailurePayload;
  const id =
    typeof payload.id === "number" && Number.isFinite(payload.id)
      ? payload.id
      : undefined;
  const name = typeof payload.name === "string" ? payload.name.trim() : "";
  const reasonSource = [payload.reason, payload.msg, payload.message].find(
    (item) => typeof item === "string" && item.trim() !== "",
  );
  const reason = typeof reasonSource === "string" ? reasonSource.trim() : "";

  if (!name && !reason && id === undefined) {
    return null;
  }

  return {
    ...(id !== undefined ? { id } : {}),
    ...(name ? { name } : {}),
    ...(reason ? { reason } : {}),
  };
};

const normalizeBatchFailureReason = (
  failure: BatchOperationFailure,
): string => {
  const name = typeof failure.name === "string" ? failure.name.trim() : "";
  const reason =
    typeof failure.reason === "string" ? failure.reason.trim() : "";

  if (name && reason) {
    return `${name}: ${reason}`;
  }

  if (reason) {
    if (typeof failure.id === "number" && Number.isFinite(failure.id)) {
      return `ID ${failure.id}: ${reason}`;
    }

    return reason;
  }

  if (name) {
    return name;
  }

  if (typeof failure.id === "number" && Number.isFinite(failure.id)) {
    return `ID ${failure.id} 下发失败`;
  }

  return "";
};

export const extractBatchFailures = (
  result: unknown,
): BatchOperationFailure[] => {
  const payload = (result ?? {}) as BatchResultPayload;

  return Array.isArray(payload.failures)
    ? payload.failures
        .map((item) => normalizeBatchFailure(item))
        .filter((item): item is BatchOperationFailure => item !== null)
    : [];
};

export const buildBatchFailureMessage = (
  result: unknown,
  fallbackSummary: string,
): string => {
  const failures = extractBatchFailures(result)
    .map((item) => normalizeBatchFailureReason(item))
    .filter((item) => item !== "");

  if (failures.length === 0) {
    return fallbackSummary;
  }

  const visibleFailures = failures.slice(0, MAX_BATCH_FAILURES_IN_TOAST);
  const hiddenCount = failures.length - visibleFailures.length;
  const hiddenSuffix = hiddenCount > 0 ? ` 等 ${failures.length} 项` : "";

  return `${fallbackSummary}：${visibleFailures.join("；")}${hiddenSuffix}`;
};
