import type { Issue } from "@/api/issues";
import type { PlanItem, TeamLoad } from "@/api/plan";
import type { SprintPlan } from "@/api/sprints";
import { MS_PER_DAY } from "./scale";

/**
 * The plan is one set of data read several ways. A view decides which rows are
 * drawn and how they are grouped; the calendar, the bands and the flags are the
 * same underneath, so switching views never changes what a date means.
 */
export type PlanView = "management" | "sprints" | "dependencies";

export const PLAN_VIEWS: Array<{ id: PlanView; label: string; hint: string }> = [
  {
    id: "management",
    label: "Timeline",
    hint: "Every issue in its hierarchy, with the milestones and the numbers.",
  },
  {
    id: "sprints",
    label: "Sprints",
    hint: "The work by iteration: each sprint with what is committed to it, then the backlog.",
  },
  {
    id: "dependencies",
    label: "Dependencies",
    hint: "Which tickets wait on which, laid out left to right.",
  },
];

export function viewFor(id: string | undefined): PlanView {
  return PLAN_VIEWS.some((view) => view.id === id) ? (id as PlanView) : "management";
}

/** What the management view narrows the rows down to. */
export interface Filters {
  /** Issue type names; empty is every type. */
  types: string[];
  /** A milestone id; empty is any. */
  milestoneId: string;
  /** A team id; empty is any team, and the work no team carries. */
  teamId: string;
}

export const NO_FILTERS: Filters = { types: [], milestoneId: "", teamId: "" };

/**
 * What every view cuts before it draws: work that has been done for longer
 * than the reader cares about, and, with a query, what the query left out.
 */
export interface Cut {
  /** Days a done ticket stays; null keeps everything, 0 cuts all that is done. */
  closedForDays: number | null;
  /** The keys a query matched; null when there is no query. */
  matched: Set<string> | null;
}

export const NO_CUT: Cut = { closedForDays: null, matched: null };

/**
 * When a done ticket was closed. The resolved time is set by a workflow rule,
 * so a workflow without that rule leaves it empty; the last change stands in.
 */
export function closedAt(issue: Issue): Date | null {
  if (issue.status.category !== "done") return null;
  return new Date(issue.resolvedAt ?? issue.updatedAt);
}

function survives(item: PlanItem, cut: Cut, now: Date): boolean {
  if (cut.matched && !cut.matched.has(item.issue.key)) return false;
  if (cut.closedForDays === null) return true;
  const closed = closedAt(item.issue);
  if (!closed) return true;
  return now.getTime() - closed.getTime() <= cut.closedForDays * MS_PER_DAY;
}

/** Applies the cut, keeping the rows above a survivor the way filters do. */
export function cutItems(items: PlanItem[], cut: Cut, now: Date): PlanItem[] {
  if (cut.closedForDays === null && cut.matched === null) return items;
  return keepWhere(items, (item) => survives(item, cut, now));
}

export function flatten(items: PlanItem[]): PlanItem[] {
  const out: PlanItem[] = [];
  for (const item of items) {
    out.push(item, ...flatten(item.children));
  }
  return out;
}

function matches(item: PlanItem, filters: Filters): boolean {
  if (filters.types.length > 0 && !filters.types.includes(item.issue.type.name)) return false;
  if (filters.milestoneId && item.issue.milestoneId !== filters.milestoneId) return false;
  if (filters.teamId && item.issue.teamId !== filters.teamId) return false;
  return true;
}

/**
 * Keeps the rows that match and the rows above them, so a filtered story is
 * still shown under its epic. A parent that matches on its own keeps only the
 * children that match too; "bugs" means bugs, not bugs and their subtasks.
 */
export function filterItems(items: PlanItem[], filters: Filters): PlanItem[] {
  if (filters === NO_FILTERS) return items;
  return keepWhere(items, (item) => matches(item, filters));
}

function keepWhere(items: PlanItem[], keep: (item: PlanItem) => boolean): PlanItem[] {
  const out: PlanItem[] = [];
  for (const item of items) {
    const children = keepWhere(item.children, keep);
    if (keep(item) || children.length > 0) {
      out.push({ ...item, children });
    }
  }
  return out;
}

/** The numbers a manager reads off the plan before looking at any one row. */
export interface Meter {
  total: number;
  scheduled: number;
  done: number;
  inProgress: number;
  todo: number;
  /** Estimate points, counting only what carries its own estimate. */
  points: number;
  donePercent: number;
  byType: Array<{ name: string; icon: string; count: number }>;
}

