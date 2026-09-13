import type { ChartGroup, ChartSeriesReport, WidgetConfig } from "@/api/reports";
import { CHART_MAX_GROUPS } from "@/config";
import type { Series } from "./charts";

/**
 * The maths behind the chart widget, kept apart from the drawing so it can be
 * tested as numbers: which colour a group wears, how a tail folds into Other,
 * where an arc or a bar sits.
 */

/** The words a chart is asked with, in the order the settings offer them. */
export const GROUP_FIELDS = ["status", "statusCategory", "type", "priority", "assignee", "team", "milestone", "label", "fixVersion", "component"] as const;
export type GroupField = (typeof GROUP_FIELDS)[number];
export type ChartShape = "bar" | "stacked" | "donut" | "line";

export const fieldLabel: Record<GroupField, string> = {
  status: "Status",
  statusCategory: "Status category",
  type: "Issue type",
  priority: "Priority",
  assignee: "Assignee",
  team: "Team",
  milestone: "Milestone",
  label: "Label",
  fixVersion: "Fix version",
  component: "Component",
};

/** What the chart says it is, from its settings: "Issues by type, split by status category". */
export function describeChart(config: WidgetConfig): string {
  const what = config.measure === "points" ? "Points" : "Issues";
  if (config.shape === "line") {
    const counts = config.series === "resolved" ? "resolved" : config.series === "open" ? "open" : "created";
    const by = config.groupBy && config.groupBy in fieldLabel ? `, by ${fieldLabel[config.groupBy as GroupField].toLowerCase()}` : "";
    return `${what} ${counts} per ${config.interval === "month" ? "month" : "week"}${by}`;
  }
  const by = fieldLabel[(config.groupBy as GroupField) ?? "status"] ?? "status";
  const split = config.shape === "stacked" && config.splitBy && config.splitBy in fieldLabel ? `, split by ${fieldLabel[config.splitBy as GroupField].toLowerCase()}` : "";
  return `${what} by ${by.toLowerCase()}${split}`;
}

/** The six series colours, in the order a dashboard hands them out; literal so Tailwind sees them. */
export const CHART_TONES = ["text-chart-1", "text-chart-2", "text-chart-3", "text-chart-4", "text-chart-5", "text-chart-6"] as const;

/** The fold of everything past the sixth group. */
export const OTHER = "Other";
const OTHER_TONE = "text-ink-subtle";

const categoryTone: Record<string, string> = {
  todo: "text-status-todo",
  in_progress: "text-status-progress",
  done: "text-status-done",
};

/**
 * Colour follows the entity, not its rank: a label keeps its tone whichever
 * size it has today, so a filter that shrinks a group does not repaint the
 * rest. Labels take tones in alphabetical order; a status wears its category.
 */
export function tonesFor(groups: ChartGroup[]): Map<string, string> {
  const tones = new Map<string, string>();
  const named = groups.filter((g) => g.label !== OTHER).map((g) => g.label).sort((a, b) => a.localeCompare(b));
  named.forEach((label, index) => tones.set(label, CHART_TONES[index % CHART_TONES.length]!));
  for (const g of groups) {
    if (g.category && categoryTone[g.category]) tones.set(g.label, categoryTone[g.category]!);
  }
  if (groups.some((g) => g.label === OTHER)) tones.set(OTHER, OTHER_TONE);
  return tones;
}

/** Everything past the largest max groups folds into Other; a seventh series is never a seventh colour. */
export function foldTail(groups: ChartGroup[], max: number = CHART_MAX_GROUPS): ChartGroup[] {
  if (groups.length <= max) return groups;
  const kept = groups.slice(0, max - 1);
  const tail = groups.slice(max - 1);
  const parts = new Map<string, number>();
  for (const g of tail) for (const part of g.parts) parts.set(part.label, (parts.get(part.label) ?? 0) + part.value);
  return [
    ...kept,
    {
      label: OTHER,
      value: tail.reduce((sum, g) => sum + g.value, 0),
      parts: [...parts.entries()].map(([label, value]) => ({ label, value })).sort((a, b) => b.value - a.value),
    },
  ];
}

export interface Arc {
  label: string;
  value: number;
  /** Fractions of the whole turn, clockwise from the top. */
  start: number;
  end: number;
  share: number;
}

/** Where each slice of a donut begins and ends, as fractions of the turn. */
export function donutLayout(groups: ChartGroup[]): Arc[] {
  const total = groups.reduce((sum, g) => sum + g.value, 0);
  if (total <= 0) return [];
  let at = 0;
  return groups
    .filter((g) => g.value > 0)
    .map((g) => {
      const share = g.value / total;
      const arc = { label: g.label, value: g.value, start: at, end: at + share, share };
      at += share;
      return arc;
    });
}

/**
 * The SVG path of a ring segment from start to end (fractions of a turn,
 * clockwise from twelve o'clock). A whole turn is drawn as two halves, since
 * an arc that ends where it starts is nothing to the renderer.
 */
export function arcPath(cx: number, cy: number, r: number, inner: number, start: number, end: number): string {
  const whole = end - start >= 1 - 1e-9;
  if (whole) return `${arcPath(cx, cy, r, inner, start, start + 0.5)} ${arcPath(cx, cy, r, inner, start + 0.5, start + 1 - 1e-6)}`;
  const point = (radius: number, turn: number) => {
    const angle = (turn - 0.25) * 2 * Math.PI;
    return `${round(cx + radius * Math.cos(angle))} ${round(cy + radius * Math.sin(angle))}`;
  };
  const large = end - start > 0.5 ? 1 : 0;
  return [`M${point(r, start)}`, `A${r} ${r} 0 ${large} 1 ${point(r, end)}`, `L${point(inner, end)}`, `A${inner} ${inner} 0 ${large} 0 ${point(inner, start)}`, "Z"].join(" ");
}

export interface Column {
  label: string;
  value: number;
  /** Height as a share of the tallest column, 0 to 1. */
  height: number;
  segments: Array<{ label: string; value: number; height: number }>;
}

/** Column heights against the tallest, with each stack's segments as shares of the same scale. */
export function columnLayout(groups: ChartGroup[]): Column[] {
  const max = Math.max(0, ...groups.map((g) => g.value));
  const scale = (value: number) => (max > 0 ? value / max : 0);
  return groups.map((g) => ({
    label: g.label,
    value: g.value,
    height: scale(g.value),
    segments: g.parts.map((part) => ({ label: part.label, value: part.value, height: scale(part.value) })),
  }));
}

/** The names a stack is split by, in a fixed order, so the legend and every bar agree. */
export function segmentNames(groups: ChartGroup[]): string[] {
  const names = new Set<string>();
  for (const g of groups) for (const part of g.parts) names.add(part.label);
  return [...names].sort((a, b) => a.localeCompare(b));
}

/** Lines for the LineChart, one per series, coloured by name in a fixed order. */
export function seriesFrom(report: ChartSeriesReport): Series[] {
  const names = report.series.map((line) => line.name).sort((a, b) => a.localeCompare(b));
  return report.series.map((line) => ({
    name: line.name,
    tone: CHART_TONES[names.indexOf(line.name) % CHART_TONES.length]!,
    points: line.points.map((point, x) => ({ x, y: point.value })),
  }));
}

/** A number as a chart writes it: whole when it is, one decimal otherwise. */
export function figure(value: number): string {
  return Number.isInteger(value) ? String(value) : value.toFixed(1);
}

/** Two decimals, which is all an SVG path in a unit box needs. */
export function round(v: number): number {
  return Math.round(v * 100) / 100;
}
