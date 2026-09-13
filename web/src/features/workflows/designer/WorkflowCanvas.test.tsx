import { describe, expect, it } from "vitest";
import { render } from "@testing-library/react";
import type { Status } from "@/api/issues";
import { WorkflowCanvas } from "./WorkflowCanvas";
import { ANYWHERE, type Design } from "./graph";

const todo: Status = { id: "s-todo", name: "To Do", category: "todo", position: 0 };
const done: Status = { id: "s-done", name: "Done", category: "done", position: 1 };
const statuses = new Map([todo, done].map((status) => [status.id, status]));

const design: Design = {
  name: "Two steps",
  description: "",
  nodes: [
    { statusId: todo.id, x: 40, y: 40 },
    { statusId: done.id, x: 360, y: 40 },
  ],
  initialId: todo.id,
  edges: [{ key: "e1", name: "Finish", description: "", from: todo.id, to: done.id, rules: [] }],
};

describe("WorkflowCanvas read-only", () => {
  it("draws the same boxes and arrows with nothing to grab", () => {
    render(<WorkflowCanvas design={design} statuses={statuses} readOnly label="Workflow Two steps" />);
    expect(document.querySelector('[data-workflow-node="To Do"]')).not.toBeNull();
    expect(document.querySelector('[data-workflow-edge="Finish"]')).not.toBeNull();
    expect(document.querySelector("[data-workflow-initial]")).not.toBeNull();
    expect(document.querySelectorAll('[role="button"]')).toHaveLength(0);
    expect(document.querySelectorAll("[data-connect-handle]")).toHaveLength(0);
    expect(document.querySelector('[data-workflow-canvas][aria-label="Workflow Two steps"]')).not.toBeNull();
  });

  // The dashed box only means something when a transition leaves it.
  it("leaves out the any-status box until a global transition needs it", () => {
    const { rerender } = render(<WorkflowCanvas design={design} statuses={statuses} readOnly />);
    expect(document.querySelector("[data-workflow-anywhere]")).toBeNull();
    rerender(
      <WorkflowCanvas
        design={{ ...design, edges: [...design.edges, { key: "e2", name: "Drop", description: "", from: ANYWHERE, to: done.id, rules: [] }] }}
        statuses={statuses}
        readOnly
      />,
    );
    expect(document.querySelector("[data-workflow-anywhere]")).not.toBeNull();
  });

  // The card says what it is: the description when there is one, else the
  // category's word; and the transition's name rides on the arrow.
  it("puts the description and the name where the reader looks", () => {
    const described = new Map(statuses);
    described.set(todo.id, { ...todo, description: "Nothing has happened yet." });
    render(<WorkflowCanvas design={design} statuses={described} readOnly />);
    const subtitles = Array.from(document.querySelectorAll("[data-workflow-subtitle]")).map((el) => el.textContent);
    expect(subtitles).toEqual(["Nothing has happened yet.", "Done"]);
    expect(document.querySelector('[data-workflow-edge-label="Finish"]')).not.toBeNull();
  });

  it("stays interactive by default", () => {
    render(<WorkflowCanvas design={design} statuses={statuses} selection={{ kind: "none" }} onSelect={() => {}} onMove={() => {}} onConnect={() => {}} />);
    expect(document.querySelectorAll('[role="button"]').length).toBeGreaterThan(0);
    expect(document.querySelectorAll("[data-connect-handle]").length).toBeGreaterThan(0);
  });
});
