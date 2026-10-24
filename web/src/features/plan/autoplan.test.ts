import { describe, expect, it } from "vitest";
import type { Alignment } from "@/api/plan";
import { describeMove, movesToSave, proposedBars } from "./autoplan";

const alignment: Alignment = {
  projectKey: "PR",
  moves: [
    {
      issueKey: "PR-2",
      from: { startDate: "2027-11-03T00:00:00Z", dueDate: "2027-11-04T00:00:00Z" },
      to: { startDate: "2027-11-08T00:00:00Z", dueDate: "2027-11-09T00:00:00Z" },
      workingDays: 2,
      after: "PR-1",
    },
    {
      issueKey: "PR-3",
      from: { startDate: "2027-11-08T00:00:00Z", dueDate: "2027-11-08T00:00:00Z" },
      to: { startDate: "2027-11-10T00:00:00Z", dueDate: "2027-11-10T00:00:00Z" },
      workingDays: 1,
      after: "PR-2",
    },
  ],
  warnings: [],
};

describe("the auto plan's proposal", () => {
  it("is saved expecting the dates it was read from", () => {
    expect(movesToSave(alignment)).toEqual([
      { key: "PR-2", expected: { startDate: "2027-11-03", dueDate: "2027-11-04" }, startDate: "2027-11-08", dueDate: "2027-11-09" },
      { key: "PR-3", expected: { startDate: "2027-11-08", dueDate: "2027-11-08" }, startDate: "2027-11-10", dueDate: "2027-11-10" },
    ]);
  });

  it("draws each moved issue where it would go, and nothing without a proposal", () => {
    expect(proposedBars(alignment)?.get("PR-3")).toEqual({ start: "2027-11-10", due: "2027-11-10" });
    expect(proposedBars(alignment)?.has("PR-1")).toBe(false);
    expect(proposedBars(null)).toBeNull();
  });

  it("says what moves, from when to when, and after what", () => {
    expect(describeMove(alignment.moves[0]!, "en-GB")).toBe("moves from 3 Nov 2027 to 8 Nov 2027, 2 working days, after PR-1");
    expect(describeMove(alignment.moves[1]!, "en-GB")).toContain("1 working day,");
  });
});
