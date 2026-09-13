import type { Progress } from "@/api/milestones";
import { ProgressBar as Bar } from "@/components/ui";

/** A milestone's progress as the kit's bar: done, then in progress, over every issue. */
export function ProgressBar({ progress, className }: { progress: Progress; className?: string }) {
  return (
    <Bar
      value={progress.percent}
      max={100}
      label={`${progress.percent}% done`}
      className={className}
      segments={[
        { value: (progress.done / (progress.issues || 1)) * 100, tone: "done" },
        { value: (progress.inProgress / (progress.issues || 1)) * 100, tone: "progress" },
      ]}
    />
  );
}
