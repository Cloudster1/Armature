import { describe, expect, it } from "vitest";
import type { Issue } from "@/api/issues";
import type { PlanItem } from "@/api/plan";
import { PLAN_ROW_HEIGHT } from "@/config";
import { decide, rowAt } from "./reparent";
import type { Row } from "./views";

function issue(key: string, type: string, level: number, parentKey?: string): Issue {
  return {
    id: key,
    key,
    type: { id: type, name: type, icon: type[0]!, level, isSubtask: level < 0 },
    projectId: "p",
    projectKey: "PR",
    summary: key,
    status: { id: "todo", name: "To Do", category: "todo", position: 0 },
    priority: "medium",
    labels: [],
    fixVersions: [],
    affectsVersions: [],
    components: [],
    timeSpentMinutes: 0,
    createdAt: "",
    updatedAt: "",
    parentKey,
  } as Issue;
}

function item(i: Issue, children: PlanItem[] = []): PlanItem {
  return { issue: i, depth: 0, derived: false, progress: { total: 0, done: 0 }, children } as PlanItem;
}

const subtask = item(issue("PR-4", "Subtask", -1, "PR-2"));
const story = item(issue("PR-2", "Story", 0, "PR-1"), [subtask]);
const epic = item(issue("PR-1", "Epic", 1), [story]);
const otherStory = item(issue("PR-3", "Story", 0));
const bug = item(issue("PR-5", "Bug", 0, "PR-1"));

describe("decide", () => {
  it("puts an issue under a row one level up", () => {
    expect(decide(otherStory, { kind: "row", item: epic })).toEqual({ kind: "into", parentKey: "PR-1" });
  });

  it("puts an issue beside a row on its own level, under that row's parent", () => {
    expect(decide(otherStory, { kind: "row", item: bug })).toEqual({ kind: "beside", parentKey: "PR-1" });
    expect(decide(story, { kind: "row", item: otherStory })).toEqual({ kind: "beside", parentKey: null });
  });

  it("makes a root of what is dropped above the rows, unless it needs a parent", () => {
    expect(decide(story, { kind: "top" })).toEqual({ kind: "top", parentKey: null });
    expect(decide(subtask, { kind: "top" })).toEqual({ refused: "A subtask needs a parent issue." });
    expect(decide(subtask, { kind: "row", item: item(issue("PR-9", "Subtask", -1)) })).toEqual({ refused: "A subtask needs a parent issue." });
  });

  it("refuses the drops the server would refuse, in its words", () => {
    expect(decide(story, { kind: "row", item: subtask })).toEqual({ refused: "That would put the issue underneath itself." });
    expect(decide(story, { kind: "row", item: story })).toEqual({ refused: "An issue cannot be its own parent." });
    expect(decide(epic, { kind: "row", item: otherStory })).toEqual({
      refused: "PR-3 is a Story, which cannot be the parent of an Epic.",
    });
    expect(decide(subtask, { kind: "row", item: epic })).toEqual({
      refused: "PR-1 is an Epic, which cannot be the parent of a Subtask.",
    });
  });
});

describe("rowAt", () => {
  const rows: Row[] = [
    { kind: "issue", key: "PR-1", item: epic, hasChildren: true, collapsed: false },
    { kind: "add", key: "add:plan", label: "plan", group: null },
  ];

  it("finds the issue row under a point and nothing under the other kinds", () => {
    expect(rowAt(rows, PLAN_ROW_HEIGHT / 2)).toEqual({ kind: "row", item: epic });
    expect(rowAt(rows, PLAN_ROW_HEIGHT * 1.5)).toBeNull();
    expect(rowAt(rows, PLAN_ROW_HEIGHT * 5)).toBeNull();
  });

  it("reads the space above the rows as the top level", () => {
    expect(rowAt(rows, -3)).toEqual({ kind: "top" });
  });
});
