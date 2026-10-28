import { useCallback, useLayoutEffect, useRef } from "react";

/** Only the newest request in the current view may publish its result. */
export function useLatestRequest(scope: string | number | null) {
  const generation = useRef(0);

  useLayoutEffect(() => {
    return () => {
      generation.current += 1;
    };
  }, [scope]);

  return useCallback(() => {
    const request = ++generation.current;

    return () => request === generation.current;
  }, []);
}
