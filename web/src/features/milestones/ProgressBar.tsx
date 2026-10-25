import type { Progress } from "@/api/milestones";
import { ProgressBar as Bar } from "@/components/ui";
import { t } from "@/i18n";

/** A milestone's progress as the kit's bar: done, then in progress, over every issue. */
export function ProgressBar({ progress, className }: { progress: Progress; className?: string }) {
  return (
    <Bar
      value={progress.percent}
      max={100}
      label={t.schedule.milestones.percentDone(progress.percent)}
      className={className}
      segments={[
        { value: (progress.done / (progress.issues || 1)) * 100, tone: "done" },
        { value: (progress.inProgress / (progress.issues || 1)) * 100, tone: "progress" },
      ]}
    />
  );
}

/** How far a milestone or a version has got, in the reader's language. */
export function progressWords(progress: Progress): string {
  if (progress.issues === 0) return t.schedule.milestones.nothingAssigned;
  return t.schedule.milestones.progress(progress.done, progress.issues, progress.percent);
}
