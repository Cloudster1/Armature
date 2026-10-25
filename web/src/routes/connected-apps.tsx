import { useState, type FormEvent } from "react";
import { Link, createRoute } from "@tanstack/react-router";
import { appRoute } from "./app";
import { ApiError } from "@/api/client";
import {
  useApproveConnectedApp,
  useConnectedApps,
  useCreateConnectedApp,
  useDeleteConnectedApp,
  useRotateConnectedAppSecret,
  useUpdateConnectedApp,
  type ConnectedApp,
  type ConnectedAppInput,
} from "@/api/oauth";
import { Button, Card, Checkbox, Drawer, EmptyState, ErrorBanner, Field, IconButton, Menu, Page, PageHeader, Table, Tag, Td, Th } from "@/components/ui";
import { Icon } from "@/components/icons";
import { useConfirm } from "@/features/shell/ConfirmProvider";
import { CONNECTED_APP_SCOPES } from "@/config";
import { t } from "@/i18n";

/** The partner tools that may ask people for a token, which administrators decide. */
export const connectedAppsRoute = createRoute({
  getParentRoute: () => appRoute,
  path: "/settings/connected-apps",
  component: ConnectedAppsPage,
});

/** Return addresses as the form takes them, one a line. */
function lines(text: string): string[] {
  return text
    .split("\n")
    .map((line) => line.trim())
    .filter(Boolean);
}

