import type { InboundApiItem, InboundRelayApiItem } from "@/api/types";

import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import toast from "react-hot-toast";
import { useLocation, useNavigate } from "react-router-dom";
import { ArrowRight, GitBranch, Layers, Server } from "lucide-react";

import { getStoredValue, setStoredValue } from "@/utils/identity-storage";
import {
  markPageCached,
  pageCached,
  useCachedState,
} from "@/hooks/use-cached-state";
import { useLocalStorageState } from "@/hooks/use-local-storage-state";
import { useVisiblePolling } from "@/hooks/use-visible-polling";
import { latestProbe } from "@/utils/latest-probe";
import { getInboundList, getInboundRelays, probeInboundRelays } from "@/api";
import { AnimatedPage } from "@/components/animated-page";
import { PageLoadingState } from "@/components/page-state";
import {
  EmptyHint,
  FilterPills,
  PageHeader,
  PageTabs,
  PageToolbar,
  ToolbarButton,
  ToolbarSelect,
  ViewModeSwitch,
} from "@/components/page-kit";
import { RelayLatencyCell } from "@/components/relay-latency";
import ForwardPage from "@/pages/forward";
import { RelayModal } from "@/pages/inbound/relay-modal";
import TunnelPage from "@/pages/tunnel";
import { TunnelMonitorView } from "@/components/lazy-charts";
import { useI18n } from "@/i18n";
import { Chip } from "@/shadcn-bridge/heroui/chip";

export type RelayTab = "lines" | "tunnels" | "rules" | "quality";

/** Route state for deep-linking (e.g. clicking "3 隧道" on a server card). */
export interface RelayPageState {
  tab?: RelayTab;
  query?: string;
}

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

type RelayLinesViewMode = "flow" | "byEntry" | "byLanding";
type RelayLinesFilter = "all" | "broken" | "unrelayed";
type RelayLinesSort = "default" | "latency_asc" | "name_asc";

/**
 * A line's own part of its name: "腾讯成都-YT → TW | Seednet FH" is shown
 * under that landing, so only "腾讯成都-YT" is new.
 */
function relayLabel(l: InboundRelayApiItem, fallback: string): string {
  const full = l.name || l.tunnelName || fallback;
  const cut = full.lastIndexOf(" → ");

  return cut > 0 ? full.slice(0, cut) : full;
}

function relayEntryKey(l: InboundRelayApiItem, fallback: string): string {
  return l.latency?.entry?.trim() || relayLabel(l, fallback);
}

function relayLatencyValue(l: InboundRelayApiItem): number | undefined {
  const lat = l.latency;

  if (!lat || lat.skipped || !lat.ok) return undefined;
  if (typeof lat.latencyMs === "number") return lat.latencyMs;
  if (typeof lat.pathMs === "number") return lat.pathMs;

  return undefined;
}

function relayEntryEndpoint(l: InboundRelayApiItem): string {
  const entry = l.entries[0];

  if (!entry) return l.shareHost || "-";

  return `${l.shareHost || entry.Host}:${entry.Port}`;
}

/** Tunnel latency, throughput and errors over time (was 监控 → 隧道). */
function QualityView() {
  return <TunnelMonitorView />;
}

const LINES_PAGE = "relay-lines";

interface EnrichedRelayLine {
  relay: InboundRelayApiItem;
  inbound: InboundApiItem;
  ok: boolean;
  entryTitle: string;
  landingServer: string;
}

