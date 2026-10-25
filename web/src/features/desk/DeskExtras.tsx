import { useState, type FormEvent } from "react";
import {
  useArticles,
  useBusinessCalendar,
  useCannedResponses,
  useCreateArticle,
  useCreateCanned,
  useDeleteArticle,
  useDeleteCanned,
  useDeskMailAddress,
  usePolicies,
  useRequestTypes,
  useSaveBusinessCalendar,
  useUpdateArticle,
  useUpdateCanned,
  useUpdatePolicy,
  type Article,
  type BusinessCalendar,
  type CannedResponse,
} from "@/api/desk";
import { Button, Card, Checkbox, ErrorBanner, Field, IconButton, Input, SectionTitle, Select, Switch, Table, Tag, Td, Th, useToast } from "@/components/ui";
import { Icon } from "@/components/icons";
import { useConfirm } from "@/features/shell/ConfirmProvider";
import { ApiError } from "@/api/client";
import { useProject, useUpdateProject } from "@/api/projects";
import { useHolidayCalendars } from "@/api/availability";
import { t } from "@/i18n";

// The desk's extras, as sections of its setup page.

/** The week's days, keyed as the server keys them; their words are read in the reader's language. */
export const WEEKDAYS: Array<{ key: string; label: string; short: string }> = ["mon", "tue", "wed", "thu", "fri", "sat", "sun"].map((key) => ({
  key,
  get label() {
    return t.desk.hours.weekdays[key]!.label;
  },
  get short() {
    return t.desk.hours.weekdays[key]!.short;
  },
}));

/** Nine to five, Monday to Friday, in the browser's zone: what a new desk starts from. */
export function defaultCalendar(timezone: string): BusinessCalendar {
  const hours: BusinessCalendar["hours"] = {};
  for (const d of WEEKDAYS.slice(0, 5)) hours[d.key] = [{ from: "09:00", to: "17:00" }];
  return { timezone, hours, holidays: [], holidayCalendarId: null, holidayCalendarName: null };
}

