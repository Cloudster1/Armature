import { useCallback, useState } from "react";
import { PLAN_DEFAULT_CLOSED_FOR_DAYS } from "@/config";

const STORAGE_KEY = "armature.plan.closedForDays";

/** Marks "show everything" in storage, since a missing key means the default. */
const EVERYTHING = "all";

/**
 * How long done work stays on the plan, as the reader last said. Kept in the
 * browser like the theme: it is a way of looking, not a fact about the project.
 */
export function readClosedForDays(): number | null {
  try {
    const value = localStorage.getItem(STORAGE_KEY);
    if (value === EVERYTHING) return null;
    if (value !== null && /^\d+$/.test(value)) return Number(value);
  } catch {
    // Storage that cannot be read leaves the default in place.
  }
  return PLAN_DEFAULT_CLOSED_FOR_DAYS;
}

export function writeClosedForDays(days: number | null): void {
  try {
    localStorage.setItem(STORAGE_KEY, days === null ? EVERYTHING : String(Math.max(0, Math.floor(days))));
  } catch {
    // A setting that does not persist still applies to this page.
  }
}

/** The stored number as state, written through on every change. */
export function useClosedForDays(): [number | null, (days: number | null) => void] {
  const [days, setDays] = useState<number | null>(readClosedForDays);
  const update = useCallback((next: number | null) => {
    writeClosedForDays(next);
    setDays(next === null ? null : Math.max(0, Math.floor(next)));
  }, []);
  return [days, update];
}
