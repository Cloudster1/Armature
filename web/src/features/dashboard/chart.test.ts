import { describe, expect, it } from "vitest";
import type { ChartGroup } from "@/api/reports";
import { CHART_TONES, OTHER, arcPath, columnLayout, describeChart, donutLayout, foldTail, segmentNames, seriesFrom, tonesFor } from "./chart";

const group = (label: string, value: number, parts: Array<[string, number]> = []): ChartGroup => ({
  label,
  value,
  parts: parts.map(([label, value]) => ({ label, value })),
});

describe("tonesFor", () => {
  it("gives a label the same colour whatever its size, and a status its category's", () => {
    const before = tonesFor([group("Task", 5), group("Bug", 2)]);
    const after = tonesFor([group("Bug", 9), group("Task", 1)]);
    expect(before.get("Bug")).toBe(after.get("Bug"));
    expect(before.get("Bug")).toBe(CHART_TONES[0]);
    expect(before.get("Task")).toBe(CHART_TONES[1]);
    const statuses = tonesFor([{ ...group("Done", 3), category: "done" }]);
    expect(statuses.get("Done")).toBe("text-status-done");
  });
});

describe("foldTail", () => {
  it("folds everything past the sixth into Other, parts included", () => {
    const groups = Array.from({ length: 8 }, (_, i) => group(`g${i}`, 10 - i, [["a", 1], ["b", 1]]));
    const folded = foldTail(groups, 6);
    expect(folded).toHaveLength(6);
    expect(folded[5]!.label).toBe(OTHER);
    expect(folded[5]!.value).toBe(5 + 4 + 3);
    expect(folded[5]!.parts).toEqual([{ label: "a", value: 3 }, { label: "b", value: 3 }]);
    expect(tonesFor(folded).get(OTHER)).toBe("text-ink-subtle");
  });

  it("leaves six or fewer alone", () => {
    const groups = [group("a", 1), group("b", 2)];
    expect(foldTail(groups)).toBe(groups);
  });
});

describe("the donut", () => {
  it("lays slices end to end around the turn", () => {
    const arcs = donutLayout([group("a", 3), group("b", 1)]);
    expect(arcs.map((a) => [a.start, a.end])).toEqual([[0, 0.75], [0.75, 1]]);
    expect(arcs[0]!.share).toBe(0.75);
    expect(donutLayout([group("a", 0)])).toEqual([]);
  });

  it("draws a quarter from twelve to three, and a whole turn as two halves", () => {
    expect(arcPath(50, 50, 40, 25, 0, 0.25)).toBe("M50 10 A40 40 0 0 1 90 50 L75 50 A25 25 0 0 0 50 25 Z");
    const whole = arcPath(50, 50, 40, 25, 0, 1);
    expect(whole.split("Z").filter(Boolean)).toHaveLength(2);
  });
});

describe("columns", () => {
  it("scales every column and segment against the tallest", () => {
    const columns = columnLayout([group("a", 4, [["x", 3], ["y", 1]]), group("b", 2)]);
    expect(columns.map((c) => c.height)).toEqual([1, 0.5]);
    expect(columns[0]!.segments.map((s) => s.height)).toEqual([0.75, 0.25]);
    expect(segmentNames([group("a", 1, [["y", 1], ["x", 1]])])).toEqual(["x", "y"]);
  });
});

describe("seriesFrom", () => {
  it("numbers the buckets along x and colours lines by name, not by order", () => {
    const series = seriesFrom({
      series: [
        { name: "Task", points: [{ start: "2026-08-03", value: 1 }, { start: "2026-08-10", value: 3 }] },
        { name: "Bug", points: [{ start: "2026-08-03", value: 0 }, { start: "2026-08-10", value: 2 }] },
      ],
      interval: "week",
      days: 30,
      measure: "count",
      counts: "created",
    });
    expect(series[0]!.points).toEqual([{ x: 0, y: 1 }, { x: 1, y: 3 }]);
    expect(series[1]!.tone).toBe(CHART_TONES[0]);
    expect(series[0]!.tone).toBe(CHART_TONES[1]);
  });
});

describe("describeChart", () => {
  it("reads a chart's settings back as a sentence, and falls back to status", () => {
    expect(describeChart({})).toBe("Issues by status");
    expect(describeChart({ groupBy: "type", measure: "points" })).toBe("Points by issue type");
    expect(describeChart({ shape: "stacked", groupBy: "team", splitBy: "statusCategory" })).toBe("Issues by team, split by status category");
    expect(describeChart({ groupBy: "nonsense" })).toBe("Issues by status");
  });

  it("says a line chart by what it counts, how often and by which field", () => {
    expect(describeChart({ shape: "line", series: "resolved", interval: "month", groupBy: "priority" })).toBe("Issues resolved per month, by priority");
    expect(describeChart({ shape: "line" })).toBe("Issues created per week");
  });
});
