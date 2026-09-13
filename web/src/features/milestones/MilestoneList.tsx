import { useState, type FormEvent } from "react";
import { Link } from "@tanstack/react-router";
import { useIssues } from "@/api/issues";
import {
  dayOf,
  describeProgress,
  formatDay,
  isOverdue,
  useCloseMilestone,
  useCreateMilestone,
  useDeleteMilestone,
  useMilestones,
  useReopenMilestone,
  useUpdateMilestone,
  type Milestone,
} from "@/api/milestones";
import { Button, Card, EmptyState, ErrorBanner, Field, cx } from "@/components/ui";
import { useConfirm } from "@/features/shell/ConfirmProvider";
import { StatusBadge, TypeBadge } from "@/features/issues/badges";
import { MAX_PAGE_SIZE } from "@/config";
import { MilestoneDashboardDialog } from "./MilestoneDashboardDialog";
import { ProgressBar } from "./ProgressBar";

/**
 * A project's milestones: what it is heading for, when, and how far along each
 * one is. Progress is read off the issues, so finishing an issue moves the bar
 * without anybody updating the milestone.
 */
export function MilestoneList({ projectKey }: { projectKey: string }) {
  const { data, isLoading, error } = useMilestones(projectKey, true);
  const milestones = data?.milestones ?? [];
  const open = milestones.filter((m) => !m.closedAt);
  const closed = milestones.filter((m) => m.closedAt);

  if (error) return <ErrorBanner>{(error as Error).message}</ErrorBanner>;
  if (isLoading) return <p className="text-sm text-ink-muted">Loading...</p>;

  return (
    <div className="space-y-6">
      <NewMilestone projectKey={projectKey} />

      {milestones.length === 0 && (
        <EmptyState
          title="No milestones yet"
          description="A milestone is a point the project is heading for. Assign issues to it and it tracks how close you are."
        />
      )}

      {open.map((milestone) => (
        <MilestoneCard key={milestone.id} milestone={milestone} />
      ))}

      {closed.length > 0 && (
        <section>
          <h2 className="mb-2 text-xs font-semibold tracking-wide text-ink-muted uppercase">Closed</h2>
          <div className="space-y-3">
            {closed.map((milestone) => (
              <MilestoneCard key={milestone.id} milestone={milestone} />
            ))}
          </div>
        </section>
      )}
    </div>
  );
}

function NewMilestone({ projectKey }: { projectKey: string }) {
  const create = useCreateMilestone(projectKey);
  const [name, setName] = useState("");
  const [dueOn, setDueOn] = useState("");
  const [description, setDescription] = useState("");

  function submit(event: FormEvent) {
    event.preventDefault();
    if (!name.trim()) return;
    create.mutate(
      { name: name.trim(), description: description.trim(), dueOn: dueOn || null },
      {
        onSuccess: () => {
          setName("");
          setDueOn("");
          setDescription("");
        },
      },
    );
  }

  return (
    <Card className="p-4">
      <form onSubmit={submit} className="flex flex-wrap items-end gap-3" data-testid="new-milestone">
        <div className="min-w-48 flex-1">
          <Field
            label="Milestone"
            id="field-milestone-name"
            value={name}
            placeholder="Release 1.0"
            onChange={(event) => setName(event.target.value)}
          />
        </div>
        <Field
          label="Due on"
          id="field-milestone-due"
          type="date"
          value={dueOn}
          onChange={(event) => setDueOn(event.target.value)}
          className="w-40"
        />
        <div className="min-w-56 flex-[2]">
          <Field
            label="What it means"
            id="field-milestone-description"
            value={description}
            placeholder="Optional."
            onChange={(event) => setDescription(event.target.value)}
          />
        </div>
        <Button type="submit" loading={create.isPending} disabled={!name.trim()}>
          Add milestone
        </Button>
      </form>
      {create.error && (
        <div className="mt-3">
          <ErrorBanner>{(create.error as Error).message}</ErrorBanner>
        </div>
      )}
    </Card>
  );
}

