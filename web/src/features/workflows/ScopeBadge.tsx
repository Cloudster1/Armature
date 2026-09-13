import type { Origin } from "@/api/workflows";
import { cx } from "@/components/ui";

/** Says which level decided a workflow. Inherited is the ordinary case and
 * reads quietly; a choice the project made for itself is worth noticing. */
export function ScopeBadge({ origin }: { origin: Origin }) {
  const ownedHere = origin.scope === "project";
  return (
    <span
      className={cx(
        "inline-flex items-center rounded-full px-2 py-0.5 text-2xs font-medium",
        ownedHere
          ? "bg-accent-subtle text-accent ring-1 ring-accent/30"
          : "bg-surface-raised text-ink-muted",
      )}
    >
      {ownedHere ? "This project" : "Organization"}
    </span>
  );
}

/** Explains the badge: which scheme answered, and whether by name or by default. */
export function originExplanation(origin: Origin): string {
  const how = origin.named ? "names this issue type" : "catches everything it does not name";
  return `${origin.schemeName} ${how}`;
}
