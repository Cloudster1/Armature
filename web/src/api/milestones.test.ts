import { describe, expect, it } from "vitest";
import { dayOf, describeProgress, formatDay, isOverdue, type Milestone } from "./milestones";

function milestone(over: Partial<Milestone> = {}): Milestone {
  return {
    id: "m1",
    projectId: "p",
    projectKey: "PR",
    name: "Beta",
    dueOn: "2026-09-24T00:00:00Z",
    position: 0,
    progress: { issues: 4, done: 1, inProgress: 1, todo: 2, percent: 25 },
    createdAt: "2026-03-01T00:00:00Z",
    updatedAt: "2026-03-01T00:00:00Z",
    ...over,
  };
}

// The server writes a date as midnight UTC; the client shows and edits days.
describe("days", () => {
  it("reads the day off the server's timestamp", () => {
    expect(dayOf("2026-09-24T00:00:00Z")).toBe("2026-09-24");
    expect(dayOf("2026-09-24")).toBe("2026-09-24");
  });

  it("writes it the way the plan does, whichever form it arrived in", () => {
    expect(formatDay("2026-09-24T00:00:00Z")).toBe(formatDay("2026-09-24"));
    expect(formatDay("2026-09-24")).toMatch(/^24 Sep\w* 2026$/);
  });
});

describe("isOverdue", () => {
  it("is overdue once the day has passed with work left", () => {
    expect(isOverdue(milestone(), new Date("2026-09-25T10:00:00Z"))).toBe(true);
    expect(isOverdue(milestone(), new Date("2026-09-24T10:00:00Z"))).toBe(false);
  });

  it("is not overdue when everything is done, closed, or undated", () => {
    expect(isOverdue(milestone({ progress: { issues: 2, done: 2, inProgress: 0, todo: 0, percent: 100 } }), new Date("2027-01-01"))).toBe(false);
    expect(isOverdue(milestone({ closedAt: "2026-09-01T00:00:00Z" }), new Date("2027-01-01"))).toBe(false);
    expect(isOverdue(milestone({ dueOn: undefined }), new Date("2027-01-01"))).toBe(false);
  });
});

describe("describeProgress", () => {
  it("says what is done, and says so when nothing is assigned", () => {
    expect(describeProgress({ issues: 4, done: 1, inProgress: 1, todo: 2, percent: 25 })).toBe("1 of 4 done, 25%");
    expect(describeProgress({ issues: 0, done: 0, inProgress: 0, todo: 0, percent: 0 })).toBe("Nothing assigned yet");
  });
});
