import type { Milestone } from "@/api/milestones";
import type { Scale } from "./scale";

export interface Marker {
  milestone: Milestone;
  /** The left edge of the day the milestone is due, on the calendar. */
  x: number;
}

/**
 * Where each dated milestone falls on the calendar. Undated ones have nowhere
 * to be drawn and are left to the progress list, which still shows them; ones
 * off either end of the window are dropped rather than drawn in the margin.
 */
export function markersFor(scale: Scale, milestones: Milestone[]): Marker[] {
  const out: Marker[] = [];
  for (const milestone of milestones) {
    if (!milestone.dueOn) continue;
    const x = scale.x(milestone.dueOn);
    if (x < 0 || x >= scale.width) continue;
    out.push({ milestone, x });
  }
  return out.sort((a, b) => a.x - b.x);
}
