import {
  GRAPH_COLUMN_GAP,
  GRAPH_EDGE_BEND,
  GRAPH_MIN_GRID_COLUMNS,
  GRAPH_NODE_HEIGHT,
  GRAPH_NODE_WIDTH,
  GRAPH_ORIGIN,
  GRAPH_ROW_GAP,
  GRAPH_SECTION_GAP,
} from "@/config";
import { bends, edgeShape, type Rect } from "@/lib/graph";

export interface GraphNode {
  key: string;
}

export interface GraphEdge {
  id: string;
  /** The blocker. */
  from: string;
  /** The blocked. */
  to: string;
}

export interface LaidEdge extends GraphEdge {
  path: string;
  /** A point on the curve where a control about the edge sits. */
  handle: { x: number; y: number };
  /** The edge closes a circle in the data and was left out of the layering. */
  cycle: boolean;
}

export interface GraphLayout {
  nodes: Map<string, Rect>;
  edges: LaidEdge[];
  width: number;
  height: number;
  /** Where the block of tickets with no dependencies starts; null when there are none. */
  unlinkedTop: number | null;
}

/** Issue keys sort by project, then by number, so PR-10 follows PR-9. */
export function keyOrder(a: string, b: string): number {
  const [pa, na] = split(a);
  const [pb, nb] = split(b);
  return pa.localeCompare(pb) || na - nb || a.localeCompare(b);
}

function split(key: string): [string, number] {
  const at = key.lastIndexOf("-");
  const n = Number(key.slice(at + 1));
  return [key.slice(0, at), Number.isFinite(n) ? n : 0];
}

/**
 * Lays the dependency graph out left to right by longest path: a ticket sits
 * one column after the last thing it waits on. Circles in old data are drawn
 * dashed and left out of the layering rather than looping forever. Tickets
 * with no dependencies sit in a grid underneath, so every ticket is on the
 * page and a first dependency can be dragged from there.
 */
export function layoutGraph(nodes: GraphNode[], edges: GraphEdge[], opts: { columns: number }): GraphLayout {
  const keys = [...new Set(nodes.map((n) => n.key))].sort(keyOrder);
  const known = new Set(keys);
  const usable = edges.filter((e) => known.has(e.from) && known.has(e.to) && e.from !== e.to);

  const out = new Map<string, GraphEdge[]>();
  for (const key of keys) out.set(key, []);
  for (const e of usable) out.get(e.from)!.push(e);
  for (const list of out.values()) list.sort((a, b) => keyOrder(a.to, b.to));

  const back = markBackEdges(keys, out);
  const forward = usable.filter((e) => !back.has(e.id));
  const linked = new Set<string>();
  for (const e of usable) {
    linked.add(e.from);
    linked.add(e.to);
  }

  const layer = layers(keys.filter((k) => linked.has(k)), forward);
  const rows = order(layer, forward);

  const rects = new Map<string, Rect>();
  let columnsUsed = 0;
  let rowsUsed = 0;
  for (const [key, at] of rows) {
    rects.set(key, {
      x: GRAPH_ORIGIN + at.column * (GRAPH_NODE_WIDTH + GRAPH_COLUMN_GAP),
      y: GRAPH_ORIGIN + at.row * (GRAPH_NODE_HEIGHT + GRAPH_ROW_GAP),
      width: GRAPH_NODE_WIDTH,
      height: GRAPH_NODE_HEIGHT,
    });
    columnsUsed = Math.max(columnsUsed, at.column + 1);
    rowsUsed = Math.max(rowsUsed, at.row + 1);
  }

  const unlinked = keys.filter((k) => !linked.has(k));
  let unlinkedTop: number | null = null;
  if (unlinked.length > 0) {
    const perRow = Math.max(GRAPH_MIN_GRID_COLUMNS, opts.columns);
    unlinkedTop = rowsUsed > 0 ? GRAPH_ORIGIN + rowsUsed * (GRAPH_NODE_HEIGHT + GRAPH_ROW_GAP) + GRAPH_SECTION_GAP : GRAPH_ORIGIN;
    unlinked.forEach((key, i) => {
      const column = i % perRow;
      const row = Math.floor(i / perRow);
      rects.set(key, {
        x: GRAPH_ORIGIN + column * (GRAPH_NODE_WIDTH + GRAPH_ROW_GAP),
        y: unlinkedTop! + GRAPH_SECTION_GAP / 2 + row * (GRAPH_NODE_HEIGHT + GRAPH_ROW_GAP),
        width: GRAPH_NODE_WIDTH,
        height: GRAPH_NODE_HEIGHT,
      });
    });
  }

  const bent = bends(
    usable.map((e) => ({ key: e.id, from: e.from, to: e.to })),
    GRAPH_EDGE_BEND,
  );
  const laid: LaidEdge[] = usable.map((e) => {
    const shape = edgeShape(rects.get(e.from)!, rects.get(e.to)!, bent.get(e.id) ?? 0);
    return { ...e, path: shape.path, handle: shape.label, cycle: back.has(e.id) };
  });

  const right = Math.max(...[...rects.values()].map((r) => r.x + r.width), GRAPH_ORIGIN);
  const bottom = Math.max(...[...rects.values()].map((r) => r.y + r.height), GRAPH_ORIGIN);
  return { nodes: rects, edges: laid, width: right + GRAPH_ORIGIN, height: bottom + GRAPH_ORIGIN, unlinkedTop };
}

