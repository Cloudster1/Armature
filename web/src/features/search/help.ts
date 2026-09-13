/**
 * What a query can say, written once for the help panel and the tests. The
 * server's catalog is the authority; this is its description for a reader.
 */
export interface QueryField {
  name: string;
  takes: string;
  example: string;
}

export const QUERY_FIELDS: QueryField[] = [
  { name: "project", takes: "a project key", example: "project = EVR" },
  { name: "key", takes: "an issue key", example: "key IN (EVR-1, EVR-2)" },
  { name: "summary", takes: "words, with ~", example: 'summary ~ "reset email"' },
  { name: "description", takes: "words, with ~", example: "description ~ smtp" },
  { name: "text", takes: "words in the summary or the description", example: "text ~ login" },
  { name: "type", takes: "an issue type's name", example: "type = Bug" },
  { name: "status", takes: "a status name", example: 'status IN ("To Do", "In Progress")' },
  { name: "statusCategory", takes: "todo, in_progress or done", example: "statusCategory != done" },
  { name: "priority", takes: "lowest to highest, in that order", example: "priority >= high" },
  { name: "assignee", takes: "an email, a name or currentUser()", example: "assignee = currentUser()" },
  { name: "reporter", takes: "an email, a name or currentUser()", example: "reporter = ada@armature.test" },
  { name: "parent", takes: "an issue key", example: "parent = EVR-7" },
  { name: "sprint", takes: "a sprint name or openSprints()", example: "sprint IN (openSprints())" },
  { name: "team", takes: "a team name", example: "team = Alpha" },
  { name: "milestone", takes: "a milestone name", example: 'milestone = "1.0"' },
  { name: "labels", takes: "label names", example: "labels IN (backend, security)" },
  { name: "fixVersion", takes: "a version name, unreleasedVersions() or releasedVersions()", example: "fixVersion IN (unreleasedVersions())" },
  { name: "affectsVersion", takes: "a version name, unreleasedVersions() or releasedVersions()", example: 'affectsVersion = "1.2"' },
  { name: "component", takes: "a component name", example: "component = Billing" },
  { name: "estimate", takes: "a number", example: "estimate > 3" },
  { name: "created", takes: "a date, a duration or a date function", example: "created >= -7d" },
  { name: "updated", takes: "a date, a duration or a date function", example: "updated >= startOfWeek()" },
  { name: "resolved", takes: "a date, a duration or a date function", example: "resolved >= startOfMonth(-1w)" },
  { name: "due", takes: "a day", example: "due <= endOfWeek()" },
  { name: "start", takes: "a day", example: "start >= 2026-09-01" },
];

export const QUERY_CUSTOM_FIELD_EXAMPLE = '"Customer" = Globex';

export const QUERY_OPERATORS = ["=", "!=", "<", "<=", ">", ">=", "~", "!~", "IN", "NOT IN", "IS EMPTY", "IS NOT EMPTY"];

export const QUERY_FUNCTIONS = [
  "currentUser()",
  "openSprints()",
  "unreleasedVersions()",
  "releasedVersions()",
  "now()",
  "startOfDay()",
  "endOfDay()",
  "startOfWeek()",
  "endOfWeek()",
  "startOfMonth()",
  "endOfMonth()",
];

export const QUERY_EXAMPLES = [
  "assignee = currentUser() AND statusCategory != done ORDER BY priority DESC",
  'project = EVR AND labels = backend AND created >= startOfWeek(-1w)',
  "(type = Bug OR priority >= high) AND sprint IN (openSprints())",
  'resolved >= -7d ORDER BY resolved DESC',
];

/**
 * A line of spaces with a caret under the character an error points at, so
 * the query is shown exactly as it was typed with the trouble marked.
 */
export function caretLine(position: number | undefined): string {
  if (!position || position < 1) return "";
  return " ".repeat(position - 1) + "^";
}
