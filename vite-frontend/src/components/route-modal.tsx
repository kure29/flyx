import type { NodeRoute, NodeRouteHop } from "@/api/types";

import { useEffect, useState } from "react";
import toast from "react-hot-toast";
import { Loader2, RefreshCw } from "lucide-react";

import { useLatestRequest } from "@/hooks/use-latest-request";
import { getNodeRoute, probeNodeRoute } from "@/api";
import { Button } from "@/shadcn-bridge/heroui/button";
import {
  Modal,
  ModalBody,
  ModalContent,
  ModalFooter,
  ModalHeader,
} from "@/shadcn-bridge/heroui/modal";

// Short tag and color per return route, premium lines warmer.
const ROUTES: Record<string, [string, string]> = {
  "电信 CN2 GIA": [
    "CN2 GIA",
    "bg-amber-500/15 text-amber-700 dark:text-amber-400",
  ],
  "电信 CN2 GT": [
    "CN2 GT",
    "bg-orange-500/15 text-orange-700 dark:text-orange-400",
  ],
  "电信 163": ["163", "bg-default-200/70 text-default-600"],
  "联通 9929": [
    "9929",
    "bg-violet-500/15 text-violet-700 dark:text-violet-400",
  ],
  "联通 4837": ["4837", "bg-sky-500/15 text-sky-700 dark:text-sky-400"],
  "移动 CMIN2": [
    "CMIN2",
    "bg-emerald-500/15 text-emerald-700 dark:text-emerald-400",
  ],
  "移动 CMI": ["CMI", "bg-teal-500/15 text-teal-700 dark:text-teal-400"],
};

const NET_CLASS: Record<string, string> = {
  CN2: "text-amber-600",
  "163": "text-default-500",
  "4837": "text-sky-600",
  "9929": "text-violet-600",
  CMIN2: "text-emerald-600",
  CMI: "text-teal-600",
  CT: "text-default-500",
};

// IPv6 hops in AS4134 may be the 163 backbone or provincial ChinaNet.
const NET_LABEL: Record<string, string> = { CT: "电信" };

/** A return route as a small colored tag; short form for tight spaces. */
export function RouteTag({
  route,
  short,
}: {
  route?: string;
  short?: boolean;
}) {
  if (!route || route === "未识别") return null;
  const [tag, cls] = ROUTES[route] ?? [route, ROUTES["电信 163"][1]];

  return (
    <span
      className={`inline-flex shrink-0 items-center whitespace-nowrap rounded px-1 font-sans text-[10px] font-medium leading-4 ${cls}`}
      title={`回程 ${route}`}
    >
      {short ? tag : route}
    </span>
  );
}

function ago(ts: number) {
  const m = Math.max(0, Math.round((Date.now() - ts) / 60000));

  if (m < 1) return "刚刚";
  if (m < 60) return `${m} 分钟前`;
  if (m < 1440) return `${Math.floor(m / 60)} 小时前`;

  return `${Math.floor(m / 1440)} 天前`;
}

function HopRow({ hop }: { hop: NodeRouteHop }) {
  return (
    <div className="grid grid-cols-[2rem_1fr_auto] items-center gap-2 py-0.5 font-mono text-xs">
      <span className="text-default-400">{hop.ttl}</span>
      <span className={`break-all ${hop.ip ? "" : "text-default-300"}`}>
        {hop.ip || "*"}
        {hop.net && (
          <span className={`ml-2 font-sans ${NET_CLASS[hop.net] ?? ""}`}>
            {NET_LABEL[hop.net] ?? hop.net}
          </span>
        )}
      </span>
      <span className="text-default-500">
        {hop.rttMs ? `${hop.rttMs.toFixed(1)} ms` : ""}
      </span>
    </div>
  );
}

