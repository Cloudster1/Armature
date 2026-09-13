import { useSchedule } from "@/api/plan";
import type { Issue } from "@/api/issues";
import { Button, Input } from "@/components/ui";
import { isoDay } from "./scale";

/**
 * The two ends of a range, outside the timeline: a date you can only set by
 * dragging is one you cannot set precisely, or with a keyboard.
 */
export function ScheduleField({ issue }: { issue: Issue }) {
  const schedule = useSchedule();

  const start = issue.startDate ? isoDay(new Date(issue.startDate)) : "";
  const due = issue.dueDate ? isoDay(new Date(issue.dueDate)) : "";

  function set(field: "startDate" | "dueDate", value: string) {
    schedule.mutate({ key: issue.key, [field]: value || null });
  }

  return (
    <span className="flex flex-col items-end gap-1">
      {/* One date a line: two date inputs side by side do not fit a sidebar field. */}
      <span className="flex items-center gap-2 text-xs text-ink-subtle">
        <label htmlFor="issue-start">From</label>
        <span className="w-36">
          <Input id="issue-start" type="date" value={start} onChange={(e) => set("startDate", e.target.value)} controlSize="sm" aria-label="Start date" />
        </span>
      </span>
      <span className="flex items-center gap-2 text-xs text-ink-subtle">
        <label htmlFor="issue-due">to</label>
        <span className="w-36">
          <Input id="issue-due" type="date" value={due} onChange={(e) => set("dueDate", e.target.value)} controlSize="sm" aria-label="Due date" />
        </span>
      </span>
      {(start || due) && (
        <Button
          variant="ghost"
          size="sm"
          className="h-6 px-1.5 text-2xs"
          loading={schedule.isPending}
          onClick={() => schedule.mutate({ key: issue.key, startDate: null, dueDate: null })}
        >
          Clear dates
        </Button>
      )}
      {schedule.error && (
        <span className="text-right text-2xs text-danger">{(schedule.error as Error).message}</span>
      )}
    </span>
  );
}
