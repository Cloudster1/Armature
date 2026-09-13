import { PLAN_DRAFT_INPUT_MIN_PX, PLAN_DRAG_THRESHOLD_PX } from "@/config";
import { addDays, day, daysBetween, type Scale } from "./scale";
import type { Row } from "./views";

/** Where a ticket made from a row lands: in a sprint, on a team, or nowhere in particular. */
export interface Placement {
  sprintId?: string;
  teamId?: string;
}

/**
 * The days a drag across empty calendar covers, whichever way it went,
 * snapped to whole days and never less than one.
 */
export function dragRange(scale: Scale, anchorX: number, x: number): { start: Date; due: Date } {
  const a = scale.dateAt(Math.min(anchorX, x));
  const b = scale.dateAt(Math.max(anchorX, x));
  const start = day(a);
  const due = daysBetween(start, day(b)) < 0 ? start : day(b);
  return { start, due: due < start ? start : due };
}

/** Whether the pointer travelled far enough for a drag to be meant. */
export function isRealDrag(anchorX: number, x: number, min = PLAN_DRAG_THRESHOLD_PX): boolean {
  return Math.abs(x - anchorX) >= min;
}

/** Where the summary box sits after a drag: over the days dragged, wide enough to type in, inside the canvas. */
export function draftBox(scale: Scale, start: Date, due: Date, canvasWidth: number): { left: number; width: number } {
  const width = Math.max(PLAN_DRAFT_INPUT_MIN_PX, scale.spanWidth(start, due));
  const left = Math.max(0, Math.min(scale.x(start), Math.max(0, canvasWidth - width)));
  return { left, width: Math.min(width, Math.max(PLAN_DRAFT_INPUT_MIN_PX, canvasWidth)) };
}

/** The add row's placement, read off the group it sits under. */
export function placementFor(row: Extract<Row, { kind: "group" }> | null): Placement {
  if (!row?.sprint) return {};
  return { sprintId: row.sprint.sprint.id, teamId: row.sprint.sprint.teamId ?? undefined };
}

/** A default range for a ticket made without dragging: today for a week. */
export function defaultRange(today: Date, spanDays: number): { start: Date; due: Date } {
  const start = day(today);
  return { start, due: addDays(start, spanDays) };
}
