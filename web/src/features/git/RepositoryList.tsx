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
import { useConfirm } from "@/features/shell/ConfirmProvider";
// Named apart: the transitions here are each called t.
import { t as words } from "@/i18n";

/**
 * The repositories connected to a project. Connecting hands back the webhook secret once, as a
 * personal token is; the controls are drawn only for those the server lets administer the project.
 */
export function RepositoryList({ projectKey, canManage }: { projectKey: string; canManage: boolean }) {
  const { data, isLoading, error } = useRepositories(projectKey);
  const [connecting, setConnecting] = useState(false);
  const [handed, setHanded] = useState<Connected | null>(null);

  const repositories = data?.repositories ?? [];

  if (error) return <ErrorBanner>{(error as Error).message}</ErrorBanner>;

  return (
    <div className="space-y-4">
      {handed && <WebhookHandout connected={handed} onDone={() => setHanded(null)} />}

      {!canManage ? null : connecting ? (
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
          <Button onClick={() => setConnecting(true)}>{words.projectSetup.repositories.connect}</Button>
        </div>
      )}

      {isLoading ? null : repositories.length === 0 && !connecting ? (
        <EmptyState title={words.projectSetup.repositories.empty} description={words.projectSetup.repositories.emptyDetail} />
      ) : (
        repositories.map((repository) => <RepositoryCard key={repository.id} repository={repository} canManage={canManage} onRotated={setHanded} />)
      )}
    </div>
  );
}

const apiPlaceholder: Record<HostKind, string> = {
  github: "https://api.github.com",
  gitlab: "https://gitlab.com/api/v4",
  gitea: "https://gitea.example.com/api/v1",
};

function ConnectForm({ projectKey, onConnected, onCancel }: { projectKey: string; onConnected: (connected: Connected) => void; onCancel: () => void }) {
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
  const fallbackWorkflowId = workflows?.assignments.find((a) => a.origin.named === false)?.workflowId ?? workflows?.assignments[0]?.workflowId ?? "";
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
          <Select id="repo-host" label={words.projectSetup.repositories.host} value={host} onChange={(e) => setHost(e.target.value as HostKind)}>
            <option value="github">GitHub</option>
            <option value="gitlab">GitLab</option>
            <option value="gitea">Gitea</option>
          </Select>
          <Field
            label={words.projectSetup.repositories.repository}
            required
            autoFocus
            value={name}
            onChange={(e) => setName(e.target.value)}
            placeholder="acme/portal"
            hint={words.projectSetup.repositories.repositoryHint}
          />
        </div>

        <div className="grid gap-4 sm:grid-cols-2">
          <Field
            label={words.projectSetup.repositories.defaultBranch}
            value={defaultBranch}
            onChange={(e) => setDefaultBranch(e.target.value)}
            hint={words.projectSetup.repositories.defaultBranchHint}
          />
          <Field
            label={words.projectSetup.repositories.api}
            value={apiBaseUrl}
            onChange={(e) => setApiBaseUrl(e.target.value)}
            placeholder={apiPlaceholder[host]}
            hint={host === "gitea" ? words.projectSetup.repositories.apiHintGitea : words.projectSetup.repositories.apiHint}
          />
        </div>

        <Field
          label={words.projectSetup.repositories.token}
          type="password"
          value={accessToken}
          onChange={(e) => setAccessToken(e.target.value)}
          hint={words.projectSetup.repositories.tokenHint}
        />

        <Field
          id="repo-transition"
          label={words.projectSetup.repositories.onMerge}
          list="merge-transitions"
          value={transitionOnMerge}
          onChange={(e) => setTransitionOnMerge(e.target.value)}
          placeholder={words.projectSetup.repositories.onMergePlaceholder}
          hint={words.projectSetup.repositories.onMergeHint}
        />
        <datalist id="merge-transitions">
          {transitions.map((t) => (
            <option key={t} value={t} />
          ))}
        </datalist>

        <div className="flex gap-2">
          <Button type="submit" loading={connect.isPending} disabled={!name.includes("/")}>
            {words.projectSetup.repositories.connectButton}
          </Button>
          <Button type="button" variant="ghost" onClick={onCancel}>
            {words.kit.cancel}
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
      <h2 className="text-sm font-semibold text-ink">{words.projectSetup.repositories.handoutTitle(repository.name)}</h2>
      <p className="mt-1 text-sm text-ink-muted">{words.projectSetup.repositories.handoutDetail}</p>
      <dl className="mt-3 space-y-2 text-sm">
        <div>
          <dt className="text-ink-muted">{words.projectSetup.repositories.payload}</dt>
          <dd className="font-mono break-all text-ink" data-testid="webhook-url">
            {webhookUrl}
          </dd>
        </div>
        <div>
          <dt className="text-ink-muted">{words.projectSetup.repositories.secret}</dt>
          <dd className="font-mono break-all text-ink" data-testid="webhook-secret">
            {repository.webhookSecret}
          </dd>
        </div>
        <div>
          <dt className="text-ink-muted">{words.projectSetup.repositories.events}</dt>
          <dd className="text-ink">
            {repository.host === "gitlab"
              ? words.projectSetup.repositories.gitlabEvents
              : repository.host === "gitea"
                ? words.projectSetup.repositories.giteaEvents
                : words.projectSetup.repositories.githubEvents}
          </dd>
        </div>
      </dl>
      <div className="mt-4">
        <Button variant="secondary" size="sm" onClick={onDone}>
          {words.projectSetup.repositories.copied}
        </Button>
      </div>
    </Card>
  );
}

