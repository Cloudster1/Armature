import { useCallback, useState } from "react";

const STORAGE_KEY = "armature.calendar.daysOff";
const HIDDEN = "hidden";

/**
 * Whether the calendar shows holidays and absences, as the reader last said.
 * Kept in the browser like the plan's settings: it is a way of looking.
 */
export function readShowDaysOff(): boolean {
  try {
    return localStorage.getItem(STORAGE_KEY) !== HIDDEN;
  } catch {
    // Storage that cannot be read leaves them shown.
    return true;
  }
}

export function writeShowDaysOff(show: boolean): void {
  try {
    if (show) localStorage.removeItem(STORAGE_KEY);
    else localStorage.setItem(STORAGE_KEY, HIDDEN);
  } catch {
    // A setting that does not persist still applies to this page.
  }
}

/** The stored choice as state, written through on every change. */
export function useShowDaysOff(): [boolean, (show: boolean) => void] {
  const [show, setShow] = useState(readShowDaysOff);
  const update = useCallback((next: boolean) => {
    writeShowDaysOff(next);
    setShow(next);
  }, []);
  return [show, update];
}
