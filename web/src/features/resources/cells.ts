import type { ResourceRow, ResourceWeek } from "@/api/resources";
import { RESOURCE_HOUR_DECIMALS } from "@/config";
import { utilisation, type Utilisation } from "@/features/plan/load";
import { number } from "@/features/plan/SprintBands";
import { DAYS_PER_WEEK, addDays, day, isoDay } from "@/features/plan/scale";
import { today } from "@/lib/week";

/** How a week stands against the hours there are, in the plan's four words. */
export function cellState(week: ResourceWeek): Utilisation {
  return utilisation(week.loadHours, week.capacityHours);
}

/** The load, the hours left and the week before days off, as shares of the cell;
 * a row with no hours of its own is measured against its busiest week. */
export function cellShares(row: ResourceRow, week: ResourceWeek): { load: number; capacity?: number; nominal?: number } {
  const nominal = week.nominalHours ?? week.capacityHours;
  const ceiling = nominal !== undefined ? Math.max(nominal, week.loadHours) : Math.max(1, ...row.weeks.map((w) => w.loadHours));
  if (ceiling <= 0) return { load: 0, capacity: 0, nominal: 0 };
  const share = (v: number) => Math.min(1, v / ceiling);
  return {
    load: share(week.loadHours),
    capacity: week.capacityHours === undefined ? undefined : share(week.capacityHours),
    nominal: nominal === undefined ? undefined : share(nominal),
  };
}

const hourScale = 10 ** RESOURCE_HOUR_DECIMALS;

/** Writes hours to the view's precision, without trailing zeros: 9.8 rather than 9.79 or 9.80. */
export function hours(value: number): string {
  return String(Math.round(value * hourScale) / hourScale);
}

/** What a week says on hover: "32 of 40 h; 1 day away; 1 holiday", and the share of the week when it is not all of it. */
export function describeHours(week: ResourceWeek, sharePercent?: number): string {
  const share = sharePercent !== undefined ? ` (${sharePercent}% of the week)` : "";
  const parts = [week.capacityHours !== undefined ? `${hours(week.loadHours)} of ${hours(week.capacityHours)} h${share}` : `${hours(week.loadHours)} h`];
  if (week.daysAway) parts.push(`${number(week.daysAway)} day${week.daysAway === 1 ? "" : "s"} away`);
  if (week.holidays) parts.push(`${week.holidays} holiday${week.holidays === 1 ? "" : "s"}`);
  return parts.join("; ");
}

/** The cell's whole title: whose week, which week, and its hours. */
export function describeCell(row: ResourceRow, week: ResourceWeek): string {
  return `${row.name}, week of ${weekLabel(week.start)}: ${describeHours(week, row.sharePercent)}`;
}

/** A week's column heading: its Monday, as "3 Mar". */
export function weekLabel(start: string): string {
  return day(start).toLocaleDateString(undefined, { day: "numeric", month: "short", timeZone: "UTC" });
}

/**
 * The Monday of the reader's week, as the API writes days. It is taken from
 * their local date, because their Monday starts at their midnight, not UTC's.
 */
export function mondayOf(date: Date): string {
  const d = day(today(date));
  const back = (d.getUTCDay() + DAYS_PER_WEEK - 1) % DAYS_PER_WEEK;
  return isoDay(addDays(d, -back));
}

/** A window of whole weeks from a Monday: its first and last day. */
export function windowFrom(monday: string, weeks: number): { from: string; to: string } {
  return { from: monday, to: isoDay(addDays(day(monday), weeks * DAYS_PER_WEEK - 1)) };
}

/** A Monday some weeks before or after another. */
export function shiftWeeks(monday: string, weeks: number): string {
  return isoDay(addDays(day(monday), weeks * DAYS_PER_WEEK));
}

/** Whether there is anybody to plan against: a team, or a person, beside the unassigned work. */
export function hasPlannedRows(rows: ResourceRow[]): boolean {
  return rows.some((row) => row.kind !== "unassigned");
}
