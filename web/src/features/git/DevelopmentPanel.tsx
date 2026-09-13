import { useState, type FormEvent } from "react";
import {
  useDevelopment,
  useRepositories,
  useCreateBranch,
  checkoutCommand,
  pullWord,
  type Branch,
  type CIRun,
  type CIStatus,
  type Repository,
} from "@/api/git";
import { Button, ErrorBanner, Field, SectionTitle, Select, Tag, cx } from "@/components/ui";
import { GIT_SHORT_SHA_LENGTH } from "@/config";
import { relativeTime } from "@/features/issues/badges";

/**
 * What the repositories know about this issue: branches, pull requests with
 * how their CI came out, and commits. Empty until something names the issue.
 */
export function DevelopmentPanel({ issueKey, projectKey }: { issueKey: string; projectKey: string }) {
  const { data, isLoading } = useDevelopment(issueKey);
  const { data: repoData } = useRepositories(projectKey);
  const [branching, setBranching] = useState(false);

  const repositories = repoData?.repositories ?? [];
  if (isLoading || !data) return null;
  // A project with no repositories has nothing to say here, and says nothing.
  if (repositories.length === 0) return null;

  const { branches, pullRequests, commits } = data;
  const empty = branches.length === 0 && pullRequests.length === 0 && commits.length === 0;

  return (
    <section data-testid="development">
      <div className="mb-2 flex items-center justify-between">
        <SectionTitle>Development</SectionTitle>
        {!branching && (
          <Button size="sm" variant="secondary" onClick={() => setBranching(true)}>
            Create branch
          </Button>
        )}
      </div>

      {branching && (
        <CreateBranch
          issueKey={issueKey}
          suggested={data.branchName}
          repositories={repositories}
          existing={branches}
          onDone={() => setBranching(false)}
        />
      )}

      {empty ? (
        <p className="text-sm text-ink-subtle">
          Nothing yet. A commit, branch or {pullWord(repositories[0]!.host)} that names {issueKey} appears here.
        </p>
      ) : (
        <div className="divide-y divide-border rounded-md border border-border bg-surface">
          {pullRequests.map((pr) => (
            <div key={pr.id} className="flex items-center gap-3 px-3 py-2 text-sm" data-pull-request={pr.number}>
              <PullStateTag state={pr.state} />
              <a href={pr.url || undefined} target="_blank" rel="noreferrer" className="min-w-0 flex-1 truncate text-ink hover:text-accent">
                <span className="font-mono text-ink-muted">#{pr.number}</span> {pr.title}
              </a>
              {pr.ci && <CIBadge run={pr.ci} />}
              <span className="shrink-0 text-ink-subtle">{pr.repository}</span>
            </div>
          ))}
          {branches.map((branch) => (
            <BranchRow key={branch.id} branch={branch} />
          ))}
          {commits.map((commit) => (
            <div key={commit.id} className="flex items-center gap-3 px-3 py-2 text-sm" data-commit={commit.sha}>
              <a href={commit.url || undefined} target="_blank" rel="noreferrer" className="shrink-0 font-mono text-ink-muted hover:text-accent">
                {commit.sha.slice(0, GIT_SHORT_SHA_LENGTH)}
              </a>
              <span className="min-w-0 flex-1 truncate text-ink" title={commit.message}>
                {commit.message.split("\n")[0]}
              </span>
              <CIForCommit runs={data.runs} sha={commit.sha} />
              <span className="shrink-0 text-ink-subtle">
                {commit.authorName ? `${commit.authorName} · ` : ""}
                {relativeTime(commit.committedAt)}
              </span>
            </div>
          ))}
        </div>
      )}
    </section>
  );
}

function PullStateTag({ state }: { state: "open" | "merged" | "closed" }) {
  const look = {
    open: "text-success border-success/40",
    merged: "text-epic border-epic/40",
    closed: "text-ink-subtle",
  }[state];
  return <Tag className={cx("bg-transparent", look)}>{state}</Tag>;
}

const ciWords: Record<CIStatus, string> = {
  pending: "running",
  success: "passed",
  failure: "failed",
  cancelled: "cancelled",
};

const ciLook: Record<CIStatus, string> = {
  pending: "bg-status-todo",
  success: "bg-status-done",
  failure: "bg-danger",
  cancelled: "bg-status-todo",
};

/** How a run came out, as a dot and a word, the way a status reads. */
export function CIBadge({ run }: { run: CIRun }) {
  return (
    <a
      href={run.url || undefined}
      target="_blank"
      rel="noreferrer"
      className="inline-flex shrink-0 items-center gap-1.5 text-xs text-ink-muted hover:text-ink"
      title={`${run.name}: ${ciWords[run.status]}`}
      data-ci={run.status}
    >
      <span aria-hidden="true" className={cx("size-2 rounded-full", ciLook[run.status])} />
      {run.name} {ciWords[run.status]}
    </a>
  );
}

function CIForCommit({ runs, sha }: { runs: CIRun[]; sha: string }) {
  const run = runs.find((r) => r.sha === sha);
  return run ? <CIBadge run={run} /> : null;
}

/**
 * One branch and how far it has come: its head, how many commits pushes have
 * brought, when the last one was, and whether it has been merged.
 */
