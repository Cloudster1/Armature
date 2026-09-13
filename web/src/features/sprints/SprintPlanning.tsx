import { useState, type FormEvent } from "react";
import { Link } from "@tanstack/react-router";
import { useIssues, type Issue } from "@/api/issues";
import { usePlan } from "@/api/plan";
import {
  overBy,
  useCompleteSprint,
  useCreateSprint,
  useDeleteSprint,
  useSetIssueSprint,
  useSprints,
  useStartSprint,
  useUpdateSprint,
  type Sprint,
  type SprintPlan,
} from "@/api/sprints";
import { Button, Card, EmptyState, ErrorBanner, Field, Input, Segmented, SelectInput, cx } from "@/components/ui";
import { useConfirm } from "@/features/shell/ConfirmProvider";
import { StatusBadge, TypeBadge } from "@/features/issues/badges";
import { describe } from "@/features/plan/SprintBands";
import { useTeams } from "@/api/teams";
import { MAX_PAGE_SIZE } from "@/config";

/**
 * Sprint planning: the backlog on one side, the sprints on the other, and the
 * capacity numbers between them so committing more work is a decision made with
 * the total in view rather than after the fact.
 */
export function SprintPlanning({ projectKey }: { projectKey: string }) {
  const { data: sprintData, isLoading, error } = useSprints(projectKey, true);
  const { data: plan } = usePlan(projectKey);
  const { data: issueData } = useIssues({ project: projectKey, limit: MAX_PAGE_SIZE });
  const { data: teamData } = useTeams(projectKey);

  // Empty is the project's own stream: the sprints and the work no team has
  // taken on. A project without teams only ever has this one.
  const [teamId, setTeamId] = useState("");
  const teams = teamData?.teams ?? [];

  const all = sprintData?.sprints ?? [];
  const sprints = all.filter((sprint) => (sprint.teamId ?? "") === teamId);
  const issues = (issueData?.issues ?? []).filter((issue) => (issue.teamId ?? "") === teamId);
  const capacity = new Map((plan?.sprints ?? []).map((row) => [row.sprint.id, row]));
  const backlog = issues.filter((issue) => !issue.sprintId);
  // A closed sprint is never offered as a destination: its report has been
  // written, and the API would refuse the move anyway.
  const open = sprints.filter((sprint) => sprint.state !== "closed");
  const states = sprints.map((sprint) => `${sprint.id}:${sprint.state}`).join("|");

  if (error) return <ErrorBanner>{(error as Error).message}</ErrorBanner>;
  if (isLoading) return <p className="text-sm text-ink-muted">Loading...</p>;

  return (
    <div className="space-y-6">
      {teams.length > 0 && (
        <div className="flex flex-wrap items-center gap-2">
          <Segmented
            label="Whose backlog"
            value={teamId}
            onChange={setTeamId}
            options={[{ id: "", name: "Nobody in particular" }, ...teams].map((team) => ({ value: team.id, label: team.name, attrs: { "data-backlog-of": team.name } }))}
          />
        </div>
      )}

      <NewSprint projectKey={projectKey} teamId={teamId} />

      {sprints.map((sprint) => (
        <SprintCard
          // Remounting when any sprint changes state drops a refusal that has
          // stopped being true: "another sprint is running" is not worth
          // leaving on screen once that sprint has been completed.
          key={`${sprint.id}-${states}`}
          sprint={sprint}
          plan={capacity.get(sprint.id)}
          issues={issues.filter((issue) => issue.sprintId === sprint.id)}
          others={sprints.filter((other) => other.id !== sprint.id && other.state !== "closed")}
        />
      ))}

      <section data-testid="backlog">
        <h2 className="mb-2 text-sm font-semibold text-ink">
          Backlog <span className="font-normal text-ink-muted">({backlog.length})</span>
        </h2>
        {backlog.length === 0 ? (
          <EmptyState
            title="Nothing in the backlog"
            description="Every issue in this project is committed to a sprint."
          />
        ) : (
          <Card className="divide-y divide-border">
            {backlog.map((issue) => (
              <IssueRow key={issue.key} issue={issue} sprints={open} />
            ))}
          </Card>
        )}
      </section>
    </div>
  );
}

