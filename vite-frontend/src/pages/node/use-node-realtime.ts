import { useEffect, useRef, useState, useSyncExternalStore } from "react";
import axios from "axios";

import { IS_DEMO, startDemoRealtime } from "@/demo";
import { getToken } from "@/utils/session";

interface NodeRealtimeMessage {
  id?: string | number;
  type?: string;
  data?: unknown;
  message?: string;
}

interface UseNodeRealtimeOptions {
  onMessage: (message: NodeRealtimeMessage) => void;
  enabled?: boolean;
}

const MAX_STANDARD_RECONNECT_ATTEMPTS = 5;
const STANDARD_RECONNECT_DELAY_MS = 3000;
const MAX_STANDARD_RECONNECT_DELAY_MS = 15000;
const FALLBACK_RECONNECT_DELAY_MS = 30000;
/** Kept open this long after the last page leaves, so switching back reuses it. */
const LINGER_MS = 60_000;
/** A first connection quicker than this shows no "connecting" notice. */
const QUIET_CONNECT_MS = 2000;
/**
 * Messages are handed to the pages at most this often. Each message used to
 * re-render the whole node list (every node reports every few seconds, and a
 * new connection replays all of them at once); delivered together, React
 * renders once per batch.
 */
const FLUSH_INTERVAL_MS = 1000;
/** The first batch after a quiet spell goes out this soon. */
const FIRST_FLUSH_MS = 50;

const getRealtimeWsUrl = (): string => {
  const baseUrl =
    axios.defaults.baseURL ||
    (import.meta.env.VITE_API_BASE
      ? `${import.meta.env.VITE_API_BASE}/api/v1/`
      : "/api/v1/");

  return (
    baseUrl.replace(/^http/, "ws").replace(/\/api\/v1\/$/, "") +
    `/system-info?type=0&secret=${getToken() || ""}`
  );
};

// One connection is shared by every page that shows live node data, so
// switching between them does not reconnect.

interface RealtimeState {
  connected: boolean;
  connecting: boolean;
  /** Connecting for the first time and not yet slow enough to mention. */
  starting: boolean;
  usingPollingFallback: boolean;
}

let state: RealtimeState = {
  connected: false,
  connecting: false,
  starting: false,
  usingPollingFallback: false,
};
const stateListeners = new Set<() => void>();
const handlers = new Set<(m: NodeRealtimeMessage) => void>();

let socket: WebSocket | null = null;
let socketUrl = "";
let attempts = 0;
let reconnectTimer: ReturnType<typeof setTimeout> | null = null;
let lingerTimer: ReturnType<typeof setTimeout> | null = null;
let quietTimer: ReturnType<typeof setTimeout> | null = null;
let queue: NodeRealtimeMessage[] = [];
let flushTimer: ReturnType<typeof setTimeout> | null = null;
let lastFlush = 0;

function flush() {
  flushTimer = null;
  lastFlush = Date.now();
  const batch = queue;

  queue = [];
  // One task: React batches every handler's state updates into one render.
  for (const m of batch) handlers.forEach((h) => h(m));
}

function enqueue(m: NodeRealtimeMessage) {
  queue.push(m);
  if (flushTimer) return;
  const wait = Math.max(
    FIRST_FLUSH_MS,
    lastFlush + FLUSH_INTERVAL_MS - Date.now(),
  );

  flushTimer = setTimeout(flush, wait);
}

function setState(patch: Partial<RealtimeState>) {
  state = { ...state, ...patch };
  stateListeners.forEach((l) => l());
}

function clearTimer(t: ReturnType<typeof setTimeout> | null) {
  if (t) clearTimeout(t);

  return null;
}

function closeSocket() {
  reconnectTimer = clearTimer(reconnectTimer);
  quietTimer = clearTimer(quietTimer);
  flushTimer = clearTimer(flushTimer);
  queue = [];
  attempts = 0;
  if (socket) {
    socket.onopen = null;
    socket.onmessage = null;
    socket.onerror = null;
    socket.onclose = null;
    if (
      socket.readyState === WebSocket.OPEN ||
      socket.readyState === WebSocket.CONNECTING
    ) {
      socket.close();
    }
    socket = null;
  }
  setState({
    connected: false,
    connecting: false,
    starting: false,
    usingPollingFallback: false,
  });
}

