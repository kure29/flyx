import type { InboundApiItem, InboundRelayApiItem } from "@/api/types";

import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import toast from "react-hot-toast";

import { getStoredValue, setStoredValue } from "@/utils/identity-storage";
import {
  markPageCached,
  pageCached,
  useCachedState,
} from "@/hooks/use-cached-state";
import { useVisiblePolling } from "@/hooks/use-visible-polling";
import { latestProbe } from "@/utils/latest-probe";
import { getInboundList, getInboundRelays, probeInboundRelays } from "@/api";
import { AnimatedPage } from "@/components/animated-page";
import { PageLoadingState } from "@/components/page-state";
import {
  EmptyHint,
  ListRow,
  PageHeader,
  PageTabs,
  PageToolbar,
  ToolbarButton,
} from "@/components/page-kit";
import { CARD_GRID, InfoCard } from "@/components/info-card";
import { RelayLatencyCell } from "@/components/relay-latency";
import { useViewMode } from "@/components/view-mode";
import ForwardPage from "@/pages/forward";
import { RelayModal } from "@/pages/inbound/relay-modal";
import TunnelPage from "@/pages/tunnel";
import { TunnelMonitorView } from "@/components/lazy-charts";
import { Button } from "@/shadcn-bridge/heroui/button";
import { Chip } from "@/shadcn-bridge/heroui/chip";

type RelayTab = "lines" | "tunnels" | "rules" | "quality";

const TAB_KEY = "flyx_relay_tab";

function readTab(): RelayTab {
  try {
    const v = getStoredValue(localStorage, TAB_KEY);

    if (v === "lines" || v === "tunnels" || v === "rules" || v === "quality")
      return v;
  } catch {
    // Remembering the tab is only a convenience.
  }

  return "lines";
}

const TABS: Array<{ key: RelayTab; label: string }> = [
  { key: "lines", label: "线路" },
  { key: "tunnels", label: "隧道" },
  { key: "rules", label: "规则" },
  { key: "quality", label: "质量" },
];

/**
 * A line's own part of its name: "腾讯成都-YT → TW | Seednet FH" is shown
 * under that landing, so only "腾讯成都-YT" is new.
 */
function relayLabel(l: InboundRelayApiItem): string {
  const full = l.name || l.tunnelName || "中转";
  const cut = full.lastIndexOf(" → ");

  return cut > 0 ? full.slice(0, cut) : full;
}

/** A landing's relay lines: one row each, its relay and its latency. */
function RelayLines({
  lines,
  renderLatency,
}: {
  lines: InboundRelayApiItem[];
  renderLatency: (l: InboundRelayApiItem) => React.ReactNode;
}) {
  return (
    <div className="divide-y divide-divider/70 rounded-xl bg-default-100/60">
      {lines.map((l) => {
        const entry = l.entries[0];
        const ok = l.forwardExists && l.forwardStatus === 1;

        return (
          <div
            key={l.id}
            className="flex min-h-9 min-w-0 items-center gap-2 px-3 py-1.5"
          >
            <span
              className={`h-1.5 w-1.5 shrink-0 rounded-full ${ok ? "bg-success" : "bg-warning"}`}
              title={ok ? "转发正常" : "转发已暂停或被删除"}
            />
            <span
              className="min-w-0 flex-1 truncate text-sm text-default-700"
              title={l.name || l.tunnelName}
            >
              {relayLabel(l)}
            </span>
            <span className="shrink-0 text-xs">
              {renderLatency(l) ?? (
                <span className="font-mono text-default-400">
                  {entry ? `${l.shareHost || entry.Host}:${entry.Port}` : "-"}
                </span>
              )}
            </span>
          </div>
        );
      })}
    </div>
  );
}

/** Tunnel latency, throughput and errors over time (was 监控 → 隧道). */
function QualityView() {
  const [viewMode, setViewMode] = useViewMode("monitor");

  return (
    <div className="space-y-3">
      <PageToolbar view={{ value: viewMode, onChange: setViewMode }} />
      <TunnelMonitorView card={viewMode === "card"} />
    </div>
  );
}

const LINES_PAGE = "relay-lines";

