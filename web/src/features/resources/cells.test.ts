import { describe, expect, it } from "vitest";
import type { ResourceRow, ResourceWeek } from "@/api/resources";
import { cellShares, cellState, describeCell, describeHours, hasPlannedRows, hours, mondayOf, shiftWeeks, windowFrom } from "./cells";

const week = (over: Partial<ResourceWeek> = {}): ResourceWeek => ({ start: "2031-03-03T00:00:00Z", loadHours: 0, issues: [], ...over });
const row = (weeks: ResourceWeek[], over: Partial<ResourceRow> = {}): ResourceRow => ({ kind: "person", id: "p1", name: "Ada", weeks, ...over });

describe("a resource cell", () => {
  it("says its hours against the hours left, and why they are short", () => {
    expect(describeHours(week({ loadHours: 32, capacityHours: 40 }))).toBe("32 of 40 h");
    expect(describeHours(week({ loadHours: 32, capacityHours: 24, nominalHours: 40, daysAway: 1, holidays: 1 }))).toBe("32 of 24 h; 1 day away; 1 holiday");
    expect(describeHours(week({ loadHours: 12.5, capacityHours: 20, daysAway: 1.5, holidays: 2 }))).toBe("12.5 of 20 h; 1.5 days away; 2 holidays");
    expect(describeHours(week({ loadHours: 6 }))).toBe("6 h");
    expect(describeCell(row([]), week({ loadHours: 6, capacityHours: 8 }))).toMatch(/^Ada, week of .+: 6 of 8 h$/);
    // A person who gives the project part of their week says so beside the hours.
    expect(describeHours(week({ loadHours: 12, capacityHours: 20, daysAway: 1 }), 50)).toBe("12 of 20 h (50% of the week); 1 day away");
    expect(describeCell(row([], { sharePercent: 0 }), week({ loadHours: 2, capacityHours: 0 }))).toMatch(/: 2 of 0 h \(0% of the week\)$/);
  });

  it("writes hours to one decimal and drops a trailing zero", () => {
    expect(hours(9.79)).toBe("9.8");
    expect(hours(6.86)).toBe("6.9");
    expect(hours(0.04)).toBe("0");
    expect(hours(40)).toBe("40");
    expect(hours(39.96)).toBe("40");
    expect(describeHours(week({ loadHours: 3.42, capacityHours: 37.333, daysAway: 0.5 }))).toBe("3.4 of 37.3 h; 0.5 days away");
  });

  it("is coloured by how full the week is", () => {
    expect(cellState(week({ loadHours: 50, capacityHours: 40 }))).toBe("over");
    expect(cellState(week({ loadHours: 38, capacityHours: 40 }))).toBe("full");
    expect(cellState(week({ loadHours: 10, capacityHours: 40 }))).toBe("under");
    expect(cellState(week({ loadHours: 10 }))).toBe("unmeasured");
    expect(cellState(week({ loadHours: 4, capacityHours: 0 }))).toBe("over");
  });

  it("measures the bar against the whole week, so the days off show as the gap above the hours left", () => {
    const short = week({ loadHours: 16, capacityHours: 24, nominalHours: 40 });
    expect(cellShares(row([short]), short)).toEqual({ load: 0.4, capacity: 0.6, nominal: 1 });
    const over = week({ loadHours: 80, capacityHours: 40, nominalHours: 40 });
    expect(cellShares(row([over]), over)).toEqual({ load: 1, capacity: 0.5, nominal: 0.5 });
    // Nobody's hours: against the busiest week of the row.
    const quiet = week({ loadHours: 5 });
    const busy = week({ loadHours: 20 });
    expect(cellShares(row([quiet, busy], { kind: "unassigned" }), quiet)).toEqual({ load: 0.25, capacity: undefined, nominal: undefined });
  });
});

describe("the resource window", () => {
  it("starts on the Monday of a week and runs whole weeks", () => {
    expect(mondayOf(new Date("2031-03-06T15:00:00Z"))).toBe("2031-03-03");
    expect(mondayOf(new Date("2031-03-09T10:00:00Z"))).toBe("2031-03-03");
    expect(mondayOf(new Date("2031-03-03T00:00:00Z"))).toBe("2031-03-03");
    expect(windowFrom("2031-03-03", 8)).toEqual({ from: "2031-03-03", to: "2031-04-27" });
    expect(shiftWeeks("2031-03-03", -4)).toBe("2031-02-03");
  });

  it("has somebody to plan against only beside the unassigned row", () => {
    expect(hasPlannedRows([row([], { kind: "unassigned", id: undefined })])).toBe(false);
    expect(hasPlannedRows([row([], { kind: "team" }), row([], { kind: "unassigned" })])).toBe(true);
  });
});
