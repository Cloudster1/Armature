import { describe, expect, it } from "vitest";
import type { PlanItem } from "@/api/plan";
import type { Issue, StatusCategory } from "@/api/issues";
import type { SprintPlan } from "@/api/sprints";
import { BACKLOG_KEY, NO_CUT, NO_FILTERS, PLAN_ADD_KEY, closedAt, cutItems, filterItems, hierarchyRows, meter, rowsFor, sprintRows, viewFor } from "./views";

function issue(key: string, type: string, over: Partial<Issue> & { category?: StatusCategory } = {}): Issue {
  const { category = "todo", ...rest } = over;
  return {
    id: key,
    key,
    type: { id: type, name: type, icon: type[0]!, level: 0, isSubtask: false },
    projectId: "p",
    projectKey: "PR",
    summary: key,
    status: { id: category, name: category, category, position: 0 },
    priority: "medium",
    labels: [],
    timeSpentMinutes: 0,
    createdAt: "",
    updatedAt: "",
    ...rest,
  } as Issue;
}

function item(i: Issue, over: Partial<PlanItem> = {}): PlanItem {
  return {
    issue: i,
    depth: 0,
    derived: false,
    progress: { total: 0, done: 0 },
    children: [],
    ...over,
  } as PlanItem;
}

const epic = item(issue("P-1", "Epic"), {
  start: "2026-03-01",
  due: "2026-03-20",
  derived: true,
  children: [
    item(issue("P-2", "Story", { sprintId: "s1", milestoneId: "m1" }), { depth: 1, start: "2026-03-01", due: "2026-03-10", estimate: 5 }),
    item(issue("P-3", "Bug", { category: "done", sprintId: "s1" }), { depth: 1, start: "2026-03-05", due: "2026-03-20", estimate: 3 }),
  ],
});
const loose = item(issue("P-4", "Task", { category: "in_progress" }), { estimate: 2 });
const items = [epic, loose];

describe("viewFor", () => {
  it("falls back to the management view for anything it does not know", () => {
    expect(viewFor("sprints")).toBe("sprints");
    expect(viewFor("nonsense")).toBe("management");
    expect(viewFor(undefined)).toBe("management");
  });
});

describe("filterItems", () => {
  it("keeps a matching child under its parent, and drops the rest of the family", () => {
    const got = filterItems(items, { ...NO_FILTERS, types: ["Bug"] });
    expect(got.map((i) => i.issue.key)).toEqual(["P-1"]);
    expect(got[0]!.children.map((i) => i.issue.key)).toEqual(["P-3"]);
  });

  it("narrows to a milestone", () => {
    const got = filterItems(items, { ...NO_FILTERS, milestoneId: "m1" });
    expect(got[0]!.children.map((i) => i.issue.key)).toEqual(["P-2"]);
  });

  it("returns the very same rows when nothing is filtered", () => {
    expect(filterItems(items, NO_FILTERS)).toBe(items);
  });
});

describe("cutItems", () => {
  const now = new Date("2026-09-04T12:00:00Z");
  const yesterday = item(issue("P-6", "Bug", { category: "done", resolvedAt: "2026-09-03T09:00:00Z" }));
  const longAgo = item(issue("P-7", "Bug", { category: "done", resolvedAt: "2026-07-01T09:00:00Z" }));
  const open = item(issue("P-8", "Story"));
  const all = [yesterday, longAgo, open];

  it("cuts what has been done for longer than the days given, and keeps the rest", () => {
    const got = cutItems(all, { closedForDays: 14, matched: null }, now);
    expect(got.map((i) => i.issue.key)).toEqual(["P-6", "P-8"]);
  });

  it("keeps everything when no days are given, and cuts all done work at zero", () => {
    expect(cutItems(all, NO_CUT, now)).toBe(all);
    expect(cutItems(all, { closedForDays: null, matched: null }, now)).toBe(all);
    expect(cutItems(all, { closedForDays: 0, matched: null }, now).map((i) => i.issue.key)).toEqual(["P-8"]);
  });

  it("takes the last change as the closing time when the workflow did not record one", () => {
    const unrecorded = issue("P-9", "Bug", { category: "done", updatedAt: "2026-06-01T00:00:00Z" });
    expect(closedAt(unrecorded)?.toISOString()).toBe("2026-06-01T00:00:00.000Z");
    expect(closedAt(issue("P-10", "Bug"))).toBeNull();
    expect(cutItems([item(unrecorded)], { closedForDays: 14, matched: null }, now)).toEqual([]);
  });

  it("keeps a cut parent above a child that survives", () => {
    const parent = item(issue("P-11", "Epic", { category: "done", resolvedAt: "2026-01-01T00:00:00Z" }), { children: [open] });
    const got = cutItems([parent], { closedForDays: 14, matched: null }, now);
    expect(got.map((i) => i.issue.key)).toEqual(["P-11"]);
    expect(got[0]!.children.map((i) => i.issue.key)).toEqual(["P-8"]);
  });

  it("keeps what a query matched and the rows above it", () => {
    const got = cutItems(items, { closedForDays: null, matched: new Set(["P-3"]) }, now);
    expect(got.map((i) => i.issue.key)).toEqual(["P-1"]);
    expect(got[0]!.children.map((i) => i.issue.key)).toEqual(["P-3"]);
    expect(cutItems(items, { closedForDays: null, matched: new Set() }, now)).toEqual([]);
  });
});

