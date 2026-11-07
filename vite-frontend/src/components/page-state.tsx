import { useEffect, useState } from "react";

import { Button } from "@/shadcn-bridge/heroui/button";
import { Spinner } from "@/shadcn-bridge/heroui/spinner";

// After this long a load is probably stuck (requests time out after 30s).
const SLOW_LOAD_MS = 10_000;

interface BaseStateProps {
  message: string;
  className?: string;
}

export const PageLoadingState = ({
  message,
  className = "h-64",
}: BaseStateProps) => {
  const [slow, setSlow] = useState(false);

  useEffect(() => {
    const timer = window.setTimeout(() => setSlow(true), SLOW_LOAD_MS);

    return () => window.clearTimeout(timer);
  }, []);

  return (
    <div
      className={`flex flex-col items-center justify-center gap-3 ${className}`}
    >
      <div className="flex items-center gap-3">
        <Spinner size="sm" />
        <span className="text-default-600">{message}</span>
      </div>
      {slow && (
        <div className="flex flex-col items-center gap-2 text-center">
          <span className="text-xs text-default-500">
            加载时间比平时长，可能是网络不稳定或面板没有响应
          </span>
          <Button
            size="sm"
            variant="flat"
            onPress={() => window.location.reload()}
          >
            重新加载
          </Button>
        </div>
      )}
    </div>
  );
};

export const PageEmptyState = ({
  message,
  className = "h-48",
}: BaseStateProps) => {
  return (
    <div className={`flex items-center justify-center ${className}`}>
      <span className="text-default-500">{message}</span>
    </div>
  );
};

export const PageErrorState = ({
  message,
  className = "h-48",
}: BaseStateProps) => {
  return (
    <div className={`flex items-center justify-center ${className}`}>
      <span className="text-danger">{message}</span>
    </div>
  );
};
