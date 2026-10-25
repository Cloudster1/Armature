import { humanDuration, type Timer } from "@/api/desk";
import { cx } from "@/components/ui";
import { t } from "@/i18n";

/**
 * Where a clock stands, as a dot and a phrase: time left, or how far over, or
 * that it stopped. Breached is red before it is anything else.
 */
export function SlaBadge({ timer, short = false }: { timer: Timer; short?: boolean }) {
  const metric = timer.metric === "first_response" ? t.desk.sla.firstResponse : t.desk.sla.resolution;
  const label = short ? "" : metric + ": ";
  let dot: string;
  let text: string;
  if (timer.completedAt) {
    text = timer.breached ? t.desk.sla.missedBy(humanDuration(-timer.remainingSeconds)) : t.desk.sla.metWith(humanDuration(timer.remainingSeconds));
    dot = timer.breached ? "bg-danger" : "bg-status-done";
  } else if (timer.breached) {
    text = t.desk.sla.over(humanDuration(-timer.remainingSeconds));
    dot = "bg-danger";
  } else if (timer.paused) {
    text = t.desk.sla.pausedLeft(humanDuration(timer.remainingSeconds));
    dot = "bg-status-todo";
  } else {
    text = t.desk.sla.left(humanDuration(timer.remainingSeconds));
    // Under an hour reads as urgent.
    dot = timer.remainingSeconds < 3600 ? "bg-warning" : "bg-status-done";
  }
  return (
    <span
      className={cx("inline-flex items-center gap-1.5 text-xs whitespace-nowrap", timer.breached ? "text-danger" : "text-ink-muted")}
      data-sla={timer.metric}
      data-sla-state={timer.completedAt ? "completed" : timer.breached ? "breached" : timer.paused ? "paused" : "running"}
      title={t.desk.sla.goal(timer.policyName, humanDuration(timer.goalMinutes * 60))}
    >
      <span aria-hidden="true" className={cx("size-2 shrink-0 rounded-full", dot)} />
      {label}
      {text}
    </span>
  );
}
