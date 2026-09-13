import { describe, expect, it } from "vitest";
import {
  PLAN_HEADER_ROW_HEIGHT,
  PLAN_MILESTONE_BAND_HEIGHT,
  PLAN_MILESTONE_FLAG_WIDTH,
  PLAN_SPRINT_BAND_HEIGHT,
} from "@/config";
import { flagSide, headerLayout, labelledFlags } from "./layout";

describe("headerLayout", () => {
  // The sidebar's header is as tall as everything stacked above the calendar
  // rows, or the rows either side of the divider drift apart.
  it("adds up exactly what is drawn", () => {
    const bare = headerLayout({ sprints: false, milestones: false });
    expect(bare.total).toBe(2 * PLAN_HEADER_ROW_HEIGHT);

    const full = headerLayout({ sprints: true, milestones: true });
    expect(full.total).toBe(full.header + full.sprintBand + full.milestoneBand);
    expect(full.total).toBe(2 * PLAN_HEADER_ROW_HEIGHT + PLAN_SPRINT_BAND_HEIGHT + PLAN_MILESTONE_BAND_HEIGHT);
  });

  it("leaves out a band with nothing to draw", () => {
    expect(headerLayout({ sprints: true, milestones: false }).milestoneBand).toBe(0);
    expect(headerLayout({ sprints: false, milestones: true }).sprintBand).toBe(0);
  });
});

describe("flagSide", () => {
  it("hangs to the right while there is room, and to the left at the edge", () => {
    expect(flagSide(100, 1000)).toBe("right");
    expect(flagSide(1000 - PLAN_MILESTONE_FLAG_WIDTH, 1000)).toBe("right");
    expect(flagSide(1000 - PLAN_MILESTONE_FLAG_WIDTH + 1, 1000)).toBe("left");
  });
});

describe("labelledFlags", () => {
  it("lets every flag speak while they are far enough apart", () => {
    expect(labelledFlags([0, PLAN_MILESTONE_FLAG_WIDTH, 3 * PLAN_MILESTONE_FLAG_WIDTH])).toEqual([true, true, true]);
  });

  // The later flag keeps its label: it is the one the earlier would cover.
  it("silences a flag whose neighbour would draw over it", () => {
    expect(labelledFlags([0, 40, 400])).toEqual([false, true, true]);
    expect(labelledFlags([0, 40, 60])).toEqual([false, false, true]);
  });
});
