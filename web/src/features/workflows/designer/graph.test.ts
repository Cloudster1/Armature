import { describe, expect, it } from "vitest";
import type { Status } from "@/api/issues";
import type { RuleType, WorkflowDetail } from "@/api/workflows";
import {
  WORKFLOW_COLUMN_GAP,
  WORKFLOW_EDGE_BEND,
  WORKFLOW_GRID,
  WORKFLOW_LAYOUT_ORIGIN_X,
  WORKFLOW_LAYOUT_ORIGIN_Y,
  WORKFLOW_NODE_HEIGHT,
  WORKFLOW_NODE_WIDTH,
  WORKFLOW_ROW_GAP,
} from "@/config";
import {
  ANYWHERE,
  addStatus,
  anywhereRect,
  bends,
  clipToRect,
  connect,
  describeRule,
  edgeShape,
  emptyDesign,
  fromWorkflow,
  bendClearOf,
  moveNode,
  nodeRect,
  problems,
  removeStatus,
  toInput,
  type Design,
  type Node,
} from "./graph";

const todo: Status = { id: "s-todo", name: "To Do", category: "todo", position: 0 };
const progress: Status = { id: "s-progress", name: "In Progress", category: "in_progress", position: 1 };
const review: Status = { id: "s-review", name: "In Review", category: "in_progress", position: 2 };
const done: Status = { id: "s-done", name: "Done", category: "done", position: 3 };
const statuses = new Map([todo, progress, review, done].map((status) => [status.id, status]));

const saved: WorkflowDetail = {
  workflow: {
    id: "w1",
    name: "Default",
    steps: [
      { id: "step-todo", status: todo, isInitial: true, position: 0, layout: { x: 100, y: 200 } },
      { id: "step-progress", status: progress, isInitial: false, position: 1 },
      { id: "step-done", status: done, isInitial: false, position: 2 },
    ],
    transitions: [
      { id: "t1", name: "Start", fromStepId: "step-todo", toStepId: "step-progress", position: 0 },
      { id: "t2", name: "Close", toStepId: "step-done", position: 1 },
    ],
  },
  rules: {
    t2: [{ id: "r1", kind: "postfunction", type: "postfunction.set_resolved", position: 0 }],
  },
};

describe("reading a saved workflow", () => {
  it("keeps the statuses where they were drawn", () => {
    const design = fromWorkflow(saved);
    expect(design.nodes.find((node) => node.statusId === todo.id)).toEqual({ statusId: todo.id, x: 100, y: 200 });
    expect(design.initialId).toBe(todo.id);
  });

  // A workflow made before the designer existed has no picture; it is given
  // one that reads left to right by category rather than a heap at the origin.
  it("lays out what was never placed by category column", () => {
    const design = fromWorkflow(saved);
    const placed = design.nodes.find((node) => node.statusId === progress.id);
    expect(placed).toEqual({
      statusId: progress.id,
      x: WORKFLOW_LAYOUT_ORIGIN_X + WORKFLOW_COLUMN_GAP,
      y: WORKFLOW_LAYOUT_ORIGIN_Y,
    });
    expect(design.nodes.find((node) => node.statusId === done.id)?.x).toBe(
      WORKFLOW_LAYOUT_ORIGIN_X + 2 * WORKFLOW_COLUMN_GAP,
    );
  });

  it("translates steps into statuses and carries the rules", () => {
    const design = fromWorkflow(saved);
    expect(design.edges).toEqual([
      { key: "t1", id: "t1", name: "Start", description: "", from: todo.id, to: progress.id, rules: [] },
      {
        key: "t2",
        id: "t2",
        name: "Close",
        description: "",
        from: ANYWHERE,
        to: done.id,
        rules: [{ kind: "postfunction", type: "postfunction.set_resolved", config: {} }],
      },
    ]);
  });

  it("round-trips into what the server takes, with layout and rules", () => {
    const input = toInput(fromWorkflow(saved));
    expect(input.steps[0]).toEqual({ statusId: todo.id, isInitial: true, layout: { x: 100, y: 200 } });
    expect(input.transitions[1]).toEqual({
      id: "t2",
      name: "Close",
      description: "",
      fromStatusId: null,
      toStatusId: done.id,
      rules: [{ kind: "postfunction", type: "postfunction.set_resolved", config: {} }],
    });
  });
});

