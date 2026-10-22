import { useState, type FormEvent } from "react";
import { Link, createRoute } from "@tanstack/react-router";
import { appRoute } from "./app";
import { useApiTokens, useCreateApiToken, useRevokeApiToken } from "@/api/auth";
import { Button, Card, Checkbox, Chip, EmptyState, ErrorBanner, Field, Page, PageHeader, Select, Tag } from "@/components/ui";
import { useConfirm } from "@/features/shell/ConfirmProvider";
import { ProjectPicker } from "@/features/projects/ProjectPicker";
import { useFormat } from "@/lib/format";
import { CONNECTED_APP_SCOPES, READ_ONLY_SCOPE, TOKEN_DAY_CHOICES, TOKEN_DEFAULT_DAYS } from "@/config";
import type { ApiToken } from "@/api/auth";

/** Days from now as the API wants it, or nothing at all for a token that never expires. */
function expiryFrom(days: number): string | undefined {
  if (days === 0) return undefined;
  const when = new Date();
  when.setDate(when.getDate() + days);
  return when.toISOString();
}

export const tokensRoute = createRoute({
  getParentRoute: () => appRoute,
  path: "/settings/tokens",
  component: TokensPage,
});

function TokensPage() {
  const { data, isLoading } = useApiTokens();
  const create = useCreateApiToken();
  const revoke = useRevokeApiToken();
  const format = useFormat();
  const confirm = useConfirm();
  const [name, setName] = useState("");
  const [readOnly, setReadOnly] = useState(false);
  const [days, setDays] = useState(TOKEN_DEFAULT_DAYS);
  // No project chosen means the token reaches wherever its owner does.
  const [projects, setProjects] = useState<string[]>([]);
  // The secret is shown once, right after creation, and never again.
  const [freshSecret, setFreshSecret] = useState<string | null>(null);

  const tokens = data?.tokens ?? [];

  function onSubmit(event: FormEvent) {
    event.preventDefault();
    if (!name.trim()) return;
    create.mutate({ name, scopes: readOnly ? [READ_ONLY_SCOPE] : [], projects, expiresAt: expiryFrom(days) }, {
      onSuccess: (result) => {
        setFreshSecret(result.token.secret ?? null);
        setName("");
        setReadOnly(false);
        setProjects([]);
        setDays(TOKEN_DEFAULT_DAYS);
      },
    });
  }

  return (
    <Page width="narrow">
      <PageHeader
        crumb={
          <Link to="/settings" className="hover:text-ink">
            Settings
          </Link>
        }
        title="Access tokens" />

      {freshSecret && (
        <Card className="mb-6 border-accent/40 bg-accent-subtle p-4">
          <p className="text-sm font-medium text-ink">Copy this token now</p>
          <p className="mt-1 text-sm text-ink-muted">
            It will not be shown again. Store it somewhere safe.
          </p>
          <code className="mt-3 block overflow-x-auto rounded border border-border bg-surface px-3 py-2 font-mono text-xs text-ink">
            {freshSecret}
          </code>
          <Button variant="ghost" size="sm" className="mt-2" onClick={() => setFreshSecret(null)}>
            Done
          </Button>
        </Card>
      )}

      <Card className="mb-6 p-5">
        <form onSubmit={onSubmit} className="flex flex-col gap-3" data-guide="token-form">
          <div className="flex items-end gap-3">
            <div className="flex-1">
              <Field
                label="New token name"
                placeholder="Deploy pipeline"
                value={name}
                onChange={(e) => setName(e.target.value)}
              />
            </div>
            <Button type="submit" loading={create.isPending} disabled={!name.trim()}>
              Create
            </Button>
          </div>
          <Checkbox
            id="field-can-only-read"
            label={
              <span>
                Can only read
                <span className="ml-2 text-xs text-ink-subtle">It reads everything you can and changes nothing.</span>
              </span>
            }
            checked={readOnly}
            onChange={(e) => setReadOnly(e.target.checked)}
          />
          <Select label="Expires" value={days} onChange={(e) => setDays(Number(e.target.value))} className="max-w-xs">
            {TOKEN_DAY_CHOICES.map((choice) => (
              <option key={choice} value={choice}>
                In {choice} days
              </option>
            ))}
            <option value={0}>Never</option>
          </Select>
          <div data-token-projects>
            <p className="text-sm font-medium text-ink">Projects</p>
            <p className="mb-2 text-xs text-ink-subtle">
              {projects.length === 0
                ? "Every project you can reach. Pick some to confine this token to them."
                : "This token reaches only what is picked here, and never more than you can. Press one to take it off."}
            </p>
            {projects.length > 0 && (
              <div className="mb-2 flex flex-wrap gap-1.5">
                {projects.map((key) => (
                  <Chip key={key} pressed data-project={key} onClick={() => setProjects((chosen) => chosen.filter((k) => k !== key))}>
                    {key}
                  </Chip>
                ))}
              </div>
            )}
            <ProjectPicker
              label="Add a project"
              value=""
              none="Choose a project to add"
              exclude={projects}
              className="max-w-xs"
              onChange={(key) => key && setProjects((chosen) => (chosen.includes(key) ? chosen : [...chosen, key]))}
            />
          </div>
        </form>
        {create.error && (
          <div className="mt-3">
            <ErrorBanner>{(create.error as Error).message}</ErrorBanner>
          </div>
        )}
      </Card>

      {isLoading ? (
        <p className="text-sm text-ink-muted">Loading tokens...</p>
      ) : tokens.length === 0 ? (
        <EmptyState
          title="No tokens yet"
          description="Create one above to call the API from a script or a CI job."
        />
      ) : (
        <Card>
          <ul className="divide-y divide-border">
            {tokens.map((token) => token.app ? (
              <ConnectedAppRow key={token.id} token={token} disconnecting={revoke.isPending && revoke.variables === token.id} onDisconnect={async () => (await confirm({ noun: "app", verb: "Disconnect", body: `${token.app?.name} can no longer act as you; it asks again if you connect it later.` })) && revoke.mutate(token.id)} />
            ) : (
              <li key={token.id} className="flex items-center justify-between gap-4 px-5 py-3" data-token={token.name} data-token-scope={token.scopes.includes(READ_ONLY_SCOPE) ? READ_ONLY_SCOPE : undefined} data-token-projects={token.projects.join(",")}>
                <div className="min-w-0">
                  <p className="flex items-center gap-2 truncate text-sm font-medium text-ink">
                    {token.name}
                    {token.scopes.includes(READ_ONLY_SCOPE) && <Tag>Read only</Tag>}
                  </p>
                  <p className="text-xs text-ink-subtle">
                    Created {format.date(token.createdAt)}
                    {token.lastUsedAt
                      ? ` · last used ${format.date(token.lastUsedAt)}`
                      : " · never used"}
                    {token.expiresAt ? ` · expires ${format.date(token.expiresAt)}` : " · never expires"}
                  </p>
                  <p className="text-xs text-ink-subtle">
                    {token.projects.length > 0 ? `Only ${token.projects.join(", ")}` : "Every project you can reach"}
                  </p>
                </div>
                <Button
                  variant="ghost"
                  size="sm"
                  onClick={async () => (await confirm({ noun: "token", verb: "Revoke", body: `${token.name} stops working at once; anything using it is refused.` })) && revoke.mutate(token.id)}
                  loading={revoke.isPending && revoke.variables === token.id}
                >
                  Revoke
                </Button>
              </li>
            ))}
          </ul>
        </Card>
      )}
    </Page>
  );
}