/** A node's return routes to each reference point, hop by hop. */
export function RouteModal({
  nodeId,
  nodeName,
  onClose,
}: {
  nodeId: number | null;
  nodeName?: string;
  onClose: () => void;
}) {
  const [route, setRoute] = useState<NodeRoute | null>(null);
  const [loading, setLoading] = useState(false);
  const [probing, setProbing] = useState(false);
  const [open, setOpen] = useState<string | null>(null);

  const beginRequest = useLatestRequest(nodeId);

  useEffect(() => {
    setProbing(false);
    if (nodeId === null) return;
    const isCurrent = beginRequest();

    setRoute(null);
    setOpen(null);
    setLoading(true);
    getNodeRoute(nodeId)
      .then((res) => {
        if (isCurrent()) setRoute(res.code === 0 ? (res.data ?? null) : null);
      })
      .catch(() => {
        if (isCurrent()) setRoute(null);
      })
      .finally(() => {
        if (isCurrent()) setLoading(false);
      });
  }, [nodeId, beginRequest]);

  const probe = async () => {
    if (nodeId === null) return;
    const isCurrent = beginRequest();

    setLoading(false);
    setProbing(true);
    try {
      const res = await probeNodeRoute(nodeId);

      if (!isCurrent()) return;

      if (res.code !== 0) {
        toast.error(res.msg || "检测失败");

        return;
      }
      setRoute(res.data ?? null);
    } catch {
      if (isCurrent()) toast.error("检测失败");
    } finally {
      if (isCurrent()) setProbing(false);
    }
  };

  return (
    <Modal
      backdrop="blur"
      classNames={{
        base: "!w-[calc(100%-32px)] !mx-auto sm:!w-full rounded-2xl overflow-hidden",
      }}
      isOpen={nodeId !== null}
      placement="center"
      scrollBehavior="inside"
      size="lg"
      onOpenChange={(v) => !v && onClose()}
    >
      <ModalContent>
        <ModalHeader closable className="flex flex-col gap-0.5">
          <h2 className="text-lg font-bold">回程路由</h2>
          <p className="text-xs font-normal text-default-500">
            {nodeName ? `${nodeName} → 各测试点` : "服务器 → 各测试点"}
            {route ? ` · ${ago(route.checkedAt)}检测` : ""}
          </p>
        </ModalHeader>
        <ModalBody className="space-y-2">
          {loading ? (
            <div className="flex justify-center py-6">
              <Loader2 className="h-5 w-5 animate-spin text-default-400" />
            </div>
          ) : !route || route.points.length === 0 ? (
            <p className="py-6 text-center text-sm text-default-500">
              还没有检测结果，点下方按钮检测
            </p>
          ) : (
            route.points.map((p) => {
              // A dual-stack node traces each point over both families.
              const legs = p.ipv6
                ? [
                    { key: `${p.name}/v4`, label: "IPv4", leg: p },
                    { key: `${p.name}/v6`, label: "IPv6", leg: p.ipv6 },
                  ]
                : [{ key: p.name, label: p.family, leg: p }];

              return (
                <div key={p.name} className="rounded-lg border border-divider">
                  {p.ipv6 && (
                    <div className="px-3 pt-2 text-sm font-medium">
                      {p.name}
                    </div>
                  )}
                  {legs.map(({ key, label, leg }) => (
                    <div key={key}>
                      <button
                        className="flex w-full items-center gap-2 px-3 py-2 text-left text-sm"
                        type="button"
                        onClick={() => setOpen(open === key ? null : key)}
                      >
                        {!p.ipv6 && (
                          <span className="shrink-0 font-medium">{p.name}</span>
                        )}
                        {label && (
                          <span className="w-9 shrink-0 rounded bg-default-100 px-1 text-center text-[10px] text-default-500">
                            {label}
                          </span>
                        )}
                        {leg.error ? (
                          <span className="min-w-0 text-xs text-rose-600">
                            {leg.error}
                          </span>
                        ) : leg.route && leg.route !== "未识别" ? (
                          <RouteTag route={leg.route} />
                        ) : (
                          <span className="text-xs text-default-400">
                            未识别线路
                          </span>
                        )}
                        {!leg.error && (
                          <span className="ml-auto text-xs text-default-400">
                            {leg.hops?.length ?? 0} 跳{" "}
                            {open === key ? "▴" : "▾"}
                          </span>
                        )}
                      </button>
                      {open === key && leg.hops && (
                        <div className="border-t border-divider px-3 py-2">
                          {leg.hops.map((h) => (
                            <HopRow key={h.ttl} hop={h} />
                          ))}
                          {!leg.reached && (
                            <p className="pt-1 text-xs text-default-400">
                              目标没有回应，只显示到最后一个有回应的路由器
                            </p>
                          )}
                        </div>
                      )}
                    </div>
                  ))}
                </div>
              );
            })
          )}
          <p className="text-xs text-default-400">
            按经过的骨干网识别：59.43 段为 CN2（不经过 202.97 即 GIA），202.97
            为 163，219.158 为联通 4837，218.105 / 210.51 为联通
            9929，223.118–223.121 为移动 CMI，其中归属 AS58807 的为 CMIN2；IPv6
            回程按每一跳的归属网络（AS4809 为 CN2，AS4134 为电信
            163）识别。每天自动检测一次。
          </p>
        </ModalBody>
        <ModalFooter>
          <Button
            color="primary"
            isDisabled={probing || nodeId === null}
            startContent={
              probing ? (
                <Loader2 className="h-4 w-4 animate-spin" />
              ) : (
                <RefreshCw className="h-4 w-4" />
              )
            }
            onPress={probe}
          >
            {probing ? "检测中…" : "重新检测"}
          </Button>
        </ModalFooter>
      </ModalContent>
    </Modal>
  );
}