function MilestoneCard({ milestone }: { milestone: Milestone }) {
  const update = useUpdateMilestone();
  const close = useCloseMilestone();
  const reopen = useReopenMilestone();
  const remove = useDeleteMilestone();
  const confirm = useConfirm();
  const [showing, setShowing] = useState(false);
  const [dashboarding, setDashboarding] = useState(false);
  const [editing, setEditing] = useState(false);
  const [name, setName] = useState(milestone.name);
  const [dueOn, setDueOn] = useState(milestone.dueOn ? dayOf(milestone.dueOn) : "");

  const isClosed = Boolean(milestone.closedAt);
  const overdue = isOverdue(milestone, new Date());
  const error = (update.error ?? close.error ?? reopen.error ?? remove.error) as Error | undefined;

  function save(event: FormEvent) {
    event.preventDefault();
    update.mutate(
      { id: milestone.id, name: name.trim(), dueOn: dueOn || null },
      { onSuccess: () => setEditing(false) },
    );
  }

  return (
    <Card className={cx("p-4", isClosed && "opacity-75")} data-milestone={milestone.name}>
      <div className="flex flex-wrap items-start justify-between gap-2">
        <div className="min-w-0">
          {editing ? (
            <form onSubmit={save} className="flex flex-wrap items-end gap-2">
              <Field label="Name" id={`milestone-${milestone.id}-name`} value={name} onChange={(e) => setName(e.target.value)} />
              <Field
                label="Due on"
                id={`milestone-${milestone.id}-due`}
                type="date"
                value={dueOn}
                onChange={(e) => setDueOn(e.target.value)}
                className="w-40"
              />
              <Button type="submit" size="sm" loading={update.isPending}>
                Save
              </Button>
              <Button type="button" size="sm" variant="ghost" onClick={() => setEditing(false)}>
                Cancel
              </Button>
            </form>
          ) : (
            <>
              <h3 className="text-sm font-semibold text-ink">
                {milestone.name}
                {isClosed && (
                  <span className="ml-2 rounded-full bg-surface-raised px-2 py-0.5 text-2xs font-medium text-ink-muted">
                    Closed
                  </span>
                )}
                {overdue && (
                  <span className="ml-2 rounded-full bg-danger-subtle px-2 py-0.5 text-2xs font-medium text-danger">
                    Overdue
                  </span>
                )}
              </h3>
              <p className="mt-0.5 text-xs text-ink-muted">
                {milestone.dueOn ? `Due ${formatDay(milestone.dueOn)}` : "No date yet"}
                {milestone.description && ` · ${milestone.description}`}
              </p>
            </>
          )}
        </div>
        <div className="flex gap-1">
          <Button size="sm" variant="ghost" onClick={() => setShowing((v) => !v)}>
            {showing ? "Hide issues" : "Show issues"}
          </Button>
          <Button size="sm" variant="ghost" onClick={() => setDashboarding(true)} data-action="milestone-dashboard" data-guide="milestone-dashboard">
            Dashboard
          </Button>
          {dashboarding && <MilestoneDashboardDialog milestone={milestone} open onClose={() => setDashboarding(false)} />}
          {!isClosed && !editing && (
            <Button size="sm" variant="ghost" onClick={() => setEditing(true)}>
              Edit
            </Button>
          )}
          {isClosed ? (
            <Button size="sm" variant="ghost" loading={reopen.isPending} onClick={() => reopen.mutate(milestone.id)}>
              Reopen
            </Button>
          ) : (
            <Button size="sm" variant="ghost" loading={close.isPending} onClick={() => close.mutate(milestone.id)}>
              Close milestone
            </Button>
          )}
          <Button size="sm" variant="ghost" loading={remove.isPending} onClick={async () => (await confirm({ noun: "milestone", body: `${milestone.name} goes; the issues counting towards it stay and count towards nothing.` })) && remove.mutate(milestone.id)}>
            Delete
          </Button>
        </div>
      </div>

      <div className="mt-3 flex items-center gap-3">
        <ProgressBar progress={milestone.progress} className="flex-1" />
        <span className="shrink-0 text-sm text-ink-muted tabular-nums" data-milestone-progress>
          {describeProgress(milestone.progress)}
        </span>
      </div>
      {milestone.progress.issues > 0 && (
        <p className="mt-1 text-xs text-ink-subtle">
          {milestone.progress.done} done, {milestone.progress.inProgress} in progress, {milestone.progress.todo} to do
        </p>
      )}

      {showing && <MilestoneIssues milestone={milestone} />}
      {error && (
        <div className="mt-3">
          <ErrorBanner>{error.message}</ErrorBanner>
        </div>
      )}
    </Card>
  );
}

function MilestoneIssues({ milestone }: { milestone: Milestone }) {
  const { data } = useIssues({ project: milestone.projectKey, milestone: milestone.id, limit: MAX_PAGE_SIZE });
  const issues = data?.issues ?? [];
  if (data && issues.length === 0) {
    return <p className="mt-3 text-sm text-ink-muted">Nothing counts towards this yet. Assign issues to it from their page.</p>;
  }
  return (
    <ul className="mt-3 divide-y divide-border rounded-md border border-border">
      {issues.map((issue) => (
        <li key={issue.key} className="flex items-center gap-3 px-3 py-1.5 text-sm">
          <TypeBadge icon={issue.type.icon} name={issue.type.name} />
          <Link to="/issues/$issueKey" params={{ issueKey: issue.key }} className="font-mono text-sm text-ink-muted hover:text-accent">
            {issue.key}
          </Link>
          <span className="min-w-0 flex-1 truncate text-ink">{issue.summary}</span>
          <StatusBadge name={issue.status.name} category={issue.status.category} />
        </li>
      ))}
    </ul>
  );
}
