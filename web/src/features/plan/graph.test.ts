import { describe, expect, it } from "vitest";
import { GRAPH_MIN_GRID_COLUMNS, GRAPH_NODE_HEIGHT, GRAPH_NODE_WIDTH } from "@/config";
import { keyOrder, layoutGraph } from "./graph";

const nodes = (...keys: string[]) => keys.map((key) => ({ key }));
const edge = (from: string, to: string) => ({ id: `${from}>${to}`, from, to });
const column = (layout: ReturnType<typeof layoutGraph>, key: string) =>
  Math.round((layout.nodes.get(key)!.x - layout.nodes.get("P-1")!.x) / (GRAPH_NODE_WIDTH + 1));

describe("layoutGraph", () => {
  it("puts a chain in three columns, in order", () => {
    const got = layoutGraph(nodes("P-1", "P-2", "P-3"), [edge("P-1", "P-2"), edge("P-2", "P-3")], { columns: 3 });
    const xs = ["P-1", "P-2", "P-3"].map((k) => got.nodes.get(k)!.x);
    expect(xs[0]).toBeLessThan(xs[1]!);
    expect(xs[1]).toBeLessThan(xs[2]!);
    expect(got.edges.map((e) => e.cycle)).toEqual([false, false]);
    expect(got.unlinkedTop).toBeNull();
  });

  it("lays a diamond out with the two middles in one column and the end after both", () => {
    const got = layoutGraph(
      nodes("P-1", "P-2", "P-3", "P-4"),
      [edge("P-1", "P-2"), edge("P-1", "P-3"), edge("P-2", "P-4"), edge("P-3", "P-4"), edge("P-1", "P-4")],
      { columns: 3 },
    );
    expect(got.nodes.get("P-2")!.x).toBe(got.nodes.get("P-3")!.x);
    expect(got.nodes.get("P-2")!.y).toBeLessThan(got.nodes.get("P-3")!.y);
    expect(got.nodes.get("P-4")!.x).toBeGreaterThan(got.nodes.get("P-2")!.x);
    expect(column(got, "P-4")).toBeGreaterThan(column(got, "P-2"));
  });

  it("draws a circle without looping, marking the edge that closes it", () => {
    const got = layoutGraph(nodes("P-1", "P-2"), [edge("P-1", "P-2"), edge("P-2", "P-1")], { columns: 3 });
    expect(got.edges.filter((e) => e.cycle)).toHaveLength(1);
    expect(got.edges.find((e) => e.cycle)!.from).toBe("P-2");
    expect(got.nodes.size).toBe(2);
    expect(got.edges.every((e) => e.path.startsWith("M "))).toBe(true);
  });

  it("puts tickets with no dependencies in a grid underneath, wrapping at the columns given", () => {
    const got = layoutGraph(nodes("P-1", "P-2", "P-3", "P-4", "P-5"), [edge("P-1", "P-2")], { columns: 2 });
    expect(got.unlinkedTop).not.toBeNull();
    const grid = ["P-3", "P-4", "P-5"].map((k) => got.nodes.get(k)!);
    expect(grid.every((r) => r.y > got.nodes.get("P-2")!.y + GRAPH_NODE_HEIGHT)).toBe(true);
    expect(grid[0]!.y).toBe(grid[1]!.y);
    expect(grid[2]!.y).toBeGreaterThan(grid[0]!.y);
    expect(grid[2]!.x).toBe(grid[0]!.x);
  });

  it("never uses fewer grid columns than the minimum", () => {
    const got = layoutGraph(nodes("P-1", "P-2", "P-3"), [], { columns: 0 });
    const ys = new Set(["P-1", "P-2"].map((k) => got.nodes.get(k)!.y));
    expect(ys.size).toBe(1);
    expect(GRAPH_MIN_GRID_COLUMNS).toBeGreaterThanOrEqual(2);
  });

  it("gives the same picture for the same input, whatever the order it came in", () => {
    const a = layoutGraph(nodes("P-3", "P-1", "P-2"), [edge("P-2", "P-3"), edge("P-1", "P-2")], { columns: 3 });
    const b = layoutGraph(nodes("P-1", "P-2", "P-3"), [edge("P-1", "P-2"), edge("P-2", "P-3")], { columns: 3 });
    expect([...a.nodes.entries()].sort()).toEqual([...b.nodes.entries()].sort());
  });

  it("leaves out an edge to a ticket that is not drawn", () => {
    const got = layoutGraph(nodes("P-1"), [edge("P-1", "P-9")], { columns: 3 });
    expect(got.edges).toEqual([]);
    expect(got.unlinkedTop).not.toBeNull();
  });
});

describe("keyOrder", () => {
  it("sorts by project then by number", () => {
    expect(["PR-10", "PR-9", "AB-1"].sort(keyOrder)).toEqual(["AB-1", "PR-9", "PR-10"]);
  });
});
