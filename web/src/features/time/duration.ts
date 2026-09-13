/**
 * Time the way people write it on a ticket: 2h 30m, 1d, 45m, 1.5h. A day is
 * eight working hours, which is what a day of work means here, and matches
 * the server's own formatting of the changelog.
 */

export const MINUTES_PER_HOUR = 60;
export const HOURS_PER_DAY = 8;
export const MINUTES_PER_DAY = MINUTES_PER_HOUR * HOURS_PER_DAY;

const PART = /(\d+(?:\.\d+)?)\s*([dhm])/g;

/**
 * Parses typed time into minutes. Bare numbers are hours, because "2" on a
 * time field means two hours to anyone who has filled one in. Returns null
 * for text that is not time, and 0 for an empty box.
 */
export function parseDuration(text: string): number | null {
  const trimmed = text.trim().toLowerCase();
  if (trimmed === "") return 0;
  if (/^\d+(\.\d+)?$/.test(trimmed)) return Math.round(Number(trimmed) * MINUTES_PER_HOUR);
  let minutes = 0;
  let consumed = "";
  for (const match of trimmed.matchAll(PART)) {
    const amount = Number(match[1]);
    const unit = match[2];
    minutes += amount * (unit === "d" ? MINUTES_PER_DAY : unit === "h" ? MINUTES_PER_HOUR : 1);
    consumed += match[0];
  }
  // Anything left over that is not whitespace was not time.
  if (consumed === "" || trimmed.replace(/\s+/g, "") !== consumed.replace(/\s+/g, "")) return null;
  return Math.round(minutes);
}

/** Minutes as people say them: 2h 30m, 1d 1h, 45m. Zero is "0m". */
export function formatDuration(minutes: number | undefined | null): string {
  if (minutes === undefined || minutes === null) return "";
  if (minutes <= 0) return "0m";
  const days = Math.floor(minutes / MINUTES_PER_DAY);
  const hours = Math.floor((minutes % MINUTES_PER_DAY) / MINUTES_PER_HOUR);
  const rest = minutes % MINUTES_PER_HOUR;
  const parts: string[] = [];
  if (days) parts.push(`${days}d`);
  if (hours) parts.push(`${hours}h`);
  if (rest) parts.push(`${rest}m`);
  return parts.join(" ");
}

/** How far along the time is, for a bar: spent against spent plus remaining. */
export function timeProgress(spent: number, remaining: number | undefined, estimate: number | undefined): number {
  const total = remaining !== undefined ? spent + remaining : estimate ?? 0;
  if (total <= 0) return spent > 0 ? 1 : 0;
  return Math.min(1, spent / total);
}
