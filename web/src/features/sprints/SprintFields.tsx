import { useSprints, useSetEstimate, useSetIssueSprint } from "@/api/sprints";
import type { Issue } from "@/api/issues";
import { Input, SelectInput, cx } from "@/components/ui";

/** Commits an issue to a sprint, or sends it back to the backlog. */
export function SprintField({ issue }: { issue: Issue }) {
  const { data } = useSprints(issue.projectKey);
  const setSprint = useSetIssueSprint();
  const sprints = data?.sprints ?? [];

  return (
    <span className="flex flex-col items-end gap-1">
      <label htmlFor="issue-sprint" className="sr-only">
        Sprint
      </label>
      <SelectInput
        controlSize="sm"
        id="issue-sprint"
        value={issue.sprintId ?? ""}
        onChange={(event) =>
          setSprint.mutate({ key: issue.key, sprintId: event.target.value || null })
        }
        className="text-xs"
      >
        <option value="">Backlog</option>
        {sprints.map((sprint) => (
          <option key={sprint.id} value={sprint.id}>
            {sprint.name}
            {sprint.state === "active" ? " (running)" : ""}
          </option>
        ))}
        {/* A closed sprint is not offered, but the issue may already be in one. */}
        {issue.sprint && !sprints.some((s) => s.id === issue.sprint?.id) && (
          <option value={issue.sprint.id}>{issue.sprint.name}</option>
        )}
      </SelectInput>
      {setSprint.error && (
        <span className="text-right text-2xs text-danger">
          {(setSprint.error as Error).message}
        </span>
      )}
    </span>
  );
}

/**
 * Sizes an issue. An empty field is unestimated, which is not the same as an
 * estimate of zero: one says nobody has decided, the other says no work.
 */
export function EstimateField({ issue }: { issue: Issue }) {
  const setEstimate = useSetEstimate();

  return (
    <span className="flex flex-col items-end gap-1">
      <label htmlFor="issue-estimate" className="sr-only">
        Estimate in points
      </label>
      <Input
        controlSize="sm"
        id="issue-estimate"
        type="number"
        min={0}
        step="0.5"
        placeholder="Not sized"
        defaultValue={issue.estimate ?? ""}
        onBlur={(event) => {
          const raw = event.target.value.trim();
          const next = raw === "" ? null : Number(raw);
          if (next !== null && Number.isNaN(next)) return;
          if (next === (issue.estimate ?? null)) return;
          setEstimate.mutate({ key: issue.key, estimate: next });
        }}
        className={cx(
          "w-24 text-right text-xs",
          "placeholder:text-ink-subtle",
        )}
      />
      {setEstimate.error && (
        <span className="text-right text-2xs text-danger">
          {(setEstimate.error as Error).message}
        </span>
      )}
    </span>
  );
}