function openSocket() {
  const url = getRealtimeWsUrl();

  // Another login (a new token) must not keep the old stream.
  if (socket && socketUrl !== url) closeSocket();
  if (
    socket &&
    (socket.readyState === WebSocket.OPEN ||
      socket.readyState === WebSocket.CONNECTING)
  ) {
    return;
  }

  const first = attempts === 0;

  setState({ connecting: true, starting: first });
  if (first) {
    quietTimer = clearTimer(quietTimer);
    quietTimer = setTimeout(() => {
      quietTimer = null;
      setState({ starting: false });
    }, QUIET_CONNECT_MS);
  }

  let ws: WebSocket;

  try {
    ws = new WebSocket(url);
  } catch {
    setState({ connected: false, connecting: false, starting: false });

    return;
  }
  socket = ws;
  socketUrl = url;

  ws.onopen = () => {
    attempts = 0;
    quietTimer = clearTimer(quietTimer);
    setState({
      connected: true,
      connecting: false,
      starting: false,
      usingPollingFallback: false,
    });
  };
  ws.onmessage = (event) => {
    try {
      const parsed = JSON.parse(event.data);

      if (parsed && typeof parsed === "object") {
        enqueue(parsed as NodeRealtimeMessage);
      }
    } catch {}
  };
  ws.onerror = () => {};
  ws.onclose = () => {
    socket = null;
    quietTimer = clearTimer(quietTimer);
    attempts += 1;
    const exhausted = attempts >= MAX_STANDARD_RECONNECT_ATTEMPTS;

    setState({
      connected: false,
      connecting: false,
      starting: false,
      usingPollingFallback: exhausted,
    });
    if (handlers.size === 0) return;
    const delay = exhausted
      ? FALLBACK_RECONNECT_DELAY_MS
      : Math.min(
          STANDARD_RECONNECT_DELAY_MS * attempts,
          MAX_STANDARD_RECONNECT_DELAY_MS,
        );

    reconnectTimer = setTimeout(() => {
      reconnectTimer = null;
      openSocket();
    }, delay);
  };
}

function subscribe(handler: (m: NodeRealtimeMessage) => void): () => void {
  handlers.add(handler);
  lingerTimer = clearTimer(lingerTimer);
  if (!reconnectTimer) openSocket();

  return () => {
    handlers.delete(handler);
    if (handlers.size > 0) return;
    lingerTimer = clearTimer(lingerTimer);
    lingerTimer = setTimeout(() => {
      lingerTimer = null;
      if (handlers.size === 0) closeSocket();
    }, LINGER_MS);
  };
}

function subscribeState(listener: () => void) {
  stateListeners.add(listener);

  return () => {
    stateListeners.delete(listener);
  };
}

const getState = () => state;

export const useNodeRealtime = ({
  onMessage,
  enabled = true,
}: UseNodeRealtimeOptions) => {
  const onMessageRef = useRef(onMessage);
  const shared = useSyncExternalStore(subscribeState, getState);
  // A page that subscribes while the shared socket is already open has
  // nothing to wait for; until its effect runs, report the shared state.
  const [subscribed, setSubscribed] = useState(false);

  useEffect(() => {
    onMessageRef.current = onMessage;
  }, [onMessage]);

  useEffect(() => {
    if (!enabled) return;
    // The online demo has no server to stream from.
    if (IS_DEMO) return startDemoRealtime((m) => onMessageRef.current(m));
    const unsubscribe = subscribe((m) => onMessageRef.current(m));

    setSubscribed(true);

    return () => {
      unsubscribe();
      setSubscribed(false);
    };
  }, [enabled]);

  if (IS_DEMO) {
    // The demo shows the nodes as connected (it has no live stream).
    return {
      wsConnected: true,
      wsConnecting: false,
      wsStarting: false,
      usingPollingFallback: false,
    };
  }

  // Before the first subscribe nothing has started yet: treat it as the
  // quiet start rather than as a dropped connection.
  const starting = shared.starting || (!subscribed && !shared.connected);

  return {
    wsConnected: shared.connected,
    wsConnecting: shared.connecting && !shared.starting,
    /** Connecting for the first time; not worth a notice yet. */
    wsStarting: enabled && starting,
    usingPollingFallback: shared.usingPollingFallback,
  };
};
