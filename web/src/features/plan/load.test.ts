import { describe, expect, it } from "vitest";
import type { Load, TeamLoad } from "@/api/plan";
import { barShare, describeLoad, loadRows, utilisation, weekCells } from "./load";
import { makeScale } from "./scale";
import { NO_FILTERS } from "./views";

function row(team: string, kind: TeamLoad["kind"], loads: number[], capacity?: number, over: Partial<TeamLoad> = {}): TeamLoad {
  return {
    team,
    kind,
    teamId: kind === "team" ? `id-${team}` : undefined,
    weeklyCapacity: capacity,
    unscheduled: 0,
    weeks: loads.map((load, i) => ({ start: `2026-09-${String(7 + i * 7).padStart(2, "0")}T00:00:00Z`, load, capacity, issues: load > 0 ? 1 : 0, unestimated: 0 })),
    ...over,
  };
}

const load: Load = {
  weeks: ["2026-09-07T00:00:00Z", "2026-09-14T00:00:00Z"],
  rows: [row("Alpha", "team", [14, 4], 10), row("Unassigned", "unassigned", [3, 0]), row("Total", "total", [17, 4], 10)],
};

describe("loadRows", () => {
  it("puts every row under one group", () => {
    const rows = loadRows(load, NO_FILTERS, new Set());
    expect(rows.map((r) => (r.kind === "group" ? `[${r.label}]` : r.key))).toEqual(["[Load by team]", "load:Alpha", "load:Unassigned", "load:Total"]);
  });

  it("collapses to the group alone", () => {
    expect(loadRows(load, NO_FILTERS, new Set(["load"]))).toHaveLength(1);
  });

  // With one team chosen the total would say nothing about what is shown.
  it("narrows to the filtered team's row", () => {
    const rows = loadRows(load, { ...NO_FILTERS, teamId: "id-Alpha" }, new Set());
    expect(rows.map((r) => r.key)).toEqual(["load", "load:Alpha"]);
  });

  it("is nothing when there is nothing to draw", () => {
    expect(loadRows({ weeks: [], rows: [] }, NO_FILTERS, new Set())).toEqual([]);
  });
});

describe("utilisation", () => {
  it("reads a week in four words", () => {
    expect(utilisation(3)).toBe("unmeasured");
    expect(utilisation(3, 10)).toBe("under");
    expect(utilisation(8.5, 10)).toBe("full");
    expect(utilisation(8.4, 10)).toBe("under");
    expect(utilisation(10, 10)).toBe("full");
    expect(utilisation(10.5, 10)).toBe("over");
    expect(utilisation(0, 0)).toBe("under");
  });
});

describe("weekCells", () => {
  it("lines the weeks up with the calendar and clips them to it", () => {
    // The window opens on Wednesday the 9th; the first week began the 7th.
    const scale = makeScale(new Date("2026-09-09"), new Date("2026-09-21"), 10);
    const cells = weekCells(scale, load.rows[0]!);
    expect(cells.map((c) => c.key)).toEqual(["2026-09-07", "2026-09-14"]);
    expect(cells[0]!.x).toBe(0);
    expect(cells[0]!.width).toBe(50);
    expect(cells[1]!.x).toBe(50);
    expect(cells[1]!.width).toBe(70);
  });
});

describe("barShare and words", () => {
  it("sizes against the capacity, or the busiest week when there is none", () => {
    expect(barShare(load.rows[0]!, load.rows[0]!.weeks[1]!)).toBeCloseTo(0.4);
    expect(barShare(load.rows[0]!, load.rows[0]!.weeks[0]!)).toBe(1);
    expect(barShare(load.rows[1]!, load.rows[1]!.weeks[0]!)).toBe(1);
  });

  it("says what the sidebar needs", () => {
    expect(describeLoad(load.rows[0]!)).toBe("10 pts/week");
    expect(describeLoad({ ...load.rows[1]!, unscheduled: 2 })).toBe("no capacity set · 2 unscheduled");
  });
});
