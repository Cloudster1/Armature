import type { FieldKind } from "@/api/fields";

/**
 * How a typed answer becomes the value the API stores, per kind. The API does
 * the checking; this only puts the text in the right shape and turns an empty
 * box into "cleared" rather than an empty string.
 */
export function valueFromInput(kind: FieldKind, raw: string | boolean): unknown {
  if (kind === "checkbox") return raw === true || raw === "true";
  const text = typeof raw === "string" ? raw.trim() : String(raw);
  if (text === "") return null;
  if (kind === "number") {
    const n = Number(text);
    return Number.isFinite(n) ? n : text;
  }
  return text;
}

/** The text a control starts with for a stored value. */
export function inputFromValue(kind: FieldKind, value: unknown): string {
  if (value === undefined || value === null) return "";
  if (kind === "checkbox") return value === true ? "true" : "";
  return String(value);
}

/** Which input type draws a kind. Select and checkbox have controls of their own. */
export function inputTypeFor(kind: FieldKind): "text" | "number" | "date" | "url" {
  switch (kind) {
    case "number":
      return "number";
    case "date":
      return "date";
    case "url":
      return "url";
    default:
      return "text";
  }
}

/** Splits the options a person typed, one per line, dropping blanks and repeats. */
export function parseOptions(text: string): string[] {
  const seen = new Set<string>();
  const out: string[] = [];
  for (const line of text.split("\n")) {
    const option = line.trim();
    if (option === "" || seen.has(option)) continue;
    seen.add(option);
    out.push(option);
  }
  return out;
}

/**
 * What to say when the browser could not parse what was typed into a number
 * or date control. The control reports an empty value then, so without this
 * the answer would be silently cleared instead of refused.
 */
export function badInputMessage(kind: FieldKind, name: string): string {
  switch (kind) {
    case "number":
      return `${name} takes a number, such as 12 or 2.5.`;
    case "date":
      return `${name} takes a date such as 2026-01-31.`;
    default:
      return `${name} cannot take that.`;
  }
}