function RepositoryCard({ repository, canManage, onRotated }: { repository: Repository; canManage: boolean; onRotated: (connected: Connected) => void }) {
  const update = useUpdateRepository();
  const rotate = useRotateSecret();
  const disconnect = useDisconnectRepository();
  const confirm = useConfirm();
  const [token, setToken] = useState("");

  const error = (update.error ?? rotate.error ?? disconnect.error) as Error | undefined;

  async function onDisconnect() {
    const sure = await confirm({
      noun: words.kit.confirm.things.repository(repository.name),
      verb: words.kit.confirm.verbs.disconnect,
      body: words.projectSetup.repositories.disconnectBody(repository.name, repository.commitCount, pullWord(repository.host)),
    });
    if (sure) disconnect.mutate(repository.id);
  }

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
              {repository.hasToken ? words.projectSetup.repositories.readWrite : words.projectSetup.repositories.readOnly}
            </Tag>
          </h2>
          <p className="mt-0.5 text-sm text-ink-muted">
            {words.projectSetup.repositories.summary(
              repository.commitCount,
              repository.openPullRequestCount,
              pullWord(repository.host),
              repository.defaultBranch,
            )}
            {repository.transitionOnMerge ? words.projectSetup.repositories.merging(repository.transitionOnMerge) : ""}
          </p>
        </div>
        {canManage && (
          <div className="flex shrink-0 gap-1">
            <Button size="sm" variant="secondary" loading={rotate.isPending} onClick={() => rotate.mutate(repository.id, { onSuccess: onRotated })}>
              {words.projectSetup.repositories.rotate}
            </Button>
            <Button size="sm" variant="ghost" loading={disconnect.isPending} onClick={onDisconnect}>
              {words.projectSetup.repositories.disconnect}
            </Button>
          </div>
        )}
      </div>

      {canManage && (
        <form
          className="mt-3 flex flex-wrap items-end gap-2"
          onSubmit={(event) => {
            event.preventDefault();
            update.mutate({ id: repository.id, accessToken: token }, { onSuccess: () => setToken("") });
          }}
        >
          <Field
            id={`token-${repository.id}`}
            label={repository.hasToken ? words.projectSetup.repositories.replaceToken : words.projectSetup.repositories.addToken}
            type="password"
            value={token}
            onChange={(e) => setToken(e.target.value)}
            className="w-72"
          />
          <Button type="submit" size="sm" variant="secondary" loading={update.isPending} disabled={!token.trim()}>
            {words.projectSetup.repositories.saveToken}
          </Button>
          {repository.hasToken && (
            <Button type="button" size="sm" variant="ghost" onClick={() => update.mutate({ id: repository.id, accessToken: "" })}>
              {words.projectSetup.repositories.removeToken}
            </Button>
          )}
        </form>
      )}

      {error && (
        <div className="mt-3">
          <ErrorBanner>{error.message}</ErrorBanner>
        </div>
      )}
    </Card>
  );
}
