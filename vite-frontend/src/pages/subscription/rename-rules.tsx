import { useState } from "react";
import { Plus, Trash2 } from "lucide-react";

/**
 * Rename rules as rows of "regex => replacement", like Sub-Store's 正则命名.
 *
 * The rules are still stored as the backend's text format, one
 * "find => replace" per line where /.../ marks a regex. Rows are always
 * written as /regex/; plain-text lines saved before are turned into an
 * escaped regex when loaded, so they keep matching the same names.
 */
interface RenameRow {
  pattern: string;
  replacement: string;
}

const escapeRegExp = (s: string) => s.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");

function parseRules(text: string): RenameRow[] {
  const rows: RenameRow[] = [];

  for (const raw of text.split("\n")) {
    const line = raw.trim();

    if (!line || line.startsWith("#")) continue;
    const at = line.indexOf("=>");
    const find = (at < 0 ? line : line.slice(0, at)).trim();
    const replacement = at < 0 ? "" : line.slice(at + 2).trim();

    if (!find) continue;
    const isRegex =
      find.length > 2 && find.startsWith("/") && find.endsWith("/");

    rows.push({
      pattern: isRegex ? find.slice(1, -1) : escapeRegExp(find),
      replacement,
    });
  }

  return rows;
}

function formatRules(rows: RenameRow[]): string {
  return rows
    .filter((r) => r.pattern.trim())
    .map((r) => `/${r.pattern.trim()}/ => ${r.replacement.trim()}`)
    .join("\n");
}

/** Why a pattern can't be saved, or "" when it's fine. */
function patternError(pattern: string): string {
  if (!pattern.trim()) return "";
  // The stored format splits each line at the first "=>".
  if (pattern.includes("=>")) return "正则里不能包含 =>";
  try {
    new RegExp(pattern);
  } catch {
    return "正则表达式有误";
  }

  return "";
}

export function RenameRules({
  value,
  onChange,
}: {
  value: string;
  onChange: (text: string) => void;
}) {
  // Rows are kept locally: a row being typed (or left empty) must not be
  // dropped by re-parsing the saved text on every keystroke.
  const [rows, setRows] = useState<RenameRow[]>(() => parseRules(value));

  const update = (next: RenameRow[]) => {
    setRows(next);
    onChange(formatRules(next));
  };
  const field =
    "h-9 w-full min-w-0 rounded-lg border bg-background px-2 font-mono text-xs";

  return (
    <div className="space-y-2">
      {rows.map((row, i) => {
        const err = patternError(row.pattern);

        return (
          <div key={i} className="space-y-1">
            <div className="flex items-center gap-1.5">
              <input
                aria-label={`第 ${i + 1} 条：正则表达式`}
                className={`${field} flex-[3] ${err ? "border-danger" : "border-divider"}`}
                placeholder="正则表达式"
                value={row.pattern}
                onChange={(e) =>
                  update(
                    rows.map((r, j) =>
                      j === i ? { ...r, pattern: e.target.value } : r,
                    ),
                  )
                }
              />
              <span className="shrink-0 text-xs text-default-400">→</span>
              <input
                aria-label={`第 ${i + 1} 条：替换为`}
                className={`${field} flex-[2] border-divider`}
                placeholder="留空删除"
                value={row.replacement}
                onChange={(e) =>
                  update(
                    rows.map((r, j) =>
                      j === i ? { ...r, replacement: e.target.value } : r,
                    ),
                  )
                }
              />
              <button
                aria-label={`删除第 ${i + 1} 条`}
                className="shrink-0 rounded-lg p-2 text-default-400 hover:bg-danger-50 hover:text-danger"
                type="button"
                onClick={() => update(rows.filter((_, j) => j !== i))}
              >
                <Trash2 className="h-4 w-4" />
              </button>
            </div>
            {err && <p className="text-xs text-danger">{err}</p>}
          </div>
        );
      })}
      <button
        className="flex h-9 w-full items-center justify-center gap-1 rounded-lg border border-dashed border-default-300 text-sm text-default-500 hover:border-primary hover:text-primary"
        type="button"
        onClick={() => setRows([...rows, { pattern: "", replacement: "" }])}
      >
        <Plus className="h-4 w-4" />
        添加改名规则
      </button>
    </div>
  );
}
