import { HOURS_PER_DAY, MINUTES_PER_HOUR, WEEKDAY_REFERENCE_MONDAY, WORKING_HOURS_DECIMALS } from "@/config";
import { WEEKDAYS, type Weekday } from "@/api/availability";

/** Minutes as the hours a person types: 480 is "8", 450 is "7.5". */
export function hoursText(minutes: number): string {
  return String(Number((minutes / MINUTES_PER_HOUR).toFixed(WORKING_HOURS_DECIMALS)));
}

/** Hours typed for a day as minutes; empty is a day off, and anything a day cannot hold is null. */
export function minutesFromHours(text: string): number | null {
  const trimmed = text.trim().replace(",", ".");
  if (trimmed === "") return 0;
  const hours = Number(trimmed);
  if (!Number.isFinite(hours) || hours < 0 || hours > HOURS_PER_DAY) return null;
  return Math.round(hours * MINUTES_PER_HOUR);
}

/** The minutes a whole week holds. */
export function weekTotal(minutes: Record<string, number>): number {
  return WEEKDAYS.reduce((sum, day) => sum + (minutes[day] ?? 0), 0);
}

/** A weekday's name in the reader's language, counted from a known Monday. */
export function weekdayName(day: Weekday, locale: string, style: "long" | "short" = "long"): string {
  const date = new Date(`${WEEKDAY_REFERENCE_MONDAY}T00:00:00Z`);
  date.setUTCDate(date.getUTCDate() + WEEKDAYS.indexOf(day));
  try {
    return new Intl.DateTimeFormat(locale, { weekday: style, timeZone: "UTC" }).format(date);
  } catch {
    return day;
  }
}

/** A holiday's YYYY-MM-DD in the reader's language. It is a date, not an instant, so no zone moves it. */
export function formatDay(day: string, locale: string): string {
  const date = new Date(`${day}T00:00:00Z`);
  if (Number.isNaN(date.getTime())) return day;
  try {
    return new Intl.DateTimeFormat(locale, { dateStyle: "medium", timeZone: "UTC" }).format(date);
  } catch {
    return day;
  }
}
