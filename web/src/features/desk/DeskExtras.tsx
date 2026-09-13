import { useState, type FormEvent } from "react";
import {
  useArticles,
  useBusinessCalendar,
  useCannedResponses,
  useCreateArticle,
  useCreateCanned,
  useDeleteArticle,
  useDeleteCanned,
  usePolicies,
  useSaveBusinessCalendar,
  useUpdateArticle,
  useUpdateCanned,
  useUpdatePolicy,
  type Article,
  type BusinessCalendar,
  type CannedResponse,
} from "@/api/desk";
import { Button, Card, Checkbox, ErrorBanner, Field, IconButton, Input, SectionTitle, Switch, Table, Tag, Td, Th, useToast } from "@/components/ui";
import { Icon } from "@/components/icons";
import { useConfirm } from "@/features/shell/ConfirmProvider";
import { useMe } from "@/api/auth";
import { ApiError } from "@/api/client";
import { useProject, useUpdateProject } from "@/api/projects";

// The desk's extras, as sections of its setup page.

export const WEEKDAYS: Array<{ key: string; label: string }> = [
  { key: "mon", label: "Monday" },
  { key: "tue", label: "Tuesday" },
  { key: "wed", label: "Wednesday" },
  { key: "thu", label: "Thursday" },
  { key: "fri", label: "Friday" },
  { key: "sat", label: "Saturday" },
  { key: "sun", label: "Sunday" },
];

/** Nine to five, Monday to Friday, in the browser's zone: what a new desk starts from. */
export function defaultCalendar(timezone: string): BusinessCalendar {
  const hours: BusinessCalendar["hours"] = {};
  for (const d of WEEKDAYS.slice(0, 5)) hours[d.key] = [{ from: "09:00", to: "17:00" }];
  return { timezone, hours, holidays: [] };
}

/** The hours in a sentence: Mon to Fri 09:00 to 17:00 (Europe/Berlin). */
export function describeCalendar(c: BusinessCalendar): string {
  const open = WEEKDAYS.filter((d) => (c.hours[d.key] ?? []).length > 0);
  if (open.length === 0) return "never open";
  const first = open[0]!;
  const same = open.every((d) => JSON.stringify(c.hours[d.key]) === JSON.stringify(c.hours[first.key]));
  const span = (c.hours[first.key] ?? []).map((s) => `${s.from} to ${s.to}`).join(", ");
  const days = same && open.length > 1 ? `${first.label.slice(0, 3)} to ${open[open.length - 1]!.label.slice(0, 3)}` : open.map((d) => d.label.slice(0, 3)).join(", ");
  return `${days} ${same ? span : "varied"} (${c.timezone})`;
}

export function KnowledgeBase({ projectKey }: { projectKey: string }) {
  const { data } = useArticles(projectKey);
  const create = useCreateArticle(projectKey);
  const update = useUpdateArticle();
  const remove = useDeleteArticle();
  const confirm = useConfirm();
  const [title, setTitle] = useState("");
  const [body, setBody] = useState("");
  const [editing, setEditing] = useState<Article | null>(null);
  const articles = data?.articles ?? [];

  function submit(event: FormEvent) {
    event.preventDefault();
    if (!title.trim()) return;
    if (editing) {
      update.mutate({ id: editing.id, title: title.trim(), body: body.trim() }, { onSuccess: () => { setEditing(null); setTitle(""); setBody(""); } });
    } else {
      create.mutate({ title: title.trim(), body: body.trim() }, { onSuccess: () => { setTitle(""); setBody(""); } });
    }
  }

  return (
    <section data-guide="knowledge-base">
      <SectionTitle className="mb-2">Knowledge base</SectionTitle>
      <p className="mb-3 text-sm text-ink-muted">Articles a customer is offered before raising a request. A draft is yours until you publish it.</p>
      <Card className="mb-4 p-4">
        <form onSubmit={submit} className="space-y-3" noValidate data-testid="new-article">
          {(create.error ?? update.error) && <ErrorBanner>{((create.error ?? update.error) as Error).message}</ErrorBanner>}
          <Field label="Article title" id="field-article-title" value={title} onChange={(e) => setTitle(e.target.value)} placeholder="Resetting your password" />
          <Field label="Article body" id="field-article-body" rows={4} value={body} onChange={(e) => setBody(e.target.value)} placeholder="Plain words, in the order a reader does them." />
          <div className="flex gap-2">
            <Button type="submit" loading={create.isPending || update.isPending} disabled={!title.trim()}>
              {editing ? "Save article" : "Add article"}
            </Button>
            {editing && (
              <Button variant="ghost" onClick={() => { setEditing(null); setTitle(""); setBody(""); }}>
                Cancel
              </Button>
            )}
          </div>
        </form>
      </Card>
      {articles.length > 0 && (
        <Table>
          <thead>
            <tr>
              <Th>Article</Th>
              <Th>State</Th>
              <Th className="w-40" />
            </tr>
          </thead>
          <tbody>
            {articles.map((a) => (
              <tr key={a.id} data-article={a.title} data-article-published={a.published ? "true" : "false"}>
                <Td>
                  <span className="block font-medium text-ink">{a.title}</span>
                  <span className="block truncate text-sm text-ink-muted">{a.body.slice(0, 120)}</span>
                </Td>
                <Td>
                  <Tag>{a.published ? "Published" : "Draft"}</Tag>
                </Td>
                <Td>
                  <span className="flex justify-end gap-1">
                    <Button variant="secondary" size="sm" onClick={() => update.mutate({ id: a.id, published: !a.published })} data-action="article-publish">
                      {a.published ? "Unpublish" : "Publish"}
                    </Button>
                    <IconButton icon={<Icon.Edit />} label={`Edit ${a.title}`} size="sm" onClick={() => { setEditing(a); setTitle(a.title); setBody(a.body); }} />
                    <IconButton icon={<Icon.Trash />} label={`Delete ${a.title}`} size="sm" onClick={async () => (await confirm({ noun: "article", verb: "Delete", body: `${a.title} goes; customers stop finding it.` })) && remove.mutate(a.id)} />
                  </span>
                </Td>
              </tr>
            ))}
          </tbody>
        </Table>
      )}
    </section>
  );
}

