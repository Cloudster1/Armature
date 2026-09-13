import type { PlanWarning, WarningKind } from "@/api/plan";

export type Severity = "warning" | "error";

// A contradiction and an omission ask different things of the reader: one is
// wrong as written and the other is not finished. Two tones say which.
const ERRORS: ReadonlySet<WarningKind> = new Set<WarningKind>(["blocked-too-early", "outside-parent", "past-milestone"]);

/** How serious a warning of this kind is. */
export function severityOf(kind: WarningKind): Severity {
  return ERRORS.has(kind) ? "error" : "warning";
}

/** The tone a row wears for its warnings, or null for none. */
export function worstOf(warnings: readonly PlanWarning[]): Severity | null {
  if (warnings.length === 0) return null;
  return warnings.some((w) => severityOf(w.kind) === "error") ? "error" : "warning";
}

/** The warnings' words, one per line, for a title or a label. */
export function wordsOf(warnings: readonly PlanWarning[]): string {
  return warnings.map((w) => w.message).join("\n");
}
