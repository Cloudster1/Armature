import { useSetIssueTeam, useTeams } from "@/api/teams";
import type { Issue } from "@/api/issues";
import { SelectInput } from "@/components/ui";

/**
 * Hands an issue to a team, or takes it back to the project at large.
 *
 * Which team carries a piece of work decides which board it appears on and
 * which backlog it sits in, which is why it is a field worth having in front of
 * somebody rather than buried in a bulk edit.
 */
export function TeamField({ issue }: { issue: Issue }) {
  const { data } = useTeams(issue.projectKey);
  const setTeam = useSetIssueTeam();
  const teams = data?.teams ?? [];

  if (teams.length === 0) {
    return <span className="text-xs text-ink-subtle">This project has no teams</span>;
  }

  return (
    <span className="flex flex-col items-end gap-1">
      <label htmlFor="issue-team" className="sr-only">
        Team
      </label>
      <SelectInput id="issue-team" controlSize="sm" value={issue.teamId ?? ""} onChange={(event) => setTeam.mutate({ key: issue.key, teamId: event.target.value || null })} className="max-w-48">
        <option value="">Nobody in particular</option>
        {teams.map((team) => (
          <option key={team.id} value={team.id}>
            {team.name}
          </option>
        ))}
      </SelectInput>
      {setTeam.error && (
        <span className="text-right text-2xs text-danger">
          {(setTeam.error as Error).message}
        </span>
      )}
    </span>
  );
}
