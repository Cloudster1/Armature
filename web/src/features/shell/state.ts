import { useCallback, useEffect, useState } from "react";
import { SIDEBAR_RAIL_BELOW_PX } from "@/config";

const SIDEBAR_KEY = "armature.sidebar";
const PROJECT_KEY = "armature.project";

export type SidebarMode = "open" | "rail";

/** Reads a preference this browser keeps; a browser that refuses storage answers null. */
export function readStorage(key: string): string | null {
  return read(key);
}

/** Writes a preference this browser keeps, or forgets it with null. */
export function writeStorage(key: string, value: string | null) {
  write(key, value);
}

function read(key: string): string | null {
  try {
    return window.localStorage.getItem(key);
  } catch {
    return null;
  }
}

function write(key: string, value: string | null) {
  try {
    if (value === null) window.localStorage.removeItem(key);
    else window.localStorage.setItem(key, value);
  } catch {
    // A browser that refuses storage still gets a sidebar; it just forgets.
  }
}

// The sidebar starts as a rail on a narrow screen and remembers what the
// reader last chose, so a laptop and a wide monitor each keep their own habit.
export function useSidebarMode(): [SidebarMode, () => void] {
  const [mode, setMode] = useState<SidebarMode>(() => {
    const stored = read(SIDEBAR_KEY);
    if (stored === "open" || stored === "rail") return stored;
    return typeof window !== "undefined" && window.innerWidth < SIDEBAR_RAIL_BELOW_PX ? "rail" : "open";
  });
  const toggle = useCallback(() => {
    setMode((current) => {
      const next = current === "open" ? "rail" : "open";
      write(SIDEBAR_KEY, next);
      return next;
    });
  }, []);
  return [mode, toggle];
}

const GROUPS_KEY = "armature.sidebar-groups";

/**
 * Which sidebar groups are folded, by id, kept per browser. Absent means the
 * group's own default, so a new group opens the way it was designed to.
 */
export function useSidebarGroups(): [Record<string, boolean>, (id: string, defaultOpen: boolean) => void] {
  const [groups, setGroups] = useState<Record<string, boolean>>(() => {
    try {
      const parsed: unknown = JSON.parse(read(GROUPS_KEY) ?? "{}");
      return parsed && typeof parsed === "object" ? (parsed as Record<string, boolean>) : {};
    } catch {
      return {};
    }
  });
  const toggle = useCallback((id: string, defaultOpen: boolean) => {
    setGroups((current) => {
      const open = current[id] ?? defaultOpen;
      const next = { ...current, [id]: !open };
      write(GROUPS_KEY, JSON.stringify(next));
      return next;
    });
  }, []);
  return [groups, toggle];
}

/** Whether the viewport is at least this wide, kept current as the window changes. */
export function useMinWidth(px: number): boolean {
  const [wide, setWide] = useState(() => typeof window !== "undefined" && window.innerWidth >= px);
  useEffect(() => {
    if (typeof window.matchMedia !== "function") return;
    const query = window.matchMedia(`(min-width: ${px}px)`);
    const update = () => setWide(query.matches);
    update();
    query.addEventListener("change", update);
    return () => query.removeEventListener("change", update);
  }, [px]);
  return wide;
}

/** The project the sidebar shows when the route names none: the last one visited. */
export function rememberProject(key: string | undefined) {
  if (key) write(PROJECT_KEY, key);
}

export function useLastProject(current: string | undefined): string | undefined {
  const [last, setLast] = useState<string | undefined>(() => read(PROJECT_KEY) ?? undefined);
  useEffect(() => {
    if (current) {
      rememberProject(current);
      setLast(current);
    }
  }, [current]);
  return current ?? last;
}
