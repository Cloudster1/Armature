import type { Status, StatusCategory } from "@/api/issues";
import type { GraphInput, RuleKind, RuleType, WorkflowDetail } from "@/api/workflows";
import {
  WORKFLOW_ANYWHERE_X,
  WORKFLOW_ANYWHERE_Y,
  WORKFLOW_CANVAS_MARGIN,
  WORKFLOW_CANVAS_MIN_HEIGHT,
  WORKFLOW_CANVAS_MIN_WIDTH,
  WORKFLOW_COLUMN_GAP,
  WORKFLOW_EDGE_BEND,
  WORKFLOW_GRID,
  WORKFLOW_LAYOUT_ORIGIN_X,
  WORKFLOW_LAYOUT_ORIGIN_Y,
  WORKFLOW_NODE_HEIGHT,
  WORKFLOW_NODE_WIDTH,
  WORKFLOW_ROW_GAP,
} from "@/config";

/**
 * The workflow as the designer holds it: statuses placed on a canvas and the
 * transitions drawn between them. Everything here is plain data and pure
 * functions, so what a valid picture is can be tested without a browser.
 */
export interface Design {
  name: string;
  description: string;
  nodes: Node[];
  /** The status new issues open in. Empty until one is chosen. */
  initialId: string;
  edges: Edge[];
}

export interface Node {
  statusId: string;
  x: number;
  y: number;
}

/** The source of a transition available from every status. */
export const ANYWHERE = "";

export interface Edge {
  /** A handle that survives renames and saves; the server id once it has one. */
  key: string;
  id?: string;
  name: string;
  description: string;
  /** ANYWHERE, or the status the transition leaves. */
  from: string;
  to: string;
  rules: DraftRule[];
}

export interface DraftRule {
  kind: RuleKind;
  type: string;
  config: Record<string, unknown>;
}

import { bends as bendsBy, edgeShape, type Connection, type Point, type Rect } from "@/lib/graph";

export { clipToRect, edgeShape } from "@/lib/graph";
export type { EdgeShape, Point, Rect } from "@/lib/graph";

export function emptyDesign(): Design {
  return { name: "", description: "", nodes: [], initialId: "", edges: [] };
}

/** Reads a saved workflow onto the canvas, laying out whatever was never placed. */
export function fromWorkflow(detail: WorkflowDetail): Design {
  const { workflow, rules } = detail;
  const statusOfStep = new Map(workflow.steps.map((step) => [step.id, step.status]));

  let design: Design = {
    name: workflow.name,
    description: workflow.description ?? "",
    nodes: workflow.steps
      .filter((step) => step.layout)
      .map((step) => ({ statusId: step.status.id, x: step.layout!.x, y: step.layout!.y })),
    initialId: workflow.steps.find((step) => step.isInitial)?.status.id ?? "",
    edges: workflow.transitions.map((transition) => ({
      key: transition.id,
      id: transition.id,
      name: transition.name,
      description: transition.description ?? "",
      from: transition.fromStepId ? (statusOfStep.get(transition.fromStepId)?.id ?? ANYWHERE) : ANYWHERE,
      to: statusOfStep.get(transition.toStepId)?.id ?? "",
      rules: (rules[transition.id] ?? []).map((rule) => ({
        kind: rule.kind,
        type: rule.type,
        config: rule.config ?? {},
      })),
    })),
  };

  for (const step of workflow.steps) {
    if (!step.layout) {
      design = { ...design, nodes: [...design.nodes, { statusId: step.status.id, ...slotFor(design.nodes, step.status.category) }] };
    }
  }
  return design;
}

export function toInput(design: Design): GraphInput {
  return {
    name: design.name,
    description: design.description,
    steps: design.nodes.map((node) => ({
      statusId: node.statusId,
      isInitial: node.statusId === design.initialId,
      layout: { x: node.x, y: node.y },
    })),
    transitions: design.edges.map((edge) => ({
      id: edge.id,
      name: edge.name,
      description: edge.description,
      fromStatusId: edge.from === ANYWHERE ? null : edge.from,
      toStatusId: edge.to,
      rules: edge.rules.map((rule) => ({ kind: rule.kind, type: rule.type, config: rule.config })),
    })),
  };
}

