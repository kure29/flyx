import type { Dispatch, SetStateAction } from "react";

import { useEffect, useState } from "react";

/**
 * The data a page last showed, kept in memory so that returning to the page
 * renders it at once while the page reloads in the background. Nothing is
 * written to storage: a reload of the panel starts empty, and logging in or
 * out clears it (see clearPageCache).
 */
const store = new Map<string, unknown>();
const loaded = new Set<string>();

/** Forgets every page's data (another user may log in next). */
export function clearPageCache(): void {
  store.clear();
  loaded.clear();
}

/** Whether the page has loaded before, so its cached state can be shown. */
export function pageCached(page: string): boolean {
  return loaded.has(page);
}

/** Records that the page's state now holds loaded data. */
export function markPageCached(page: string): void {
  loaded.add(page);
}

/**
 * useState that starts from the value it last had on this page (when the
 * page has loaded before) and keeps the cache up to date.
 */
export function useCachedState<T>(
  page: string,
  name: string,
  initial: T,
): [T, Dispatch<SetStateAction<T>>] {
  const key = `${page}/${name}`;
  const [value, setValue] = useState<T>(() =>
    loaded.has(page) && store.has(key) ? (store.get(key) as T) : initial,
  );

  useEffect(() => {
    store.set(key, value);
  }, [key, value]);

  return [value, setValue];
}