/** The hours in a sentence: Mon to Fri 09:00 to 17:00 (Europe/Berlin). */
export function describeCalendar(c: BusinessCalendar): string {
  const open = WEEKDAYS.filter((d) => (c.hours[d.key] ?? []).length > 0);
  if (open.length === 0) return t.desk.hours.neverOpen;
  const first = open[0]!;
  const same = open.every((d) => JSON.stringify(c.hours[d.key]) === JSON.stringify(c.hours[first.key]));
  const span = (c.hours[first.key] ?? []).map((s) => t.desk.hours.span(s.from, s.to)).join(", ");
  const days = same && open.length > 1 ? t.desk.hours.dayRange(first.short, open[open.length - 1]!.short) : open.map((d) => d.short).join(", ");
  return `${days} ${same ? span : t.desk.hours.varied} (${c.timezone})`;
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
      update.mutate(
        { id: editing.id, title: title.trim(), body: body.trim() },
        {
          onSuccess: () => {
            setEditing(null);
            setTitle("");
            setBody("");
          },
        },
      );
    } else {
      create.mutate(
        { title: title.trim(), body: body.trim() },
        {
          onSuccess: () => {
            setTitle("");
            setBody("");
          },
        },
      );
    }
  }

  return (
    <section data-guide="knowledge-base">
      <SectionTitle className="mb-2">{t.desk.knowledge.title}</SectionTitle>
      <p className="mb-3 text-sm text-ink-muted">{t.desk.knowledge.intro}</p>
      <Card className="mb-4 p-4">
        <form onSubmit={submit} className="space-y-3" noValidate data-testid="new-article">
          {(create.error ?? update.error) && <ErrorBanner>{((create.error ?? update.error) as Error).message}</ErrorBanner>}
          <Field
            label={t.desk.knowledge.articleTitle}
            id="field-article-title"
            value={title}
            onChange={(e) => setTitle(e.target.value)}
            placeholder={t.desk.knowledge.titlePlaceholder}
          />
          <Field
            label={t.desk.knowledge.articleBody}
            id="field-article-body"
            rows={4}
            value={body}
            onChange={(e) => setBody(e.target.value)}
            placeholder={t.desk.knowledge.bodyPlaceholder}
          />
          <div className="flex gap-2">
            <Button type="submit" loading={create.isPending || update.isPending} disabled={!title.trim()}>
              {editing ? t.desk.knowledge.save : t.desk.knowledge.add}
            </Button>
            {editing && (
              <Button
                variant="ghost"
                onClick={() => {
                  setEditing(null);
                  setTitle("");
                  setBody("");
                }}
              >
                {t.kit.cancel}
              </Button>
            )}
          </div>
        </form>
      </Card>
      {articles.length > 0 && (
        <Table>
          <thead>
            <tr>
              <Th>{t.desk.knowledge.article}</Th>
              <Th>{t.desk.knowledge.state}</Th>
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
                  <Tag>{a.published ? t.desk.knowledge.published : t.desk.knowledge.draft}</Tag>
                </Td>
                <Td>
                  <span className="flex justify-end gap-1">
                    <Button variant="secondary" size="sm" onClick={() => update.mutate({ id: a.id, published: !a.published })} data-action="article-publish">
                      {a.published ? t.desk.knowledge.unpublish : t.desk.knowledge.publish}
                    </Button>
                    <IconButton
                      icon={<Icon.Edit />}
                      label={t.desk.knowledge.edit(a.title)}
                      size="sm"
                      onClick={() => {
                        setEditing(a);
                        setTitle(a.title);
                        setBody(a.body);
                      }}
                    />
                    <IconButton
                      icon={<Icon.Trash />}
                      label={t.desk.knowledge.delete(a.title)}
                      size="sm"
                      onClick={async () =>
                        (await confirm({
                          noun: t.kit.confirm.things.article,
                          verb: t.kit.confirm.verbs.delete,
                          body: t.desk.knowledge.deleteBody(a.title),
                        })) && remove.mutate(a.id)
                      }
                    />
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
      update.mutate(
        { id: editing.id, name: name.trim(), body: body.trim() },
        {
          onSuccess: () => {
            setEditing(null);
            setName("");
            setBody("");
          },
        },
      );
    } else {
      create.mutate(
        { name: name.trim(), body: body.trim() },
        {
          onSuccess: () => {
            setName("");
            setBody("");
          },
        },
      );
    }
  }

  return (
    <section data-guide="canned-responses">
      <SectionTitle className="mb-2">{t.desk.canned.title}</SectionTitle>
      <p className="mb-3 text-sm text-ink-muted">
        {t.desk.canned.introStart} <span className="font-mono text-xs">{"{{customer.name}}"}</span>,{" "}
        <span className="font-mono text-xs">{"{{issue.key}}"}</span>, <span className="font-mono text-xs">{"{{issue.summary}}"}</span> {t.desk.canned.and}{" "}
        <span className="font-mono text-xs">{"{{agent.name}}"}</span> {t.desk.canned.introEnd}
      </p>
      <Card className="mb-4 p-4">
        <form onSubmit={submit} className="space-y-3" noValidate data-testid="new-canned">
          {(create.error ?? update.error) && <ErrorBanner>{((create.error ?? update.error) as Error).message}</ErrorBanner>}
          <Field
            label={t.desk.canned.name}
            id="field-canned-name"
            value={name}
            onChange={(e) => setName(e.target.value)}
            placeholder={t.desk.canned.namePlaceholder}
          />
          <Field
            label={t.desk.canned.body}
            id="field-canned-body"
            rows={3}
            value={body}
            onChange={(e) => setBody(e.target.value)}
            placeholder={t.desk.canned.bodyPlaceholder}
          />
          <div className="flex gap-2">
            <Button type="submit" loading={create.isPending || update.isPending} disabled={!name.trim() || !body.trim()}>
              {editing ? t.desk.canned.save : t.desk.canned.add}
            </Button>
            {editing && (
              <Button
                variant="ghost"
                onClick={() => {
                  setEditing(null);
                  setName("");
                  setBody("");
                }}
              >
                {t.kit.cancel}
              </Button>
            )}
          </div>
        </form>
      </Card>
      {responses.length > 0 && (
        <Table>
          <thead>
            <tr>
              <Th>{t.desk.canned.nameHead}</Th>
              <Th>{t.desk.canned.words}</Th>
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
                    <IconButton
                      icon={<Icon.Edit />}
                      label={t.desk.canned.edit(c.name)}
                      size="sm"
                      onClick={() => {
                        setEditing(c);
                        setName(c.name);
                        setBody(c.body);
                      }}
                    />
                    <IconButton
                      icon={<Icon.Trash />}
                      label={t.desk.canned.delete(c.name)}
                      size="sm"
                      onClick={async () =>
                        (await confirm({
                          noun: t.kit.confirm.things.cannedResponse,
                          verb: t.kit.confirm.verbs.delete,
                          body: t.desk.canned.deleteBody(c.name),
                        })) && remove.mutate(c.id)
                      }
                    />
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
  const { data: holidayCalendars } = useHolidayCalendars();
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
      <SectionTitle className="mb-2">{t.desk.hours.title}</SectionTitle>
      <p className="mb-3 text-sm text-ink-muted">{t.desk.hours.intro}</p>
      <Card className="p-4">
        <form
          onSubmit={(e) => {
            e.preventDefault();
            save.mutate(calendar, {
              onSuccess: () => {
                setDraft(null);
                toast.success(t.desk.hours.saved);
              },
            });
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
                      <Input
                        type="time"
                        aria-label={t.desk.hours.opens(d.label)}
                        controlSize="sm"
                        value={spans[0]!.from}
                        onChange={(e) => setSpan(d.key, "from", e.target.value)}
                        className="w-28"
                      />
                      <span className="text-sm text-ink-subtle">{t.desk.hours.to}</span>
                      <Input
                        type="time"
                        aria-label={t.desk.hours.closes(d.label)}
                        controlSize="sm"
                        value={spans[0]!.to}
                        onChange={(e) => setSpan(d.key, "to", e.target.value)}
                        className="w-28"
                      />
                    </>
                  )}
                </div>
              );
            })}
          </div>
          <Field
            label={t.desk.hours.timezone}
            id="field-calendar-timezone"
            value={calendar.timezone}
            onChange={(e) => setDraft({ ...calendar, timezone: e.target.value })}
            hint={t.desk.hours.timezoneHint}
            className="max-w-xs"
          />
          <Select
            label={t.desk.hours.holidaysFrom}
            id="field-calendar-holiday-calendar"
            value={calendar.holidayCalendarId ?? ""}
            onChange={(e) => {
              const chosen = holidayCalendars?.calendars.find((c) => c.id === e.target.value);
              setDraft({ ...calendar, holidayCalendarId: chosen?.id ?? null, holidayCalendarName: chosen?.name ?? null });
            }}
            hint={t.desk.hours.holidaysHint}
            className="max-w-xs"
          >
            <option value="">{t.desk.hours.none}</option>
            {(holidayCalendars?.calendars ?? []).map((c) => (
              <option key={c.id} value={c.id}>
                {c.name}
              </option>
            ))}
          </Select>
          <Field
            label={t.desk.hours.extraDays}
            id="field-calendar-holidays"
            value={calendar.holidays.join(", ")}
            onChange={(e) =>
              setDraft({
                ...calendar,
                holidays: e.target.value
                  .split(",")
                  .map((h) => h.trim())
                  .filter(Boolean),
              })
            }
            placeholder="2026-12-25, 2026-12-26"
            hint={t.desk.hours.extraDaysHint}
          />
          <Button type="submit" loading={save.isPending} data-action="save-hours">
            {saved ? t.desk.hours.save : t.desk.hours.set}
          </Button>
        </form>
      </Card>
      {saved && (
        <div className="mt-4 space-y-2" data-goal-calendars>
          <p className="text-sm text-ink-muted">{t.desk.hours.whichGoals(describeCalendar(calendar))}</p>
          {(policyData?.policies ?? []).map((p) => (
            <Checkbox
              key={p.id}
              label={t.desk.hours.countsOnly(p.name)}
              checked={p.useCalendar}
              onChange={(e) => updatePolicy.mutate({ id: p.id, useCalendar: e.target.checked })}
              data-policy-calendar={p.metric}
            />
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

/** Where people without an account come in: the desk's key, and no organization. */
export function doorAddress(origin: string, projectKey: string): string {
  return `${origin}/desk?${new URLSearchParams({ desk: projectKey })}`;
}

// The desk's public address, whether the door asks for a code, the domains it
// trusts and what a new mail becomes. Off, an address is taken on trust, so
// whoever gives one is a customer of this desk alone; the list narrows that.
export function Door({ projectKey, editable }: { projectKey: string; editable: boolean }) {
  const { data } = useProject(projectKey);
  const { data: typeData } = useRequestTypes(projectKey);
  const { data: mailData } = useDeskMailAddress(projectKey);
  const update = useUpdateProject();
  const toast = useToast();
  const [draft, setDraft] = useState("");
  const project = data?.project;
  if (!project) return null;
  const address = doorAddress(window.location.origin, project.key);
  const verifies = project.portalVerifies;
  const domains = project.trustedDomains ?? [];
  const domainError = update.error instanceof ApiError ? update.error.fields?.trustedDomains : undefined;
  const requestTypes = typeData?.requestTypes ?? [];
  const mailAddress = mailData?.address ?? "";

  function trust(event: FormEvent) {
    event.preventDefault();
    const domain = cleanDomain(draft);
    if (!domain) return;
    update.mutate(
      { key: project!.key, trustedDomains: [...domains, domain] },
      {
        onSuccess: () => {
          setDraft("");
          toast.success(t.desk.door.trusting(domain));
        },
      },
    );
  }

  return (
    <section data-guide="desk-door">
      <SectionTitle className="mb-2">{t.desk.door.title}</SectionTitle>
      <p className="mb-3 text-sm text-ink-muted">{t.desk.door.intro}</p>
      <Card className="space-y-4 p-4">
        <div className="flex items-center gap-2">
          <code className="min-w-0 flex-1 truncate rounded-control bg-surface-raised px-2 py-1 font-mono text-xs text-ink" data-desk-address>
            {address}
          </code>
          <IconButton
            icon={<Icon.Copy />}
            label={t.desk.door.copy}
            size="sm"
            data-action="copy-desk-address"
            onClick={() => navigator.clipboard.writeText(address).then(() => toast.success(t.desk.door.copied))}
          />
        </div>
        <div className="flex items-start gap-3">
          <Switch
            checked={verifies}
            disabled={!editable || update.isPending}
            label={t.desk.door.askCode}
            data-action="toggle-door"
            data-door-verifies={String(verifies)}
            onChange={(checked) =>
              update.mutate(
                { key: project.key, portalVerifies: checked },
                { onSuccess: () => toast.success(checked ? t.desk.door.asksAgain : t.desk.door.isOpen) },
              )
            }
          />
          <div className="text-sm">
            <p className="text-ink">{t.desk.door.askCode}</p>
            <p className="text-ink-muted">{t.desk.door.askCodeDetail}</p>
          </div>
        </div>
        <div className="space-y-2 border-t border-border pt-4" data-trusted-domains>
          <p className="text-sm text-ink">{t.desk.door.trustedDomains}</p>
          <p className="text-sm text-ink-muted">{t.desk.door.trustedHint}</p>
          {domains.length > 0 && (
            <ul className="flex flex-wrap gap-2">
              {domains.map((domain) => (
                <li key={domain}>
                  <Tag className="gap-1 py-0.5 text-xs" data-trusted-domain={domain}>
                    {domain}
                    {editable && (
                      <IconButton
                        icon={<Icon.X />}
                        label={t.desk.door.stopTrusting(domain)}
                        size="xs"
                        disabled={update.isPending}
                        onClick={() =>
                          update.mutate(
                            { key: project.key, trustedDomains: domains.filter((d) => d !== domain) },
                            { onSuccess: () => toast.success(t.desk.door.noLongerTrusting(domain)) },
                          )
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
                <Field
                  label={t.desk.door.trustedDomain}
                  id="field-trusted-domain"
                  value={draft}
                  onChange={(e) => setDraft(e.target.value)}
                  placeholder="acme.test"
                  error={domainError}
                  controlSize="sm"
                />
              </div>
              <Button type="submit" size="sm" variant="secondary" data-action="add-domain" disabled={!cleanDomain(draft) || update.isPending}>
                {t.desk.door.add}
              </Button>
            </form>
          )}
        </div>
        <div className="space-y-2 border-t border-border pt-4" data-mail-requests>
          <p className="text-sm text-ink">{t.desk.door.mailRequests}</p>
          {mailAddress ? (
            <p className="text-sm text-ink-muted">
              {t.desk.door.mailTo}{" "}
              <code className="rounded-control bg-surface-raised px-1 font-mono text-xs text-ink" data-desk-mail-address>
                {mailAddress}
              </code>{" "}
              {t.desk.door.mailBecomes}
            </p>
          ) : (
            <p className="text-sm text-ink-muted">{t.desk.door.noMailbox}</p>
          )}
          <div className="max-w-xs">
            <Select
              label={t.desk.door.mailType}
              id="field-mail-request-type"
              controlSize="sm"
              value={project.mailRequestTypeId ?? ""}
              disabled={!editable || update.isPending}
              data-action="mail-request-type"
              onChange={(e) => {
                const chosen = e.target.value;
                update.mutate(
                  { key: project.key, mailRequestTypeId: chosen || null },
                  { onSuccess: () => toast.success(chosen ? t.desk.door.mailRaises : t.desk.door.mailRaisesNone) },
                );
              }}
            >
              <option value="">{t.desk.door.none}</option>
              {requestTypes.map((type) => (
                <option key={type.id} value={type.id}>
                  {type.name}
                </option>
              ))}
            </Select>
          </div>
        </div>
        {update.error && !domainError && <ErrorBanner>{(update.error as Error).message}</ErrorBanner>}
      </Card>
    </section>
  );
}
