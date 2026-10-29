import { describe, expect, it } from "vitest";
import type { IssueType, Status } from "@/api/issues";
import type { Assignment, Workflow } from "@/api/workflows";
import { landingStatus, typeChoices, workflowFor } from "./typeChange";

const todo: Status = { id: "s-todo", name: "To Do", category: "todo", position: 0 };
const doing: Status = { id: "s-doing", name: "In Progress", category: "in_progress", position: 1 };
const done: Status = { id: "s-done", name: "Done", category: "done", position: 2 };

function workflow(statuses: Status[], initial: Status): Workflow {
  return {
    id: "w1",
    name: "Triage",
    steps: statuses.map((status, position) => ({ id: `step-${status.id}`, status, isInitial: status.id === initial.id, position })),
    transitions: [],
  };
}

function type(id: string, level: number, isSubtask = false): IssueType {
  return { id, name: id, icon: "task", level, isSubtask, position: 0, builtin: true, retired: false };
}

describe("where a type change leaves the status", () => {
  it("keeps a status the new workflow has", () => {
    expect(landingStatus(workflow([todo, doing, done], todo), doing)).toBeNull();
  });

  it("falls to the new workflow's first status when it lacks the issue's", () => {
    expect(landingStatus(workflow([done, todo], todo), doing)).toEqual(todo);
  });

  it("names no workflow for a type the project does not map", () => {
    const assignments = [{ issueTypeId: "bug", workflowId: "w1" }] as Assignment[];
    expect(workflowFor(assignments, "bug")).toBe("w1");
    expect(workflowFor(assignments, "story")).toBeUndefined();
  });
});

describe("the types an issue may become", () => {
  const types = [type("epic", 1), type("story", 0), type("task", 0), type("bug", 0), type("subtask", -1, true)];

  it("offers only the other types at its own level", () => {
    const issue = { type: { ...type("task", 0), name: "Task" } };
    expect(typeChoices(types, issue).map((t) => t.id)).toEqual(["story", "bug"]);
  });

  it("keeps a subtask among subtasks", () => {
    const issue = { type: { ...type("subtask", -1, true), name: "Subtask" } };
    expect(typeChoices([...types, type("chore", -1, true)], issue).map((t) => t.id)).toEqual(["chore"]);
  });
});
