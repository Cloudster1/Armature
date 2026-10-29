import { useState, type FormEvent } from "react";
import { Link, createRoute } from "@tanstack/react-router";
import { adminRoute } from "./admin";
import {
  useAutomationCatalog,
  useCreateWebhook,
  useDeleteWebhook,
  useDeliveries,
  useRedeliver,
  useRotateWebhookSecret,
  useTestWebhook,
  useUpdateWebhook,
  useWebhooks,
  type Webhook,
} from "@/api/automation";
import {
  Button,
  Card,
  Checkbox,
  Drawer,
  EmptyState,
  ErrorBanner,
  Field,
  IconButton,
  Menu,
  Page,
  Select,
  PageHeader,
  Table,
  Tag,
  Td,
  Th,
  useToast,
} from "@/components/ui";
import { Icon } from "@/components/icons";
import { useConfirm } from "@/features/shell/ConfirmProvider";
import { useFormat } from "@/lib/format";
// Named apart: the topic list calls each topic t.
import { t as words } from "@/i18n";

/** Where the organization's events are posted, signed, with a log of every try. */
export const webhooksRoute = createRoute({
  getParentRoute: () => adminRoute,
  path: "/webhooks",
  component: WebhooksPage,
});

function WebhooksPage() {
  const { data, isLoading } = useWebhooks();
  const { data: catalog } = useAutomationCatalog();
  const create = useCreateWebhook();
  const update = useUpdateWebhook();
  const remove = useDeleteWebhook();
  const rotate = useRotateWebhookSecret();
  const test = useTestWebhook();
  const confirm = useConfirm();
  const toast = useToast();
  const [adding, setAdding] = useState(false);
  const [name, setName] = useState("");
  const [url, setUrl] = useState("");
  const [topics, setTopics] = useState<string[]>(["*"]);
  const [version, setVersion] = useState(1);
  // The secret is shown once, right after it is made or rotated, and never again.
  const [fresh, setFresh] = useState<{ name: string; secret: string } | null>(null);
  const [logOf, setLogOf] = useState<Webhook | null>(null);

  const hooks = data?.webhooks ?? [];
  const allTopics = catalog?.topics ?? ["*"];
  const versions = catalog?.versions ?? [1];

  function onSubmit(event: FormEvent) {
    event.preventDefault();
    create.mutate(
      { name: name.trim(), url: url.trim(), topics, version },
      {
        onSuccess: (r) => {
          setFresh({ name: r.webhook.name, secret: r.webhook.secret ?? "" });
          setAdding(false);
          setName("");
          setUrl("");
          setTopics(["*"]);
          setVersion(1);
        },
      },
    );
  }

  return (
    <Page width="content">
      <PageHeader
        crumb={
          <Link to="/admin" className="hover:text-ink">
            {words.settings.crumb}
          </Link>
        }
        title={words.access.webhooks.title}
        meta={words.access.webhooks.meta}
        actions={
          <Button icon={<Icon.Plus />} onClick={() => setAdding(true)} data-action="new-webhook" data-guide="new-webhook">
            {words.access.webhooks.add}
          </Button>
        }
      />

      {fresh && (
        <Card className="mb-4 p-4" data-webhook-secret>
          <p className="text-sm text-ink">
            {words.access.webhooks.secretFor} <span className="font-medium">{fresh.name}</span>. {words.access.webhooks.secretCarries}{" "}
            <span className="font-mono">X-Armature-Signature-256: sha256=HMAC(body)</span> {words.access.webhooks.withIt}{" "}
            <span className="font-mono">X-Armature-Signature-Timestamped-256: sha256=HMAC(timestamp.body)</span> {words.access.webhooks.beside}{" "}
            <span className="font-mono">X-Armature-Timestamp</span>
            {words.access.webhooks.timestampHint}
          </p>
          <div className="mt-2 flex items-center gap-2">
            <code className="rounded-control bg-surface-raised px-2 py-1 font-mono text-sm text-ink" data-secret-value>
              {fresh.secret}
            </code>
            <Button variant="ghost" size="sm" onClick={() => setFresh(null)}>
              {words.access.done}
            </Button>
          </div>
        </Card>
      )}

      {adding && (
        <Card className="mb-4 p-5">
          <form onSubmit={onSubmit} className="space-y-4" noValidate data-webhook-form>
            {create.error && <ErrorBanner>{(create.error as Error).message}</ErrorBanner>}
            <Field
              label={words.access.webhooks.name}
              required
              value={name}
              onChange={(e) => setName(e.target.value)}
              placeholder={words.access.webhooks.namePlaceholder}
            />
            <Field
              label={words.access.webhooks.address}
              required
              value={url}
              onChange={(e) => setUrl(e.target.value)}
              placeholder="https://example.com/hooks/armature"
              hint={words.access.webhooks.addressHint}
            />
            <fieldset className="space-y-2">
              <legend className="text-sm font-medium text-ink-muted">{words.access.webhooks.topics}</legend>
              <div className="flex flex-wrap gap-x-4 gap-y-1">
                {allTopics.map((t) => (
                  <Checkbox
                    key={t}
                    label={t === "*" ? words.access.webhooks.everything : t}
                    checked={topics.includes(t)}
                    onChange={(e) => setTopics(e.target.checked ? [...topics, t] : topics.filter((x) => x !== t))}
                    data-topic={t}
                  />
                ))}
              </div>
            </fieldset>
            <Select
              label={words.access.webhooks.version}
              value={String(version)}
              onChange={(e) => setVersion(Number(e.target.value))}
              hint={words.access.webhooks.versionHint}
              data-webhook-version-pick
            >
              {versions.map((v) => (
                <option key={v} value={v}>
                  {words.access.webhooks.versionName(v)}
                </option>
              ))}
            </Select>
            <div className="flex gap-2">
              <Button type="submit" loading={create.isPending} disabled={!name.trim() || !url.trim()} data-action="save-webhook">
                {words.access.webhooks.addWebhook}
              </Button>
              <Button variant="ghost" onClick={() => setAdding(false)}>
                {words.kit.cancel}
              </Button>
            </div>
          </form>
        </Card>
      )}

      {isLoading ? null : hooks.length === 0 ? (
        <EmptyState icon={<Icon.Hook />} title={words.access.webhooks.empty} description={words.access.webhooks.emptyDetail} />
      ) : (
        <Table>
          <thead>
            <tr>
              <Th>{words.access.newUser.name}</Th>
              <Th>{words.access.webhooks.address}</Th>
              <Th>{words.access.webhooks.topics}</Th>
              <Th>{words.access.webhooks.version}</Th>
              <Th>{words.access.users.state}</Th>
              <Th className="w-12" />
            </tr>
          </thead>
          <tbody>
            {hooks.map((h) => (
              <tr key={h.id} data-webhook={h.name} data-webhook-enabled={h.enabled ? "true" : "false"} data-webhook-partner={h.partner?.name}>
                <Td className="font-medium text-ink">
                  <span className="flex items-center gap-2">
                    {h.name}
                    {h.partner && <Tag title={words.access.webhooks.managesIt(h.partner.name)}>{words.access.webhooks.managedBy(h.partner.name)}</Tag>}
                  </span>
                </Td>
                <Td className="max-w-xs truncate font-mono text-xs text-ink-muted">{h.url}</Td>
                <Td className="text-sm text-ink-muted">{h.topics.includes("*") ? words.access.webhooks.everythingLower : h.topics.join(", ")}</Td>
                <Td className="text-sm text-ink-muted" data-webhook-version={h.version}>
                  {words.access.webhooks.versionName(h.version)}
                </Td>
                <Td>
                  <Tag>{h.enabled ? words.access.webhooks.on : words.access.users.off}</Tag>
                </Td>
                <Td>
                  <Menu
                    label={words.access.matrix.actionsFor(h.name)}
                    align="end"
                    trigger={(props) => (
                      <IconButton
                        icon={<Icon.More />}
                        label={words.access.matrix.actionsFor(h.name)}
                        size="sm"
                        onClick={props.toggle}
                        aria-haspopup={props["aria-haspopup"]}
                        aria-expanded={props["aria-expanded"]}
                        data-webhook-menu={h.name}
                      />
                    )}
                    items={[
                      {
                        label: words.access.webhooks.deliveries,
                        icon: <Icon.Clock />,
                        onSelect: () => setLogOf(h),
                        attrs: { "data-action": "webhook-deliveries" },
                      },
                      {
                        label: words.access.webhooks.sendTest,
                        icon: <Icon.Bolt />,
                        onSelect: () =>
                          test.mutate(h.id, {
                            onSuccess: (r) =>
                              toast[r.delivery.deliveredAt ? "success" : "error"](
                                r.delivery.deliveredAt
                                  ? words.access.webhooks.delivered(r.delivery.status)
                                  : words.access.webhooks.notDelivered(r.delivery.error ?? ""),
                              ),
                          }),
                        attrs: { "data-action": "webhook-test" },
                      },
                      {
                        label: h.enabled ? words.access.webhooks.turnOff : words.access.webhooks.turnOn,
                        icon: h.enabled ? <Icon.EyeOff /> : <Icon.Eye />,
                        onSelect: () => update.mutate({ id: h.id, name: h.name, url: h.url, topics: h.topics, enabled: !h.enabled }),
                        attrs: { "data-action": "webhook-toggle" },
                      },
                      // A connected app's own secret is the app's to rotate.
                      ...(h.partner
                        ? []
                        : [
                            {
                              label: words.access.apps.rotate,
                              icon: <Icon.Key />,
                              onSelect: () => rotate.mutate(h.id, { onSuccess: (r) => setFresh({ name: r.webhook.name, secret: r.webhook.secret ?? "" }) }),
                              attrs: { "data-action": "webhook-rotate" },
                            },
                          ]),
                      {
                        label: words.access.delete,
                        icon: <Icon.Trash />,
                        danger: true,
                        onSelect: async () =>
                          (await confirm({
                            noun: words.kit.confirm.things.webhook,
                            verb: words.kit.confirm.verbs.delete,
                            body: words.access.webhooks.deleteBody(h.name),
                          })) && remove.mutate(h.id),
                        attrs: { "data-action": "webhook-delete" },
                      },
                    ]}
                  />
                </Td>
              </tr>
            ))}
          </tbody>
        </Table>
      )}
      {logOf && <DeliveryLog hook={logOf} onClose={() => setLogOf(null)} />}
    </Page>
  );
}