describe("placing statuses", () => {
  it("stacks statuses of one category down their column", () => {
    let design = addStatus(emptyDesign(), progress);
    design = addStatus(design, review);
    const [first, second] = design.nodes as [Node, Node];
    expect(first.x).toBe(second.x);
    expect(second.y - first.y).toBe(WORKFLOW_ROW_GAP);
  });

  it("makes the first status placed the one issues open in", () => {
    const design = addStatus(addStatus(emptyDesign(), progress), todo);
    expect(design.initialId).toBe(progress.id);
  });

  it("adds a status once", () => {
    const design = addStatus(addStatus(emptyDesign(), todo), todo);
    expect(design.nodes).toHaveLength(1);
  });

  it("snaps a dropped status to the grid and keeps it on the canvas", () => {
    const design = moveNode(addStatus(emptyDesign(), todo), todo.id, 13, -20);
    expect(design.nodes[0]).toEqual({ statusId: todo.id, x: 2 * WORKFLOW_GRID, y: 0 });
  });

  it("removes a status along with the transitions that touched it", () => {
    let design = fromWorkflow(saved);
    design = removeStatus(design, progress.id);
    expect(design.nodes.map((node) => node.statusId)).toEqual([todo.id, done.id]);
    expect(design.edges.map((edge) => edge.name)).toEqual(["Close"]);
  });

  it("forgets the opening status when it is removed", () => {
    expect(removeStatus(fromWorkflow(saved), todo.id).initialId).toBe("");
  });

});

describe("drawing transitions", () => {
  it("connects two statuses and hands back the new edge", () => {
    const base = fromWorkflow(saved);
    const result = connect(base, progress.id, done.id, "Finish");
    expect(result).not.toBeNull();
    const edge = result!.design.edges.find((each) => each.key === result!.key);
    expect(edge).toMatchObject({ name: "Finish", from: progress.id, to: done.id, rules: [] });
    expect(edge?.id).toBeUndefined();
  });

  it("refuses a move from a status to itself", () => {
    expect(connect(fromWorkflow(saved), done.id, done.id, "Loop")).toBeNull();
  });

  it("refuses an end the canvas does not have", () => {
    expect(connect(fromWorkflow(saved), todo.id, review.id, "Review")).toBeNull();
    expect(connect(fromWorkflow(saved), todo.id, ANYWHERE, "Nowhere")).toBeNull();
  });

  it("can start from any status", () => {
    const result = connect(fromWorkflow(saved), ANYWHERE, todo.id, "Reopen");
    expect(result?.design.edges.at(-1)?.from).toBe(ANYWHERE);
  });
});

describe("edge geometry", () => {
  const left = { x: 0, y: 0, width: 100, height: 40 };
  const right = { x: 300, y: 0, width: 100, height: 40 };

  it("leaves a rect at its border, not its centre", () => {
    expect(clipToRect(left, { x: 500, y: 20 })).toEqual({ x: 100, y: 20 });
    expect(clipToRect(left, { x: 50, y: 500 })).toEqual({ x: 50, y: 40 });
  });

  it("runs straight when nothing else is between the pair", () => {
    const shape = edgeShape(left, right, 0);
    expect(shape.path).toBe("M 100 20 Q 200 20 300 20");
    expect(shape.label).toEqual({ x: 200, y: 20 });
  });

  it("bows out to the side it is told to", () => {
    const shape = edgeShape(left, right, 40);
    expect(shape.label.y).toBeGreaterThan(20);
    expect(edgeShape(left, right, -40).label.y).toBeLessThan(20);
  });

  // "Start" and "Stop" between the same two statuses must not lie on top of
  // each other, or one label hides the other and one arrow cannot be clicked.
  it("bends opposite directions to opposite sides", () => {
    const design: Design = {
      ...emptyDesign(),
      edges: [
        { key: "a", name: "Start", description: "", from: "s1", to: "s2", rules: [] },
        { key: "b", name: "Stop", description: "", from: "s2", to: "s1", rules: [] },
        { key: "c", name: "Elsewhere", description: "", from: "s1", to: "s3", rules: [] },
      ],
    };
    const bent = bends(design.edges);
    expect(bent.get("c")).toBe(0);
    // Drawn, the two curves' control points sit on opposite sides of the line.
    const left = { x: 0, y: 0, width: 100, height: 40 };
    const right = { x: 300, y: 0, width: 100, height: 40 };
    const control = (path: string) => Number(path.split(" ")[5]);
    const start = control(edgeShape(left, right, bent.get("a")!).path);
    const stop = control(edgeShape(right, left, bent.get("b")!).path);
    expect(start).not.toBe(20);
    expect(stop).not.toBe(20);
    expect(Math.sign(start - 20)).toBe(-Math.sign(stop - 20));
  });

  it("spreads several moves in one direction either side of the straight one", () => {
    const edges = ["a", "b", "c"].map((key) => ({
      key,
      name: key,
      description: "",
      from: "s1",
      to: "s2",
      rules: [],
    }));
    const bent = bends(edges);
    expect([bent.get("a"), bent.get("b"), bent.get("c")]).toEqual([0, WORKFLOW_EDGE_BEND, -WORKFLOW_EDGE_BEND]);
  });

  it("sizes rects the way the canvas draws them", () => {
    const design = addStatus(emptyDesign(), todo);
    const node = design.nodes[0] as Node;
    expect(nodeRect(node)).toEqual({ x: node.x, y: node.y, width: WORKFLOW_NODE_WIDTH, height: WORKFLOW_NODE_HEIGHT });
  });
});

