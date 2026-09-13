import { describe, expect, it } from "vitest";
import type { Status } from "@/api/issues";
import { ANYWHERE_NODE_ID, fromNodeId, toFlow, toNodeId } from "./flow";
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
  edges: [
    { key: "e1", name: "Finish", description: "", from: todo.id, to: done.id, rules: [] },
    { key: "e2", name: "Reopen", description: "", from: done.id, to: todo.id, rules: [] },
  ],
};

describe("toFlow", () => {
  it("gives the any-status box a name React Flow accepts and takes it back", () => {
    expect(toNodeId(ANYWHERE)).toBe(ANYWHERE_NODE_ID);
    expect(fromNodeId(ANYWHERE_NODE_ID)).toBe(ANYWHERE);
    expect(fromNodeId(toNodeId(todo.id))).toBe(todo.id);
  });

  it("puts the box's place and name where the suite reads them", () => {
    const { nodes } = toFlow(design, statuses, { kind: "node", statusId: todo.id }, false);
    const node = nodes.find((each) => each.id === todo.id);
    expect(node?.position).toEqual({ x: 40, y: 40 });
    expect(node?.domAttributes).toMatchObject({ "data-workflow-node": "To Do", "data-x": 40, "data-y": 40, "aria-pressed": true });
    expect(node?.ariaRole).toBe("button");
    expect(node?.selected).toBe(true);
  });

  it("bows two transitions between the same pair apart", () => {
    const { edges } = toFlow(design, statuses, { kind: "none" }, false);
    const [finish, reopen] = edges;
    expect(finish?.data?.shape.path).not.toBe(reopen?.data?.shape.path);
    expect(finish?.domAttributes).toMatchObject({ "data-workflow-edge": "Finish" });
  });

  it("read-only, nothing can be pressed and the any-status box waits for a global transition", () => {
    const { nodes, edges } = toFlow(design, statuses, { kind: "none" }, true);
    expect(nodes.every((node) => node.ariaRole === undefined && !node.draggable && !node.focusable)).toBe(true);
    expect(edges.every((edge) => edge.ariaRole === undefined && !edge.focusable && edge.data?.readOnly)).toBe(true);
    expect(nodes.some((node) => node.id === ANYWHERE_NODE_ID)).toBe(false);
    const withGlobal = { ...design, edges: [...design.edges, { key: "e3", name: "Drop", description: "", from: ANYWHERE, to: done.id, rules: [] }] };
    expect(toFlow(withGlobal, statuses, { kind: "none" }, true).nodes.some((node) => node.id === ANYWHERE_NODE_ID)).toBe(true);
  });

  it("leaves out an edge whose end is not drawn", () => {
    const missing = { ...design, nodes: design.nodes.slice(0, 1) };
    expect(toFlow(missing, statuses, { kind: "none" }, false).edges).toHaveLength(0);
  });
});