function DeliveryLog({ hook, onClose }: { hook: Webhook; onClose: () => void }) {
  const { data } = useDeliveries(hook.id);
  const redeliver = useRedeliver();
  const format = useFormat();
  const deliveries = data?.deliveries ?? [];
  return (
    <Drawer open onClose={onClose} title={words.access.webhooks.deliveriesTo(hook.name)} attrs={{ "data-webhook-log": hook.name }}>
      {deliveries.length === 0 ? (
        <p className="text-sm text-ink-muted">{words.access.webhooks.nothingSent}</p>
      ) : (
        <ul className="divide-y divide-border">
          {deliveries.map((d) => (
            <li
              key={d.id}
              className="flex items-center gap-3 py-2.5 text-sm"
              data-delivery={d.topic}
              data-delivery-state={d.deliveredAt ? "delivered" : d.nextAttemptAt ? "waiting" : "failed"}
            >
              <Tag className={d.deliveredAt ? "" : d.nextAttemptAt ? "text-ink-subtle" : "text-danger"}>
                {d.deliveredAt
                  ? words.access.webhooks.stateDelivered
                  : d.nextAttemptAt
                    ? words.access.webhooks.stateWaiting
                    : words.access.webhooks.stateFailed}
              </Tag>
              <span className="min-w-0 flex-1">
                <span className="block text-ink">
                  {d.topic} <span className="text-ink-subtle">{words.access.webhooks.attempt(d.attempt)}</span>
                </span>
                <span className="block truncate text-xs text-ink-muted">
                  {d.deliveredAt
                    ? words.access.webhooks.answered(d.status)
                    : d.error || (d.nextAttemptAt ? words.access.webhooks.nextTry(format.relative(d.nextAttemptAt)) : "")}
                </span>
              </span>
              <span className="text-xs text-ink-subtle">{format.relative(d.createdAt)}</span>
              {!d.nextAttemptAt && (
                <Button
                  variant="secondary"
                  size="sm"
                  loading={redeliver.isPending}
                  onClick={() => redeliver.mutate({ endpointId: hook.id, deliveryId: d.id })}
                  data-action="redeliver"
                >
                  {words.access.webhooks.again}
                </Button>
              )}
            </li>
          ))}
        </ul>
      )}
    </Drawer>
  );
}
