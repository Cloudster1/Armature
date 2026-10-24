import { ApiError } from "@/api/client";
import type { Alignment, PlanMove, ScheduleMoveInput } from "@/api/plan";
import { formatDay } from "@/lib/week";
import { t } from "@/i18n";

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
  const days = t.plan.autoPlan.workingDays(move.workingDays);
  const to = formatDay(dayOf(move.to.startDate), locale);
  const after = move.after ? t.plan.autoPlan.after(move.after) : "";
  if (!move.from) return t.plan.autoPlan.placedFrom(to, days, after);
  return t.plan.autoPlan.movesFrom(formatDay(dayOf(move.from.startDate), locale), to, days, after);
}

/** What the proposal would do, counted, as the preview's first line. */
export function summarise(alignment: Alignment): string {
  const placed = alignment.moves.filter((move) => !move.from).length;
  const moved = alignment.moves.length - placed;
  const words = t.plan.autoPlan;
  const parts = [moved > 0 && words.wouldMove(words.issues(moved)), placed > 0 && words.wouldPlace(words.issues(placed))].filter((part): part is string =>
    Boolean(part),
  );
  if (parts.length === 0) return alignment.unplaced.length > 0 ? words.nothingPlaceable : words.alreadyAligned;
  return words.summary(parts);
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
  if (key) return t.plan.autoPlan.undoChanged(key);
  return error instanceof Error ? error.message : t.plan.autoPlan.undoFailed;
}

/** What the toast says after a blocker was dragged later, and its button; nothing when no issue it blocks starts too early. */
export function followOffer(key: string, moves: number): { message: string; action: string } | null {
  if (moves === 0) return null;
  const issues = t.plan.autoPlan.issues(moves);
  return { message: t.plan.autoPlan.followMessage(issues, key, moves === 1), action: t.plan.autoPlan.followAction(issues) };
}
