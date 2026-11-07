// The settings form a status page theme declares under "config" in its
// theme.json. The shape and the rules for what can be drawn follow monitor's
// panel (github.com/monitor-probe/monitor, web-admin/src/lib/api.ts, MIT), so
// a theme declares its form the same way for either.

/** One field of the form. */
export interface ThemeConfigField {
  key: string;
  type: "string" | "text" | "number" | "boolean" | "select";
  label?: string;
  help?: string;
  default: unknown;
  options?: { value: string; label?: string }[];
  min?: number;
  max?: number;
}

/** A heading between fields; it holds no value. */
export interface ThemeConfigTitle {
  type: "title";
  label: string;
}

const FIELD_TYPES = ["string", "text", "number", "boolean", "select"];

/** Whether value is one the field can hold, the test a theme applies too. */
export function fits(field: ThemeConfigField, value: unknown): boolean {
  switch (field.type) {
    case "boolean":
      return typeof value === "boolean";
    case "number":
      return (
        typeof value === "number" &&
        Number.isFinite(value) &&
        (field.min === undefined || value >= field.min) &&
        (field.max === undefined || value <= field.max)
      );
    case "select":
      return !!field.options?.some((o) => o.value === value);
    default:
      return typeof value === "string";
  }
}

/**
 * The entries of the form that can be drawn, headings included, in the
 * manifest's order. The manifest is the theme author's, so a malformed entry
 * is left out rather than failing the form: a heading without a label or
 * without a field under it, a duplicate or empty key, an unknown type, a label
 * or help that is not text, a select whose options are not all non-empty
 * strings, or a default the field could not hold.
 */
export function configForm(
  config: unknown,
): Array<ThemeConfigField | ThemeConfigTitle> {
  if (!Array.isArray(config)) return [];
  const seen = new Set<string>();
  const text = (v: unknown) => v === undefined || typeof v === "string";
  const drawable = config.filter(
    (entry): entry is ThemeConfigField | ThemeConfigTitle => {
      if (typeof entry !== "object" || entry === null) return false;
      const e = entry as Record<string, unknown>;

      if (e.type === "title") {
        return typeof e.label === "string" && e.label !== "";
      }
      const { key, type, label, help, options, min, max } = e;
      const ok =
        typeof key === "string" &&
        key !== "" &&
        !seen.has(key) &&
        typeof type === "string" &&
        FIELD_TYPES.includes(type) &&
        text(label) &&
        text(help) &&
        (type !== "select" ||
          (Array.isArray(options) &&
            options.length > 0 &&
            options.every(
              (o) =>
                typeof o?.value === "string" && o.value !== "" && text(o.label),
            ))) &&
        [min, max].every((b) => b === undefined || typeof b === "number") &&
        fits(e as unknown as ThemeConfigField, e.default);

      if (ok) seen.add(key as string);

      return ok;
    },
  );

  return drawable.filter((entry, i) => {
    const next = drawable[i + 1];

    return (
      entry.type !== "title" || (next !== undefined && next.type !== "title")
    );
  });
}

export function configFields(
  form: Array<ThemeConfigField | ThemeConfigTitle>,
): ThemeConfigField[] {
  return form.filter((e): e is ThemeConfigField => e.type !== "title");
}

/** The form split at its headings; fields before the first form "通用". */
export function configSections(
  form: Array<ThemeConfigField | ThemeConfigTitle>,
): Array<{ label: string; fields: ThemeConfigField[] }> {
  const sections: Array<{ label: string; fields: ThemeConfigField[] }> = [];

  for (const entry of form) {
    if (entry.type === "title") {
      sections.push({ label: entry.label, fields: [] });
    } else {
      if (sections.length === 0) sections.push({ label: "通用", fields: [] });
      sections[sections.length - 1].fields.push(entry);
    }
  }

  return sections;
}

/** What each field shows: the saved value while the field can hold it, else the default. */
export function configValues(
  fields: ThemeConfigField[],
  saved: Record<string, unknown>,
): Record<string, unknown> {
  return Object.fromEntries(
    fields.map((f) => [
      f.key,
      fits(f, saved[f.key]) ? saved[f.key] : f.default,
    ]),
  );
}

/**
 * What to save: only the values that differ from their defaults, so a theme
 * changing a default later reaches every site that never touched it. Keys the
 * form does not declare (left by an older version of the theme) are kept.
 */
export function configOverrides(
  fields: ThemeConfigField[],
  saved: Record<string, unknown>,
  values: Record<string, unknown>,
): Record<string, unknown> {
  const next = { ...saved };

  for (const f of fields) {
    if (values[f.key] === f.default) delete next[f.key];
    else next[f.key] = values[f.key];
  }

  return next;
}
