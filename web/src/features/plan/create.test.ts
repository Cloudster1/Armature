import { describe, expect, it } from "vitest";
import type { SprintPlan } from "@/api/sprints";
import { draftBox, dragRange, isRealDrag, placementFor } from "./create";
import { makeScale } from "./scale";

const scale = makeScale(new Date("2026-09-07"), new Date("2026-10-05"), 10);

describe("dragRange", () => {
  it("covers the days dragged, inclusive, either way round", () => {
    const forward = dragRange(scale, 20, 61);
    expect([forward.start.toISOString().slice(0, 10), forward.due.toISOString().slice(0, 10)]).toEqual(["2026-09-09", "2026-09-13"]);
    const backward = dragRange(scale, 61, 20);
    expect(backward).toEqual(forward);
  });

  it("is never shorter than a day", () => {
    const one = dragRange(scale, 20, 23);
    expect(one.start).toEqual(one.due);
  });
});

describe("isRealDrag", () => {
  it("tells a drag from a click by how far the pointer went", () => {
    expect(isRealDrag(100, 104)).toBe(false);
    expect(isRealDrag(100, 108)).toBe(true);
    expect(isRealDrag(100, 90)).toBe(true);
  });
});

describe("draftBox", () => {
  it("sits over the days dragged and is wide enough to type in", () => {
    const box = draftBox(scale, new Date("2026-09-09"), new Date("2026-09-10"), 1000);
    expect(box.left).toBe(20);
    expect(box.width).toBe(160);
  });

  it("stays inside the canvas", () => {
    const box = draftBox(scale, new Date("2026-10-03"), new Date("2026-10-04"), 280);
    expect(box.left + box.width).toBeLessThanOrEqual(280);
  });
});

describe("placementFor", () => {
  it("reads the sprint and its team off a sprint group, and nothing off the backlog", () => {
    const sprint = { sprint: { id: "s1", name: "Sprint A", teamId: "t1" } } as unknown as SprintPlan;
    expect(placementFor({ kind: "group", key: "sprint:s1", label: "Sprint A", sprint, count: 0, collapsed: false })).toEqual({ sprintId: "s1", teamId: "t1" });
    expect(placementFor({ kind: "group", key: "sprint:backlog", label: "Backlog", sprint: null, count: 0, collapsed: false })).toEqual({});
    expect(placementFor(null)).toEqual({});
  });
});