function ConnectedAppsPage() {
  const { data, isLoading } = useConnectedApps();
  const create = useCreateConnectedApp();
  const approve = useApproveConnectedApp();
  const rotate = useRotateConnectedAppSecret();
  const remove = useDeleteConnectedApp();
  const confirm = useConfirm();
  const [adding, setAdding] = useState(false);
  const [editing, setEditing] = useState<ConnectedApp | null>(null);
  // The secret is shown once, right after it is made or rotated, and never again.
  const [fresh, setFresh] = useState<{ app: ConnectedApp; secret: string } | null>(null);
  const apps = data?.clients ?? [];
  const showSecret = (r: { client: ConnectedApp } | void) => r && r.client.secret && setFresh({ app: r.client, secret: r.client.secret });

  return (
    <Page width="content">
      <PageHeader
        crumb={
          <Link to="/settings" className="hover:text-ink">
            {t.settings.crumb}
          </Link>
        }
        title={t.access.apps.title}
        meta={t.access.apps.meta}
        actions={
          <Button icon={<Icon.Plus />} onClick={() => setAdding(true)} data-action="new-connected-app">
            {t.access.apps.add}
          </Button>
        }
      />

      {fresh && (
        <Card className="mb-4 p-4" data-connected-app-secret>
          <p className="text-sm text-ink">
            {t.access.apps.secretFor} <span className="font-medium">{fresh.app.name}</span>. {t.access.apps.secretNow}
          </p>
          <dl className="mt-2 grid grid-cols-[auto_1fr] items-center gap-x-3 gap-y-1 text-sm">
            <dt className="text-ink-muted">{t.access.apps.clientId}</dt>
            <dd>
              <code className="rounded-control bg-surface-raised px-2 py-1 font-mono text-ink" data-client-id>
                {fresh.app.id}
              </code>
            </dd>
            <dt className="text-ink-muted">{t.access.apps.secret}</dt>
            <dd>
              <code className="rounded-control bg-surface-raised px-2 py-1 font-mono text-ink" data-client-secret>
                {fresh.secret}
              </code>
            </dd>
          </dl>
          <Button variant="ghost" size="sm" className="mt-2" onClick={() => setFresh(null)}>
            {t.access.done}
          </Button>
        </Card>
      )}

      {adding && (
        <Card className="mb-4 p-5">
          <AppForm
            submit={t.access.apps.addApp}
            pending={create.isPending}
            error={create.error}
            withConfidential
            onCancel={() => setAdding(false)}
            onSubmit={(input) =>
              create.mutate(input, {
                onSuccess: (r) => {
                  setAdding(false);
                  showSecret(r);
                },
              })
            }
          />
        </Card>
      )}

      {isLoading ? null : apps.length === 0 ? (
        <EmptyState icon={<Icon.Key />} title={t.access.apps.empty} description={t.access.apps.emptyDetail} />
      ) : (
        <Table>
          <thead>
            <tr>
              <Th>{t.access.apps.app}</Th>
              <Th>{t.access.apps.mayAsk}</Th>
              <Th>{t.access.apps.connected}</Th>
              <Th className="w-12" />
            </tr>
          </thead>
          <tbody>
            {apps.map((app) => (
              <tr key={app.id} data-connected-app={app.name} data-connected-app-status={app.status}>
                <Td>
                  <span className="flex items-center gap-2 font-medium text-ink">
                    {app.name}
                    {app.status === "pending" && <Tag>{t.access.apps.waiting}</Tag>}
                  </span>
                  <span className="block truncate text-xs text-ink-muted">{app.clientUri || app.redirectUris[0]}</span>
                  {app.webhook && (
                    <span className="block truncate text-xs text-ink-muted" data-connected-app-webhook>
                      {t.access.apps.webhook(app.webhook.url, app.webhook.topics.join(", "))}
                      {app.webhook.enabled ? "" : t.access.apps.webhookOff}
                    </span>
                  )}
                </Td>
                <Td className="text-sm text-ink-muted">{app.scopes.join(", ")}</Td>
                <Td className="text-sm text-ink-muted">{t.access.groups.people(app.grants)}</Td>
                <Td>
                  <Menu
                    label={t.access.matrix.actionsFor(app.name)}
                    align="end"
                    trigger={(props) => (
                      <IconButton
                        icon={<Icon.More />}
                        label={t.access.matrix.actionsFor(app.name)}
                        size="sm"
                        onClick={props.toggle}
                        aria-haspopup={props["aria-haspopup"]}
                        aria-expanded={props["aria-expanded"]}
                        data-connected-app-menu={app.name}
                      />
                    )}
                    items={[
                      ...(app.status === "pending"
                        ? [
                            {
                              label: t.access.apps.allow,
                              icon: <Icon.Check />,
                              onSelect: () => approve.mutate(app.id),
                              attrs: { "data-action": "connected-app-allow" },
                            },
                          ]
                        : []),
                      { label: t.access.apps.edit, icon: <Icon.Edit />, onSelect: () => setEditing(app), attrs: { "data-action": "connected-app-edit" } },
                      ...(app.confidential
                        ? [
                            {
                              label: t.access.apps.rotate,
                              icon: <Icon.Key />,
                              onSelect: () => rotate.mutate(app.id, { onSuccess: showSecret }),
                              attrs: { "data-action": "connected-app-rotate" },
                            },
                          ]
                        : []),
                      {
                        label: t.access.remove,
                        icon: <Icon.Trash />,
                        danger: true,
                        onSelect: async () =>
                          (await confirm({
                            noun: t.kit.confirm.things.app,
                            verb: t.kit.confirm.verbs.remove,
                            body: t.access.apps.removeBody(app.name),
                          })) && remove.mutate(app.id),
                        attrs: { "data-action": "connected-app-delete" },
                      },
                    ]}
                  />
                </Td>
              </tr>
            ))}
          </tbody>
        </Table>
      )}
      {editing && <EditApp app={editing} onClose={() => setEditing(null)} />}
    </Page>
  );
}

function EditApp({ app, onClose }: { app: ConnectedApp; onClose: () => void }) {
  const update = useUpdateConnectedApp();
  return (
    <Drawer open onClose={onClose} title={t.access.apps.editTitle(app.name)} attrs={{ "data-connected-app-edit": app.name }}>
      <AppForm
        app={app}
        submit={t.access.save}
        pending={update.isPending}
        error={update.error}
        onCancel={onClose}
        onSubmit={(input) => update.mutate({ id: app.id, ...input }, { onSuccess: onClose })}
      />
    </Drawer>
  );
}

