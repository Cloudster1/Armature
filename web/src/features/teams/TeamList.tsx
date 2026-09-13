import { useState, type FormEvent } from "react";
import { useMembers } from "@/api/issues";
import {
  useAddTeamMember,
  useCreateTeam,
  useDeleteTeam,
  useRemoveTeamMember,
  useUpdateTeam,
  useTeam,
  useTeams,
  type Team,
} from "@/api/teams";
import { useCreateBoard } from "@/api/boards";
import { Button, Card, EmptyState, ErrorBanner, Field, Input, SelectInput } from "@/components/ui";
import { useConfirm } from "@/features/shell/ConfirmProvider";
import { Avatar } from "@/features/issues/badges";

/**
 * The teams inside a project, and who is on them.
 *
 * A team is narrower than the organization, never wider: the people it can be
 * made of are the people already here. What a team gets is a board and a
 * backlog of its own, not access to anything it did not have.
 */
export function TeamList({ projectKey }: { projectKey: string }) {
  const { data, isLoading, error } = useTeams(projectKey);
  const teams = data?.teams ?? [];

  if (error) return <ErrorBanner>{(error as Error).message}</ErrorBanner>;

  return (
    <div className="space-y-6">
      <NewTeam projectKey={projectKey} />

      {isLoading && <p className="text-sm text-ink-muted">Loading...</p>}
      {!isLoading && teams.length === 0 && (
        <EmptyState
          title="No teams yet"
          description="A project without teams has one board and one backlog, which is all most projects need."
        />
      )}

      <div className="space-y-4">
        {teams.map((team) => (
          <TeamCard key={team.id} team={team} projectKey={projectKey} />
        ))}
      </div>
    </div>
  );
}

/**
 * How much the team can take on in a week, in points. Saved when the field is
 * left; emptied, it says the team has not said, which the plan reads
 * differently from zero.
 */
function CapacityField({ team }: { team: Team }) {
  const update = useUpdateTeam();
  const [value, setValue] = useState(team.weeklyCapacity === undefined ? "" : String(team.weeklyCapacity));

  function save() {
    const trimmed = value.trim();
    const next = trimmed === "" ? null : Number(trimmed);
    if (next !== null && (Number.isNaN(next) || next < 0)) return;
    if ((next ?? undefined) === team.weeklyCapacity) return;
    update.mutate({ id: team.id, weeklyCapacity: next });
  }

  return (
    <label className="mt-2 flex items-center gap-2 text-xs text-ink-muted">
      <span>Capacity</span>
      <Input
        type="number"
        min={0}
        step={0.5}
        value={value}
        onChange={(e) => setValue(e.target.value)}
        onBlur={save}
        onKeyDown={(e) => {
        if (e.key === "Enter") (e.target as HTMLInputElement).blur();
        }}
        placeholder="not said"
        aria-label={`Weekly capacity of ${team.name}`}
        data-team-capacity={team.name}
        controlSize="sm"
        className="w-20 text-right"
      />
      <span>pts/week</span>
      {update.error && <span className="text-danger">{(update.error as Error).message}</span>}
    </label>
  );
}

