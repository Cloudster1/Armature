import { useEffect, useRef, useState } from "react";

/** A form's fields seeded from the server: an edited field is kept when the
 *  server answers again, so a refetch landing mid-typing takes nothing back. */
export function useDraft<T extends Record<string, string>>(server: T | undefined) {
  const [draft, setDraft] = useState<T | undefined>(server);
  const base = useRef<T | undefined>(server);

  useEffect(() => {
    if (!server || server === base.current) return;
    const was = base.current;
    base.current = server;
    setDraft((current) => {
      if (!current || !was) return server;
      const next = { ...server };
      for (const key of Object.keys(server) as (keyof T)[]) {
        if (current[key] !== was[key]) next[key] = current[key];
      }
      return next;
    });
  }, [server]);

  const set = <K extends keyof T>(key: K, value: T[K]) => setDraft((current) => (current ? { ...current, [key]: value } : current));
  return [draft, set] as const;
}
