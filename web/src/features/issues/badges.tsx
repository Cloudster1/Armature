import type { Priority, StatusCategory } from "@/api/issues";
import { cx } from "@/components/ui";
import { Icon, type IconName } from "@/components/icons";
import { relativeIn } from "@/lib/format";

/**
 * Status colour carries meaning here, so it is keyed on the category rather
 * than the status name: a workflow can call its states anything, but every
 * state is still to-do, in-progress or done.
 */
const categoryDot: Record<StatusCategory, string> = {
  todo: "bg-status-todo",
  in_progress: "bg-status-progress",
  done: "bg-status-done",
};

const categoryInk: Record<StatusCategory, string> = {
  todo: "text-ink-muted",
  in_progress: "text-ink",
  done: "text-ink-muted",
};

// A dot and a word rather than a coloured pill: at list density a row of pills
// is a row of shapes, and the word is what somebody actually reads.
export function StatusBadge({
  name,
  category,
  className,
}: {
  name: string;
  category: StatusCategory;
  className?: string;
}) {
  return (
    <span
      className={cx(
        "inline-flex items-center gap-1.5 text-sm whitespace-nowrap",
        categoryInk[category],
        `status-${category}`,
        className,
      )}
    >
      <span aria-hidden="true" className={cx("size-2 shrink-0 rounded-full", categoryDot[category])} />
      {name}
    </span>
  );
}

const priorityOrder: Priority[] = ["lowest", "low", "medium", "high", "highest"];

const priorityStyles: Record<Priority, string> = {
  lowest: "text-ink-subtle",
  low: "text-ink-subtle",
  medium: "text-ink-muted",
  high: "text-warning",
  highest: "text-danger",
};

const priorityLabels: Record<Priority, string> = {
  lowest: "Lowest",
  low: "Low",
  medium: "Medium",
  high: "High",
  highest: "Highest",
};

export function PriorityBadge({ priority }: { priority: Priority }) {
  const index = priorityOrder.indexOf(priority);
  return (
    <span
      className={cx("inline-flex items-center gap-1 text-xs font-medium", priorityStyles[priority])}
      title={`${priorityLabels[priority]} priority`}
    >
      {/* A small bar chart reads faster than a word at list density, but the
          word stays available to screen readers and on hover. */}
      <span aria-hidden="true" className="inline-flex items-end gap-px">
        {[0, 1, 2].map((bar) => (
          <span
            key={bar}
            className={cx(
              "w-0.5 rounded-sm bg-current",
              bar === 0 ? "h-1.5" : bar === 1 ? "h-2" : "h-2.5",
              index >= (bar + 1) * 1.5 ? "opacity-100" : "opacity-25",
            )}
          />
        ))}
      </span>
      <span className="sr-only">{priorityLabels[priority]} priority</span>
    </span>
  );
}

const typeStyles: Record<string, string> = {
  bug: "bg-danger/10 text-danger",
  story: "bg-success/10 text-success",
  epic: "bg-epic/10 text-epic",
  initiative: "bg-warning/10 text-warning",
  subtask: "bg-surface-raised text-ink-muted",
  task: "bg-surface-raised text-ink-muted",
};

const typeIcons: Record<string, IconName> = {
  bug: "Bug",
  story: "Story",
  epic: "Epic",
  initiative: "Initiative",
  subtask: "Subtask",
  task: "Task",
};

export function TypeBadge({ icon, name }: { icon: string; name: string }) {
  const Glyph = Icon[typeIcons[icon] ?? "Task"];
  return (
    <span title={name} className={cx("inline-flex size-4 shrink-0 items-center justify-center rounded-[3px]", typeStyles[icon] ?? typeStyles.task)}>
      <Glyph className="size-3" />
      <span className="sr-only">{name}</span>
    </span>
  );
}

// Avatar lives in the kit now; re-exported so the fourteen call sites keep
// their import until the sweep moves them.
export { Avatar } from "@/components/ui";

/** A short relative phrase in the browser's terms; useFormat gives the reader's. */
export function relativeTime(iso: string): string {
  return relativeIn(iso, navigator.language, Intl.DateTimeFormat().resolvedOptions().timeZone);
}