describe("meter", () => {
  it("counts the tickets, the schedule, the states and the points", () => {
    const got = meter(items);
    expect(got.total).toBe(4);
    expect(got.scheduled).toBe(3);
    expect(got.done).toBe(1);
    expect(got.inProgress).toBe(1);
    expect(got.todo).toBe(2);
    expect(got.donePercent).toBe(25);
    expect(got.points).toBe(10);
  });

  it("counts by type, most common first", () => {
    const got = meter([...items, item(issue("P-5", "Bug"))]);
    expect(got.byType.map((t) => `${t.name}:${t.count}`)).toEqual(["Bug:2", "Epic:1", "Story:1", "Task:1"]);
  });
});

describe("rows", () => {
  const sprint = (id: string, name: string): SprintPlan =>
    ({ sprint: { id, name, state: "active", projectId: "p", projectKey: "PR", position: 0, createdAt: "", updatedAt: "" }, committed: 8, completed: 3, issues: 2, unestimated: 0 }) as SprintPlan;

  it("walks the hierarchy and drops collapsed branches", () => {
    expect(hierarchyRows(items, new Set()).map((r) => r.key)).toEqual(["P-1", "P-2", "P-3", "P-4"]);
    expect(hierarchyRows(items, new Set(["P-1"])).map((r) => r.key)).toEqual(["P-1", "P-4"]);
  });

  // The sprint view is about iterations: what is in each one, and what is
  // waiting. An epic that only holds committed stories is not waiting.
  it("groups the work by sprint with the backlog last, containers left out", () => {
    const rows = sprintRows(items, [sprint("s1", "Sprint 1")], new Set()).filter((r) => r.kind !== "add");
    expect(rows.map((r) => (r.kind === "group" ? `[${r.label} ${r.count}]` : r.key))).toEqual([
      "[Sprint 1 2]",
      "P-2",
      "P-3",
      "[Backlog 1]",
      "P-4",
    ]);
    expect(rows.filter((r) => r.kind === "issue").every((r) => r.kind === "issue" && r.item.depth === 0)).toBe(true);
  });

  it("folds a group away when it is collapsed", () => {
    const rows = sprintRows(items, [sprint("s1", "Sprint 1")], new Set([BACKLOG_KEY])).filter((r) => r.kind !== "add");
    expect(rows.map((r) => r.key)).toEqual(["sprint:s1", "P-2", "P-3", BACKLOG_KEY]);
  });

  it("shows a parent under the sprint it was itself committed to", () => {
    const committedEpic = { ...epic, issue: { ...epic.issue, sprintId: "s1" } };
    const rows = sprintRows([committedEpic, loose], [sprint("s1", "Sprint 1")], new Set());
    expect(rows.filter((r) => r.kind === "issue").map((r) => r.key)).toEqual(["P-1", "P-2", "P-3", "P-4"]);
  });

  it("narrows to a team and keeps the rows above its work", () => {
    const story = issue("PR-2", "Story", { teamId: "t-alpha" });
    const epic = item(issue("PR-1", "Epic"), { children: [item(story), item(issue("PR-3", "Story", { teamId: "t-beta" }))] });
    const rows = hierarchyRows(filterItems([epic], { ...NO_FILTERS, teamId: "t-alpha" }), new Set());
    expect(rows.map((r) => r.key)).toEqual(["PR-1", "PR-2"]);
  });

  it("ends every open group with a line to add to it, and the management view with one", () => {
    const a1 = item(issue("PR-1", "Story", { sprintId: "s1" }));
    const b1 = item(issue("PR-2", "Story"));
    const sprint = { sprint: { id: "s1", name: "Sprint A", teamId: "t1" }, committed: 0, completed: 0, issues: 1, unestimated: 0 } as unknown as SprintPlan;
    const rows = rowsFor("sprints", [a1, b1], [sprint], new Set());
    expect(rows.map((r) => (r.kind === "group" ? `[${r.label}]` : r.kind === "add" ? `+${r.label}` : r.key))).toEqual([
      "[Sprint A]", "PR-1", "+Sprint A", "[Backlog]", "PR-2", "+Backlog",
    ]);
    const folded = rowsFor("sprints", [a1, b1], [sprint], new Set(["sprint:s1"]));
    expect(folded.filter((r) => r.kind === "add")).toHaveLength(1);
    const management = rowsFor("management", [a1, b1], [sprint], new Set());
    expect(management[management.length - 1]).toMatchObject({ kind: "add", key: PLAN_ADD_KEY });
  });
});
