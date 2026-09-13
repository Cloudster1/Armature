import { humanDuration, metricName, type Timer } from "@/api/desk";
import { cx } from "@/components/ui";

/**
 * Where a clock stands, as a dot and a phrase: time left, or how far over, or
 * that it stopped. Breached is red before it is anything else.
 */
export function SlaBadge({ timer, short = false }: { timer: Timer; short?: boolean }) {
  const label = short ? "" : metricName(timer.metric) + ": ";
  let dot = "bg-status-done";
  let text: string;
  if (timer.completedAt) {
    text = timer.breached ? `missed by ${humanDuration(-timer.remainingSeconds)}` : `met with ${humanDuration(timer.remainingSeconds)} to spare`;
    dot = timer.breached ? "bg-danger" : "bg-status-done";
  } else if (timer.breached) {
    text = `${humanDuration(-timer.remainingSeconds)} over`;
    dot = "bg-danger";
  } else if (timer.paused) {
    text = `paused, ${humanDuration(timer.remainingSeconds)} left`;
    dot = "bg-status-todo";
  } else {
    text = `${humanDuration(timer.remainingSeconds)} left`;
    // Under an hour reads as urgent.
    dot = timer.remainingSeconds < 3600 ? "bg-warning" : "bg-status-done";
  }
  return (
    <span
      className={cx("inline-flex items-center gap-1.5 text-xs whitespace-nowrap", timer.breached ? "text-danger" : "text-ink-muted")}
      data-sla={timer.metric}
      data-sla-state={timer.completedAt ? "completed" : timer.breached ? "breached" : timer.paused ? "paused" : "running"}
      title={`${timer.policyName}: goal ${humanDuration(timer.goalMinutes * 60)}`}
    >
      <span aria-hidden="true" className={cx("size-2 shrink-0 rounded-full", dot)} />
      {label}
      {text}
    </span>
  );
}
