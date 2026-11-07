export const removeItemsById = <T extends { id: number }>(
  items: T[],
  ids: Iterable<number>,
): T[] => {
  const idSet = new Set(ids);

  return items.filter((item) => !idSet.has(item.id));
};

export const replaceItemById = <T extends { id: number }>(
  items: T[],
  nextItem: T,
): T[] => {
  return items.map((item) => (item.id === nextItem.id ? nextItem : item));
};

export const upsertItemById = <T extends { id: number }>(
  items: T[],
  nextItem: T,
  options?: { prepend?: boolean },
): T[] => {
  const existingIndex = items.findIndex((item) => item.id === nextItem.id);

  if (existingIndex >= 0) {
    return replaceItemById(items, nextItem);
  }

  if (options?.prepend) {
    return [nextItem, ...items];
  }

  return [...items, nextItem];
};
