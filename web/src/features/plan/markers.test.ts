import { describe, expect, it } from "vitest";
import type { Milestone } from "@/api/milestones";
import { markersFor } from "./markers";
import { day, makeScale } from "./scale";

function milestone(over: Partial<Milestone> = {}): Milestone {
  return {
    id: "m1",
    projectId: "p",
    projectKey: "PR",
    name: "Beta",
    dueOn: "2026-03-10",
    position: 0,
    progress: { issues: 4, done: 1, inProgress: 1, todo: 2, percent: 25 },
    createdAt: "2026-03-01T00:00:00Z",
    updatedAt: "2026-03-01T00:00:00Z",
    ...over,
  };
}

describe("markersFor", () => {
  const scale = makeScale(day("2026-03-02"), day("2026-03-30"), 10);

  it("puts a dated milestone on the day it is due", () => {
    const [marker] = markersFor(scale, [milestone()]);
    expect(marker?.x).toBe(8 * 10);
  });

  // A milestone with no date is an intention; the list still shows it.
  it("leaves undated milestones and ones off the window out", () => {
    const got = markersFor(scale, [
      milestone({ id: "a", dueOn: undefined }),
      milestone({ id: "b", dueOn: "2026-02-01" }),
      milestone({ id: "c", dueOn: "2026-03-30" }),
      milestone({ id: "d", dueOn: "2026-03-29" }),
    ]);
    expect(got.map((m) => m.milestone.id)).toEqual(["d"]);
  });

  it("orders markers left to right", () => {
    const got = markersFor(scale, [milestone({ id: "late", dueOn: "2026-03-20" }), milestone({ id: "soon", dueOn: "2026-03-05" })]);
    expect(got.map((m) => m.milestone.id)).toEqual(["soon", "late"]);
  });
});