export function CannedResponses({ projectKey }: { projectKey: string }) {
  const { data } = useCannedResponses(projectKey);
  const create = useCreateCanned(projectKey);
  const update = useUpdateCanned();
  const remove = useDeleteCanned();
  const confirm = useConfirm();
  const [name, setName] = useState("");
  const [body, setBody] = useState("");
  const [editing, setEditing] = useState<CannedResponse | null>(null);
  const responses = data?.responses ?? [];

  function submit(event: FormEvent) {
    event.preventDefault();
    if (!name.trim() || !body.trim()) return;
    if (editing) {
      update.mutate({ id: editing.id, name: name.trim(), body: body.trim() }, { onSuccess: () => { setEditing(null); setName(""); setBody(""); } });
    } else {
      create.mutate({ name: name.trim(), body: body.trim() }, { onSuccess: () => { setName(""); setBody(""); } });
    }
  }

  return (
    <section data-guide="canned-responses">
      <SectionTitle className="mb-2">Canned responses</SectionTitle>
      <p className="mb-3 text-sm text-ink-muted">
        Replies agents keep ready. <span className="font-mono text-xs">{"{{customer.name}}"}</span>, <span className="font-mono text-xs">{"{{issue.key}}"}</span>, <span className="font-mono text-xs">{"{{issue.summary}}"}</span> and <span className="font-mono text-xs">{"{{agent.name}}"}</span> are filled in when one is inserted.
      </p>
      <Card className="mb-4 p-4">
        <form onSubmit={submit} className="space-y-3" noValidate data-testid="new-canned">
          {(create.error ?? update.error) && <ErrorBanner>{((create.error ?? update.error) as Error).message}</ErrorBanner>}
          <Field label="Response name" id="field-canned-name" value={name} onChange={(e) => setName(e.target.value)} placeholder="Greeting" />
          <Field label="Response body" id="field-canned-body" rows={3} value={body} onChange={(e) => setBody(e.target.value)} placeholder="Hello {{customer.name}}, thanks for writing about {{issue.summary}}." />
          <div className="flex gap-2">
            <Button type="submit" loading={create.isPending || update.isPending} disabled={!name.trim() || !body.trim()}>
              {editing ? "Save response" : "Add response"}
            </Button>
            {editing && (
              <Button variant="ghost" onClick={() => { setEditing(null); setName(""); setBody(""); }}>
                Cancel
              </Button>
            )}
          </div>
        </form>
      </Card>
      {responses.length > 0 && (
        <Table>
          <thead>
            <tr>
              <Th>Name</Th>
              <Th>Words</Th>
              <Th className="w-24" />
            </tr>
          </thead>
          <tbody>
            {responses.map((c) => (
              <tr key={c.id} data-canned={c.name}>
                <Td className="font-medium text-ink">{c.name}</Td>
                <Td className="max-w-md truncate text-sm text-ink-muted">{c.body}</Td>
                <Td>
                  <span className="flex justify-end gap-1">
                    <IconButton icon={<Icon.Edit />} label={`Edit ${c.name}`} size="sm" onClick={() => { setEditing(c); setName(c.name); setBody(c.body); }} />
                    <IconButton icon={<Icon.Trash />} label={`Delete ${c.name}`} size="sm" onClick={async () => (await confirm({ noun: "canned response", verb: "Delete", body: `${c.name} goes.` })) && remove.mutate(c.id)} />
                  </span>
                </Td>
              </tr>
            ))}
          </tbody>
        </Table>
      )}
    </section>
  );
}

