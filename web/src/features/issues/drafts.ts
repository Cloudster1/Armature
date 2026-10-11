import type { Doc } from "@/api/issues";

/** What an unsaved draft on an issue is for. */
export type DraftKind = "description" | "comment";

/** A draft that was kept; its doc is null when everything was deleted. */
export interface KeptDraft {
  doc: Doc | null;
}

// Held for the tab, not the browser: a draft is words not yet sent, and the
// page is keyed by issue, so what was typed has to live outside it.
const kept = new Map<string, KeptDraft>();

function slot(kind: DraftKind, issueKey: string): string {
  return `${kind}:${issueKey}`;
}

export function readDraft(kind: DraftKind, issueKey: string): KeptDraft | undefined {
  return kept.get(slot(kind, issueKey));
}

export function keepDraft(kind: DraftKind, issueKey: string, doc: Doc | null) {
  kept.set(slot(kind, issueKey), { doc });
}

export function forgetDraft(kind: DraftKind, issueKey: string) {
  kept.delete(slot(kind, issueKey));
}