describe("saying what would stop a save", () => {
  it("names each thing the server would refuse", () => {
    const design: Design = {
      name: "",
      description: "",
      nodes: [{ statusId: todo.id, x: 0, y: 0 }, { statusId: done.id, x: 300, y: 0 }],
      initialId: "",
      edges: [
        { key: "a", name: "", description: "", from: todo.id, to: done.id, rules: [] },
        { key: "b", name: "Close", description: "", from: ANYWHERE, to: done.id, rules: [] },
        { key: "c", name: "close", description: "", from: ANYWHERE, to: todo.id, rules: [] },
      ],
    };
    expect(problems(design, statuses)).toEqual([
      "The workflow needs a name.",
      "Choose the status new issues open in.",
      "Every transition needs a name.",
      "Two transitions called close leave any status.",
    ]);
  });

  it("has nothing to say about a workable design", () => {
    expect(problems(fromWorkflow(saved), statuses)).toEqual([]);
  });
});

describe("describing a rule", () => {
  const catalogue: RuleType[] = [
    {
      kind: "condition",
      type: "condition.org_role",
      label: "Actor holds a role",
      description: "",
      options: [{ name: "roles", label: "Roles", kind: "roles", required: true, choices: ["owner", "admin"] }],
    },
    { kind: "validator", type: "validator.comment_required", label: "Comment required", description: "", options: [] },
  ];

  it("uses the label and spells out the configuration", () => {
    expect(describeRule({ kind: "validator", type: "validator.comment_required", config: {} }, catalogue)).toBe(
      "Comment required",
    );
    expect(
      describeRule({ kind: "condition", type: "condition.org_role", config: { roles: ["admin", "owner"] } }, catalogue),
    ).toBe("Actor holds a role (Roles: admin, owner)");
  });

  it("falls back to the type name for a rule the catalogue does not know", () => {
    expect(describeRule({ kind: "condition", type: "condition.moon_phase", config: {} }, catalogue)).toBe(
      "condition.moon_phase",
    );
  });
});

describe("the any-status box", () => {
  // A third to-do status would otherwise be laid out on top of it.
  it("is never laid out over", () => {
    let design = emptyDesign();
    for (const id of ["a", "b", "c", "d"]) {
      design = addStatus(design, { id, name: id, category: "todo", position: 0 });
    }
    const anywhere = anywhereRect();
    for (const node of design.nodes) {
      const overlaps =
        Math.abs(node.x - anywhere.x) < WORKFLOW_NODE_WIDTH && Math.abs(node.y - anywhere.y) < WORKFLOW_NODE_HEIGHT;
      expect(overlaps).toBe(false);
    }
  });
});

describe("an arrow bows round a card in its way", () => {
  const left = { x: 0, y: 0, width: 240, height: 72 };
  const middle = { x: 352, y: 0, width: 240, height: 72 };
  const right = { x: 704, y: 0, width: 240, height: 72 };

  it("stays straight when nothing is in the way", () => {
    expect(bendClearOf(left, middle, [right], 0, 64)).toBe(0);
  });

  it("bows when the straight line would run through another card", () => {
    const bend = bendClearOf(left, right, [middle], 0, 64);
    expect(bend).not.toBe(0);
    expect(Math.abs(bend) % 64).toBe(0);
  });

  it("keeps the pair's bend when that already clears", () => {
    expect(bendClearOf(left, middle, [right], 64, 64)).toBe(64);
  });
});
