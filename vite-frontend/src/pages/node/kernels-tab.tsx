import type {
  KernelApiItem,
  KernelCapabilitiesApiData,
  NodeApiItem,
} from "@/api/types";

import { useCallback, useEffect, useState, useRef } from "react";

import { useVisiblePolling } from "@/hooks/use-visible-polling";
import { getKernelCapabilities, getKernelList, getNodeList } from "@/api";
import { PageLoadingState } from "@/components/page-state";
import { type InstallRequest, KernelPanel } from "@/pages/inbound/kernel-panel";

/** The kernels installed on each node, under 节点 → 内核. */
export function KernelsTab({
  installRequest,
  onInstallRequestHandled,
}: {
  installRequest: InstallRequest | null;
  onInstallRequestHandled: () => void;
}) {
  const [loading, setLoading] = useState(true);
  const [caps, setCaps] = useState<KernelCapabilitiesApiData | null>(null);
  const [nodes, setNodes] = useState<NodeApiItem[]>([]);
  const [kernels, setKernels] = useState<KernelApiItem[]>([]);

  const loadPending = useRef(0);

  const load = useCallback(async (options?: { silent?: boolean }) => {
    const silent = options?.silent ?? false;

    if (silent && loadPending.current > 0) return;
    loadPending.current += 1;
    try {
      const [capsRes, nodeRes, kernelRes] = await Promise.all([
        getKernelCapabilities().catch(() => null),
        getNodeList().catch(() => null),
        getKernelList().catch(() => null),
      ]);

      if (capsRes?.code === 0) setCaps(capsRes.data);
      if (nodeRes?.code === 0) setNodes(nodeRes.data || []);
      if (kernelRes?.code === 0) setKernels(kernelRes.data || []);
    } finally {
      loadPending.current -= 1;
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    void load();
  }, [load]);
  useVisiblePolling(
    useCallback(() => load({ silent: true }), [load]),
    30_000,
  );

  if (loading) return <PageLoadingState message="正在加载..." />;

  return (
    <KernelPanel
      caps={caps}
      installRequest={installRequest}
      kernels={kernels}
      nodes={nodes}
      onChanged={load}
      onInstallRequestHandled={onInstallRequestHandled}
    />
  );
}
