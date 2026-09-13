import type { CalendarItem } from "@/api/calendar";
import { CALENDAR_MAX_ITEMS_PER_DAY } from "@/config";

/**
 * The geometry of a month: which days the grid shows, and what lies on each.
 * Kept apart from the drawing so it can be tested as dates and numbers.
 */

/** A day as the API writes it, YYYY-MM-DD, without a time zone to get wrong. */
export function isoDay(d: Date): string {
  return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, "0")}-${String(d.getDate()).padStart(2, "0")}`;
}

/** Reads YYYY-MM-DD as a local date at midnight. */
export function parseDay(s: string): Date {
  const [y, m, d] = s.split("-").map(Number);
  return new Date(y!, (m ?? 1) - 1, d ?? 1);
}

/** The day some days later, in the same YYYY-MM-DD form. */
export function shiftDay(s: string, days: number): string {
  const d = parseDay(s);
  d.setDate(d.getDate() + days);
  return isoDay(d);
}

/** Whole days from one day to another; negative when b is earlier. */
export function daysBetween(a: string, b: string): number {
  return Math.round((parseDay(b).getTime() - parseDay(a).getTime()) / 86_400_000);
}

export interface Cell {
  day: string;
  /** Whether the day belongs to the month being shown, or pads the first or last week. */
  inMonth: boolean;
  weekend: boolean;
}

/** The six weeks a month grid draws, Monday first, padded from the neighbours. */
export function monthGrid(year: number, month: number): Cell[] {
  const first = new Date(year, month - 1, 1);
  // Monday is 0 here; JavaScript's Sunday is 0.
  const lead = (first.getDay() + 6) % 7;
  const start = new Date(year, month - 1, 1 - lead);
  const cells: Cell[] = [];
  for (let i = 0; i < 42; i++) {
    const d = new Date(start.getFullYear(), start.getMonth(), start.getDate() + i);
    cells.push({ day: isoDay(d), inMonth: d.getMonth() === month - 1, weekend: d.getDay() === 0 || d.getDay() === 6 });
  }
  return cells;
}

/** YYYY-MM for the month some months away. */
export function shiftMonth(year: number, month: number, by: number): { year: number; month: number } {
  const d = new Date(year, month - 1 + by, 1);
  return { year: d.getFullYear(), month: d.getMonth() + 1 };
}

/** The month's name and year, as the header says it. */
export function monthTitle(year: number, month: number): string {
  return new Date(year, month - 1, 1).toLocaleDateString(undefined, { month: "long", year: "numeric" });
}

export interface Placed {
  item: CalendarItem;
  /** Whether this day is the item's first, last, or one in between; a bar is drawn from these. */
  starts: boolean;
  ends: boolean;
}

export interface DayItems {
  shown: Placed[];
  /** How many more lie on the day than the cell has room for. */
  more: number;
}

/**
 * What lies on each day of the grid. A range lies on every day it covers, and
 * the ranges come before the single days so bars line up across cells. Past
 * the room a cell has, the rest is counted rather than drawn.
 */
export function placeItems(cells: Cell[], items: CalendarItem[], max: number = CALENDAR_MAX_ITEMS_PER_DAY): Map<string, DayItems> {
  const sorted = [...items].sort((a, b) => {
    const spanA = daysBetween(a.from, a.to);
    const spanB = daysBetween(b.from, b.to);
    if (spanA !== spanB) return spanB - spanA;
    if (a.from !== b.from) return a.from.localeCompare(b.from);
    return a.title.localeCompare(b.title);
  });
  const byDay = new Map<string, DayItems>();
  for (const cell of cells) {
    const onDay = sorted.filter((it) => it.from <= cell.day && it.to >= cell.day);
    const placed = onDay.map((item) => ({ item, starts: item.from === cell.day, ends: item.to === cell.day }));
    byDay.set(cell.day, { shown: placed.slice(0, max), more: Math.max(0, placed.length - max) });
  }
  return byDay;
}

/** Where an issue lands when its bar is dragged by some days: both ends move together. */
export function draggedSchedule(item: CalendarItem, toDay: string, grabbedDay: string): { startDate: string; dueDate: string } {
  const by = daysBetween(grabbedDay, toDay);
  return { startDate: shiftDay(item.from, by), dueDate: shiftDay(item.to, by) };
}
