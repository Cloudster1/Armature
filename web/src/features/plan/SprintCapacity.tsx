import { overBy, type SprintPlan } from "@/api/sprints";
import { cx } from "@/components/ui";
import { number } from "./SprintBands";

/**
 * What each sprint holds, against what the team said fits.
 *
 * The unestimated count sits next to the total on purpose: a sprint of eight
 * points with five unsized issues in it is not a sprint of eight points, and a
 * bar on its own would say otherwise.
 */
export function SprintCapacity({ sprints }: { sprints: SprintPlan[] }) {
  if (sprints.length === 0) return null;

  return (
    <section>
      <h2 className="mb-2 text-xs font-semibold tracking-wide text-ink-muted uppercase">
        Capacity
      </h2>
      <ul className="space-y-2" data-testid="plan-capacity">
        {sprints.map((plan) => (
          <CapacityRow key={plan.sprint.id} plan={plan} />
        ))}
      </ul>
    </section>
  );
}

function CapacityRow({ plan }: { plan: SprintPlan }) {
  const capacity = plan.sprint.capacity ?? 0;
  const over = overBy(plan) > 0;
  // The bar is drawn against whichever is larger, so an over-committed sprint
  // still shows how far past the line it has gone rather than clipping at it.
  const full = Math.max(capacity, plan.committed) || 1;

  return (
    <li className="text-sm" data-sprint-capacity={plan.sprint.name}>
      <div className="flex items-baseline justify-between gap-2">
        <span className="font-medium text-ink">
          {plan.sprint.name}
          {plan.sprint.state === "active" && (
            <span className="ml-2 rounded-full bg-accent-subtle px-2 py-0.5 text-2xs font-medium text-accent">
              Running
            </span>
          )}
        </span>
        <span className={cx("tabular-nums", over ? "text-danger" : "text-ink-muted")}>
          {number(plan.committed)}
          {plan.sprint.capacity != null && ` of ${number(plan.sprint.capacity)}`} pts
          {plan.unestimated > 0 && `, ${plan.unestimated} unestimated`}
        </span>
      </div>

      <div className="mt-1 h-2 w-full overflow-hidden rounded-full bg-surface-raised">
        <div className="flex h-full">
          <span
            className="h-full bg-success"
            style={{ width: `${(plan.completed / full) * 100}%` }}
          />
          <span
            className={cx("h-full", over ? "bg-danger" : "bg-accent")}
            style={{ width: `${((plan.committed - plan.completed) / full) * 100}%` }}
          />
        </div>
      </div>
    </li>
  );
}
