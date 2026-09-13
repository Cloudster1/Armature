import {
  PLAN_HEADER_ROW_HEIGHT,
  PLAN_MILESTONE_BAND_HEIGHT,
  PLAN_MILESTONE_FLAG_WIDTH,
  PLAN_SPRINT_BAND_HEIGHT,
} from "@/config";

/**
 * The stack above the rows on the calendar side, and therefore the height of
 * the sidebar's header too. Both sides read it from here, and every block in
 * it is sized border-box, so the rows either side of the divider start on the
 * same pixel.
 */
export interface HeaderLayout {
  header: number;
  sprintBand: number;
  milestoneBand: number;
  total: number;
}

export function headerLayout({ sprints, milestones }: { sprints: boolean; milestones: boolean }): HeaderLayout {
  const header = 2 * PLAN_HEADER_ROW_HEIGHT;
  const sprintBand = sprints ? PLAN_SPRINT_BAND_HEIGHT : 0;
  const milestoneBand = milestones ? PLAN_MILESTONE_BAND_HEIGHT : 0;
  return { header, sprintBand, milestoneBand, total: header + sprintBand + milestoneBand };
}

/**
 * Which side of its line a milestone flag hangs on. It reads to the right of
 * the line until that would run off the calendar, then to the left, so the
 * flag never widens the calendar past its own edge.
 */
export function flagSide(x: number, calendarWidth: number): "right" | "left" {
  return x + PLAN_MILESTONE_FLAG_WIDTH > calendarWidth ? "left" : "right";
}

/**
 * Which flags have room for their label. At a coarse zoom two milestones can
 * fall within one flag's width of each other, and two labels drawn over each
 * other read as neither; the earlier keeps its diamond and gives up its words.
 */
export function labelledFlags(xs: number[]): boolean[] {
  return xs.map((x, i) => {
    const next = xs[i + 1];
    return next === undefined || next - x >= PLAN_MILESTONE_FLAG_WIDTH;
  });
}
