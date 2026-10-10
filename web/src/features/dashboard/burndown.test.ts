import { createElement } from "react";
import { describe, expect, it } from "vitest";
import { render } from "@testing-library/react";
import type { SprintBurndown } from "@/api/reports";
import { axisLabels, burndownSeries, describeScope } from "./burndown";
import { LineChart } from "./charts";

function curve(points: Array<[string, number, number]>, over: Partial<SprintBurndown> = {}): SprintBurndown {
  return {
    sprint: { id: "s1", name: "Sprint 1", startsOn: "2026-09-07T00:00:00Z", endsOn: "2026-09-11T00:00:00Z" },
    live: true,
    points: points.map(([day, scope, done]) => ({ day, scope, done, remaining: scope - done, issues: 2, issuesDone: done > 0 ? 1 : 0, unestimated: 0 })),
    ...over,
  };
}

describe("burndownSeries", () => {
  it("draws the ideal from the first day's scope to nothing on the last day", () => {
    const [, ideal] = burndownSeries(curve([["2026-09-07", 8, 0], ["2026-09-08", 8, 3]]));
    expect(ideal!.points).toEqual([{ x: 0, y: 8 }, { x: 4, y: 0 }]);
    expect(ideal!.dashed).toBe(true);
  });

  it("steps the scope up when an issue is added mid-sprint", () => {
    const [scope, , remaining] = burndownSeries(curve([["2026-09-07", 8, 0], ["2026-09-09", 11, 3]]));
    expect(scope!.step).toBe(true);
    expect(scope!.points).toEqual([{ x: 0, y: 8 }, { x: 2, y: 11 }]);
    expect(remaining!.points).toEqual([{ x: 0, y: 8 }, { x: 2, y: 8 }]);
  });

  // A sprint that started before the first snapshot was written has no history
  // to show for those days, and the chart must not pretend it does.
  it("does not invent days before the first snapshot", () => {
    const [scope, ideal, remaining] = burndownSeries(curve([["2026-09-09", 8, 2]]));
    expect(remaining!.points).toEqual([{ x: 2, y: 6 }]);
    expect(scope!.points[0]!.x).toBe(2);
    expect(ideal!.points[0]).toEqual({ x: 2, y: 8 });
  });

  it("spans the points when the sprint has no dates", () => {
    const c = curve([["2026-09-07", 8, 0], ["2026-09-10", 8, 8]], { sprint: { id: "s", name: "Undated" } });
    const [, ideal] = burndownSeries(c);
    expect(ideal!.points[1]).toEqual({ x: 3, y: 0 });
    expect(axisLabels(c)).toEqual(["7/9", "8/9", "10/9"]);
  });

  // On a sprint's first day the report holds a single point; it must still show.
  it("marks the only point on a sprint's first day", () => {
    const c = curve([["2026-09-07", 8, 0]]);
    const [, , remaining] = burndownSeries(c);
    expect(remaining!.points).toEqual([{ x: 0, y: 8 }]);
    render(createElement(LineChart, { series: burndownSeries(c), xLabels: axisLabels(c) }));
    expect(document.querySelector('[data-series="remaining"] [data-marker]')).not.toBeNull();
  });

  it("starts the axis at the first point when the sprint was started two days early", () => {
    const c = curve([["2026-09-05", 8, 0], ["2026-09-07", 8, 2], ["2026-09-08", 8, 3]]);
    const [scope, ideal, remaining] = burndownSeries(c);
    expect(remaining!.points.map((p) => p.x)).toEqual([0, 2, 3]);
    expect(scope!.points[0]!.x).toBe(0);
    // The ideal still reaches nothing on the sprint's last day, six days after the first point.
    expect(ideal!.points).toEqual([{ x: 0, y: 8 }, { x: 6, y: 0 }]);
    expect(axisLabels(c)).toEqual(["5/9", "8/9", "11/9"]);
  });

  it("ends the axis at the last point when the sprint ended two days late", () => {
    const days = ["2026-09-07", "2026-09-09", "2026-09-11", "2026-09-13"];
    const c = curve(days.map((d, i): [string, number, number] => [d, 8, i * 2]));
    const series = burndownSeries(c);
    const [, ideal, remaining] = series;
    expect(remaining!.points.map((p) => p.x)).toEqual([0, 2, 4, 6]);
    expect(ideal!.points).toEqual([{ x: 0, y: 8 }, { x: 4, y: 0 }]);
    // The right edge is the largest x any series draws, so the label under it must name that day.
    expect(Math.max(...series.flatMap((s) => s.points.map((p) => p.x)))).toBe(6);
    expect(axisLabels(c)).toEqual(["7/9", "10/9", "13/9"]);
  });
});

describe("describeScope", () => {
  it("says what is left, how the scope moved and what is unsized", () => {
    const c = curve([["2026-09-07", 8, 0], ["2026-09-09", 11, 3]]);
    c.points[1]!.unestimated = 2;
    expect(describeScope(c)).toBe("8 of 11 pts left · scope 8 to 11 pts · 2 unestimated · 1/2 issues done");
    expect(describeScope(curve([]))).toBe("No day has been written down yet.");
  });
});
