import { getForwardList, getInboundRelays } from "@/api";
import { NETWORK_LOST } from "@/api/error-message";

const sleep = (ms: number) => new Promise((r) => setTimeout(r, ms));
const RETRY_DELAYS_MS = [2000, 3000, 5000];

/**
 * True when the response never made it back (connection dropped during a
 * whole-process realm restart, Axios timeout, or proxy 502/504/524).
 */
export function isLostOrTimedOut(res: { code: number; msg: string }): boolean {
  if (res.code !== -1) {
    return false;
  }

  return (
    res.msg === NETWORK_LOST ||
    /timeout|502|503|504|520|522|524/i.test(res.msg || "")
  );
}

/**
 * Creating a forward on a realm tunnel restarts realm on the entry node.
 * When the browser reaches the panel through that same node (or the first
 * realm install outlives an intermediate proxy timeout), the HTTP request
 * can fail even though the forward was committed. Poll once the connection
 * returns so we do not leave the modal open and trigger a duplicate-port
 * error on retry.
 */
export async function forwardCreatedDespiteDrop(
  res: { code: number; msg: string },
  prevMaxId: number,
  expected: {
    tunnelId: number;
    name: string;
    remoteAddr: string;
    inPort?: number | null;
  },
): Promise<boolean> {
  if (!isLostOrTimedOut(res)) {
    return false;
  }
  const wantName = expected.name.trim();
  const wantRemote = expected.remoteAddr.trim();
  const wantPort =
    typeof expected.inPort === "number" && expected.inPort > 0
      ? expected.inPort
      : 0;

  for (const wait of RETRY_DELAYS_MS) {
    await sleep(wait);
    const list = await getForwardList();

    if (list.code === 0) {
      const found = (list.data ?? []).some(
        (item) =>
          item.id > prevMaxId &&
          item.tunnelId === expected.tunnelId &&
          item.name === wantName &&
          item.remoteAddr === wantRemote &&
          (wantPort === 0 || item.inPort === wantPort),
      );

      if (found) {
        return true;
      }
    }
  }

  return false;
}

/**
 * Same check for editing a forward whose realm restart dropped the reply.
 */
export async function forwardUpdatedDespiteDrop(
  res: { code: number; msg: string },
  id: number | undefined,
  prevUpdated: number | undefined,
): Promise<boolean> {
  if (!isLostOrTimedOut(res) || !id) {
    return false;
  }

  for (const wait of RETRY_DELAYS_MS) {
    await sleep(wait);
    const list = await getForwardList();

    if (list.code === 0) {
      const item = (list.data ?? []).find((f) => f.id === id);

      if (
        item &&
        typeof item.updatedTime === "number" &&
        item.updatedTime !== prevUpdated
      ) {
        return true;
      }
    }
  }

  return false;
}

/**
 * Same check for creating an inbound relay backed by a realm port-forward.
 */
export async function relayCreatedDespiteDrop(
  res: { code: number; msg: string },
  inboundId: number,
  prevMaxId: number,
  expected: {
    tunnelId: number;
    name?: string;
    inPort?: number;
  },
): Promise<boolean> {
  if (!isLostOrTimedOut(res)) {
    return false;
  }
  const wantName = expected.name?.trim() || "";
  const wantPort =
    typeof expected.inPort === "number" && expected.inPort > 0
      ? expected.inPort
      : 0;

  for (const wait of RETRY_DELAYS_MS) {
    await sleep(wait);
    const list = await getInboundRelays(inboundId);

    if (list.code === 0) {
      const found = (list.data ?? []).some(
        (rel) =>
          rel.id > prevMaxId &&
          rel.tunnelId === expected.tunnelId &&
          (!wantName || rel.name === wantName) &&
          (wantPort === 0 || rel.entries.some((e) => e.Port === wantPort)),
      );

      if (found) {
        return true;
      }
    }
  }

  return false;
}

export const FORWARD_CREATE_DROP_NOTICE =
  "规则已创建成功（保存时连接短暂中断，可能是中转进程重启或正在通过该节点访问面板）";

export const FORWARD_UPDATE_DROP_NOTICE =
  "规则已保存（保存时连接短暂中断，可能是中转进程重启或正在通过该节点访问面板）";

export const RELAY_CREATE_DROP_NOTICE =
  "中转转发已创建成功（保存时连接短暂中断，可能是中转进程重启或正在通过该节点访问面板）";
