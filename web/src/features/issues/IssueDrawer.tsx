import { createContext, useCallback, useContext, useEffect, useMemo, useState, type ReactNode } from "react";
import { useLocation } from "@tanstack/react-router";
import { isEditing } from "@/components/ui";

interface DrawerApi {
  open: (issueKey: string) => void;
  close: () => void;
  /** The issue the panel shows, or null while it is closed. */
  current: string | null;
  /** The visible issues in reading order, as the list on the page registered them. */
  keys: string[];
  register: (keys: string[]) => void;
  /** Moves the panel to the next (1) or previous (-1) issue of the registered list. */
  step: (delta: 1 | -1) => void;
}

const DrawerContext = createContext<DrawerApi | null>(null);

const nowhere: DrawerApi = { open: () => {}, close: () => {}, current: null, keys: [], register: () => {}, step: () => {} };

/** Opens an issue beside the list or board it was found in; without the shell it does nothing. */
export function useIssueDrawer(): DrawerApi {
  return useContext(DrawerContext) ?? nowhere;
}

/**
 * A list or board tells the panel which issues it shows and in what order, so
 * the arrow keys and the panel's own buttons walk the page the reader sees.
 */
export function useIssueList(keys: string[]) {
  const { register } = useIssueDrawer();
  const joined = keys.join("\n");
  useEffect(() => {
    register(joined ? joined.split("\n") : []);
    return () => register([]);
  }, [register, joined]);
}

/** Where the current issue sits in the registered list; -1 when it is not in it. */
export function indexOf(keys: string[], current: string | null): number {
  return current === null ? -1 : keys.indexOf(current);
}

// One panel for the shell. It closes when the route changes, so following a
// link inside it lands on a page and not on a page behind a panel.
export function IssueDrawerProvider({ children }: { children: ReactNode }) {
  const [current, setCurrent] = useState<string | null>(null);
  const [keys, setKeys] = useState<string[]>([]);
  const { pathname } = useLocation();
  useEffect(() => setCurrent(null), [pathname]);
  const open = useCallback((issueKey: string) => setCurrent(issueKey), []);
  const close = useCallback(() => setCurrent(null), []);
  const register = useCallback((next: string[]) => setKeys(next), []);
  const step = useCallback(
    (delta: 1 | -1) => {
      setCurrent((now) => {
        if (keys.length === 0) return now;
        const at = indexOf(keys, now);
        // An issue the list no longer shows steps to the list's first entry.
        const next = at < 0 ? 0 : Math.min(Math.max(at + delta, 0), keys.length - 1);
        return keys[next] ?? now;
      });
    },
    [keys],
  );

  // Up and down walk the list while the panel is open, unless the keys were
  // typed into a field or something else already answered them.
  useEffect(() => {
    if (current === null) return;
    function onKey(event: KeyboardEvent) {
      if (event.defaultPrevented || isEditing(event.target)) return;
      if (event.key !== "ArrowDown" && event.key !== "ArrowUp") return;
      const target = event.target as Element | null;
      const inScope = target === document.body || target === null || target.closest("[data-issue-list], [data-issue-panel]") !== null;
      if (!inScope) return;
      event.preventDefault();
      step(event.key === "ArrowDown" ? 1 : -1);
    }
    document.addEventListener("keydown", onKey);
    return () => document.removeEventListener("keydown", onKey);
  }, [current, step]);

  const api = useMemo(() => ({ open, close, current, keys, register, step }), [open, close, current, keys, register, step]);
  return <DrawerContext.Provider value={api}>{children}</DrawerContext.Provider>;
}
