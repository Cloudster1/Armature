import { Link } from "@tanstack/react-router";
import { describeProgress, formatDay, isOverdue, type Milestone } from "@/api/milestones";
import { cx } from "@/components/ui";
import { PLAN_MILESTONE_BAND_HEIGHT } from "@/config";
import { ProgressBar } from "@/features/milestones/ProgressBar";
import { flagSide, labelledFlags } from "./layout";
import { markersFor } from "./markers";
import type { Scale } from "./scale";

/**
 * The milestones drawn above the timeline as flags on the day they are due,
 * so the bars below can be read as "before the release" or "after it".
 */
export function MilestoneBand({ scale, milestones }: { scale: Scale; milestones: Milestone[] }) {
  const markers = markersFor(scale, milestones);
  const labelled = labelledFlags(markers.map((m) => m.x));
  if (!milestones.some((m) => m.dueOn)) return null;

  return (
    <div
      className="relative border-b border-border"
      style={{ height: PLAN_MILESTONE_BAND_HEIGHT }}
      data-testid="plan-milestone-band"
    >
      {markers.map(({ milestone, x }, index) => {
        const overdue = isOverdue(milestone, new Date());
        const side = flagSide(x, scale.width);
        const spoken = labelled[index] ?? true;
        return (
          <div
            key={milestone.id}
            data-milestone-flag={milestone.name}
            data-flag-side={side}
            title={`${milestone.name}: ${describeProgress(milestone.progress)}`}
            className={cx(
              "absolute top-0.5 bottom-0.5 flex items-center gap-1 px-1.5 text-2xs whitespace-nowrap",
              side === "right" ? "rounded-r" : "flex-row-reverse rounded-l",
              overdue ? "bg-danger-subtle text-danger" : "bg-accent-subtle text-accent",
            )}
            style={side === "right" ? { left: x } : { right: scale.width - x }}
          >
            <span aria-hidden="true" className="text-2xs">◆</span>
            {spoken && <span className="font-medium">{milestone.name}</span>}
            {spoken && <span className="tabular-nums opacity-80">{milestone.progress.percent}%</span>}
          </div>
        );
      })}
    </div>
  );
}

/** The dashed line each dated milestone drops through the rows. */
export function MilestoneLines({ scale, milestones }: { scale: Scale; milestones: Milestone[] }) {
  return (
    <>
      {markersFor(scale, milestones).map(({ milestone, x }) => (
        <span
          key={milestone.id}
          aria-hidden="true"
          className={cx(
            "pointer-events-none absolute top-0 bottom-0 w-0 border-l border-dashed",
            isOverdue(milestone, new Date()) ? "border-danger/60" : "border-accent/60",
          )}
          style={{ left: x }}
        />
      ))}
    </>
  );
}

/** How far along each milestone is, dated or not. */
export function MilestoneProgress({ projectKey, milestones }: { projectKey: string; milestones: Milestone[] }) {
  if (milestones.length === 0) return null;
  return (
    <section>
      <div className="mb-2 flex items-baseline justify-between">
        <h2 className="text-xs font-semibold tracking-wide text-ink-muted uppercase">Milestones</h2>
        <Link to="/projects/$projectKey/milestones" params={{ projectKey }} className="text-sm text-ink-muted hover:text-ink">
          All milestones
        </Link>
      </div>
      <ul className="space-y-2" data-testid="plan-milestones">
        {milestones.map((milestone) => (
          <li key={milestone.id} className="text-sm" data-plan-milestone={milestone.name}>
            <div className="flex items-baseline justify-between gap-2">
              <span className="font-medium text-ink">
                {milestone.name}
                {milestone.dueOn && (
                  <span className="ml-2 text-xs font-normal text-ink-muted">due {formatDay(milestone.dueOn)}</span>
                )}
              </span>
              <span className="text-ink-muted tabular-nums">{describeProgress(milestone.progress)}</span>
            </div>
            <ProgressBar progress={milestone.progress} className="mt-1" />
          </li>
        ))}
      </ul>
    </section>
  );
}
