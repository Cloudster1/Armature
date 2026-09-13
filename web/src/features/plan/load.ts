import type { Load, LoadWeek, TeamLoad } from "@/api/plan";
import { PLAN_LOAD_FULL_RATIO } from "@/config";
import { DAYS_PER_WEEK, isoDay, type Scale } from "./scale";
import { number } from "./SprintBands";
import type { Filters, Row } from "./views";

/** The key of the collapsible group the load rows sit under. */
export const LOAD_GROUP_KEY = "load";

/**
 * The load rows for the bottom of the management view: a team each, the work
 * no team carries, and the total, under one collapsible group. A team filter
 * narrows them to that team's row, since the total would then say nothing
 * about what is shown.
 */
export function loadRows(load: Load, filters: Filters, collapsed: Set<string>): Row[] {
  const rows = filters.teamId ? load.rows.filter((row) => row.teamId === filters.teamId) : load.rows;
  if (rows.length === 0 || load.weeks.length === 0) return [];
  const isCollapsed = collapsed.has(LOAD_GROUP_KEY);
  const out: Row[] = [
    {
      kind: "group",
      key: LOAD_GROUP_KEY,
      label: "Load by team",
      sprint: null,
      count: rows.length,
      detail: "points per week against capacity",
      collapsed: isCollapsed,
    },
  ];
  if (!isCollapsed) {
    for (const row of rows) out.push({ kind: "load", key: `load:${row.team}`, row });
  }
  return out;
}

export type Utilisation = "unmeasured" | "under" | "full" | "over";

/** How a week stands against its capacity, in the four words the colours mean. */
export function utilisation(load: number, capacity?: number): Utilisation {
  if (capacity === undefined) return "unmeasured";
  if (load > capacity) return "over";
  if (load >= capacity * PLAN_LOAD_FULL_RATIO && capacity > 0) return "full";
  return "under";
}

/** Where each week of a row sits on the calendar, clipped to the canvas. */
export function weekCells(scale: Scale, row: TeamLoad): Array<{ key: string; x: number; width: number; week: LoadWeek }> {
  const out: Array<{ key: string; x: number; width: number; week: LoadWeek }> = [];
  for (const week of row.weeks) {
    const start = new Date(week.start);
    const rawX = scale.x(start);
    const x = Math.max(0, rawX);
    const right = Math.min(scale.width, rawX + DAYS_PER_WEEK * scale.pxPerDay);
    if (right <= x) continue;
    out.push({ key: isoDay(start), x, width: right - x, week });
  }
  return out;
}

/** How tall a week's bar is, as a share of the row: against capacity where there is one, else against the row's busiest week. */
export function barShare(row: TeamLoad, week: LoadWeek): number {
  const ceiling = week.capacity !== undefined ? Math.max(week.capacity, week.load) : Math.max(1, ...row.weeks.map((w) => w.load));
  if (ceiling <= 0) return 0;
  return Math.min(1, week.load / ceiling);
}

/** What the sidebar says about a row: its capacity, or that none was set, and what is off the calendar. */
export function describeLoad(row: TeamLoad): string {
  const parts = [row.weeklyCapacity !== undefined ? `${number(row.weeklyCapacity)} pts/week` : "no capacity set"];
  if (row.unscheduled > 0) parts.push(`${row.unscheduled} unscheduled`);
  return parts.join(" · ");
}

/** What a week's cell says on hover. */
export function describeWeek(row: TeamLoad, week: LoadWeek): string {
  const load = week.capacity !== undefined ? `${number(week.load)}/${number(week.capacity)} pts` : `${number(week.load)} pts`;
  const extra = week.unestimated > 0 ? `, ${week.unestimated} unestimated` : "";
  return `${row.team}, week of ${isoDay(new Date(week.start))}: ${load} over ${week.issues} issue${week.issues === 1 ? "" : "s"}${extra}`;
}