function SprintCard({
  sprint,
  plan,
  issues,
  others,
}: {
  sprint: Sprint;
  plan?: SprintPlan;
  issues: Issue[];
  others: Sprint[];
}) {
  const start = useStartSprint();
  const complete = useCompleteSprint();
  const remove = useDeleteSprint();
  const confirm = useConfirm();
  const [moveTo, setMoveTo] = useState("");

  const error = (start.error ?? complete.error ?? remove.error) as Error | undefined;
  const over = plan ? overBy(plan) > 0 : false;
  const closed = sprint.state === "closed";

  return (
    <section data-sprint={sprint.name}>
      <Card className={cx("p-4", sprint.state === "active" && "border-accent")}>
        <div className="flex flex-wrap items-start justify-between gap-2">
          <div>
            <h2 className="text-sm font-semibold text-ink">
              {sprint.name}
              <StateBadge state={sprint.state} />
            </h2>
            <p className="mt-0.5 text-xs text-ink-muted">
              {dates(sprint)}
              {plan && ` · ${describe(plan)}`}
              {plan && plan.unestimated > 0 && ` · ${plan.unestimated} unestimated`}
              {closed && sprint.completed != null && ` · ${sprint.completed} completed`}
            </p>
            {sprint.goal && <p className="mt-1 text-sm text-ink-muted">{sprint.goal}</p>}
          </div>

          <div className="flex shrink-0 flex-wrap items-center gap-1">
            <Link
              to="/projects/$projectKey/sprints/$sprintId/board"
              params={{ projectKey: sprint.projectKey, sprintId: sprint.id }}
              data-sprint-board={sprint.name}
              className="inline-flex h-7 items-center rounded-md px-2.5 text-sm font-medium text-ink-muted hover:bg-surface-raised hover:text-ink"
            >
              Board
            </Link>
            {sprint.state === "future" && (
              <>
                <Button size="sm" loading={start.isPending} onClick={() => start.mutate(sprint.id)}>
                  Start sprint
                </Button>
                {issues.length === 0 && (
                  <Button
                    size="sm"
                    variant="ghost"
                    loading={remove.isPending}
                    onClick={async () => (await confirm({ noun: "sprint", body: `${sprint.name} goes; its issues return to the backlog.` })) && remove.mutate(sprint.id)}
                  >
                    Delete
                  </Button>
                )}
              </>
            )}
            {sprint.state === "active" && (
              <>
                <label htmlFor={`carry-${sprint.id}`} className="sr-only">
                  Carry unfinished work to
                </label>
                <SelectInput id={`carry-${sprint.id}`} value={moveTo} onChange={(event) => setMoveTo(event.target.value)}>
                  <option value="">Carry the rest to the backlog</option>
                  {others.map((other) => (
                    <option key={other.id} value={other.id}>
                      Carry the rest to {other.name}
                    </option>
                  ))}
                </SelectInput>
                <Button
                  size="sm"
                  loading={complete.isPending}
                  onClick={() => complete.mutate({ id: sprint.id, moveTo: moveTo || null })}
                >
                  Complete sprint
                </Button>
              </>
            )}
          </div>
        </div>

        {over && plan && (
          <p className="mt-2 text-sm text-danger">
            {describe(plan)} committed. This is more than the team said fits, which is worth
            deciding on rather than discovering later.
          </p>
        )}

        {!closed && <SprintSettings sprint={sprint} />}

        {error && <ErrorBanner>{error.message}</ErrorBanner>}
      </Card>

      {issues.length > 0 && (
        <Card className="mt-2 divide-y divide-border">
          {issues.map((issue) => (
            <IssueRow key={issue.key} issue={issue} sprints={closed ? [] : others} current={sprint} />
          ))}
        </Card>
      )}
    </section>
  );
}

function StateBadge({ state }: { state: Sprint["state"] }) {
  if (state === "future") return null;
  return (
    <span
      className={cx(
        "ml-2 rounded-full px-2 py-0.5 text-2xs font-medium",
        state === "active" ? "bg-accent-subtle text-accent" : "bg-surface-raised text-ink-muted",
      )}
    >
      {state === "active" ? "Running" : "Completed"}
    </span>
  );
}

function dates(sprint: Sprint): string {
  if (!sprint.startsOn || !sprint.endsOn) return "No dates yet";
  return `${shortDay(sprint.startsOn)} to ${shortDay(sprint.endsOn)}`;
}

function shortDay(iso: string): string {
  return new Date(iso).toLocaleDateString(undefined, {
    day: "numeric",
    month: "short",
    timeZone: "UTC",
  });
}