/** Relay lines with 3-segment flow view, entry-grouped, and landing-grouped cards. */
function LinesView() {
  const { t } = useI18n();
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
  const [viewMode, setViewMode] = useLocalStorageState<RelayLinesViewMode>(
    "relay-lines-view-mode",
    "flow",
  );
  const [filterMode, setFilterMode] = useState<RelayLinesFilter>("all");
  const [sortMode, setSortMode] = useLocalStorageState<RelayLinesSort>(
    "relay-lines-sort-mode",
    "default",
  );
  const [query, setQuery] = useState("");
  const [probingIds, setProbingIds] = useState<Set<number>>(new Set());
  const loadingRef = useRef(false);
  const reloadRef = useRef(false);

  const load = useCallback(
    async (silent = false): Promise<void> => {
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
          toast.error(inRes.msg || relRes.msg || t("加载失败"));
      } catch {
        if (!silent) toast.error(t("加载失败"));
      } finally {
        loadingRef.current = false;
        setLoading(false);
        if (reloadRef.current) {
          reloadRef.current = false;
          void load(true);
        }
      }
    },
    [t],
  );

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
          toast.error(res.msg || t("检测失败"));

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
        toast.error(t("检测失败"));
      } finally {
        setProbingIds((prev) => {
          const next = new Set(prev);

          busy.forEach((id) => next.delete(id));

          return next;
        });
      }
    },
    [relays, t],
  );

  const latencyOf = (l: InboundRelayApiItem) =>
    l.forwardExists ? (
      <RelayLatencyCell
        latency={l.latency}
        probing={probingIds.has(l.id)}
        onProbe={() => probe([l.id])}
      />
    ) : null;

  const inboundById = useMemo(
    () => new Map(inbounds.map((i) => [i.id, i] as const)),
    [inbounds],
  );

  const relayedInboundIds = useMemo(
    () => new Set(relays.map((r) => r.inboundId)),
    [relays],
  );

  const allLines = useMemo<EnrichedRelayLine[]>(() => {
    const out: EnrichedRelayLine[] = [];

    for (const relay of relays) {
      const inbound = inboundById.get(relay.inboundId);

      if (!inbound) continue;
      out.push({
        relay,
        inbound,
        ok: relay.forwardExists && relay.forwardStatus === 1,
        entryTitle: relayEntryKey(relay, t("中转")),
        landingServer: inbound.external
          ? inbound.sourceName
            ? `${t("订阅源")} · ${inbound.sourceName}`
            : t("导入的节点")
          : inbound.nodeName || t("未知服务器"),
      });
    }

    return out;
  }, [relays, inboundById, t]);

  const unrelayedInbounds = useMemo(
    () => inbounds.filter((i) => !relayedInboundIds.has(i.id)),
    [inbounds, relayedInboundIds],
  );

  const summary = useMemo(() => {
    let ok = 0;
    let broken = 0;

    for (const line of allLines) {
      if (line.ok) ok++;
      else broken++;
    }

    return {
      total: allLines.length,
      ok,
      broken,
      coveredInbounds: relayedInboundIds.size,
      unrelayed: unrelayedInbounds.length,
    };
  }, [allLines, relayedInboundIds, unrelayedInbounds]);

  const q = query.trim().toLowerCase();

  const filteredLines = useMemo(() => {
    const matched = allLines.filter((line) => {
      if (filterMode === "broken" && line.ok) return false;
      if (q) {
        const hay = [
          line.entryTitle,
          line.relay.name,
          line.relay.tunnelName,
          line.relay.shareHost,
          relayEntryEndpoint(line.relay),
          line.inbound.name,
          line.landingServer,
        ]
          .filter(Boolean)
          .join(" ")
          .toLowerCase();

        if (!hay.includes(q)) return false;
      }

      return true;
    });

    if (sortMode === "default") return matched;
    const copy = [...matched];

    if (sortMode === "latency_asc") {
      copy.sort(
        (a, b) =>
          (relayLatencyValue(a.relay) ?? Number.POSITIVE_INFINITY) -
          (relayLatencyValue(b.relay) ?? Number.POSITIVE_INFINITY),
      );
    } else if (sortMode === "name_asc") {
      copy.sort((a, b) =>
        a.inbound.name.localeCompare(b.inbound.name, "zh-CN"),
      );
    }

    return copy;
  }, [allLines, filterMode, q, sortMode]);

  const filteredUnrelayed = useMemo(() => {
    if (!q) return unrelayedInbounds;

    return unrelayedInbounds.filter((i) =>
      [i.name, i.nodeName, i.sourceName, i.protocol]
        .filter(Boolean)
        .join(" ")
        .toLowerCase()
        .includes(q),
    );
  }, [unrelayedInbounds, q]);

  const groupedLines = useMemo(() => {
    const map = new Map<string, EnrichedRelayLine[]>();

    for (const line of filteredLines) {
      const key = viewMode === "byEntry" ? line.entryTitle : line.landingServer;
      const list = map.get(key);

      if (list) list.push(line);
      else map.set(key, [line]);
    }

    return [...map.entries()].sort((a, b) => a[0].localeCompare(b[0], "zh-CN"));
  }, [filteredLines, viewMode]);

  if (loading) return <PageLoadingState message="正在加载..." />;

  const showUnrelayedView =
    filterMode === "unrelayed" ||
    (allLines.length === 0 && inbounds.length > 0);

  return (
    <div className="space-y-3">
      <PageToolbar
        placeholder={t("搜索入口、隧道、落地节点或端口")}
        search={query}
        onSearch={setQuery}
      >
        {!showUnrelayedView && (
          <>
            <ToolbarSelect
              ariaLabel={t("排序方式")}
              className="max-w-[7.5rem]"
              options={[
                { value: "default", label: t("默认排序") },
                { value: "latency_asc", label: t("延迟最低") },
                { value: "name_asc", label: t("按落地名称") },
              ]}
              value={sortMode}
              onChange={(v) => setSortMode(v as RelayLinesSort)}
            />
            <ViewModeSwitch
              ariaLabel={t("视角切换")}
              options={[
                {
                  key: "flow",
                  label: t("链路流"),
                  icon: <GitBranch className="h-3.5 w-3.5" />,
                },
                {
                  key: "byEntry",
                  label: t("按入口"),
                  icon: <Server className="h-3.5 w-3.5" />,
                },
                {
                  key: "byLanding",
                  label: t("按落地"),
                  icon: <Layers className="h-3.5 w-3.5" />,
                },
              ]}
              value={viewMode}
              onChange={setViewMode}
            />
          </>
        )}
        {relays.length > 0 && (
          <ToolbarButton onPress={() => probe([])}>
            {probingIds.size > 0 ? t("测延迟中…") : t("测延迟")}
          </ToolbarButton>
        )}
      </PageToolbar>

      {inbounds.length > 0 && (
        <div className="flex flex-wrap items-center justify-between gap-2">
          <FilterPills
            ariaLabel={t("筛选线路")}
            className="mb-0"
            items={[
              { key: "all", label: t("全部线路"), count: summary.total },
              ...(summary.broken > 0
                ? [
                    {
                      key: "broken" as const,
                      label: t("异常 / 暂停"),
                      count: summary.broken,
                      tone: "warning" as const,
                    },
                  ]
                : []),
              {
                key: "unrelayed",
                label: t("未中转节点"),
                count: summary.unrelayed,
              },
            ]}
            value={filterMode}
            onChange={setFilterMode}
          />
          {summary.total > 0 && (
            <span className="hidden text-xs tabular-nums text-default-400 sm:inline">
              {t("{ok} 条正常 · 覆盖 {count} 个落地节点", {
                ok: summary.ok,
                count: summary.coveredInbounds,
              })}
            </span>
          )}
        </div>
      )}

      {inbounds.length === 0 ? (
        <EmptyHint
          text={t("还没有节点。先在“节点”里搭建或导入，再回来对接中转")}
        />
      ) : showUnrelayedView ? (
        filteredUnrelayed.length === 0 ? (
          <EmptyHint
            text={
              query
                ? t("没有匹配的未中转节点")
                : t("所有节点都已经配置了中转线路")
            }
          />
        ) : (
          <div className="grid grid-cols-1 gap-2.5 md:grid-cols-2 xl:grid-cols-3">
            {filteredUnrelayed.map((item) => {
              const landing = item.external
                ? item.sourceName || t("导入的节点")
                : item.nodeName;

              return (
                <button
                  key={item.id}
                  className="phone-card phone-card-fill flex items-center justify-between gap-3 rounded-xl border border-dashed border-divider bg-content1 p-3.5 text-left transition-colors hover:border-foreground"
                  type="button"
                  onClick={() => setEditing(item)}
                >
                  <div className="min-w-0">
                    <div className="flex items-center gap-1.5">
                      <span className="truncate text-sm font-medium text-foreground">
                        {item.name}
                      </span>
                      {item.relayOnly && (
                        <Chip size="sm" variant="flat">
                          {t("仅中转")}
                        </Chip>
                      )}
                    </div>
                    <div className="mt-0.5 truncate text-xs text-default-400">
                      {landing} · :{item.port}
                    </div>
                  </div>
                  <span className="shrink-0 rounded-md bg-default-100 px-2 py-1 text-xs font-medium text-default-700">
                    {t("添加中转")}
                  </span>
                </button>
              );
            })}
          </div>
        )
      ) : filteredLines.length === 0 ? (
        <EmptyHint
          text={query ? t("没有匹配的中转线路") : t("还没有中转线路")}
        />
      ) : viewMode === "flow" ? (
        <div className="space-y-2">
          {filteredLines.map((line) => {
            const { relay, inbound, ok, entryTitle, landingServer } = line;
            const endpoint = relayEntryEndpoint(relay);

            return (
              <div
                key={relay.id}
                className="phone-card phone-card-fill cursor-pointer rounded-xl border border-divider bg-content1 p-3 transition-colors hover:border-default-400"
                role="button"
                tabIndex={0}
                onClick={() => setEditing(inbound)}
                onKeyDown={(e) => {
                  if (e.target !== e.currentTarget) return;
                  if (e.key === "Enter" || e.key === " ") {
                    e.preventDefault();
                    setEditing(inbound);
                  }
                }}
              >
                <div className="grid grid-cols-1 items-center gap-2.5 md:grid-cols-12">
                  {/* Entry segment */}
                  <div className="flex min-w-0 items-center gap-2.5 md:col-span-4">
                    <span
                      className={`h-2 w-2 shrink-0 rounded-full ${
                        ok
                          ? "bg-success ring-[3px] ring-success/20"
                          : "bg-warning ring-[3px] ring-warning/20"
                      }`}
                      title={
                        ok
                          ? t("转发正常")
                          : t("转发已暂停或被删除，不会出现在订阅中")
                      }
                    />
                    <div className="min-w-0">
                      <div className="truncate text-sm font-medium text-foreground">
                        {entryTitle}
                      </div>
                      <div className="truncate font-mono text-xs text-default-400">
                        {endpoint}
                      </div>
                    </div>
                  </div>

                  {/* Middle tunnel bridge */}
                  <div className="flex min-w-0 items-center gap-2 md:col-span-3">
                    <div className="hidden h-px flex-1 bg-divider md:block" />
                    <div className="inline-flex max-w-full items-center gap-1.5 rounded-lg border border-divider bg-default-50 px-2.5 py-1 text-xs dark:bg-default-100/50">
                      <span className="truncate font-medium text-default-700">
                        {relay.tunnelName || t("端口转发")}
                      </span>
                      <span className="shrink-0 rounded bg-content1 px-1 py-0.5 text-[10px] text-default-400">
                        {relay.mode === "tunnel" ? t("隧道") : t("端口")}
                      </span>
                    </div>
                    <div className="hidden h-px flex-1 bg-divider md:block" />
                    <ArrowRight className="hidden h-3.5 w-3.5 shrink-0 text-default-400 md:block" />
                  </div>

                  {/* Landing & latency */}
                  <div className="flex min-w-0 items-center justify-between gap-2 md:col-span-5">
                    <div className="min-w-0">
                      <div className="flex items-center gap-1.5">
                        <span className="truncate text-sm font-medium text-foreground">
                          {inbound.name}
                        </span>
                        {inbound.relayOnly && (
                          <Chip className="shrink-0" size="sm" variant="flat">
                            {t("仅中转")}
                          </Chip>
                        )}
                      </div>
                      <div className="truncate text-xs text-default-400">
                        {landingServer} · :{inbound.port}
                      </div>
                    </div>
                    <div
                      className="shrink-0"
                      role="presentation"
                      onClick={(e) => e.stopPropagation()}
                    >
                      {latencyOf(relay) ?? (
                        <span className="text-xs text-warning-600 dark:text-warning">
                          {ok ? "—" : t("已暂停")}
                        </span>
                      )}
                    </div>
                  </div>
                </div>
              </div>
            );
          })}
        </div>
      ) : (
        <div className="grid grid-cols-1 gap-3 md:grid-cols-2">
          {groupedLines.map(([groupTitle, lines]) => {
            const brokenCount = lines.filter((l) => !l.ok).length;

            return (
              <div
                key={groupTitle}
                className="phone-card phone-card-fill overflow-hidden rounded-xl border border-divider bg-content1"
              >
                <div className="flex items-center justify-between gap-2 border-b border-divider bg-default-50/70 px-3.5 py-2.5 dark:bg-default-100/40">
                  <span className="truncate text-sm font-medium text-foreground">
                    {groupTitle}
                  </span>
                  <div className="flex shrink-0 items-center gap-1.5 text-xs tabular-nums">
                    {brokenCount > 0 && (
                      <span className="rounded bg-warning/15 px-1.5 py-0.5 text-[11px] text-warning-600 dark:text-warning">
                        {t("{count} 异常", { count: brokenCount })}
                      </span>
                    )}
                    <span className="text-default-400">
                      {t("{count} 条", { count: lines.length })}
                    </span>
                  </div>
                </div>
                <div className="divide-y divide-divider/80">
                  {lines.map((line) => {
                    const { relay, inbound, ok, entryTitle } = line;

                    return (
                      <div
                        key={relay.id}
                        className="flex cursor-pointer items-center justify-between gap-2.5 px-3.5 py-2.5 transition-colors hover:bg-default-50"
                        role="button"
                        tabIndex={0}
                        onClick={() => setEditing(inbound)}
                        onKeyDown={(e) => {
                          if (e.target !== e.currentTarget) return;
                          if (e.key === "Enter" || e.key === " ") {
                            e.preventDefault();
                            setEditing(inbound);
                          }
                        }}
                      >
                        <div className="flex min-w-0 items-center gap-2">
                          <span
                            className={`h-1.5 w-1.5 shrink-0 rounded-full ${
                              ok ? "bg-success" : "bg-warning"
                            }`}
                          />
                          <div className="min-w-0">
                            <div className="truncate text-xs font-medium text-foreground">
                              {viewMode === "byEntry"
                                ? inbound.name
                                : entryTitle}
                            </div>
                            <div className="truncate font-mono text-[11px] text-default-400">
                              {viewMode === "byEntry"
                                ? `${relay.tunnelName || t("端口转发")} · ${relayEntryEndpoint(relay)}`
                                : `${inbound.name} · ${relay.tunnelName || t("端口转发")}`}
                            </div>
                          </div>
                        </div>
                        <div
                          className="shrink-0"
                          role="presentation"
                          onClick={(e) => e.stopPropagation()}
                        >
                          {latencyOf(relay) ?? (
                            <span className="font-mono text-xs text-default-400">
                              {ok ? "—" : t("已暂停")}
                            </span>
                          )}
                        </div>
                      </div>
                    );
                  })}
                </div>
              </div>
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
  const { t } = useI18n();
  const location = useLocation();
  const navigate = useNavigate();
  const routeState = (location.state ?? {}) as RelayPageState;
  const [tab, setTabState] = useState<RelayTab>(routeState.tab ?? readTab());
  const [initialTunnelQuery, setInitialTunnelQuery] = useState(
    routeState.tab === "tunnels" ? (routeState.query ?? "") : "",
  );

  const setTab = (next: RelayTab) => {
    setTabState(next);
    try {
      setStoredValue(localStorage, TAB_KEY, next);
    } catch {
      // ignore
    }
  };

  useEffect(() => {
    const s = location.state as RelayPageState | null;

    if (s?.tab) {
      setTab(s.tab);
      if (s.tab === "tunnels" && s.query !== undefined) {
        setInitialTunnelQuery(s.query);
      }
      navigate(location.pathname, { replace: true });
    }
  }, [location.pathname, location.state, navigate]);

  return (
    <AnimatedPage className="px-4 py-6 lg:px-10 lg:py-8">
      <PageHeader
        description={t(
          "让客户端先连中转再到节点：线路按节点管理，隧道和规则是底层的转发配置",
        )}
        title={t("中转")}
      />
      <PageTabs
        tabs={TABS.map((item) => ({ ...item, label: t(item.label) }))}
        value={tab}
        onChange={setTab}
      />
      {tab === "lines" ? (
        <LinesView />
      ) : tab === "tunnels" ? (
        <TunnelPage
          embedded
          initialQuery={initialTunnelQuery}
          onInitialQueryConsumed={() => setInitialTunnelQuery("")}
        />
      ) : tab === "quality" ? (
        <QualityView />
      ) : (
        <ForwardPage embedded />
      )}
    </AnimatedPage>
  );
}