export function BusinessHours({ projectKey }: { projectKey: string }) {
  const { data, error } = useBusinessCalendar(projectKey);
  const save = useSaveBusinessCalendar(projectKey);
  const { data: policyData } = usePolicies(projectKey);
  const updatePolicy = useUpdatePolicy();
  const toast = useToast();
  const [draft, setDraft] = useState<BusinessCalendar | null>(null);
  const zone = Intl.DateTimeFormat().resolvedOptions().timeZone || "UTC";
  const calendar = draft ?? data?.calendar ?? (error ? defaultCalendar(zone) : null);
  const saved = Boolean(data?.calendar);
  if (!calendar) return null;

  const setDay = (key: string, open: boolean) => setDraft({ ...calendar, hours: { ...calendar.hours, [key]: open ? [{ from: "09:00", to: "17:00" }] : [] } });
  const setSpan = (key: string, which: "from" | "to", value: string) =>
    setDraft({ ...calendar, hours: { ...calendar.hours, [key]: [{ ...(calendar.hours[key]?.[0] ?? { from: "09:00", to: "17:00" }), [which]: value }] } });

  return (
    <section data-guide="business-hours">
      <SectionTitle className="mb-2">Business hours</SectionTitle>
      <p className="mb-3 text-sm text-ink-muted">When the desk is open. A goal that counts business hours stops at closing time and resumes in the morning.</p>
      <Card className="p-4">
        <form
          onSubmit={(e) => {
            e.preventDefault();
            save.mutate(calendar, { onSuccess: () => { setDraft(null); toast.success("Business hours saved"); } });
          }}
          className="space-y-4"
          noValidate
          data-business-hours
        >
          {save.error && <ErrorBanner>{(save.error as Error).message}</ErrorBanner>}
          <div className="grid gap-2 sm:grid-cols-2">
            {WEEKDAYS.map((d) => {
              const spans = calendar.hours[d.key] ?? [];
              const open = spans.length > 0;
              return (
                <div key={d.key} className="flex items-center gap-3" data-weekday={d.key} data-open={open ? "true" : "false"}>
                  <Checkbox label={d.label} checked={open} onChange={(e) => setDay(d.key, e.target.checked)} className="w-28" data-weekday-open={d.key} />
                  {open && (
                    <>
                      <Input type="time" aria-label={`${d.label} opens`} controlSize="sm" value={spans[0]!.from} onChange={(e) => setSpan(d.key, "from", e.target.value)} className="w-28" />
                      <span className="text-sm text-ink-subtle">to</span>
                      <Input type="time" aria-label={`${d.label} closes`} controlSize="sm" value={spans[0]!.to} onChange={(e) => setSpan(d.key, "to", e.target.value)} className="w-28" />
                    </>
                  )}
                </div>
              );
            })}
          </div>
          <Field label="Time zone" id="field-calendar-timezone" value={calendar.timezone} onChange={(e) => setDraft({ ...calendar, timezone: e.target.value })} hint="The zone the hours are read in, such as Europe/Berlin." className="max-w-xs" />
          <Field label="Days off" id="field-calendar-holidays" value={calendar.holidays.join(", ")} onChange={(e) => setDraft({ ...calendar, holidays: e.target.value.split(",").map((h) => h.trim()).filter(Boolean) })} placeholder="2026-12-25, 2026-12-26" hint="Days written as YYYY-MM-DD, separated by commas." />
          <Button type="submit" loading={save.isPending} data-action="save-hours">
            {saved ? "Save business hours" : "Set business hours"}
          </Button>
        </form>
      </Card>
      {saved && (
        <div className="mt-4 space-y-2" data-goal-calendars>
          <p className="text-sm text-ink-muted">Which goals count only these hours ({describeCalendar(calendar)}):</p>
          {(policyData?.policies ?? []).map((p) => (
            <Checkbox key={p.id} label={`${p.name} counts business hours only`} checked={p.useCalendar} onChange={(e) => updatePolicy.mutate({ id: p.id, useCalendar: e.target.checked })} data-policy-calendar={p.metric} />
          ))}
        </div>
      )}
    </section>
  );
}