/** Relay lines grouped by the landing they lead to. */
function LinesView() {
  // A revisit shows the last data at once and reloads in the background.
  const [loading, setLoading] = useState(() => !pageCached(LINES_PAGE));
  const [inbounds, setInbounds] = useCachedState<InboundApiItem[]>(
    LINES_PAGE,
    "inbounds",
    [],
  );
  const [relays, setRelays] = useCachedState<InboundRelayApiItem[]>(
    LINES_PAGE,
    "relays",
    [],
  );
  const [editing, setEditing] = useState<InboundApiItem | null>(null);
  const [onlyWithRelays, setOnlyWithRelays] = useState(false);
  const [query, setQuery] = useState("");
  const [viewMode, setViewMode] = useViewMode("relay");
  const card = viewMode === "card";
  const [probingIds, setProbingIds] = useState<Set<number>>(new Set());
  const loadingRef = useRef(false);
  // A reload asked for during a silent poll (after an edit) runs after it.
  const reloadRef = useRef(false);

  const load = useCallback(async (silent = false): Promise<void> => {
    if (loadingRef.current) {
      if (!silent) reloadRef.current = true;

      return;
    }
    loadingRef.current = true;
    try {
      const [inRes, relRes] = await Promise.all([
        getInboundList(),
        getInboundRelays(0),
      ]);

      if (inRes.code === 0) setInbounds(inRes.data ?? []);
      if (relRes.code === 0) {
        setRelays((prev) => {
          const previous = new Map(prev.map((r) => [r.id, r]));

          return (relRes.data ?? []).map((r) => ({
            ...r,
            latency: latestProbe(r.latency, previous.get(r.id)?.latency),
          }));
        });
      }
      if (inRes.code === 0 && relRes.code === 0) markPageCached(LINES_PAGE);
      if (!silent && (inRes.code !== 0 || relRes.code !== 0))
        toast.error(inRes.msg || relRes.msg || "加载失败");
    } catch {
      if (!silent) toast.error("加载失败");
    } finally {
      loadingRef.current = false;
      setLoading(false);
      if (reloadRef.current) {
        reloadRef.current = false;
        void load(true);
      }
    }
  }, []);

  useEffect(() => {
    load();
  }, [load]);
  useVisiblePolling(useCallback(() => load(true), [load]));

  /** Measures the given lines (all when empty) and merges the results. */
  const probe = useCallback(
    async (ids: number[]) => {
      const busy = ids.length > 0 ? ids : relays.map((r) => r.id);

      setProbingIds((prev) => new Set([...prev, ...busy]));
      try {
        const res = await probeInboundRelays(ids);

        if (res.code !== 0) {
          toast.error(res.msg || "检测失败");

          return;
        }
        const data = res.data ?? {};

        setRelays((prev) =>
          prev.map((r) =>
            data[String(r.id)]
              ? { ...r, latency: latestProbe(data[String(r.id)], r.latency) }
              : r,
          ),
        );
      } catch {
        toast.error("检测失败");
      } finally {
        setProbingIds((prev) => {
          const next = new Set(prev);

          busy.forEach((id) => next.delete(id));

          return next;
        });
      }
    },
    [relays],
  );

  const latencyOf = (l: InboundRelayApiItem) =>
    l.forwardExists ? (
      <RelayLatencyCell
        latency={l.latency}
        probing={probingIds.has(l.id)}
        onProbe={() => probe([l.id])}
      />
    ) : null;

  const byInbound = useMemo(() => {
    const m = new Map<number, InboundRelayApiItem[]>();

    for (const r of relays) {
      m.set(r.inboundId, [...(m.get(r.inboundId) ?? []), r]);
    }

    return m;
  }, [relays]);

  if (loading) return <PageLoadingState message="正在加载..." />;

  const q = query.trim().toLowerCase();
  const shown = inbounds.filter(
    (i) =>
      (!onlyWithRelays || (byInbound.get(i.id)?.length ?? 0) > 0) &&
      (!q || i.name.toLowerCase().includes(q)),
  );

  return (
    <div className="space-y-3">
      <PageToolbar
        placeholder="搜索节点名称"
        search={query}
        view={{ value: viewMode, onChange: setViewMode }}
        onSearch={setQuery}
      >
        <ToolbarButton
          active={onlyWithRelays}
          onPress={() => setOnlyWithRelays((v) => !v)}
        >
          只看有中转的
        </ToolbarButton>
        {relays.length > 0 && (
          <ToolbarButton onPress={() => probe([])}>
            {probingIds.size > 0 ? "测延迟中…" : "测延迟"}
          </ToolbarButton>
        )}
      </PageToolbar>

      {shown.length === 0 ? (
        <EmptyHint
          text={
            inbounds.length === 0
              ? "还没有节点。先在“节点”里搭建或导入，再回来对接中转"
              : query
                ? "没有匹配的节点"
                : "还没有中转线路"
          }
        />
      ) : (
        <div className={card ? CARD_GRID : "grid grid-cols-1 gap-2"}>
          {shown.map((i) => {
            const lines = byInbound.get(i.id) ?? [];
            const broken = lines.some(
              (l) => !(l.forwardExists && l.forwardStatus === 1),
            );
            const landing = i.external ? "导入的节点" : i.nodeName;
            const summary =
              lines.length === 0
                ? `${landing} · 无中转，点击添加`
                : `${landing} · ${lines.length} 条中转${broken ? " · 有暂停" : ""}`;
            const lineList = lines.length > 0 && (
              <RelayLines lines={lines} renderLatency={latencyOf} />
            );

            if (card) {
              return (
                <InfoCard
                  key={i.id}
                  actions={
                    lines.length === 0
                      ? [
                          {
                            key: "add",
                            label: "添加中转",
                            color: "primary",
                            onPress: () => setEditing(i),
                          },
                        ]
                      : undefined
                  }
                  badges={
                    i.relayOnly && (
                      <Chip size="sm" variant="flat">
                        仅中转
                      </Chip>
                    )
                  }
                  headerRight={
                    lines.length > 0 && (
                      <Button
                        className="h-7 min-w-0 px-2.5 text-xs"
                        size="sm"
                        variant="light"
                        onPress={() => setEditing(i)}
                      >
                        管理
                      </Button>
                    )
                  }
                  rows={[]}
                  status={
                    lines.length === 0
                      ? "无中转"
                      : broken
                        ? "有暂停"
                        : `${lines.length} 条中转`
                  }
                  title={i.name}
                  tone={
                    lines.length === 0
                      ? "default"
                      : broken
                        ? "warning"
                        : "success"
                  }
                  onPress={() => setEditing(i)}
                >
                  <div className="space-y-2">
                    <p className="truncate text-xs text-default-500">
                      节点：{landing}
                    </p>
                    {lineList}
                  </div>
                </InfoCard>
              );
            }

            return (
              <ListRow
                key={i.id}
                badges={
                  i.relayOnly && (
                    <Chip className="shrink-0" size="sm" variant="flat">
                      仅中转
                    </Chip>
                  )
                }
                footer={
                  lineList && <div className="mt-2 lg:hidden">{lineList}</div>
                }
                meta={summary}
                right={
                  lineList && (
                    // Wide screens: the lines sit beside the landing.
                    <div className="hidden w-[30rem] lg:block xl:w-[38rem]">
                      {lineList}
                    </div>
                  )
                }
                status={lines.length === 0 ? "off" : broken ? "warn" : "ok"}
                statusTitle={
                  lines.length === 0
                    ? "无中转"
                    : broken
                      ? "有中转转发已暂停或被删除"
                      : "中转正常"
                }
                title={i.name}
                onPress={() => setEditing(i)}
              />
            );
          })}
        </div>
      )}

      <RelayModal
        item={editing}
        onChanged={() => load()}
        onClose={() => setEditing(null)}
      />
    </div>
  );
}

/** 中转: relay lines per landing, plus the tunnel and rule pages. */
export default function RelayPage() {
  const [tab, setTabState] = useState<RelayTab>(readTab);
  const setTab = (t: RelayTab) => {
    setTabState(t);
    try {
      setStoredValue(localStorage, TAB_KEY, t);
    } catch {
      // ignore
    }
  };

  return (
    <AnimatedPage className="px-3 py-6 lg:px-6">
      <PageHeader
        description="让客户端先连中转再到节点：线路按节点管理，隧道和规则是底层的转发配置"
        title="中转"
      />
      <PageTabs tabs={TABS} value={tab} onChange={setTab} />
      {tab === "lines" ? (
        <LinesView />
      ) : tab === "tunnels" ? (
        <TunnelPage embedded />
      ) : tab === "quality" ? (
        <QualityView />
      ) : (
        <ForwardPage embedded />
      )}
    </AnimatedPage>
  );
}
