/**
 * The mapping between dates and pixels. It is kept free of React so the
 * arithmetic behind every bar, tick and drag can be tested on its own.
 */

import { PLAN_MIN_PX_PER_DAY, PLAN_WEEK_LABEL_MIN_PX_PER_DAY } from "@/config";

export const MS_PER_DAY = 86_400_000;
export const DAYS_PER_WEEK = 7;

export interface Zoom {
  id: "fit" | "weeks" | "months" | "quarters";
  label: string;
  /** Pixels per day; zero for the fitted zoom until it has measured. */
  pxPerDay: number;
  /** Which boundaries get a labelled column under the month row. */
  ticks: "week" | "month";
}

/** Fit shows the whole plan in the space it has; the others are fixed densities that scroll. */
export const ZOOM_FIT: Zoom = { id: "fit", label: "Fit", pxPerDay: 0, ticks: "week" };
export const ZOOM_WEEKS: Zoom = { id: "weeks", label: "Weeks", pxPerDay: 22, ticks: "week" };
export const ZOOM_MONTHS: Zoom = { id: "months", label: "Months", pxPerDay: 7, ticks: "week" };
export const ZOOM_QUARTERS: Zoom = { id: "quarters", label: "Quarters", pxPerDay: 2.6, ticks: "month" };

export const ZOOMS: Zoom[] = [ZOOM_FIT, ZOOM_WEEKS, ZOOM_MONTHS, ZOOM_QUARTERS];

/** The zoom with this id, falling back to the closest view rather than nothing. */
export function zoomFor(id: string): Zoom {
  return ZOOMS.find((z) => z.id === id) ?? ZOOM_FIT;
}

/**
 * Resolves the fitted zoom against the width on offer: the whole range fits,
 * down to a floor below which the calendar scrolls instead, and weeks are
 * labelled only while a week is wide enough to carry a label. Any other zoom
 * comes back as it is.
 */
export function fitted(zoom: Zoom, from: Date, to: Date, width: number): Zoom {
  if (zoom.id !== "fit") return zoom;
  const days = Math.max(1, daysBetween(day(from), day(to)));
  const pxPerDay = Math.max(PLAN_MIN_PX_PER_DAY, width / days);
  return { ...zoom, pxPerDay, ticks: pxPerDay >= PLAN_WEEK_LABEL_MIN_PX_PER_DAY ? "week" : "month" };
}

/**
 * The end of a window that is at least as wide as the pane at this zoom. A
 * short plan at a coarse zoom would otherwise be a strip of calendar beside a
 * blank pane; the extra days are added at the end, which is where the future
 * is.
 */
export function filled(from: Date, to: Date, pxPerDay: number, width: number): Date {
  const shown = daysBetween(day(from), day(to));
  const needed = Math.ceil(width / pxPerDay);
  return shown >= needed ? day(to) : addDays(day(from), needed);
}

/** Midnight UTC on the day a value falls in. */
export function day(value: string | number | Date): Date {
  const d = new Date(value);
  return new Date(Date.UTC(d.getUTCFullYear(), d.getUTCMonth(), d.getUTCDate()));
}

export function addDays(from: Date, days: number): Date {
  return new Date(from.getTime() + days * MS_PER_DAY);
}

/** Whole days between two dates, which is what a drag is measured in. */
export function daysBetween(from: Date, to: Date): number {
  return Math.round((day(to).getTime() - day(from).getTime()) / MS_PER_DAY);
}

/** The date part of an ISO timestamp, which is what the API takes back. */
export function isoDay(date: Date): string {
  return day(date).toISOString().slice(0, 10);
}

export interface Scale {
  from: Date;
  /** The exclusive right edge: the first day that is off the end of the view. */
  to: Date;
  pxPerDay: number;
  width: number;
  /** The left edge of a day. */
  x(date: string | number | Date): number;
  /** The date at a pixel offset, snapped to a whole day. */
  dateAt(x: number): Date;
  /** The width of an inclusive range: a one-day task is one day wide. */
  spanWidth(start: string | number | Date, due: string | number | Date): number;
}

export function makeScale(from: Date, to: Date, pxPerDay: number): Scale {
  const start = day(from);
  const end = day(to);
  return {
    from: start,
    to: end,
    pxPerDay,
    width: Math.max(0, daysBetween(start, end) * pxPerDay),
    x: (date) => daysBetween(start, day(date)) * pxPerDay,
    dateAt: (x) => addDays(start, Math.round(x / pxPerDay)),
    spanWidth: (s, d) => Math.max(pxPerDay, (daysBetween(day(s), day(d)) + 1) * pxPerDay),
  };
}

export interface Tick {
  key: string;
  label: string;
  x: number;
  width: number;
  /** Weekend and month-start columns are shaded so the eye can count. */
  emphasis: boolean;
}

