import { overBy, type SprintPlan } from "@/api/sprints";
import { cx } from "@/components/ui";
import { PLAN_SPRINT_BAND_HEIGHT } from "@/config";
import { barFor, type Scale } from "./scale";

/**
 * The sprints drawn across the top of the timeline, so the dates below can be
 * read as "in this sprint" rather than as bare days.
 *
 * A sprint with no dates has nothing to draw and is left to the capacity list,
 * which still shows what it holds.
 */
export function SprintBands({ scale, sprints }: { scale: Scale; sprints: SprintPlan[] }) {
  const drawable = sprints.filter((plan) => plan.sprint.startsOn && plan.sprint.endsOn);
  if (drawable.length === 0) return null;

  return (
    <div
      className="relative border-b border-border bg-surface-raised/50"
      style={{ height: PLAN_SPRINT_BAND_HEIGHT }}
      data-testid="plan-sprint-bands"
    >
      {drawable.map((plan) => {
        const bar = barFor(scale, plan.sprint.startsOn, plan.sprint.endsOn);
        if (!bar) return null;
        const over = overBy(plan) > 0;

        return (
          <div
            key={plan.sprint.id}
            data-sprint-band={plan.sprint.name}
            title={`${plan.sprint.name}: ${describe(plan)}`}
            className={cx(
              "absolute top-1 bottom-1 flex items-center gap-1.5 overflow-hidden rounded px-2 text-2xs whitespace-nowrap",
              over
                ? "bg-danger-subtle text-danger ring-1 ring-danger/40"
                : plan.sprint.state === "active"
                  ? "bg-accent-subtle text-accent ring-1 ring-accent/40"
                  : "bg-surface text-ink-muted ring-1 ring-border",
            )}
            style={{ left: bar.left, width: bar.width }}
          >
            <span className="truncate font-medium">{plan.sprint.name}</span>
            <span className="shrink-0 tabular-nums">{describe(plan)}</span>
          </div>
        );
      })}
    </div>
  );
}

/** The one line a person reads off a sprint: how much is in it, against how
 * much fits. */
export function describe(plan: SprintPlan): string {
  const committed = number(plan.committed);
  if (plan.sprint.capacity === undefined || plan.sprint.capacity === null) {
    return `${committed} pts`;
  }
  return `${committed}/${number(plan.sprint.capacity)} pts`;
}

/** Writes an estimate the way a person does: 5 rather than 5.00. */
export function number(value: number): string {
  return String(Math.round(value * 100) / 100);
}
