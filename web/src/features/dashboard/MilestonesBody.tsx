import { Link } from "@tanstack/react-router";
import { describeProgress, formatDay, isOverdue } from "@/api/milestones";
import { useReportSource, type MilestoneRow, type MilestonesReport } from "@/api/reports";
import { Tag } from "@/components/ui";
import { ProgressBar } from "@/features/milestones/ProgressBar";
import { MS_PER_DAY } from "@/features/plan/scale";
import { figure } from "./chart";
import { Stat } from "./charts";
import { useWidgetReport, type BodyProps } from "./useWidgetReport";
import { Loading, WidgetState } from "./WidgetState";

/** How far a due day is from today, in whole days; negative once it has passed. */
export function daysUntil(dueOn: string, today: Date): number {
  const due = Date.parse(`${dueOn.slice(0, 10)}T00:00:00Z`);
  const now = Date.UTC(today.getUTCFullYear(), today.getUTCMonth(), today.getUTCDate());
  return Math.round((due - now) / MS_PER_DAY);
}

/** "Due 24 Sep 2026, 12 days left", "Due 24 Sep 2026, overdue by 3 days", or the close. */
export function describeDue(row: MilestoneRow, today: Date): string {
  if (row.closedAt) return `Closed ${formatDay(row.closedAt)}`;
  if (!row.dueOn) return "No due day yet";
  const left = daysUntil(row.dueOn, today);
  const when = formatDay(row.dueOn);
  if (left > 1) return `Due ${when}, ${left} days left`;
  if (left === 1) return `Due ${when}, tomorrow`;
  if (left === 0) return `Due ${when}, today`;
  const over = -left;
  return `Due ${when}, overdue by ${over} ${over === 1 ? "day" : "days"}`;
}

/**
 * The open milestones with their progress, or one milestone in full. The
 * numbers are the milestone card's own, narrowed by the dashboard's filter.
 */
export function MilestonesBody({ projectKey, widget, narrow }: BodyProps) {
  const { data, error } = useWidgetReport<MilestonesReport>(projectKey, widget, narrow);
  const { readOnly } = useReportSource();
  if (error) return <WidgetState error={error} />;
  if (!data) return <Loading />;
  const today = new Date();
  const one = widget.config?.milestoneId;
  if (one) {
    const row = data.milestones[0];
    if (!row) return <p className="text-sm text-ink-subtle">That milestone is gone. Pick another in the widget's settings.</p>;
    return <OneMilestone row={row} today={today} />;
  }
  if (data.milestones.length === 0) {
    return (
      <p className="text-sm text-ink-subtle">
        No open milestones.{" "}
        {!readOnly && (
          <Link to="/projects/$projectKey/milestones" params={{ projectKey }} className="text-accent hover:underline">
            Set one on the milestones page.
          </Link>
        )}
      </p>
    );
  }
  return (
    <ul className="space-y-3" data-milestones-widget="">
      {data.milestones.map((row) => (
        <li key={row.id} data-milestone-widget={row.name}>
          <div className="mb-1 flex items-baseline justify-between gap-2 text-sm">
            <span className="min-w-0 truncate font-medium text-ink">{row.name}</span>
            <span className="shrink-0 text-xs text-ink-muted tabular-nums">{describeProgress(row.progress)}</span>
          </div>
          <ProgressBar progress={row.progress} />
          <p className="mt-1 flex items-center gap-2 text-2xs text-ink-subtle">
            {describeDue(row, today)}
            {isOverdue(row, today) && <Tag className="bg-danger-subtle text-danger">Overdue</Tag>}
          </p>
        </li>
      ))}
    </ul>
  );
}

function OneMilestone({ row, today }: { row: MilestoneRow; today: Date }) {
  const overdue = isOverdue(row, today);
  return (
    <div data-milestone-widget={row.name} data-milestone-single="">
      <div className="flex items-start justify-between gap-3">
        <div className="min-w-0">
          <p className="truncate text-sm font-medium text-ink">{row.name}</p>
          <p className="mt-0.5 flex items-center gap-2 text-xs text-ink-muted">
            {describeDue(row, today)}
            {overdue && <Tag className="bg-danger-subtle text-danger">Overdue</Tag>}
            {row.closedAt && <Tag>Closed</Tag>}
          </p>
        </div>
        <Stat value={`${row.progress.percent}%`} label="done" />
      </div>
      <ProgressBar progress={row.progress} className="mt-3" />
      <dl className="mt-3 grid grid-cols-3 gap-2 text-xs">
        <div>
          <dt className="text-ink-subtle">Done</dt>
          <dd className="font-medium text-ink tabular-nums">{row.progress.done}</dd>
        </div>
        <div>
          <dt className="text-ink-subtle">In progress</dt>
          <dd className="font-medium text-ink tabular-nums">{row.progress.inProgress}</dd>
        </div>
        <div>
          <dt className="text-ink-subtle">To do</dt>
          <dd className="font-medium text-ink tabular-nums">{row.progress.todo}</dd>
        </div>
      </dl>
      <p className="mt-2 text-xs text-ink-subtle tabular-nums" data-milestone-progress="">
        {describeProgress(row.progress)}
        {row.points > 0 && ` · ${figure(row.donePoints)} of ${figure(row.points)} pts`}
      </p>
    </div>
  );
}
