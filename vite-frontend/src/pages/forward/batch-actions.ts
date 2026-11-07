import type { BatchOperationFailure, BatchOperationResult } from "@/api/types";

import {
  batchChangeTunnel,
  batchDeleteForwards,
  batchPauseForwards,
  batchRedeployForwards,
  batchResumeForwards,
} from "@/api";
import {
  buildBatchFailureMessage,
  extractBatchFailures,
  extractApiErrorMessage,
} from "@/api/error-message";

export interface ForwardBatchActionOutcome {
  toastVariant: "success" | "error";
  toastMessage: string;
  shouldRefresh: boolean;
  resultTitle?: string;
  resultSummary?: string;
  failureDetails?: BatchOperationFailure[];
  progressPercent?: number;
  progressLabel?: string;
  closeDeleteModal?: boolean;
  closeChangeTunnelModal?: boolean;
  resetTargetTunnel?: boolean;
}

const normalizeBatchResult = (value: unknown): BatchOperationResult => {
  const raw = (value ?? {}) as Partial<BatchOperationResult>;

  return {
    successCount: Number(raw.successCount ?? 0),
    failCount: Number(raw.failCount ?? 0),
    failures: extractBatchFailures(raw),
  };
};

const buildBatchToast = (
  result: BatchOperationResult,
  successText: string,
  resultTitle: string,
): Pick<
  ForwardBatchActionOutcome,
  | "toastVariant"
  | "toastMessage"
  | "resultTitle"
  | "resultSummary"
  | "failureDetails"
> => {
  if (result.failCount === 0) {
    return {
      toastVariant: "success",
      toastMessage: successText,
      resultTitle,
      resultSummary: successText,
      failureDetails: [],
    };
  }

  return {
    toastVariant: "error",
    toastMessage: buildBatchFailureMessage(
      result,
      `成功 ${result.successCount} 项，失败 ${result.failCount} 项`,
    ),
    resultTitle,
    resultSummary: `成功 ${result.successCount} 项，失败 ${result.failCount} 项`,
    failureDetails: result.failures || [],
  };
};

export const executeForwardBatchDelete = async (
  ids: number[],
): Promise<ForwardBatchActionOutcome> => {
  try {
    const response = await batchDeleteForwards(ids);

    if (response.code !== 0) {
      return {
        toastVariant: "error",
        toastMessage: response.msg || "删除失败",
        shouldRefresh: false,
      };
    }

    const summary = normalizeBatchResult(response.data);

    return {
      ...buildBatchToast(
        summary,
        `成功删除 ${summary.successCount} 项`,
        "批量删除结果",
      ),
      shouldRefresh: true,
      progressPercent: 100,
      progressLabel: `删除完成：成功 ${summary.successCount} 项`,
      closeDeleteModal: true,
    };
  } catch (error) {
    return {
      toastVariant: "error",
      toastMessage: extractApiErrorMessage(error, "删除失败"),
      shouldRefresh: false,
    };
  }
};

export const executeForwardBatchToggleService = async (
  ids: number[],
  enable: boolean,
): Promise<ForwardBatchActionOutcome> => {
  const fallback = enable ? "启用失败" : "停用失败";

  try {
    const response = enable
      ? await batchResumeForwards(ids)
      : await batchPauseForwards(ids);

    if (response.code !== 0) {
      return {
        toastVariant: "error",
        toastMessage: response.msg || fallback,
        shouldRefresh: false,
      };
    }

    const summary = normalizeBatchResult(response.data);

    return {
      ...buildBatchToast(
        summary,
        enable
          ? `成功启用 ${summary.successCount} 项`
          : `成功停用 ${summary.successCount} 项`,
        enable ? "批量启用结果" : "批量停用结果",
      ),
      shouldRefresh: true,
      progressPercent: 100,
      progressLabel: `${enable ? "启用" : "停用"}完成：成功 ${summary.successCount} 项`,
    };
  } catch (error) {
    return {
      toastVariant: "error",
      toastMessage: extractApiErrorMessage(error, fallback),
      shouldRefresh: false,
    };
  }
};

export const executeForwardBatchRedeploy = async (
  ids: number[],
): Promise<ForwardBatchActionOutcome> => {
  try {
    const response = await batchRedeployForwards(ids);

    if (response.code !== 0) {
      return {
        toastVariant: "error",
        toastMessage: response.msg || "下发失败",
        shouldRefresh: false,
      };
    }

    const summary = normalizeBatchResult(response.data);

    return {
      ...buildBatchToast(
        summary,
        `成功重新下发 ${summary.successCount} 项`,
        "批量下发结果",
      ),
      shouldRefresh: true,
      progressPercent: 100,
      progressLabel: `重新下发完成：成功 ${summary.successCount} 项`,
    };
  } catch (error) {
    return {
      toastVariant: "error",
      toastMessage: extractApiErrorMessage(error, "下发失败"),
      shouldRefresh: false,
    };
  }
};

export const executeForwardBatchChangeTunnel = async (
  ids: number[],
  targetTunnelId: number,
): Promise<ForwardBatchActionOutcome> => {
  try {
    const response = await batchChangeTunnel({
      forwardIds: ids,
      targetTunnelId,
    });

    if (response.code !== 0) {
      return {
        toastVariant: "error",
        toastMessage: response.msg || "隧道失败",
        shouldRefresh: false,
      };
    }

    const summary = normalizeBatchResult(response.data);

    return {
      ...buildBatchToast(
        summary,
        `成功换隧道 ${summary.successCount} 项`,
        "批量换隧道结果",
      ),
      shouldRefresh: true,
      progressPercent: 100,
      progressLabel: `批量换隧道完成：成功 ${summary.successCount} 项`,
      closeChangeTunnelModal: true,
      resetTargetTunnel: true,
    };
  } catch (error) {
    return {
      toastVariant: "error",
      toastMessage: extractApiErrorMessage(error, "隧道失败"),
      shouldRefresh: false,
    };
  }
};