function TeamCard({ team, projectKey }: { team: Team; projectKey: string }) {
  const { data } = useTeam(team.id);
  const { data: memberData } = useMembers();
  const add = useAddTeamMember();
  const remove = useRemoveTeamMember();
  const deleteTeam = useDeleteTeam();
  const confirm = useConfirm();
  const createBoard = useCreateBoard(projectKey);
  const [picked, setPicked] = useState("");

  const members = data?.team?.members ?? [];
  const onTeam = new Set(members.map((member) => member.userId));
  // A portal customer is not somebody who can be put on a team.
  const available = (memberData?.members ?? []).filter(
    (person) => !onTeam.has(person.id) && person.role !== "customer",
  );
  const error = (add.error ?? remove.error ?? deleteTeam.error ?? createBoard.error) as
    | Error
    | undefined;

  return (
    <Card className="p-4" data-team={team.name}>
      <div className="flex flex-wrap items-start justify-between gap-2">
        <div>
          <h2 className="text-sm font-semibold text-ink">{team.name}</h2>
          <p className="mt-0.5 text-xs text-ink-muted">
            {team.memberCount} {team.memberCount === 1 ? "person" : "people"} ·{" "}
            {team.issueCount} {team.issueCount === 1 ? "issue" : "issues"} ·{" "}
            {team.boardCount} {team.boardCount === 1 ? "board" : "boards"}
          </p>
          {team.description && <p className="mt-1 text-sm text-ink-muted">{team.description}</p>}
          <CapacityField team={team} />
        </div>

        <div className="flex shrink-0 gap-1">
          {team.boardCount === 0 && (
            <Button
              size="sm"
              variant="secondary"
              loading={createBoard.isPending}
              onClick={() =>
                createBoard.mutate({ name: `${team.name} board`, teamId: team.id })
              }
            >
              Give it a board
            </Button>
          )}
          <Button
            size="sm"
            variant="ghost"
            loading={deleteTeam.isPending}
            onClick={async () => (await confirm({ noun: "team", body: `${team.name} goes with its board and backlog; its issues keep their assignees.` })) && deleteTeam.mutate(team.id)}
          >
            Delete
          </Button>
        </div>
      </div>

      <ul className="mt-3 space-y-1">
        {members.map((member) => (
          <li key={member.userId} className="flex items-center gap-2 text-sm">
            <Avatar name={member.name} />
            <span className="text-ink">{member.name}</span>
            {member.lead && (
              <span className="rounded-full bg-accent-subtle px-2 py-0.5 text-2xs font-medium text-accent">
                Lead
              </span>
            )}
            <Button
              size="sm"
              variant="ghost"
              className="ml-auto h-6 px-1.5 text-2xs"
              onClick={async () => (await confirm({ noun: "member", verb: "Remove", body: `${member.name} leaves ${team.name}.` })) && remove.mutate({ id: team.id, userId: member.userId })}
            >
              Remove
            </Button>
          </li>
        ))}
        {members.length === 0 && <li className="text-sm text-ink-subtle">Nobody on it yet.</li>}
      </ul>

      <div className="mt-3 flex flex-wrap items-center gap-2">
        <label htmlFor={`add-${team.id}`} className="sr-only">
          Add somebody to {team.name}
        </label>
        <SelectInput id={`add-${team.id}`} value={picked} onChange={(event) => setPicked(event.target.value)}>
          <option value="">Add somebody...</option>
          {available.map((person) => (
            <option key={person.id} value={person.id}>
              {person.name}
            </option>
          ))}
        </SelectInput>
        <Button
          size="sm"
          disabled={!picked}
          loading={add.isPending}
          onClick={() => {
            add.mutate({ id: team.id, userId: picked, lead: members.length === 0 });
            setPicked("");
          }}
        >
          Add
        </Button>
      </div>

      {error && <ErrorBanner>{error.message}</ErrorBanner>}
    </Card>
  );
}

function NewTeam({ projectKey }: { projectKey: string }) {
  const create = useCreateTeam(projectKey);
  const [name, setName] = useState("");

  function onSubmit(event: FormEvent) {
    event.preventDefault();
    if (!name.trim()) return;
    create.mutate({ name: name.trim() }, { onSuccess: () => setName("") });
  }

  return (
    <form onSubmit={onSubmit} className="flex flex-wrap items-end gap-2">
      <Field
        label="New team"
        value={name}
        placeholder="Platform"
        onChange={(event) => setName(event.target.value)}
        className="w-56"
      />
      <Button type="submit" loading={create.isPending} disabled={!name.trim()}>
        Form team
      </Button>
      {create.error && <ErrorBanner>{(create.error as Error).message}</ErrorBanner>}
    </form>
  );
}
