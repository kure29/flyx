import { useState } from "react";
import { LayoutGrid, List } from "lucide-react";

import { getStoredValue, setStoredValue } from "@/utils/identity-storage";

/** How a page shows its items: one per row, or as cards in a grid. */
export type ViewMode = "list" | "card";

const storageKey = (page: string) => `flyx_view_${page}`;

function readMode(page: string): ViewMode {
  try {
    return getStoredValue(localStorage, storageKey(page)) === "card"
      ? "card"
      : "list";
  } catch {
    return "list";
  }
}

/** The page's view mode, remembered per page in this browser. */
export function useViewMode(page: string): [ViewMode, (m: ViewMode) => void] {
  const [mode, setModeState] = useState<ViewMode>(() => readMode(page));
  const setMode = (m: ViewMode) => {
    setModeState(m);
    try {
      setStoredValue(localStorage, storageKey(page), m);
    } catch {
      // ignore: the choice just isn't remembered
    }
  };

  return [mode, setMode];
}

export function ViewModeToggle({
  value,
  onChange,
}: {
  value: ViewMode;
  onChange: (m: ViewMode) => void;
}) {
  const options: Array<[ViewMode, string, typeof List]> = [
    ["list", "列表", List],
    ["card", "卡片", LayoutGrid],
  ];

  return (
    <div
      aria-label="展示方式"
      className="flex h-9 shrink-0 items-center gap-0.5 rounded-full bg-default-100 p-1"
      role="radiogroup"
    >
      {options.map(([m, label, Icon]) => (
        <button
          key={m}
          aria-checked={value === m}
          aria-label={label}
          className={`flex h-7 w-8 items-center justify-center rounded-full transition-colors ${
            value === m
              ? "bg-background text-foreground shadow-sm"
              : "text-default-400 hover:text-default-600"
          }`}
          role="radio"
          title={label}
          type="button"
          onClick={() => onChange(m)}
        >
          <Icon className="h-4 w-4" />
        </button>
      ))}
    </div>
  );
}
