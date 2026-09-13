import { describe, expect, it } from "vitest";
import { formatDay } from "@/api/milestones";
import { daysUntil, describeDue } from "./MilestonesBody";

const on = (day: string) => formatDay(`${day}T00:00:00Z`);

const today = new Date("2026-09-06T10:00:00Z");
const row = (over: Partial<{ dueOn: string; closedAt: string }>) => ({
  id: "m1",
  name: "Release 1",
  progress: { issues: 4, done: 1, inProgress: 1, todo: 2, percent: 25 },
  points: 0,
  donePoints: 0,
  ...over,
});

describe("describeDue", () => {
  it("counts the days left, names today and tomorrow, and says how far overdue", () => {
    expect(daysUntil("2026-09-18", today)).toBe(12);
    expect(describeDue(row({ dueOn: "2026-09-18T00:00:00Z" }), today)).toBe(`Due ${on("2026-09-18")}, 12 days left`);
    expect(describeDue(row({ dueOn: "2026-09-07T00:00:00Z" }), today)).toBe(`Due ${on("2026-09-07")}, tomorrow`);
    expect(describeDue(row({ dueOn: "2026-09-06T00:00:00Z" }), today)).toBe(`Due ${on("2026-09-06")}, today`);
    expect(describeDue(row({ dueOn: "2026-09-05T00:00:00Z" }), today)).toBe(`Due ${on("2026-09-05")}, overdue by 1 day`);
    expect(describeDue(row({ dueOn: "2026-09-01T00:00:00Z" }), today)).toBe(`Due ${on("2026-09-01")}, overdue by 5 days`);
  });

  it("says when a milestone closed, and that an undated one has no day yet", () => {
    expect(describeDue(row({ dueOn: "2026-09-01T00:00:00Z", closedAt: "2026-09-03T12:00:00Z" }), today)).toBe(`Closed ${on("2026-09-03")}`);
    expect(describeDue(row({}), today)).toBe("No due day yet");
  });
});