/** The edges that close a circle, found by a depth-first walk without recursion. */
function markBackEdges(keys: string[], out: Map<string, GraphEdge[]>): Set<string> {
  const state = new Map<string, "open" | "done">();
  const back = new Set<string>();
  for (const start of keys) {
    if (state.has(start)) continue;
    const stack: Array<{ key: string; next: number }> = [{ key: start, next: 0 }];
    state.set(start, "open");
    while (stack.length > 0) {
      const top = stack[stack.length - 1]!;
      const edges = out.get(top.key)!;
      if (top.next >= edges.length) {
        state.set(top.key, "done");
        stack.pop();
        continue;
      }
      const edge = edges[top.next++]!;
      const seen = state.get(edge.to);
      if (seen === "open") back.add(edge.id);
      else if (!seen) {
        state.set(edge.to, "open");
        stack.push({ key: edge.to, next: 0 });
      }
    }
  }
  return back;
}

/** Each linked node's column: one past the furthest thing it waits on. */
function layers(keys: string[], forward: GraphEdge[]): Map<string, number> {
  const indegree = new Map<string, number>();
  const out = new Map<string, string[]>();
  for (const k of keys) {
    indegree.set(k, 0);
    out.set(k, []);
  }
  for (const e of forward) {
    indegree.set(e.to, (indegree.get(e.to) ?? 0) + 1);
    out.get(e.from)!.push(e.to);
  }
  const layer = new Map<string, number>();
  const queue = keys.filter((k) => indegree.get(k) === 0);
  for (const k of queue) layer.set(k, 0);
  while (queue.length > 0) {
    const k = queue.shift()!;
    for (const to of out.get(k)!) {
      layer.set(to, Math.max(layer.get(to) ?? 0, layer.get(k)! + 1));
      indegree.set(to, indegree.get(to)! - 1);
      if (indegree.get(to) === 0) queue.push(to);
    }
  }
  return layer;
}

/**
 * Rows within each column: by key at first, then twice pulled towards the
 * average row of what each node waits on, so arrows run short and level.
 */
function order(layer: Map<string, number>, forward: GraphEdge[]): Map<string, { column: number; row: number }> {
  const columns = new Map<number, string[]>();
  for (const [key, column] of layer) columns.set(column, [...(columns.get(column) ?? []), key]);
  for (const list of columns.values()) list.sort(keyOrder);
  const preds = new Map<string, string[]>();
  for (const e of forward) preds.set(e.to, [...(preds.get(e.to) ?? []), e.from]);

  const rowOf = new Map<string, number>();
  const settle = () => {
    for (const list of columns.values()) list.forEach((key, row) => rowOf.set(key, row));
  };
  settle();
  const depth = Math.max(-1, ...columns.keys());
  for (let sweep = 0; sweep < 2; sweep++) {
    for (let column = 1; column <= depth; column++) {
      const list = columns.get(column);
      if (!list) continue;
      const centre = (key: string) => {
        const above = preds.get(key) ?? [];
        if (above.length === 0) return rowOf.get(key)!;
        return above.reduce((sum, p) => sum + (rowOf.get(p) ?? 0), 0) / above.length;
      };
      list.sort((a, b) => centre(a) - centre(b) || keyOrder(a, b));
      list.forEach((key, row) => rowOf.set(key, row));
    }
  }

  const out = new Map<string, { column: number; row: number }>();
  for (const [column, list] of columns) list.forEach((key, row) => out.set(key, { column, row }));
  return out;
}