const columnOf: Record<StatusCategory, number> = { todo: 0, in_progress: 1, done: 2 };

/**
 * The first free place in a category's column. Columns follow the categories
 * left to right, so an unplaced workflow already reads as to-do, doing, done.
 */
export function slotFor(nodes: Node[], category: StatusCategory): Point {
  const x = WORKFLOW_LAYOUT_ORIGIN_X + columnOf[category] * WORKFLOW_COLUMN_GAP;
  // The any-status box has a place of its own that nothing is laid out over.
  const occupied = [anywhereRect(), ...nodes];
  for (let row = 0; ; row++) {
    const y = WORKFLOW_LAYOUT_ORIGIN_Y + row * WORKFLOW_ROW_GAP;
    const taken = occupied.some(
      (each) => Math.abs(each.x - x) < WORKFLOW_NODE_WIDTH && Math.abs(each.y - y) < WORKFLOW_NODE_HEIGHT,
    );
    if (!taken) return { x, y };
  }
}

/** Puts a status on the canvas. The first one placed is where issues open. */
export function addStatus(design: Design, status: Status): Design {
  if (design.nodes.some((node) => node.statusId === status.id)) return design;
  return {
    ...design,
    nodes: [...design.nodes, { statusId: status.id, ...slotFor(design.nodes, status.category) }],
    initialId: design.initialId || status.id,
  };
}

/**
 * Takes a status off the canvas along with every transition touching it. A
 * transition into a status that is no longer here would be refused on save, so
 * the designer drops it rather than letting it be saved.
 */
export function removeStatus(design: Design, statusId: string): Design {
  return {
    ...design,
    nodes: design.nodes.filter((node) => node.statusId !== statusId),
    initialId: design.initialId === statusId ? "" : design.initialId,
    edges: design.edges.filter((edge) => edge.from !== statusId && edge.to !== statusId),
  };
}

export function setInitial(design: Design, statusId: string): Design {
  return { ...design, initialId: statusId };
}

/** Drops a status where it was dragged, snapped to the grid and kept on the canvas. */
export function moveNode(design: Design, statusId: string, x: number, y: number): Design {
  const snap = (value: number) => Math.max(0, Math.round(value / WORKFLOW_GRID) * WORKFLOW_GRID);
  return {
    ...design,
    nodes: design.nodes.map((node) =>
      node.statusId === statusId ? { ...node, x: snap(x), y: snap(y) } : node,
    ),
  };
}

let nextKey = 0;

/** Draws a transition. A status cannot move to itself, so that hands back nothing. */
export function connect(
  design: Design,
  from: string,
  to: string,
  name: string,
): { design: Design; key: string } | null {
  if (from === to || to === ANYWHERE) return null;
  const known = (id: string) => id === ANYWHERE || design.nodes.some((node) => node.statusId === id);
  if (!known(from) || !known(to)) return null;

  nextKey += 1;
  const key = `new-${nextKey}`;
  const edge: Edge = { key, name, description: "", from, to, rules: [] };
  return { design: { ...design, edges: [...design.edges, edge] }, key };
}

export function updateEdge(design: Design, key: string, patch: Partial<Omit<Edge, "key" | "id">>): Design {
  return {
    ...design,
    edges: design.edges.map((edge) => (edge.key === key ? { ...edge, ...patch } : edge)),
  };
}

export function removeEdge(design: Design, key: string): Design {
  return { ...design, edges: design.edges.filter((edge) => edge.key !== key) };
}

export function nodeRect(node: Point): Rect {
  return { x: node.x, y: node.y, width: WORKFLOW_NODE_WIDTH, height: WORKFLOW_NODE_HEIGHT };
}

export function anywhereRect(): Rect {
  return {
    x: WORKFLOW_ANYWHERE_X,
    y: WORKFLOW_ANYWHERE_Y,
    width: WORKFLOW_NODE_WIDTH,
    height: WORKFLOW_NODE_HEIGHT,
  };
}

/** The rect of an edge's end, or nothing if it points at a status not drawn. */
export function rectOf(design: Design, id: string): Rect | undefined {
  if (id === ANYWHERE) return anywhereRect();
  const node = design.nodes.find((each) => each.statusId === id);
  return node ? nodeRect(node) : undefined;
}