const MONTHS = ["Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sep", "Oct", "Nov", "Dec"];

/** The month row across the top of the timeline. */
export function monthTicks(scale: Scale): Tick[] {
  const out: Tick[] = [];
  let cursor = new Date(Date.UTC(scale.from.getUTCFullYear(), scale.from.getUTCMonth(), 1));

  while (cursor < scale.to) {
    const next = new Date(Date.UTC(cursor.getUTCFullYear(), cursor.getUTCMonth() + 1, 1));
    const from = cursor < scale.from ? scale.from : cursor;
    const to = next > scale.to ? scale.to : next;
    out.push({
      key: `${cursor.getUTCFullYear()}-${cursor.getUTCMonth()}`,
      label: `${MONTHS[cursor.getUTCMonth()]} ${cursor.getUTCFullYear()}`,
      x: scale.x(from),
      width: daysBetween(from, to) * scale.pxPerDay,
      emphasis: false,
    });
    cursor = next;
  }
  return out;
}

/**
 * The row under the months. Weeks start on Monday, because a plan is read in
 * working weeks and a week that starts on Sunday splits every one of them.
 */
export function weekTicks(scale: Scale): Tick[] {
  const out: Tick[] = [];
  // getUTCDay counts from Sunday; shifting by one puts Monday at zero.
  const offset = (scale.from.getUTCDay() + DAYS_PER_WEEK - 1) % DAYS_PER_WEEK;
  let cursor = addDays(scale.from, -offset);

  while (cursor < scale.to) {
    const next = addDays(cursor, DAYS_PER_WEEK);
    const from = cursor < scale.from ? scale.from : cursor;
    const to = next > scale.to ? scale.to : next;
    out.push({
      key: isoDay(cursor),
      label: `${cursor.getUTCDate()} ${MONTHS[cursor.getUTCMonth()]}`,
      x: scale.x(from),
      width: daysBetween(from, to) * scale.pxPerDay,
      emphasis: cursor.getUTCDate() <= DAYS_PER_WEEK,
    });
    cursor = next;
  }
  return out;
}

/** The top row when a month is too narrow to label: quarters. */
export function quarterTicks(scale: Scale): Tick[] {
  const out: Tick[] = [];
  const firstMonthOfQuarter = Math.floor(scale.from.getUTCMonth() / MONTHS_PER_QUARTER) * MONTHS_PER_QUARTER;
  let cursor = new Date(Date.UTC(scale.from.getUTCFullYear(), firstMonthOfQuarter, 1));

  while (cursor < scale.to) {
    const next = new Date(Date.UTC(cursor.getUTCFullYear(), cursor.getUTCMonth() + MONTHS_PER_QUARTER, 1));
    const from = cursor < scale.from ? scale.from : cursor;
    const to = next > scale.to ? scale.to : next;
    out.push({
      key: `${cursor.getUTCFullYear()}-q${cursor.getUTCMonth() / MONTHS_PER_QUARTER}`,
      label: `Q${cursor.getUTCMonth() / MONTHS_PER_QUARTER + 1} ${cursor.getUTCFullYear()}`,
      x: scale.x(from),
      width: daysBetween(from, to) * scale.pxPerDay,
      emphasis: false,
    });
    cursor = next;
  }
  return out;
}

const MONTHS_PER_QUARTER = 3;

/** The lower row of the header: the boundaries this zoom can label. */
export function ticksFor(scale: Scale, zoom: Zoom): Tick[] {
  return zoom.ticks === "month" ? monthTicks(scale) : weekTicks(scale);
}

/**
 * The upper row. It has to be coarser than the lower one, or the two rows say
 * the same thing twice.
 */
export function headingTicks(scale: Scale, zoom: Zoom): Tick[] {
  return zoom.ticks === "month" ? quarterTicks(scale) : monthTicks(scale);
}

/** A bar's position, or null when the issue has no range to draw. */
export function barFor(
  scale: Scale,
  start: string | null | undefined,
  due: string | null | undefined,
): { left: number; width: number } | null {
  if (!start || !due) return null;
  return { left: scale.x(start), width: scale.spanWidth(start, due) };
}

/**
 * Where a drag lands. Moving keeps the length and shifts both ends; resizing
 * moves one end and never past the other, so a range cannot turn inside out.
 */
export function applyDrag(
  start: Date,
  due: Date,
  mode: "move" | "start" | "end",
  days: number,
): { start: Date; due: Date } {
  if (days === 0) return { start, due };
  switch (mode) {
    case "move":
      return { start: addDays(start, days), due: addDays(due, days) };
    case "start": {
      const moved = addDays(start, days);
      return { start: moved > due ? due : moved, due };
    }
    case "end": {
      const moved = addDays(due, days);
      return { start, due: moved < start ? start : moved };
    }
  }
}