interface AppFormProps {
  app?: ConnectedApp;
  submit: string;
  pending: boolean;
  error: Error | null;
  /** Only an app being added says whether it keeps a secret; that never changes after. */
  withConfidential?: boolean;
  onSubmit: (input: ConnectedAppInput) => void;
  onCancel: () => void;
}

function AppForm({ app, submit, pending, error, withConfidential, onSubmit, onCancel }: AppFormProps) {
  const [name, setName] = useState(app?.name ?? "");
  const [clientUri, setClientUri] = useState(app?.clientUri ?? "");
  const [redirects, setRedirects] = useState((app?.redirectUris ?? []).join("\n"));
  const [scopes, setScopes] = useState<string[]>(app?.scopes ?? ["read"]);
  const [confidential, setConfidential] = useState(true);
  const [keycloak, setKeycloak] = useState(app?.keycloakClientId ?? "");
  const fields = error instanceof ApiError ? error.fields : {};

  function send(event: FormEvent) {
    event.preventDefault();
    onSubmit({
      name: name.trim(),
      clientUri: clientUri.trim(),
      redirectUris: lines(redirects),
      scopes,
      keycloakClientId: keycloak.trim(),
      ...(withConfidential ? { confidential } : {}),
    });
  }

  return (
    <form onSubmit={send} className="space-y-4" noValidate data-connected-app-form>
      {error && Object.keys(fields).length === 0 && <ErrorBanner>{error.message}</ErrorBanner>}
      <Field label={t.access.apps.name} required value={name} onChange={(e) => setName(e.target.value)} placeholder="Stator" error={fields.name} />
      <Field
        label={t.access.apps.address}
        value={clientUri}
        onChange={(e) => setClientUri(e.target.value)}
        placeholder="https://stator.example.com"
        hint={t.access.apps.addressHint}
        error={fields.clientUri}
      />
      <Field
        label={t.access.apps.returns}
        rows={3}
        value={redirects}
        onChange={(e) => setRedirects(e.target.value)}
        placeholder="https://stator.example.com/oauth/callback"
        hint={t.access.apps.returnsHint}
        error={fields.redirectUris}
      />
      <fieldset className="space-y-2">
        <legend className="text-sm font-medium text-ink-muted">{t.access.apps.itMayAsk}</legend>
        {fields.scopes && <p className="text-sm text-danger">{fields.scopes}</p>}
        {CONNECTED_APP_SCOPES.map((s) => (
          <Checkbox
            key={s.scope}
            label={
              <span>
                {s.words} <span className="font-mono text-xs text-ink-subtle">{s.scope}</span>
              </span>
            }
            checked={scopes.includes(s.scope)}
            disabled={s.scope === "read"}
            onChange={(e) => setScopes(e.target.checked ? [...scopes, s.scope] : scopes.filter((x) => x !== s.scope))}
            data-scope={s.scope}
          />
        ))}
      </fieldset>
      {withConfidential && (
        <Checkbox
          label={
            <span>
              {t.access.apps.keepsSecret}
              <span className="ml-2 text-xs text-ink-subtle">{t.access.apps.keepsSecretDetail}</span>
            </span>
          }
          checked={confidential}
          onChange={(e) => setConfidential(e.target.checked)}
          data-confidential
        />
      )}
      <Field
        label={t.access.apps.keycloak}
        value={keycloak}
        onChange={(e) => setKeycloak(e.target.value)}
        placeholder="stator"
        hint={t.access.apps.keycloakHint}
      />
      <div className="flex gap-2">
        <Button type="submit" loading={pending} disabled={!name.trim() || lines(redirects).length === 0} data-action="save-connected-app">
          {submit}
        </Button>
        <Button variant="ghost" onClick={onCancel}>
          {t.kit.cancel}
        </Button>
      </div>
    </form>
  );
}
