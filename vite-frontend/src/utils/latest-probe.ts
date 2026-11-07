/** A list request can finish after a newer manual/background measurement. */
export function latestProbe<T extends { checkedAt: number }>(
  incoming: T | undefined,
  existing: T | undefined,
): T | undefined {
  return existing && incoming && existing.checkedAt > incoming.checkedAt
    ? existing
    : incoming;
}
