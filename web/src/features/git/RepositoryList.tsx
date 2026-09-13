import { useState, type FormEvent } from "react";
import {
  useConnectRepository,
  useDisconnectRepository,
  useRepositories,
  useRotateSecret,
  useUpdateRepository,
  hostLabel,
  pullWord,
  type Connected,
  type HostKind,
  type Repository,
} from "@/api/git";
import { useProjectWorkflows, useWorkflow } from "@/api/workflows";
import { Button, Card, EmptyState, ErrorBanner, Field, Select, Tag, cx } from "@/components/ui";

/**
 * The repositories connected to a project.
 *
 * Connecting one hands back a webhook address and a secret exactly once, the
 * way a personal access token is handed back: the host needs both, and the
 * tracker keeps only what it needs to check a delivery.
 */
export function RepositoryList({ projectKey }: { projectKey: string }) {
  const { data, isLoading, error } = useRepositories(projectKey);
  const [connecting, setConnecting] = useState(false);
  const [handed, setHanded] = useState<Connected | null>(null);

  const repositories = data?.repositories ?? [];

  if (error) return <ErrorBanner>{(error as Error).message}</ErrorBanner>;

  return (
    <div className="space-y-4">
      {handed && <WebhookHandout connected={handed} onDone={() => setHanded(null)} />}

      {connecting ? (
        <ConnectForm
          projectKey={projectKey}
          onConnected={(connected) => {
            setConnecting(false);
            setHanded(connected);
          }}
          onCancel={() => setConnecting(false)}
        />
      ) : (
        <div className="flex justify-end">
          <Button onClick={() => setConnecting(true)}>Connect repository</Button>
        </div>
      )}

      {isLoading ? null : repositories.length === 0 && !connecting ? (
        <EmptyState
          title="No repositories connected"
          description="Connect one and commits, branches, pull requests and CI runs that name an issue by key appear on that issue. A commit message can move the issue too: CP-4 #start-progress."
        />
      ) : (
        repositories.map((repository) => (
          <RepositoryCard key={repository.id} repository={repository} projectKey={projectKey} onRotated={setHanded} />
        ))
      )}
    </div>
  );
}

const apiPlaceholder: Record<HostKind, string> = {
  github: "https://api.github.com",
  gitlab: "https://gitlab.com/api/v4",
  gitea: "https://gitea.example.com/api/v1",
};

function ConnectForm({
  projectKey,
  onConnected,
  onCancel,
}: {
  projectKey: string;
  onConnected: (connected: Connected) => void;
  onCancel: () => void;
}) {
  const connect = useConnectRepository(projectKey);
  const { data: workflows } = useProjectWorkflows(projectKey);
  const [host, setHost] = useState<HostKind>("github");
  const [name, setName] = useState("");
  const [apiBaseUrl, setApiBaseUrl] = useState("");
  const [defaultBranch, setDefaultBranch] = useState("main");
  const [accessToken, setAccessToken] = useState("");
  const [transitionOnMerge, setTransitionOnMerge] = useState("");

  // The names on offer come from the project's fallback workflow, which is
  // the one most of its issues follow. The field stays free text, because a
  // type with a workflow of its own may name a transition the fallback lacks.
  const fallbackWorkflowId = workflows?.assignments.find((a) => a.origin.named === false)?.workflowId
    ?? workflows?.assignments[0]?.workflowId ?? "";
  const { data: workflow } = useWorkflow(fallbackWorkflowId);
  const transitions = [...new Set((workflow?.workflow?.transitions ?? []).map((t) => t.name))];

  function onSubmit(event: FormEvent) {
    event.preventDefault();
    connect.mutate(
      {
        host,
        name: name.trim(),
        apiBaseUrl: apiBaseUrl.trim() || undefined,
        defaultBranch: defaultBranch.trim() || undefined,
        accessToken: accessToken.trim() || undefined,
        transitionOnMerge: transitionOnMerge || undefined,
      },
      { onSuccess: onConnected },
    );
  }

  return (
    <Card className="p-5">
      <form onSubmit={onSubmit} className="space-y-4" noValidate>
        {connect.error && <ErrorBanner>{(connect.error as Error).message}</ErrorBanner>}

        <div className="grid gap-4 sm:grid-cols-[10rem_1fr]">
          <Select id="repo-host" label="Host" value={host} onChange={(e) => setHost(e.target.value as HostKind)}>
            <option value="github">GitHub</option>
            <option value="gitlab">GitLab</option>
            <option value="gitea">Gitea</option>
          </Select>
          <Field
            label="Repository"
            required
            autoFocus
            value={name}
            onChange={(e) => setName(e.target.value)}
            placeholder="acme/portal"
            hint="As the host names it: owner/name."
          />
        </div>

        <div className="grid gap-4 sm:grid-cols-2">
          <Field
            label="Default branch"
            value={defaultBranch}
            onChange={(e) => setDefaultBranch(e.target.value)}
            hint="Branches made from an issue start here."
          />
          <Field
            label="API address"
            value={apiBaseUrl}
            onChange={(e) => setApiBaseUrl(e.target.value)}
            placeholder={apiPlaceholder[host]}
            hint={host === "gitea" ? "Your Gitea's address, with /api/v1." : "Leave empty for the public host."}
          />
        </div>

        <Field
          label="Access token"
          type="password"
          value={accessToken}
          onChange={(e) => setAccessToken(e.target.value)}
          hint="Optional. With one, the tracker can make branches and comment on pull requests. Without, it only listens."
        />

        <Field
          id="repo-transition"
          label="When a pull request is merged, take"
          list="merge-transitions"
          value={transitionOnMerge}
          onChange={(e) => setTransitionOnMerge(e.target.value)}
          placeholder="Leave the issue where it is"
          hint="The name of a transition, as the workflow calls it."
        />
        <datalist id="merge-transitions">
          {transitions.map((t) => (
            <option key={t} value={t} />
          ))}
        </datalist>

        <div className="flex gap-2">
          <Button type="submit" loading={connect.isPending} disabled={!name.includes("/")}>
            Connect
          </Button>
          <Button type="button" variant="ghost" onClick={onCancel}>
            Cancel
          </Button>
        </div>
      </form>
    </Card>
  );
}

