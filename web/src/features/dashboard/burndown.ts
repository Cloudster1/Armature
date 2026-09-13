import type { BurndownPoint, SprintBurndown } from "@/api/reports";
import { MS_PER_DAY } from "@/features/plan/scale";
import type { Series } from "./charts";

/** Days from the sprint's first day to a date, so every series shares an axis. */
function dayIndex(from: Date, iso: string): number {
  return Math.round((new Date(iso.slice(0, 10)).getTime() - from.getTime()) / MS_PER_DAY);
}

/** The first and last day the chart spans: the sprint's dates when it has them, else its points. */
export function chartSpan(curve: SprintBurndown): { from: Date; to: Date } {
  const days = curve.points.map((p) => p.day.slice(0, 10));
  const first = curve.sprint.startsOn?.slice(0, 10) ?? days[0] ?? new Date().toISOString().slice(0, 10);
  const last = curve.sprint.endsOn?.slice(0, 10) ?? days[days.length - 1] ?? first;
  return { from: new Date(first), to: new Date(last < first ? first : last) };
}

/**
 * The three lines of a burndown: what is left, day by day; the scope, stepped,
 * so an issue added mid-sprint shows as a step up; and the line the sprint
 * should follow, from the first day's scope to nothing on the last day. Days
 * before the first snapshot are not invented.
 */
export function burndownSeries(curve: SprintBurndown): Series[] {
  const { from, to } = chartSpan(curve);
  const at = (p: BurndownPoint) => dayIndex(from, p.day);
  const remaining = curve.points.map((p) => ({ x: at(p), y: p.remaining }));
  const scope = curve.points.map((p) => ({ x: at(p), y: p.scope }));
  const length = Math.max(1, dayIndex(from, to.toISOString()));
  const first = curve.points[0];
  const ideal = first ? [{ x: at(first), y: first.scope }, { x: length, y: 0 }] : [];
  return [
    { name: "scope", tone: "text-ink-subtle", points: scope, step: true },
    { name: "ideal", tone: "text-ink-muted", points: ideal, dashed: true },
    { name: "remaining", tone: "text-accent", points: remaining },
  ];
}

/** What to say under the chart: how the scope moved and what is unsized. */
export function describeScope(curve: SprintBurndown): string {
  const first = curve.points[0];
  const last = curve.points[curve.points.length - 1];
  if (!first || !last) return "No day has been written down yet.";
  const parts = [`${last.remaining} of ${last.scope} pts left`];
  if (last.scope !== first.scope) parts.push(`scope ${first.scope} to ${last.scope} pts`);
  if (last.unestimated > 0) parts.push(`${last.unestimated} unestimated`);
  parts.push(`${last.issuesDone}/${last.issues} issues done`);
  return parts.join(" · ");
}

/** Short day labels for the axis: the first day, the last, and the middle. */
export function axisLabels(curve: SprintBurndown): string[] {
  const { from, to } = chartSpan(curve);
  const label = (d: Date) => `${d.getUTCDate()}/${d.getUTCMonth() + 1}`;
  const mid = new Date((from.getTime() + to.getTime()) / 2);
  return [label(from), label(mid), label(to)];
}
