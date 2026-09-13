import { useState, type FormEvent } from "react";
import { Link, createRoute } from "@tanstack/react-router";
import { appRoute } from "./app";
import { useAutomationCatalog, useCreateWebhook, useDeleteWebhook, useDeliveries, useRedeliver, useRotateWebhookSecret, useTestWebhook, useUpdateWebhook, useWebhooks, type Webhook } from "@/api/automation";
import { Button, Card, Checkbox, Drawer, EmptyState, ErrorBanner, Field, IconButton, Menu, Page, PageHeader, Table, Tag, Td, Th, useToast } from "@/components/ui";
import { Icon } from "@/components/icons";
import { useConfirm } from "@/features/shell/ConfirmProvider";
import { useFormat } from "@/lib/format";

/** Where the organization's events are posted, signed, with a log of every try. */
export const webhooksRoute = createRoute({
  getParentRoute: () => appRoute,
  path: "/settings/webhooks",
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
  // The secret is shown once, right after it is made or rotated, and never again.
  const [fresh, setFresh] = useState<{ name: string; secret: string } | null>(null);
  const [logOf, setLogOf] = useState<Webhook | null>(null);

  const hooks = data?.webhooks ?? [];
  const allTopics = catalog?.topics ?? ["*"];

  function onSubmit(event: FormEvent) {
    event.preventDefault();
    create.mutate(
      { name: name.trim(), url: url.trim(), topics },
      {
        onSuccess: (r) => {
          setFresh({ name: r.webhook.name, secret: r.webhook.secret ?? "" });
          setAdding(false);
          setName("");
          setUrl("");
          setTopics(["*"]);
        },
      },
    );
  }

  return (
    <Page width="content">
      <PageHeader
        crumb={
          <Link to="/settings" className="hover:text-ink">
            Settings
          </Link>
        }
        title="Webhooks"
        meta="Where events are posted, signed with each endpoint's secret"
        actions={
          <Button icon={<Icon.Plus />} onClick={() => setAdding(true)} data-action="new-webhook" data-guide="new-webhook">
            Add a webhook
          </Button>
        }
      />

      {fresh && (
        <Card className="mb-4 p-4" data-webhook-secret>
          <p className="text-sm text-ink">
            The secret for <span className="font-medium">{fresh.name}</span>. Copy it now; it is not shown again. Each delivery carries <span className="font-mono">X-Armature-Signature-256: sha256=HMAC(body)</span> with it.
          </p>
          <div className="mt-2 flex items-center gap-2">
            <code className="rounded-control bg-surface-raised px-2 py-1 font-mono text-sm text-ink" data-secret-value>
              {fresh.secret}
            </code>
            <Button variant="ghost" size="sm" onClick={() => setFresh(null)}>
              Done
            </Button>
          </div>
        </Card>
      )}

      {adding && (
        <Card className="mb-4 p-5">
          <form onSubmit={onSubmit} className="space-y-4" noValidate data-webhook-form>
            {create.error && <ErrorBanner>{(create.error as Error).message}</ErrorBanner>}
            <Field label="Webhook name" required value={name} onChange={(e) => setName(e.target.value)} placeholder="Chat room" />
            <Field label="Address" required value={url} onChange={(e) => setUrl(e.target.value)} placeholder="https://example.com/hooks/armature" hint="Events are posted here as JSON." />
            <fieldset className="space-y-2">
              <legend className="text-sm font-medium text-ink-muted">Topics</legend>
              <div className="flex flex-wrap gap-x-4 gap-y-1">
                {allTopics.map((t) => (
                  <Checkbox
                    key={t}
                    label={t === "*" ? "Everything" : t}
                    checked={topics.includes(t)}
                    onChange={(e) => setTopics(e.target.checked ? [...topics, t] : topics.filter((x) => x !== t))}
                    data-topic={t}
                  />
                ))}
              </div>
            </fieldset>
            <div className="flex gap-2">
              <Button type="submit" loading={create.isPending} disabled={!name.trim() || !url.trim()} data-action="save-webhook">
                Add webhook
              </Button>
              <Button variant="ghost" onClick={() => setAdding(false)}>
                Cancel
              </Button>
            </div>
          </form>
        </Card>
      )}

      {isLoading ? null : hooks.length === 0 ? (
        <EmptyState icon={<Icon.Hook />} title="No webhooks yet" description="An endpoint is posted every event it subscribes to, signed with a secret it is shown once, with a log of every try." />
      ) : (
        <Table>
          <thead>
            <tr>
              <Th>Name</Th>
              <Th>Address</Th>
              <Th>Topics</Th>
              <Th>State</Th>
              <Th className="w-12" />
            </tr>
          </thead>
          <tbody>
            {hooks.map((h) => (
              <tr key={h.id} data-webhook={h.name} data-webhook-enabled={h.enabled ? "true" : "false"}>
                <Td className="font-medium text-ink">{h.name}</Td>
                <Td className="max-w-xs truncate font-mono text-xs text-ink-muted">{h.url}</Td>
                <Td className="text-sm text-ink-muted">{h.topics.includes("*") ? "everything" : h.topics.join(", ")}</Td>
                <Td>
                  <Tag>{h.enabled ? "On" : "Off"}</Tag>
                </Td>
                <Td>
                  <Menu
                    label={`Actions for ${h.name}`}
                    align="end"
                    trigger={(props) => <IconButton icon={<Icon.More />} label={`Actions for ${h.name}`} size="sm" onClick={props.toggle} aria-haspopup={props["aria-haspopup"]} aria-expanded={props["aria-expanded"]} data-webhook-menu={h.name} />}
                    items={[
                      { label: "Deliveries", icon: <Icon.Clock />, onSelect: () => setLogOf(h), attrs: { "data-action": "webhook-deliveries" } },
                      { label: "Send a test", icon: <Icon.Bolt />, onSelect: () => test.mutate(h.id, { onSuccess: (r) => toast[r.delivery.deliveredAt ? "success" : "error"](r.delivery.deliveredAt ? `Delivered, ${r.delivery.status}` : `Not delivered: ${r.delivery.error}`) }), attrs: { "data-action": "webhook-test" } },
                      { label: h.enabled ? "Turn off" : "Turn on", icon: h.enabled ? <Icon.EyeOff /> : <Icon.Eye />, onSelect: () => update.mutate({ id: h.id, name: h.name, url: h.url, topics: h.topics, enabled: !h.enabled }), attrs: { "data-action": "webhook-toggle" } },
                      { label: "Rotate the secret", icon: <Icon.Key />, onSelect: () => rotate.mutate(h.id, { onSuccess: (r) => setFresh({ name: r.webhook.name, secret: r.webhook.secret ?? "" }) }), attrs: { "data-action": "webhook-rotate" } },
                      {
                        label: "Delete",
                        icon: <Icon.Trash />,
                        danger: true,
                        onSelect: async () => (await confirm({ noun: "webhook", verb: "Delete", body: `${h.name} stops receiving at once, and its log goes with it.` })) && remove.mutate(h.id),
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
    <Drawer open onClose={onClose} title={`Deliveries to ${hook.name}`} attrs={{ "data-webhook-log": hook.name }}>
      {deliveries.length === 0 ? (
        <p className="text-sm text-ink-muted">Nothing has been sent here yet. Send a test from the menu.</p>
      ) : (
        <ul className="divide-y divide-border">
          {deliveries.map((d) => (
            <li key={d.id} className="flex items-center gap-3 py-2.5 text-sm" data-delivery={d.topic} data-delivery-state={d.deliveredAt ? "delivered" : d.nextAttemptAt ? "waiting" : "failed"}>
              <Tag className={d.deliveredAt ? "" : d.nextAttemptAt ? "text-ink-subtle" : "text-danger"}>{d.deliveredAt ? "delivered" : d.nextAttemptAt ? "waiting" : "failed"}</Tag>
              <span className="min-w-0 flex-1">
                <span className="block text-ink">
                  {d.topic} <span className="text-ink-subtle">attempt {d.attempt}</span>
                </span>
                <span className="block truncate text-xs text-ink-muted">{d.deliveredAt ? `answered ${d.status}` : d.error || (d.nextAttemptAt ? `next try ${format.relative(d.nextAttemptAt)}` : "")}</span>
              </span>
              <span className="text-xs text-ink-subtle">{format.relative(d.createdAt)}</span>
              {!d.nextAttemptAt && (
                <Button variant="secondary" size="sm" loading={redeliver.isPending} onClick={() => redeliver.mutate({ endpointId: hook.id, deliveryId: d.id })} data-action="redeliver">
                  Again
                </Button>
              )}
            </li>
          ))}
        </ul>
      )}
    </Drawer>
  );
}
