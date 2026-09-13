/**
 * A dashboard's filter in the tile's own terms, and the one query it becomes.
 * The controls are a friendly way to write the search language; every widget
 * is narrowed by the same clause search and the plan would use.
 */
export type Category = "" | "todo" | "in_progress" | "done";
export type DateField = "created" | "updated" | "resolved";

export interface FilterSpec {
  types: string[];
  team: string;
  /** An email address, or UNASSIGNED for issues nobody holds. */
  assignee: string;
  category: Category;
  field: DateField;
  /** How many days back the date field must fall; 0 is any time. */
  days: number;
  /** A milestone's name, which is what the search language matches. */
  milestone: string;
  /** Anything the controls cannot say, in the search language. */
  query: string;
  /** A saved search's id, whose query is read live and ANDed in. */
  filterId: string;
}

/** The filter's text values, as they travel in the address; days is a number and stands apart. */
export const FILTER_TEXT_KEYS = ["types", "team", "assignee", "category", "field", "milestone", "fq", "sf"] as const;

/** The assignee value that means "nobody". */
export const UNASSIGNED = "-";

export const EMPTY_FILTER: FilterSpec = { types: [], team: "", assignee: "", category: "", field: "created", days: 0, milestone: "", query: "", filterId: "" };

const CATEGORIES: Category[] = ["", "todo", "in_progress", "done"];
const FIELDS: DateField[] = ["created", "updated", "resolved"];

/** A name as a query literal: quoted, with the quote and the backslash escaped. */
export function quote(text: string): string {
  return `"${text.replace(/\\/g, "\\\\").replace(/"/g, '\\"')}"`;
}

/** Finds a saved search's query by id; unknown is nothing, not an error. */
export type SavedQueryLookup = (id: string) => string | undefined;

/** The clause every narrowing widget is asked with; empty when nothing is set. A saved search's query is read live through the lookup. */
export function composeQuery(spec: FilterSpec, savedQuery: SavedQueryLookup = () => undefined): string {
  const parts: string[] = [];
  const fromSaved = spec.filterId ? savedQuery(spec.filterId)?.trim() : undefined;
  if (fromSaved) parts.push(`(${fromSaved})`);
  if (spec.types.length === 1) parts.push(`type = ${quote(spec.types[0]!)}`);
  else if (spec.types.length > 1) parts.push(`type IN (${spec.types.map(quote).join(", ")})`);
  if (spec.team) parts.push(`team = ${quote(spec.team)}`);
  if (spec.assignee === UNASSIGNED) parts.push("assignee IS EMPTY");
  else if (spec.assignee) parts.push(`assignee = ${quote(spec.assignee)}`);
  if (spec.category) parts.push(`statusCategory = ${spec.category}`);
  if (spec.days > 0) parts.push(`${spec.field} >= -${spec.days}d`);
  if (spec.milestone) parts.push(`milestone = ${quote(spec.milestone)}`);
  const extra = spec.query.trim();
  if (extra) parts.push(parts.length > 0 ? `(${extra})` : extra);
  return parts.join(" AND ");
}

/** How many of the tile's controls say something, for the badge on the tile. */
export function activeCount(spec: FilterSpec): number {
  return [spec.types.length > 0, spec.team !== "", spec.assignee !== "", spec.category !== "", spec.days > 0, spec.milestone !== "", spec.query.trim() !== "", spec.filterId !== ""].filter(Boolean).length;
}

export function isEmpty(spec: FilterSpec): boolean {
  return activeCount(spec) === 0;
}

/** Reads the filter from the address, ignoring anything that is not one of its values. */
export function parseSearch(search: Record<string, unknown>, fallback: FilterSpec = EMPTY_FILTER): FilterSpec {
  const text = (key: string) => (typeof search[key] === "string" ? (search[key] as string) : undefined);
  const category = text("category");
  const field = text("field");
  const days = Number(search.days);
  const types = text("types");
  const touched = search.days !== undefined || FILTER_TEXT_KEYS.some((key) => text(key) !== undefined);
  if (!touched) return fallback;
  return {
    types: types ? types.split(",").filter(Boolean) : [],
    team: text("team") ?? "",
    assignee: text("assignee") ?? "",
    category: CATEGORIES.includes(category as Category) ? (category as Category) : "",
    field: FIELDS.includes(field as DateField) ? (field as DateField) : "created",
    days: Number.isFinite(days) && days > 0 ? Math.floor(days) : 0,
    milestone: text("milestone") ?? "",
    query: text("fq") ?? "",
    filterId: text("sf") ?? "",
  };
}

/** The filter as it travels in the address. */
export interface FilterSearch {
  types?: string;
  team?: string;
  assignee?: string;
  category?: string;
  field?: string;
  days?: number;
  milestone?: string;
  fq?: string;
  sf?: string;
}

/** The filter as address parameters; days is always written so an emptied filter beats the saved default. */
export function toSearch(spec: FilterSpec): FilterSearch {
  return {
    types: spec.types.length > 0 ? spec.types.join(",") : undefined,
    team: spec.team || undefined,
    assignee: spec.assignee || undefined,
    category: spec.category || undefined,
    field: spec.days > 0 && spec.field !== "created" ? spec.field : undefined,
    days: spec.days,
    milestone: spec.milestone || undefined,
    fq: spec.query.trim() || undefined,
    sf: spec.filterId || undefined,
  };
}