/** How far each transition bows out, so two running opposite ways stay apart. */
export function bends(edges: Edge[]): Map<string, number> {
  return bendsBy(edges as Connection[], WORKFLOW_EDGE_BEND);
}

/** How many points along a curve are checked against the cards it must miss. */
const CURVE_SAMPLES = 9;

/** How close to a card's edge a curve may pass without counting as through it. */
const CLEARANCE = 6;

/**
 * The bend an arrow takes so it does not run through a card that is neither
 * end: the pair's bend first, then further out to either side until the curve
 * is clear. An arrow that cannot be cleared keeps the pair's bend.
 */
export function bendClearOf(from: Rect, to: Rect, obstacles: Rect[], base: number, step: number): number {
  const candidates = [base, base + step, base - step, base + 2 * step, base - 2 * step];
  for (const bend of candidates) {
    if (!crossesAny(edgeShape(from, to, bend).path, obstacles)) return bend;
  }
  return base;
}

/** Whether a quadratic path, sampled along its length, enters any of the rects. */
function crossesAny(path: string, obstacles: Rect[]): boolean {
  const numbers = path.match(/-?\d+(\.\d+)?/g)?.map(Number) ?? [];
  if (numbers.length < 6) return false;
  const [x0 = 0, y0 = 0, cx = 0, cy = 0, x1 = 0, y1 = 0] = numbers;
  for (let i = 1; i < CURVE_SAMPLES; i++) {
    const t = i / CURVE_SAMPLES;
    const x = (1 - t) * (1 - t) * x0 + 2 * (1 - t) * t * cx + t * t * x1;
    const y = (1 - t) * (1 - t) * y0 + 2 * (1 - t) * t * cy + t * t * y1;
    if (obstacles.some((r) => x > r.x + CLEARANCE && x < r.x + r.width - CLEARANCE && y > r.y + CLEARANCE && y < r.y + r.height - CLEARANCE)) {
      return true;
    }
  }
  return false;
}

/** How big the canvas has to be to show everything, with room to drag into. */
export function canvasSize(design: Design): { width: number; height: number } {
  const rects = [anywhereRect(), ...design.nodes.map(nodeRect)];
  const right = Math.max(...rects.map((rect) => rect.x + rect.width));
  const bottom = Math.max(...rects.map((rect) => rect.y + rect.height));
  return {
    width: Math.max(WORKFLOW_CANVAS_MIN_WIDTH, right + WORKFLOW_CANVAS_MARGIN),
    height: Math.max(WORKFLOW_CANVAS_MIN_HEIGHT, bottom + WORKFLOW_CANVAS_MARGIN),
  };
}

/**
 * What would stop this design from saving, in the words the server would use.
 * Shown before the round trip, so the button says why it is not going to work.
 */
export function problems(design: Design, statuses: Map<string, Status>): string[] {
  const out: string[] = [];
  if (design.name.trim() === "") out.push("The workflow needs a name.");
  if (design.nodes.length === 0) out.push("Put at least one status on the canvas.");
  else if (!design.initialId) out.push("Choose the status new issues open in.");
  if (design.edges.some((edge) => edge.name.trim() === "")) out.push("Every transition needs a name.");

  const seen = new Set<string>();
  for (const edge of design.edges) {
    const key = `${edge.from}|${edge.name.trim().toLowerCase()}`;
    if (edge.name.trim() && seen.has(key)) {
      const from = edge.from === ANYWHERE ? "any status" : (statuses.get(edge.from)?.name ?? "the same status");
      out.push(`Two transitions called ${edge.name.trim()} leave ${from}.`);
    }
    seen.add(key);
  }
  return out;
}

/** A rule as one line for the inspector, with its configuration spelled out. */
export function describeRule(rule: DraftRule, catalogue: RuleType[]): string {
  const type = catalogue.find((each) => each.type === rule.type);
  if (!type) return rule.type;
  const parts = type.options
    .map((option) => {
      const value = rule.config[option.name];
      if (Array.isArray(value)) return value.length ? `${option.label}: ${value.join(", ")}` : "";
      return value ? `${option.label}: ${String(value)}` : "";
    })
    .filter(Boolean);
  return parts.length ? `${type.label} (${parts.join("; ")})` : type.label;
}
