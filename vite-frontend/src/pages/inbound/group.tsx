import { useState } from "react";
import { ChevronDown } from "lucide-react";

import { getStoredValue, setStoredValue } from "@/utils/identity-storage";

export type GroupBy = "node" | "kernel";

const GROUP_BY_KEY = "flyx_inbound_group_by";
const COLLAPSED_KEY = "flyx_inbound_collapsed";

function readStorage(key: string): string | null {
  try {
    return getStoredValue(localStorage, key);
  } catch {
    return null;
  }
}

function writeStorage(key: string, value: string) {
  try {
    setStoredValue(localStorage, key, value);
  } catch {
    // Remembering the layout is only a convenience.
  }
}

/** Grouping mode shared by the kernel and inbound sections, remembered. */
export function useGroupBy(): [GroupBy, (g: GroupBy) => void] {
  const [groupBy, setGroupBy] = useState<GroupBy>(() =>
    readStorage(GROUP_BY_KEY) === "kernel" ? "kernel" : "node",
  );

  return [
    groupBy,
    (g) => {
      setGroupBy(g);
      writeStorage(GROUP_BY_KEY, g);
    },
  ];
}

export function GroupToggle({
  value,
  onChange,
}: {
  value: GroupBy;
  onChange: (g: GroupBy) => void;
}) {
  return (
    <div className="inline-grid grid-cols-2 gap-1 rounded-xl bg-default-100 p-1 text-xs">
      {(
        [
          ["node", "按服务器"],
          ["kernel", "按内核"],
        ] as const
      ).map(([key, label]) => (
        <button
          key={key}
          className={`rounded-lg px-3 py-1 transition-colors ${
            value === key
              ? "bg-background shadow-sm font-medium"
              : "text-default-500"
          }`}
          type="button"
          onClick={() => onChange(key)}
        >
          {label}
        </button>
      ))}
    </div>
  );
}

export interface Group<T> {
  key: string;
  title: string;
  items: T[];
}

/** Groups items by key, ordered by title so the layout stays stable. */
export function groupItems<T>(
  items: T[],
  keyOf: (item: T) => string,
  titleOf: (key: string, first: T) => string,
): Group<T>[] {
  const map = new Map<string, Group<T>>();

  for (const item of items) {
    const key = keyOf(item);
    let g = map.get(key);

    if (!g) {
      g = { key, title: titleOf(key, item), items: [] };
      map.set(key, g);
    }
    g.items.push(item);
  }

  return [...map.values()].sort((a, b) =>
    a.title.localeCompare(b.title, "zh-CN"),
  );
}

function readCollapsed(): Set<string> {
  try {
    const raw = readStorage(COLLAPSED_KEY);

    return new Set(raw ? (JSON.parse(raw) as string[]) : []);
  } catch {
    return new Set();
  }
}

/** Remembers which groups are folded, per section. */
export function useCollapsed(): [
  (id: string) => boolean,
  (id: string) => void,
] {
  const [collapsed, setCollapsed] = useState<Set<string>>(readCollapsed);

  return [
    (id) => collapsed.has(id),
    (id) => {
      setCollapsed((prev) => {
        const next = new Set(prev);

        if (next.has(id)) next.delete(id);
        else next.add(id);
        writeStorage(COLLAPSED_KEY, JSON.stringify([...next]));

        return next;
      });
    },
  ];
}

export function GroupHeader({
  title,
  count,
  unit,
  collapsed,
  extra,
  onToggle,
}: {
  title: string;
  count: number;
  unit: string;
  collapsed: boolean;
  extra?: React.ReactNode;
  onToggle: () => void;
}) {
  return (
    <button
      className="flex w-full items-center gap-2 py-2 text-left"
      type="button"
      onClick={onToggle}
    >
      <ChevronDown
        className={`h-4 w-4 shrink-0 text-default-500 transition-transform ${
          collapsed ? "-rotate-90" : ""
        }`}
      />
      <span className="truncate text-sm font-semibold">{title}</span>
      <span className="shrink-0 text-xs text-default-500">
        {count} {unit}
      </span>
      {extra && <span className="ml-auto flex shrink-0 gap-1">{extra}</span>}
    </button>
  );
}
