import { useMilestones, useSetIssueMilestone } from "@/api/milestones";
import type { Issue } from "@/api/issues";
import { SelectInput } from "@/components/ui";

/** Makes an issue count towards a milestone, or towards none. */
export function MilestoneField({ issue }: { issue: Issue }) {
  const { data } = useMilestones(issue.projectKey);
  const setMilestone = useSetIssueMilestone();
  const milestones = data?.milestones ?? [];

  return (
    <span className="flex flex-col items-end gap-1">
      <label htmlFor="issue-milestone" className="sr-only">
        Milestone
      </label>
      <SelectInput id="issue-milestone" controlSize="sm" value={issue.milestoneId ?? ""} onChange={(event) => setMilestone.mutate({ key: issue.key, milestoneId: event.target.value || null })} className="max-w-48">
        <option value="">None</option>
        {milestones.map((milestone) => (
          <option key={milestone.id} value={milestone.id}>
            {milestone.name}
          </option>
        ))}
        {/* A closed milestone is not offered, but the issue may already count towards one. */}
        {issue.milestone && !milestones.some((m) => m.id === issue.milestone?.id) && (
          <option value={issue.milestone.id}>{issue.milestone.name}</option>
        )}
      </SelectInput>
      {setMilestone.error && (
        <span className="text-right text-2xs text-danger">
          {(setMilestone.error as Error).message}
        </span>
      )}
    </span>
  );
}