function BranchRow({ branch }: { branch: Branch }) {
  return (
    <div
      className="flex items-center gap-3 px-3 py-2 text-sm"
      data-branch={branch.name}
      data-branch-repository={branch.repository}
      data-branch-merged={branch.mergedAt ? "true" : undefined}
    >
      <Tag className={cx(branch.mergedAt && "bg-transparent text-epic border-epic/40")}>{branch.mergedAt ? "merged" : "branch"}</Tag>
      <a href={branch.url || undefined} target="_blank" rel="noreferrer" className="min-w-0 flex-1 truncate font-mono text-ink hover:text-accent">
        {branch.name}
      </a>
      {branch.headSha && (
        <span className="shrink-0 font-mono text-ink-muted" title="The commit the branch points at" data-branch-head={branch.headSha}>
          {branch.headSha.slice(0, GIT_SHORT_SHA_LENGTH)}
        </span>
      )}
      <span className="shrink-0 text-ink-subtle">
        {branch.commitCount} commit{branch.commitCount === 1 ? "" : "s"}
        {branch.pushedAt ? ` · pushed ${relativeTime(branch.pushedAt)}` : ""}
      </span>
      <span className="shrink-0 text-ink-subtle">{branch.repository}</span>
    </div>
  );
}

/** Which repository to offer first: one the tracker can write to that has no branch for the issue yet. */
export function firstRepositoryToBranch(repositories: Repository[], existing: Branch[], made: Branch[]): Repository | undefined {
  const has = (r: Repository) => [...existing, ...made].some((b) => b.repository === r.name);
  return (
    repositories.find((r) => r.hasToken && !has(r)) ??
    repositories.find((r) => r.hasToken) ??
    repositories[0]
  );
}

/**
 * Makes a branch for the issue in a repository of the person's choosing. The
 * name is shown before it exists, so it can be changed; so is the branch it
 * starts from. The form stays open once a branch is made, offering the next
 * repository, because work on one issue often lands in more than one.
 */
function CreateBranch({
  issueKey,
  suggested,
  repositories,
  existing,
  onDone,
}: {
  issueKey: string;
  suggested: string;
  repositories: Repository[];
  existing: Branch[];
  onDone: () => void;
}) {
  const create = useCreateBranch(issueKey);
  const [made, setMade] = useState<Branch[]>([]);
  const [repositoryId, setRepositoryId] = useState(() => firstRepositoryToBranch(repositories, existing, [])?.id ?? "");
  const [name, setName] = useState(suggested);
  const [from, setFrom] = useState(() => firstRepositoryToBranch(repositories, existing, [])?.defaultBranch ?? "main");

  const repository = repositories.find((r) => r.id === repositoryId) ?? repositories[0]!;
  const branchIn = (r: Repository) => [...existing, ...made].find((b) => b.repository === r.name);

  function choose(id: string) {
    setRepositoryId(id);
    const next = repositories.find((r) => r.id === id);
    if (next) setFrom(next.defaultBranch);
  }

  function onSubmit(event: FormEvent) {
    event.preventDefault();
    create.mutate(
      { repositoryId, name: name.trim(), from: from.trim() },
      {
        onSuccess: ({ branch }) => {
          const all = [...made, branch];
          setMade(all);
          const next = firstRepositoryToBranch(repositories, existing, all);
          if (next) choose(next.id);
        },
      },
    );
  }

  return (
    <form
      onSubmit={onSubmit}
      className="mb-3 space-y-3 rounded-md border border-border bg-surface p-3"
      noValidate
      data-testid="create-branch"
    >
      {made.map((branch) => (
        <div
          key={branch.id}
          className="flex flex-wrap items-center gap-2 rounded-md border border-accent/40 bg-accent-subtle/40 px-3 py-2 text-sm"
          data-testid="branch-made"
        >
          <span className="text-ink-muted">Made on {branch.repository}. To work on it:</span>
          <code className="rounded bg-surface-raised px-1.5 py-0.5 font-mono text-ink" data-testid="checkout-command">
            {checkoutCommand(branch.name)}
          </code>
        </div>
      ))}

      <Select id="branch-repository" label="Repository" value={repositoryId} onChange={(e) => choose(e.target.value)}>
        {repositories.map((r) => (
          <option key={r.id} value={r.id} disabled={!r.hasToken}>
            {r.name}
            {!r.hasToken ? " (read only: no access token)" : branchIn(r) ? ` (has ${branchIn(r)!.name})` : ""}
          </option>
        ))}
      </Select>
      {!repository.hasToken && (
        <p className="text-sm text-ink-subtle" data-testid="no-token">
          The tracker can only listen to {repository.name}. Add an access token on the repositories page and it can make branches there.
        </p>
      )}

      <div className="grid gap-3 sm:grid-cols-[1fr_9rem]">
        <Field
          id="branch-name"
          label="Branch name"
          value={name}
          onChange={(e) => setName(e.target.value)}
          className="font-mono"
          autoFocus
        />
        <Field
          id="branch-from"
          label="Start from"
          value={from}
          onChange={(e) => setFrom(e.target.value)}
          className="font-mono"
        />
      </div>
      {create.error && <ErrorBanner>{(create.error as Error).message}</ErrorBanner>}
      <div className="flex gap-2">
        <Button type="submit" size="sm" loading={create.isPending} disabled={!name.trim() || !repository.hasToken}>
          Create
        </Button>
        <Button type="button" size="sm" variant="ghost" onClick={onDone}>
          {made.length > 0 ? "Done" : "Cancel"}
        </Button>
      </div>
    </form>
  );
}