/** A connected app's grant: what it may do in the consent page's words, and Disconnect. */
function ConnectedAppRow({ token, disconnecting, onDisconnect }: { token: ApiToken; disconnecting: boolean; onDisconnect: () => void }) {
  const format = useFormat();
  const words = token.scopes.map((scope) => CONNECTED_APP_SCOPES.find((s) => s.scope === scope)?.words ?? scope);
  return (
    <li className="flex items-center justify-between gap-4 px-5 py-3" data-token={token.name} data-connected-app-grant={token.app?.name}>
      <div className="min-w-0">
        <p className="flex items-center gap-2 truncate text-sm font-medium text-ink">
          {token.app?.name}
          <Tag>Connected app</Tag>
        </p>
        <p className="text-xs text-ink-subtle">{words.join(" · ")}</p>
        <p className="text-xs text-ink-subtle">
          Connected {format.date(token.createdAt)}
          {token.lastUsedAt ? ` · last used ${format.date(token.lastUsedAt)}` : " · never used"}
          {token.expiresAt && ` · ends ${format.date(token.expiresAt)} unless it is used`}
        </p>
      </div>
      <Button variant="ghost" size="sm" onClick={onDisconnect} loading={disconnecting} data-action="disconnect-app">
        Disconnect
      </Button>
    </li>
  );
}
