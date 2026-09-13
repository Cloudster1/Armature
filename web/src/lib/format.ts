import { useMemo } from "react";
import { useMe } from "@/api/auth";

/** How the reader wants time written: their profile's zone and language, else the browser's. */
export interface Format {
  date: (iso: string) => string;
  dateTime: (iso: string) => string;
  time: (iso: string) => string;
  relative: (iso: string, now?: number) => string;
  number: (value: number) => string;
  timezone: string;
  locale: string;
}

const MINUTE = 60;
const HOUR = 60 * MINUTE;
const DAY = 24 * HOUR;
const MONTH = 30 * DAY;

// The short forms the interface always used, kept: a list column reads "2h
// ago", not "2 hours ago". Anything older than a month is a date in the
// reader's language.
export function relativeIn(iso: string, locale: string, timezone: string, now = Date.now()): string {
  const then = new Date(iso).getTime();
  const seconds = Math.round((now - then) / 1000);
  if (seconds < MINUTE) return "just now";
  const minutes = Math.round(seconds / MINUTE);
  if (minutes < 60) return `${minutes}m ago`;
  const hours = Math.round(seconds / HOUR);
  if (hours < 24) return `${hours}h ago`;
  const days = Math.round(seconds / DAY);
  if (seconds < MONTH) return `${days}d ago`;
  return new Intl.DateTimeFormat(locale, { dateStyle: "medium", timeZone: timezone }).format(new Date(iso));
}

export function makeFormat(locale: string, timezone: string): Format {
  const safe = (options: Intl.DateTimeFormatOptions) => {
    try {
      return new Intl.DateTimeFormat(locale, { ...options, timeZone: timezone });
    } catch {
      return new Intl.DateTimeFormat(undefined, options);
    }
  };
  const date = safe({ dateStyle: "medium" });
  const dateTime = safe({ dateStyle: "medium", timeStyle: "short" });
  const time = safe({ timeStyle: "short" });
  const number = new Intl.NumberFormat(locale);
  return {
    date: (iso) => date.format(new Date(iso)),
    dateTime: (iso) => dateTime.format(new Date(iso)),
    time: (iso) => time.format(new Date(iso)),
    relative: (iso, now) => relativeIn(iso, locale, timezone, now),
    number: (value) => number.format(value),
    timezone,
    locale,
  };
}

/** The reader's formatting, from their profile; before it loads, the browser's. */
export function useFormat(): Format {
  const { data } = useMe();
  const locale = data?.principal?.user.locale || navigator.language;
  const timezone = data?.principal?.user.timezone || Intl.DateTimeFormat().resolvedOptions().timeZone;
  return useMemo(() => makeFormat(locale, timezone), [locale, timezone]);
}