export function meter(items: PlanItem[]): Meter {
  const all = flatten(items);
  const byType = new Map<string, { name: string; icon: string; count: number }>();
  const out: Meter = { total: all.length, scheduled: 0, done: 0, inProgress: 0, todo: 0, points: 0, donePercent: 0, byType: [] };
  for (const item of all) {
    if (item.start && item.due) out.scheduled++;
    switch (item.issue.status.category) {
      case "done":
        out.done++;
        break;
      case "in_progress":
        out.inProgress++;
        break;
      default:
        out.todo++;
    }
    if (item.estimate !== undefined && !item.estimateDerived) out.points += item.estimate;
    const type = byType.get(item.issue.type.name) ?? { name: item.issue.type.name, icon: item.issue.type.icon, count: 0 };
    type.count++;
    byType.set(type.name, type);
  }
  out.donePercent = out.total ? Math.floor((out.done / out.total) * 100) : 0;
  out.byType = [...byType.values()].sort((a, b) => b.count - a.count || a.name.localeCompare(b.name));
  return out;
}

/** One visible line of the plan. */
export type Row =
  | { kind: "issue"; key: string; item: PlanItem; hasChildren: boolean; collapsed: boolean }
  | {
      kind: "group";
      key: string;
      label: string;
      /** Absent for the backlog, which is the work no sprint has taken on. */
      sprint: SprintPlan | null;
      count: number;
      /** Said instead of the count, for a group that is not a set of issues. */
      detail?: string;
      collapsed: boolean;
    }
  | { kind: "load"; key: string; row: TeamLoad }
  | {
      /** A line to make a ticket on, under the group it will land in. */
      kind: "add";
      key: string;
      label: string;
      group: Extract<Row, { kind: "group" }> | null;
    };

export const BACKLOG_KEY = "sprint:backlog";

/** The key of the management view's own add row. */
export const PLAN_ADD_KEY = "add:plan";

/** The hierarchy, roots first, with collapsed branches dropped. */
export function hierarchyRows(items: PlanItem[], collapsed: Set<string>): Row[] {
  const out: Row[] = [];
  for (const item of items) {
    const isCollapsed = collapsed.has(item.issue.key);
    out.push({ kind: "issue", key: item.issue.key, item, hasChildren: item.children.length > 0, collapsed: isCollapsed });
    if (!isCollapsed) out.push(...hierarchyRows(item.children, collapsed));
  }
  return out;
}

/**
 * The work by sprint: a group per sprint in the order the sprints run, each
 * followed by what is committed to it, and the backlog last. Rows are flat
 * here; the hierarchy is the other view's business. An issue that only holds
 * others is not backlog: the backlog is work waiting to be taken on, and an
 * epic is taken on through its stories.
 */
export function sprintRows(items: PlanItem[], sprints: SprintPlan[], collapsed: Set<string>): Row[] {
  const all = flatten(items)
    .filter((item) => item.children.length === 0 || item.issue.sprintId)
    .map((item) => ({ ...item, depth: 0, children: [] }));
  const out: Row[] = [];
  const group = (key: string, label: string, sprint: SprintPlan | null, members: PlanItem[]) => {
    const isCollapsed = collapsed.has(key);
    const head: Row = { kind: "group", key, label, sprint, count: members.length, collapsed: isCollapsed };
    out.push(head);
    if (!isCollapsed) {
      for (const item of members) {
        out.push({ kind: "issue", key: item.issue.key, item, hasChildren: false, collapsed: false });
      }
      // Each open group ends with a line to add to it, so a ticket is made
      // where it will sit.
      out.push({ kind: "add", key: `add:${key}`, label, group: head as Extract<Row, { kind: "group" }> });
    }
  };
  for (const plan of sprints) {
    group(`sprint:${plan.sprint.id}`, plan.sprint.name, plan, all.filter((item) => item.issue.sprintId === plan.sprint.id));
  }
  const known = new Set(sprints.map((plan) => plan.sprint.id));
  group(BACKLOG_KEY, "Backlog", null, all.filter((item) => !item.issue.sprintId || !known.has(item.issue.sprintId)));
  return out;
}

export function rowsFor(view: PlanView, items: PlanItem[], sprints: SprintPlan[], collapsed: Set<string>): Row[] {
  if (view === "sprints") return sprintRows(items, sprints, collapsed);
  return [...hierarchyRows(items, collapsed), { kind: "add", key: PLAN_ADD_KEY, label: "plan", group: null }];
}
