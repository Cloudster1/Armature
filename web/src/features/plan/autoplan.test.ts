import { describe, expect, it } from "vitest";
import { ApiError } from "@/api/client";
import type { Alignment } from "@/api/plan";
import { describeMove, followOffer, movesBack, movesToSave, proposedBars, summarise, undoRefusal } from "./autoplan";

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
  unplaced: [],
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

describe("undoing an auto plan", () => {
  it("puts each issue back, expecting the dates the auto plan set", () => {
    expect(movesBack(movesToSave(alignment))[0]).toEqual({
      key: "PR-2",
      expected: { startDate: "2027-11-08", dueDate: "2027-11-09" },
      startDate: "2027-11-03",
      dueDate: "2027-11-04",
    });
  });

  it("names the issue changed since, and what to do about it", () => {
    const refused = new ApiError(409, { code: "moved_since", message: "PR-2 was moved since these dates were read, so nothing was saved.", fields: { issueKey: "PR-2" } });
    expect(undoRefusal(refused)).toBe("PR-2 was changed after the auto plan, so nothing was undone. Move it back by hand or run auto plan again.");
    expect(undoRefusal(new ApiError(403, { code: "forbidden", message: "You may not move these issues." }))).toBe("You may not move these issues.");
  });
});

describe("placing work without dates", () => {
  const placed: Alignment = {
    projectKey: "PR",
    moves: [
      { issueKey: "PR-4", to: { startDate: "2027-11-11T00:00:00Z", dueDate: "2027-11-12T00:00:00Z" }, placed: true, workingDays: 2, after: "PR-3" },
      { issueKey: "PR-5", to: { startDate: "2026-10-01T00:00:00Z", dueDate: "2026-10-01T00:00:00Z" }, placed: true, workingDays: 1, after: "" },
    ],
    unplaced: [{ issueKey: "PR-6", reason: "Not placed: it has no estimate." }],
    warnings: [],
  };

  it("is saved expecting the issue to have no dates yet", () => {
    expect(movesToSave(placed)[0]).toEqual({ key: "PR-4", expected: { startDate: null, dueDate: null }, startDate: "2027-11-11", dueDate: "2027-11-12" });
  });

  it("reads as placed, after its blocker when it has one", () => {
    expect(describeMove(placed.moves[0]!, "en-GB")).toBe("is placed from 11 Nov 2027, 2 working days, after PR-3");
    expect(describeMove(placed.moves[1]!, "en-GB")).toBe("is placed from 1 Oct 2026, 1 working day");
  });

  it("is counted apart from the moves", () => {
    expect(summarise({ ...placed, moves: [...alignment.moves, ...placed.moves] })).toBe(
      "2 issues would move to start after the issues blocking them, and 2 issues without dates would be placed. Nothing is saved until you apply.",
    );
    expect(summarise(placed)).toBe("2 issues without dates would be placed. Nothing is saved until you apply.");
    expect(summarise({ ...placed, moves: [] })).toBe("Nothing can be placed or needs to move.");
    expect(summarise({ ...placed, moves: [], unplaced: [] })).toBe("Every issue already starts after the issues blocking it.");
  });
});

describe("the offer after a blocker is dragged later", () => {
  it("is made only when something it blocks now starts too early, and counts them", () => {
    expect(followOffer("PR-1", 0)).toBeNull();
    expect(followOffer("PR-1", 1)).toEqual({ message: "1 issue that PR-1 blocks now starts before it ends.", action: "Move the 1 issue it blocks" });
    expect(followOffer("PR-1", 3)?.action).toBe("Move the 3 issues it blocks");
  });
});
