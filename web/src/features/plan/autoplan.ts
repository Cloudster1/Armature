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
    expected: { startDate: dayOf(move.from.startDate), dueDate: dayOf(move.from.dueDate) },
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
  return `moves from ${formatDay(dayOf(move.from.startDate), locale)} to ${formatDay(dayOf(move.to.startDate), locale)}, ${days}, after ${move.after}`;
}
