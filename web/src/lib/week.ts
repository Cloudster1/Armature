import { ABSENCE_MAX_DAYS, HOURS_PER_DAY, MINUTES_PER_HOUR, MS_PER_DAY, WEEKDAY_REFERENCE_MONDAY, WORKING_HOURS_DECIMALS } from "@/config";
import { WEEKDAYS, type Absence, type Weekday } from "@/api/availability";

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

/** Today as YYYY-MM-DD where the reader is, which is the day they mean by today. */
export function today(now: Date = new Date()): string {
  const pad = (n: number) => String(n).padStart(2, "0");
  return `${now.getFullYear()}-${pad(now.getMonth() + 1)}-${pad(now.getDate())}`;
}

/** What is wrong with an absence before it is sent, as a sentence; null when nothing is. */
export function absenceProblem(startsOn: string, endsOn: string, halfDay: boolean): string | null {
  if (!startsOn) return "Choose the first day away.";
  if (endsOn < startsOn) return "The last day is before the first. Swap them.";
  const days = (Date.parse(`${endsOn}T00:00:00Z`) - Date.parse(`${startsOn}T00:00:00Z`)) / MS_PER_DAY;
  if (days >= ABSENCE_MAX_DAYS) return `One absence runs at most ${ABSENCE_MAX_DAYS} days. Record a longer one in parts.`;
  if (halfDay && endsOn !== startsOn) return "Only a single day can be a half day. Make the last day the first, or untick half day.";
  return null;
}

/** An absence's days in the reader's language: one day, half of one, or a first and a last. */
export function formatAbsence(absence: Pick<Absence, "startsOn" | "endsOn" | "halfDay">, locale: string): string {
  const first = formatDay(absence.startsOn, locale);
  if (absence.startsOn !== absence.endsOn) return `${first} to ${formatDay(absence.endsOn, locale)}`;
  return absence.halfDay ? `${first}, half the day` : first;
}
