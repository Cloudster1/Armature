import { ApiError } from "@/api/client";
import type { Alignment, PlanMove, ScheduleMoveInput } from "@/api/plan";
import { formatDay } from "@/lib/week";

/** A day as the server writes it, cut to YYYY-MM-DD. */
export function dayOf(value: string): string {
  return value.slice(0, 10);
}

/** The moves as the set the server saves, each expecting the dates the proposal was read from. */
export function movesToSave(alignment: Alignment): ScheduleMoveInput[] {
  return alignment.moves.map((move) => ({
    key: move.issueKey,
    expected: move.from ? { startDate: dayOf(move.from.startDate), dueDate: dayOf(move.from.dueDate) } : { startDate: null, dueDate: null },
    startDate: dayOf(move.to.startDate),
    dueDate: dayOf(move.to.dueDate),
  }));
}

/** Where each moved issue would go, by key, for the ghost bars. */
export function proposedBars(alignment: Alignment | null): Map<string, { start: string; due: string }> | null {
  if (!alignment) return null;
  return new Map(alignment.moves.map((move) => [move.issueKey, { start: dayOf(move.to.startDate), due: dayOf(move.to.dueDate) }]));
}

/** One move as a line a lead reads: what moves, from when to when, and why. */
export function describeMove(move: PlanMove, locale: string): string {
  const days = `${move.workingDays} working day${move.workingDays === 1 ? "" : "s"}`;
  const to = formatDay(dayOf(move.to.startDate), locale);
  const after = move.after ? `, after ${move.after}` : "";
  if (!move.from) return `is placed from ${to}, ${days}${after}`;
  return `moves from ${formatDay(dayOf(move.from.startDate), locale)} to ${to}, ${days}${after}`;
}

/** What the proposal would do, counted, as the preview's first line. */
export function summarise(alignment: Alignment): string {
  const placed = alignment.moves.filter((move) => !move.from).length;
  const moved = alignment.moves.length - placed;
  const issues = (n: number) => `${n} issue${n === 1 ? "" : "s"}`;
  const parts = [
    moved > 0 && `${issues(moved)} would move to start after the issues blocking them`,
    placed > 0 && `${issues(placed)} without dates would be placed`,
  ].filter(Boolean);
  if (parts.length === 0)
    return alignment.unplaced.length > 0 ? "Nothing can be placed or needs to move." : "Every issue already starts after the issues blocking it.";
  return `${parts.join(", and ")}. Nothing is saved until you apply.`.replace(/^./, (c) => c.toUpperCase());
}

/** The moves that put an applied auto plan back, each expecting the dates it set. */
export function movesBack(applied: ScheduleMoveInput[]): ScheduleMoveInput[] {
  return applied.map((move) => ({
    key: move.key,
    expected: { startDate: move.startDate, dueDate: move.dueDate },
    startDate: move.expected.startDate,
    dueDate: move.expected.dueDate,
  }));
}

/** Why an undo saved nothing, in words about the undo rather than about a preview. */
export function undoRefusal(error: unknown): string {
  const key = error instanceof ApiError && error.code === "moved_since" ? error.fields.issueKey : undefined;
  if (key) return `${key} was changed after the auto plan, so nothing was undone. Move it back by hand or run auto plan again.`;
  return error instanceof Error ? error.message : "The auto plan could not be undone. Try again.";
}

/** What the toast says after a blocker was dragged later, and its button; nothing when no issue it blocks starts too early. */
export function followOffer(key: string, moves: number): { message: string; action: string } | null {
  if (moves === 0) return null;
  const issues = `${moves} issue${moves === 1 ? "" : "s"}`;
  return { message: `${issues} that ${key} blocks now ${moves === 1 ? "starts" : "start"} before it ends.`, action: `Move the ${issues} it blocks` };
}
