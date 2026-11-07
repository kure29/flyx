import { getInboundList } from "@/api";
import { NETWORK_LOST } from "@/api/error-message";

const sleep = (ms: number) => new Promise((r) => setTimeout(r, ms));

/**
 * Saving an inbound restarts its kernel. When the browser reaches the panel
 * through that same node, the restart cuts the request before the answer
 * arrives although the change was saved. Ask again once the node is back:
 * true when the inbound's updatedTime moved past prevUpdated.
 */
export async function savedDespiteDrop(
  res: { code: number; msg: string },
  id: number | undefined,
  prevUpdated: number,
): Promise<boolean> {
  if (res.code !== -1 || res.msg !== NETWORK_LOST || !id) {
    return false;
  }
  for (const wait of [2000, 3000, 5000]) {
    await sleep(wait);
    const list = await getInboundList();

    if (list.code === 0) {
      const item = list.data?.find((i) => i.id === id);

      return Boolean(item && item.updatedTime !== prevUpdated);
    }
  }

  return false;
}

export const DROP_NOTICE =
  "已保存。保存时连接中断了，可能是内核重启时你正在用这个节点上网";