// ---------------------------------------------------------------- door ------

/** A domain the way the server stores it: lower case, no leading @, no spaces. */
export function cleanDomain(raw: string): string {
  return raw.trim().replace(/^@/, "").toLowerCase();
}

// The desk's public address, whether the door asks for a code, and the
// domains it trusts. Off, an address is taken on trust, so whoever gives one
// is a customer of this desk alone; the list narrows that to their domains.
export function Door({ projectKey, editable }: { projectKey: string; editable: boolean }) {
  const { data } = useProject(projectKey);
  const { data: session } = useMe();
  const update = useUpdateProject();
  const toast = useToast();
  const [draft, setDraft] = useState("");
  const project = data?.project;
  const slug = session?.principal?.org?.slug;
  if (!project || !slug) return null;
  const address = `${window.location.origin}/desk/${slug}?desk=${project.key}`;
  const verifies = project.portalVerifies;
  const domains = project.trustedDomains ?? [];
  const domainError = update.error instanceof ApiError ? update.error.fields?.trustedDomains : undefined;

  function trust(event: FormEvent) {
    event.preventDefault();
    const domain = cleanDomain(draft);
    if (!domain) return;
    update.mutate(
      { key: project!.key, trustedDomains: [...domains, domain] },
      {
        onSuccess: () => {
          setDraft("");
          toast.success(`Trusting ${domain}`);
        },
      },
    );
  }

  return (
    <section data-guide="desk-door">
      <SectionTitle className="mb-2">The door</SectionTitle>
      <p className="mb-3 text-sm text-ink-muted">Where people without an account come in. Share the address; it names this desk.</p>
      <Card className="space-y-4 p-4">
        <div className="flex items-center gap-2">
          <code className="min-w-0 flex-1 truncate rounded-control bg-surface-raised px-2 py-1 font-mono text-xs text-ink" data-desk-address>
            {address}
          </code>
          <IconButton icon={<Icon.Copy />} label="Copy the address" size="sm" data-action="copy-desk-address" onClick={() => navigator.clipboard.writeText(address).then(() => toast.success("Address copied"))} />
        </div>
        <div className="flex items-start gap-3">
          <Switch
            checked={verifies}
            disabled={!editable || update.isPending}
            label="Ask for a code by mail before letting somebody in"
            data-action="toggle-door"
            data-door-verifies={String(verifies)}
            onChange={(checked) =>
              update.mutate({ key: project.key, portalVerifies: checked }, { onSuccess: () => toast.success(checked ? "The door asks for a code again" : "The door is open") })
            }
          />
          <div className="text-sm">
            <p className="text-ink">Ask for a code by mail before letting somebody in</p>
            <p className="text-ink-muted">Off, anyone who gives an address is that address for this desk, and sees this desk alone. Turning it back on signs out everyone who came in without a code.</p>
          </div>
        </div>
        <div className="space-y-2 border-t border-border pt-4" data-trusted-domains>
          <p className="text-sm text-ink">Trusted domains</p>
          <p className="text-sm text-ink-muted">Empty means any address. Applies to the door and to replies by mail.</p>
          {domains.length > 0 && (
            <ul className="flex flex-wrap gap-2">
              {domains.map((domain) => (
                <li key={domain}>
                  <Tag className="gap-1 py-0.5 text-xs" data-trusted-domain={domain}>
                    {domain}
                    {editable && (
                      <IconButton
                        icon={<Icon.X />}
                        label={`Stop trusting ${domain}`}
                        size="xs"
                        disabled={update.isPending}
                        onClick={() =>
                          update.mutate({ key: project.key, trustedDomains: domains.filter((d) => d !== domain) }, { onSuccess: () => toast.success(`No longer trusting ${domain}`) })
                        }
                      />
                    )}
                  </Tag>
                </li>
              ))}
            </ul>
          )}
          {editable && (
            <form onSubmit={trust} className="flex items-end gap-2" noValidate>
              <div className="flex-1">
                <Field label="Trusted domain" id="field-trusted-domain" value={draft} onChange={(e) => setDraft(e.target.value)} placeholder="acme.test" error={domainError} controlSize="sm" />
              </div>
              <Button type="submit" size="sm" variant="secondary" data-action="add-domain" disabled={!cleanDomain(draft) || update.isPending}>
                Add
              </Button>
            </form>
          )}
        </div>
        {update.error && !domainError && <ErrorBanner>{(update.error as Error).message}</ErrorBanner>}
      </Card>
    </section>
  );
}

