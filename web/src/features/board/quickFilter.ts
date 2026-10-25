import type { Card } from "@/api/boards";
import type { LabelRef, ParentRef, TypeRef, UserRef } from "@/api/issues";
import { t } from "@/i18n";

/** Narrows the cards a board already loaded, for the moment, leaving its stored scope alone.
 * Chips of different kinds AND together; the values within one chip OR together. */
export interface QuickFilter {
  mine?: boolean;
  /** User ids, or UNASSIGNED for cards nobody holds. */
  assignees: string[];
  /** Label ids. */
  labels: string[];
  /** Type names. */
  types: string[];
  /** The keys of the epics the cards sit under. */
  epics: string[];
  /** Matched against a card's key and summary, ignoring case. */
  q?: string;
}

/** The assignee value that means "nobody", as on the dashboard. */
export const UNASSIGNED = "-";

export const EMPTY_QUICK_FILTER: QuickFilter = { assignees: [], labels: [], types: [], epics: [] };

export function isFiltered(filter: QuickFilter): boolean {
  return Boolean(filter.mine) || filter.assignees.length + filter.labels.length + filter.types.length + filter.epics.length > 0 || Boolean(filter.q?.trim());
}

/** The cards the filter lets through, in the order they came. */
export function filterCards(cards: Card[], filter: QuickFilter, meId: string): Card[] {
  if (!isFiltered(filter)) return cards;
  const text = filter.q?.trim().toLowerCase() ?? "";
  return cards.filter((card) => {
    const assignee = card.assignee?.id ?? UNASSIGNED;
    if (filter.mine && (!meId || assignee !== meId)) return false;
    if (filter.assignees.length > 0 && !filter.assignees.includes(assignee)) return false;
    if (filter.labels.length > 0 && !(card.labels ?? []).some((label) => filter.labels.includes(label.id))) return false;
    if (filter.types.length > 0 && !filter.types.includes(card.type.name)) return false;
    if (filter.epics.length > 0 && !(card.parent && filter.epics.includes(card.parent.key))) return false;
    if (text && !card.key.toLowerCase().includes(text) && !card.summary.toLowerCase().includes(text)) return false;
    return true;
  });
}

export interface QuickFilterOptions {
  assignees: UserRef[];
  labels: LabelRef[];
  types: TypeRef[];
  epics: ParentRef[];
}

/** What is on the board to filter by, so a chip offers only what can match. */
export function optionsFrom(cards: Card[]): QuickFilterOptions {
  const assignees = new Map<string, UserRef>();
  const labels = new Map<string, LabelRef>();
  const types = new Map<string, TypeRef>();
  const epics = new Map<string, ParentRef>();
  let unassigned = false;
  for (const card of cards) {
    if (card.assignee) assignees.set(card.assignee.id, card.assignee);
    else unassigned = true;
    for (const label of card.labels ?? []) labels.set(label.id, label);
    types.set(card.type.name, card.type);
    if (card.parent) epics.set(card.parent.key, card.parent);
  }
  const byName = <T extends { name: string }>(a: T, b: T) => a.name.localeCompare(b.name);
  return {
    assignees: [...[...assignees.values()].sort(byName), ...(unassigned ? [{ id: UNASSIGNED, name: t.kit.unassigned }] : [])],
    labels: [...labels.values()].sort(byName),
    types: [...types.values()].sort(byName),
    epics: [...epics.values()].sort((a, b) => a.summary.localeCompare(b.summary)),
  };
}

/** The filter as it travels in the address, lists comma-joined. */
export interface QuickFilterSearch {
  mine?: boolean;
  assignee?: string;
  label?: string;
  type?: string;
  epic?: string;
  q?: string;
}

/** Reads the filter from the address, ignoring anything that is not one of its values. */
export function parseSearch(search: Record<string, unknown>): QuickFilter {
  const list = (key: string) => (typeof search[key] === "string" ? (search[key] as string).split(",").filter(Boolean) : []);
  // The router reads a value that looks like a number as one, so a search for 42 arrives as 42.
  const q = typeof search.q === "string" || typeof search.q === "number" ? String(search.q) : "";
  return {
    ...(search.mine === true || search.mine === "true" ? { mine: true } : {}),
    assignees: list("assignee"),
    labels: list("label"),
    types: list("type"),
    epics: list("epic"),
    ...(q.trim() ? { q } : {}),
  };
}

export function toSearch(filter: QuickFilter): QuickFilterSearch {
  const joined = (values: string[]) => (values.length > 0 ? values.join(",") : undefined);
  return {
    mine: filter.mine || undefined,
    assignee: joined(filter.assignees),
    label: joined(filter.labels),
    type: joined(filter.types),
    epic: joined(filter.epics),
    q: filter.q?.trim() ? filter.q : undefined,
  };
}