/**
 * Shown once, right after connecting or rotating: the address and the secret
 * the host needs. The secret is not retrievable afterwards, only replaced.
 */
function WebhookHandout({ connected, onDone }: { connected: Connected; onDone: () => void }) {
  const { repository, webhookUrl } = connected;
  return (
    <Card className="border-accent/40 bg-accent-subtle/40 p-5" data-testid="webhook-handout">
      <h2 className="text-sm font-semibold text-ink">Now tell {repository.name} where to send its webhooks</h2>
      <p className="mt-1 text-sm text-ink-muted">
        Copy this secret now. It is not shown again; it can only be replaced.
      </p>
      <dl className="mt-3 space-y-2 text-sm">
        <div>
          <dt className="text-ink-muted">Payload URL</dt>
          <dd className="font-mono break-all text-ink" data-testid="webhook-url">
            {webhookUrl}
          </dd>
        </div>
        <div>
          <dt className="text-ink-muted">Secret</dt>
          <dd className="font-mono break-all text-ink" data-testid="webhook-secret">
            {repository.webhookSecret}
          </dd>
        </div>
        <div>
          <dt className="text-ink-muted">Events</dt>
          <dd className="text-ink">
            {repository.host === "gitlab"
              ? "Push, merge request and pipeline events, content type JSON."
              : repository.host === "gitea"
                ? "Push, create, delete and pull request events, content type JSON."
                : "Pushes, pull requests, check runs and workflow runs, content type application/json."}
          </dd>
        </div>
      </dl>
      <div className="mt-4">
        <Button variant="secondary" size="sm" onClick={onDone}>
          I have copied it
        </Button>
      </div>
    </Card>
  );
}

function RepositoryCard({
  repository,
  projectKey,
  onRotated,
}: {
  repository: Repository;
  projectKey: string;
  onRotated: (connected: Connected) => void;
}) {
  const update = useUpdateRepository();
  const rotate = useRotateSecret();
  const disconnect = useDisconnectRepository();
  const [token, setToken] = useState("");
  void projectKey;

  const error = (update.error ?? rotate.error ?? disconnect.error) as Error | undefined;

  return (
    <Card className="p-4" data-repository={repository.name}>
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div className="min-w-0">
          <h2 className="flex items-center gap-2 text-sm font-semibold text-ink">
            <a href={repository.url} className="hover:text-accent" target="_blank" rel="noreferrer">
              {repository.name}
            </a>
            <Tag>{hostLabel(repository.host)}</Tag>
            <Tag className={cx(repository.hasToken ? "text-success" : "text-ink-subtle")}>
              {repository.hasToken ? "read and write" : "read only"}
            </Tag>
          </h2>
          <p className="mt-0.5 text-sm text-ink-muted">
            {repository.commitCount} commit{repository.commitCount === 1 ? "" : "s"} ·{" "}
            {repository.openPullRequestCount} open {pullWord(repository.host)}
            {repository.openPullRequestCount === 1 ? "" : "s"} · branches from {repository.defaultBranch}
            {repository.transitionOnMerge ? ` · merging takes ${repository.transitionOnMerge}` : ""}
          </p>
        </div>
        <div className="flex shrink-0 gap-1">
          <Button
            size="sm"
            variant="secondary"
            loading={rotate.isPending}
            onClick={() => rotate.mutate(repository.id, { onSuccess: onRotated })}
          >
            Rotate secret
          </Button>
          <Button
            size="sm"
            variant="ghost"
            loading={disconnect.isPending}
            onClick={() => disconnect.mutate(repository.id)}
          >
            Disconnect
          </Button>
        </div>
      </div>

      <form
        className="mt-3 flex flex-wrap items-end gap-2"
        onSubmit={(event) => {
          event.preventDefault();
          update.mutate({ id: repository.id, accessToken: token }, { onSuccess: () => setToken("") });
        }}
      >
        <Field
          id={`token-${repository.id}`}
          label={repository.hasToken ? "Replace the access token" : "Add an access token"}
          type="password"
          value={token}
          onChange={(e) => setToken(e.target.value)}
          className="w-72"
        />
        <Button type="submit" size="sm" variant="secondary" loading={update.isPending} disabled={!token.trim()}>
          Save token
        </Button>
        {repository.hasToken && (
          <Button
            type="button"
            size="sm"
            variant="ghost"
            onClick={() => update.mutate({ id: repository.id, accessToken: "" })}
          >
            Remove token
          </Button>
        )}
      </form>

      {error && (
        <div className="mt-3">
          <ErrorBanner>{error.message}</ErrorBanner>
        </div>
      )}
    </Card>
  );
}
