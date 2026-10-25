import type { BurndownPoint, SprintBurndown } from "@/api/reports";
import { MS_PER_DAY } from "@/features/plan/scale";
import type { Series } from "./charts";
import { t } from "@/i18n";

/** Days from the sprint's first day to a date, so every series shares an axis. */
function dayIndex(from: Date, iso: string): number {
  return Math.round((new Date(iso.slice(0, 10)).getTime() - from.getTime()) / MS_PER_DAY);
}

/**
 * The first and last day the chart spans: the sprint's dates widened to its
 * points, so a sprint started early or ended late keeps every day on the chart.
 */
export function chartSpan(curve: SprintBurndown): { from: Date; to: Date } {
  const days = curve.points.map((p) => p.day.slice(0, 10));
  const firstDays = [curve.sprint.startsOn?.slice(0, 10), days[0]].filter((d): d is string => Boolean(d)).sort();
  const lastDays = [curve.sprint.endsOn?.slice(0, 10), days[days.length - 1]].filter((d): d is string => Boolean(d)).sort();
  const first = firstDays[0] ?? new Date().toISOString().slice(0, 10);
  const last = lastDays[lastDays.length - 1] ?? first;
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
  const lastDay = Math.max(1, dayIndex(from, curve.sprint.endsOn ?? to.toISOString()));
  const first = curve.points[0];
  const ideal = first
    ? [
        { x: at(first), y: first.scope },
        { x: lastDay, y: 0 },
      ]
    : [];
  return [
    { name: t.dashboard.burndown.scope, tone: "text-ink-subtle", points: scope, step: true },
    { name: t.dashboard.burndown.ideal, tone: "text-ink-muted", points: ideal, dashed: true },
    { name: t.dashboard.burndown.remaining, tone: "text-accent", points: remaining },
  ];
}

/** What to say under the chart: how the scope moved and what is unsized. */
export function describeScope(curve: SprintBurndown): string {
  const first = curve.points[0];
  const last = curve.points[curve.points.length - 1];
  const words = t.dashboard.burndown;
  if (!first || !last) return words.noDay;
  const parts = [words.left(last.remaining, last.scope)];
  if (last.scope !== first.scope) parts.push(words.scopeMoved(first.scope, last.scope));
  if (last.unestimated > 0) parts.push(words.unestimated(last.unestimated));
  parts.push(words.issuesDone(last.issuesDone, last.issues));
  return parts.join(" · ");
}

/** Short day labels for the axis: the first day, the last, and the middle. */
export function axisLabels(curve: SprintBurndown): string[] {
  const { from, to } = chartSpan(curve);
  const label = (d: Date) => t.dashboard.chart.dayMonth(d.getUTCDate(), d.getUTCMonth() + 1);
  const mid = new Date((from.getTime() + to.getTime()) / 2);
  return [label(from), label(mid), label(to)];
}
