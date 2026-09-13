import { useCallback, useState } from "react";
import { RECENT_QUERIES_MAX } from "@/config";

const KEY = "armature.recentQueries";

function read(): string[] {
  try {
    const parsed: unknown = JSON.parse(localStorage.getItem(KEY) ?? "[]");
    return Array.isArray(parsed) ? parsed.filter((q): q is string => typeof q === "string") : [];
  } catch {
    return [];
  }
}

/** The last few queries this browser ran, newest first; a query run again moves to the front. */
export function useRecentQueries(): [string[], (query: string) => void] {
  const [recent, setRecent] = useState<string[]>(read);
  const remember = useCallback((query: string) => {
    if (!query.trim()) return;
    const next = [query, ...read().filter((q) => q !== query)].slice(0, RECENT_QUERIES_MAX);
    localStorage.setItem(KEY, JSON.stringify(next));
    setRecent(next);
  }, []);
  return [recent, remember];
}
