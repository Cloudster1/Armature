import { t } from "@/i18n";

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
  {
    name: "project",
    get takes() {
      return t.search.help.fields.project!;
    },
    example: "project = EVR",
  },
  {
    name: "key",
    get takes() {
      return t.search.help.fields.key!;
    },
    example: "key IN (EVR-1, EVR-2)",
  },
  {
    name: "summary",
    get takes() {
      return t.search.help.fields.summary!;
    },
    example: 'summary ~ "reset email"',
  },
  {
    name: "description",
    get takes() {
      return t.search.help.fields.description!;
    },
    example: "description ~ smtp",
  },
  {
    name: "text",
    get takes() {
      return t.search.help.fields.text!;
    },
    example: "text ~ login",
  },
  {
    name: "type",
    get takes() {
      return t.search.help.fields.type!;
    },
    example: "type = Bug",
  },
  {
    name: "status",
    get takes() {
      return t.search.help.fields.status!;
    },
    example: 'status IN ("To Do", "In Progress")',
  },
  {
    name: "statusCategory",
    get takes() {
      return t.search.help.fields.statusCategory!;
    },
    example: "statusCategory != done",
  },
  {
    name: "priority",
    get takes() {
      return t.search.help.fields.priority!;
    },
    example: "priority >= high",
  },
  {
    name: "assignee",
    get takes() {
      return t.search.help.fields.assignee!;
    },
    example: "assignee = currentUser()",
  },
  {
    name: "reporter",
    get takes() {
      return t.search.help.fields.reporter!;
    },
    example: "reporter = ada@armature.test",
  },
  {
    name: "watcher",
    get takes() {
      return t.search.help.fields.watcher!;
    },
    example: "watcher = currentUser()",
  },
  {
    name: "parent",
    get takes() {
      return t.search.help.fields.parent!;
    },
    example: "parent = EVR-7",
  },
  {
    name: "sprint",
    get takes() {
      return t.search.help.fields.sprint!;
    },
    example: "sprint IN (openSprints())",
  },
  {
    name: "team",
    get takes() {
      return t.search.help.fields.team!;
    },
    example: "team = Alpha",
  },
  {
    name: "milestone",
    get takes() {
      return t.search.help.fields.milestone!;
    },
    example: 'milestone = "1.0"',
  },
  {
    name: "company",
    get takes() {
      return t.search.help.fields.company!;
    },
    example: "company = Globex",
  },
  {
    name: "labels",
    get takes() {
      return t.search.help.fields.labels!;
    },
    example: "labels IN (backend, security)",
  },
  {
    name: "fixVersion",
    get takes() {
      return t.search.help.fields.fixVersion!;
    },
    example: "fixVersion IN (unreleasedVersions())",
  },
  {
    name: "affectsVersion",
    get takes() {
      return t.search.help.fields.affectsVersion!;
    },
    example: 'affectsVersion = "1.2"',
  },
  {
    name: "component",
    get takes() {
      return t.search.help.fields.component!;
    },
    example: "component = Billing",
  },
  {
    name: "estimate",
    get takes() {
      return t.search.help.fields.estimate!;
    },
    example: "estimate > 3",
  },
  {
    name: "created",
    get takes() {
      return t.search.help.fields.created!;
    },
    example: "created >= -7d",
  },
  {
    name: "updated",
    get takes() {
      return t.search.help.fields.updated!;
    },
    example: "updated >= startOfWeek()",
  },
  {
    name: "resolved",
    get takes() {
      return t.search.help.fields.resolved!;
    },
    example: "resolved >= startOfMonth(-1w)",
  },
  {
    name: "due",
    get takes() {
      return t.search.help.fields.due!;
    },
    example: "due <= endOfWeek()",
  },
  {
    name: "start",
    get takes() {
      return t.search.help.fields.start!;
    },
    example: "start >= 2026-09-01",
  },
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
  "project = EVR AND labels = backend AND created >= startOfWeek(-1w)",
  "(type = Bug OR priority >= high) AND sprint IN (openSprints())",
  "resolved >= -7d ORDER BY resolved DESC",
];

/**
 * A line of spaces with a caret under the character an error points at, so
 * the query is shown exactly as it was typed with the trouble marked.
 */
export function caretLine(position: number | undefined): string {
  if (!position || position < 1) return "";
  return " ".repeat(position - 1) + "^";
}