/** The dates and the capacity, editable in place while the sprint is not over. */
function SprintSettings({ sprint }: { sprint: Sprint }) {
  const update = useUpdateSprint();

  function set(field: "startsOn" | "endsOn", value: string) {
    update.mutate({ id: sprint.id, [field]: value || null });
  }

  return (
    <div className="mt-3 flex flex-wrap items-center gap-2 text-xs text-ink-muted">
      <label htmlFor={`from-${sprint.id}`}>From</label>
      <div className="w-36">
        <Input id={`from-${sprint.id}`} type="date" controlSize="sm" value={sprint.startsOn ? sprint.startsOn.slice(0, 10) : ""} onChange={(event) => set("startsOn", event.target.value)} />
      </div>
      <label htmlFor={`to-${sprint.id}`}>to</label>
      <div className="w-36">
        <Input id={`to-${sprint.id}`} type="date" controlSize="sm" value={sprint.endsOn ? sprint.endsOn.slice(0, 10) : ""} onChange={(event) => set("endsOn", event.target.value)} />
      </div>
      <label htmlFor={`capacity-${sprint.id}`} className="ml-2">
        Capacity
      </label>
      <Input
        id={`capacity-${sprint.id}`}
        controlSize="sm"
        type="number"
        min={0}
        step="1"
        placeholder="Not set"
        defaultValue={sprint.capacity ?? ""}
        onBlur={(event) => {
          const raw = event.target.value.trim();
          const next = raw === "" ? null : Number(raw);
          if (next !== null && Number.isNaN(next)) return;
          if (next === (sprint.capacity ?? null)) return;
          update.mutate({ id: sprint.id, capacity: next });
        }}
        className="w-24 text-right"
      />
      <span>points</span>
      {update.error && <ErrorBanner>{(update.error as Error).message}</ErrorBanner>}
    </div>
  );
}

function IssueRow({
  issue,
  sprints,
  current,
}: {
  issue: Issue;
  sprints: Sprint[];
  current?: Sprint;
}) {
  const setSprint = useSetIssueSprint();
  const movable = current?.state !== "closed";

  return (
    <div className="flex flex-wrap items-center gap-2 px-3 py-2 text-sm" data-issue={issue.key}>
      <TypeBadge icon={issue.type.icon} name={issue.type.name} />
      <Link
        to="/issues/$issueKey"
        params={{ issueKey: issue.key }}
        className="shrink-0 font-mono text-2xs text-ink-muted hover:text-accent"
      >
        {issue.key}
      </Link>
      <span className="min-w-0 flex-1 truncate text-ink">{issue.summary}</span>
      <StatusBadge name={issue.status.name} category={issue.status.category} />
      <span className="w-14 shrink-0 text-right text-xs tabular-nums text-ink-muted">
        {issue.estimate != null ? `${issue.estimate} pts` : "unsized"}
      </span>
      {movable && (
        <>
          <label htmlFor={`move-${issue.key}`} className="sr-only">
            Move {issue.key} to
          </label>
          <SelectInput id={`move-${issue.key}`} value={issue.sprintId ?? ""} onChange={(event) => setSprint.mutate({ key: issue.key, sprintId: event.target.value || null })} controlSize="sm" className="text-xs">
            <option value="">Backlog</option>
            {current && <option value={current.id}>{current.name}</option>}
            {sprints.map((sprint) => (
              <option key={sprint.id} value={sprint.id}>
                {sprint.name}
              </option>
            ))}
          </SelectInput>
        </>
      )}
    </div>
  );
}

function NewSprint({ projectKey, teamId }: { projectKey: string; teamId: string }) {
  const create = useCreateSprint(projectKey);
  const [name, setName] = useState("");

  function onSubmit(event: FormEvent) {
    event.preventDefault();
    if (!name.trim()) return;
    create.mutate(
      { name: name.trim(), teamId: teamId || null },
      { onSuccess: () => setName("") },
    );
  }

  return (
    <form onSubmit={onSubmit} className="flex flex-wrap items-end gap-2">
      <Field
        label="New sprint"
        value={name}
        placeholder="Sprint 4"
        onChange={(event) => setName(event.target.value)}
        className="w-56"
      />
      <Button type="submit" loading={create.isPending} disabled={!name.trim()}>
        Add sprint
      </Button>
      {create.error && <ErrorBanner>{(create.error as Error).message}</ErrorBanner>}
    </form>
  );
}
