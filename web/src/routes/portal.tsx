import { DocView } from "@/features/editor/DocView";
import { useRef, useState, type ChangeEvent, type FormEvent } from "react";
import { Link, Navigate, createRoute } from "@tanstack/react-router";
import { appRoute } from "./app";
import { useMe } from "@/api/auth";
import { formatSize, useUploadAttachment, useUploadLimit } from "@/api/attachments";
import { AttachmentPanel } from "@/features/attachments/AttachmentPanel";
import { PortalSendRow, type Attaching, type NotAttached } from "@/features/desk/PortalSendRow";
import {
  useDesks,
  useFollow,
  useMyRequests,
  usePortalArticle,
  usePortalArticles,
  useRaiseRequest,
  useReply,
  useRequest,
  useRequestWatchers,
  useUnfollow,
  type PortalRequest,
  type RequestType,
} from "@/api/desk";
import {
  Button,
  Card,
  Checkbox,
  EmptyState,
  ErrorBanner,
  Field,
  Input,
  OptionCard,
  Page,
  PageHeader,
  SectionTitle,
  Select,
  Table,
  Td,
  Th,
  Textarea,
} from "@/components/ui";
import { Avatar, StatusBadge, RelativeTime } from "@/features/issues/badges";
import { categoryHeading, groupByCategory, prefillDetails } from "@/features/desk/templates";
import { sharedByCompany } from "@/features/desk/company";
import { t } from "@/i18n";

/**
 * The customer portal: raise a request, follow it, reply. A customer sees
 * their own requests and the public side of the conversation, nothing else.
 */
export const portalRoute = createRoute({
  getParentRoute: () => appRoute,
  path: "/portal",
  component: MyRequests,
});

export const portalNewRoute = createRoute({
  getParentRoute: () => appRoute,
  path: "/portal/new",
  component: RaiseRequest,
});

export const portalRequestRoute = createRoute({
  getParentRoute: () => appRoute,
  path: "/portal/requests/$issueKey",
  component: RequestPage,
});

/** One article a customer reads, from the search before raising a request. */
export const portalArticleRoute = createRoute({
  getParentRoute: () => appRoute,
  path: "/portal/articles/$articleId",
  component: ArticlePage,
});

// The knowledge base at the door: a customer types what is wrong and is
// offered what was written about it before they ask a person.
function ArticleSearch({ projectKey }: { projectKey: string | undefined }) {
  const [query, setQuery] = useState("");
  const { data } = usePortalArticles(projectKey, query);
  const articles = data?.articles ?? [];
  if (!projectKey || (articles.length === 0 && !query)) return null;
  return (
    <div className="space-y-2" data-article-search>
      <Field
        label={t.desk.portal.oneOfThese}
        id="field-article-search"
        type="search"
        value={query}
        onChange={(e) => setQuery(e.target.value)}
        placeholder={t.desk.portal.searchPlaceholder}
        hint={t.desk.portal.searchHint}
      />
      {articles.length > 0 ? (
        <ul className="grid gap-2 sm:grid-cols-2">
          {articles.map((a) => (
            <li key={a.id}>
              <Link
                to="/portal/articles/$articleId"
                params={{ articleId: a.id }}
                className="block rounded-control border border-border p-3 hover:bg-surface-raised"
                data-article={a.title}
              >
                <span className="block text-sm font-medium text-ink">{a.title}</span>
                <span className="block truncate text-xs text-ink-muted">{a.body.slice(0, 100)}</span>
              </Link>
            </li>
          ))}
        </ul>
      ) : (
        query && <p className="text-sm text-ink-subtle">{t.desk.portal.nothingWritten}</p>
      )}
    </div>
  );
}

function ArticlePage() {
  const { articleId } = portalArticleRoute.useParams();
  const { data, error } = usePortalArticle(articleId);
  const article = data?.article;
  return (
    <Page width="narrow">
      <PageHeader
        crumb={
          <Link to="/portal/new" className="hover:text-ink">
            {t.desk.portal.raise}
          </Link>
        }
        title={article?.title ?? t.desk.portal.article}
      />
      {error && <ErrorBanner>{(error as Error).message}</ErrorBanner>}
      {article && (
        <Card className="p-5" data-portal-article={article.title}>
          <p className="whitespace-pre-wrap text-sm text-ink">{article.body}</p>
          <p className="mt-4 text-sm text-ink-muted">
            {t.desk.portal.didNotHelp}{" "}
            <Link to="/portal/new" className="text-accent hover:underline">
              {t.desk.portal.raiseDot}
            </Link>
          </p>
        </Card>
      )}
    </Page>
  );
}

function MyRequests() {
  const { data, isLoading } = useMyRequests();
  const { data: session } = useMe();
  const requests = data?.requests ?? [];
  const shared = data?.shared ?? [];
  // Somebody who came in without a code sees one desk; the heading says which,
  // so a short list is not a surprise.
  const desk = session?.principal?.portalDesk;

  return (
    <Page width="content" className="space-y-8">
      <PageHeader
        title={desk ? t.desk.portal.yourRequestsAt(desk) : t.desk.portal.yourRequests}
        meta={data ? t.desk.portal.count(requests.length) : undefined}
        actions={
          <Link to="/portal/new" className="inline-flex h-8 items-center rounded-md bg-primary px-3 text-sm font-medium text-on-primary hover:bg-primary-hover">
            {t.desk.portal.raise}
          </Link>
        }
      />
      {isLoading ? null : requests.length === 0 ? (
        <EmptyState
          title={t.desk.portal.nothingRaised}
          description={t.desk.portal.nothingRaisedDetail}
          action={
            <Link to="/portal/new" className="text-sm font-medium text-accent hover:underline">
              {t.desk.portal.raise}
            </Link>
          }
        />
      ) : (
        <RequestTable requests={requests} />
      )}
      {sharedByCompany(shared).map((group) => (
        <section key={group.company} data-shared-by={group.company}>
          <SectionTitle className="mb-2">{t.desk.portal.sharedBy(group.company)}</SectionTitle>
          <p className="mb-3 text-sm text-ink-muted">{t.desk.portal.sharedByDetail(group.company)}</p>
          <RequestTable requests={group.requests} showReporter />
        </section>
      ))}
    </Page>
  );
}

function RequestTable({ requests, showReporter = false }: { requests: PortalRequest[]; showReporter?: boolean }) {
  return (
    <Table>
      <thead>
        <tr>
          <Th className="w-24">{t.desk.portal.reference}</Th>
          <Th>{t.desk.portal.request}</Th>
          {showReporter && <Th className="w-40">{t.desk.portal.raisedBy}</Th>}
          <Th className="w-40">{t.desk.portal.kind}</Th>
          <Th className="w-40">{t.desk.portal.status}</Th>
          <Th className="w-24 text-right">{t.desk.portal.updated}</Th>
        </tr>
      </thead>
      <tbody>
        {requests.map((r) => (
          <tr key={r.key} className="group hover:bg-surface-raised/60" data-request={r.key}>
            <Td className="font-mono text-sm text-ink-muted">{r.key}</Td>
            <Td className="max-w-0">
              <Link to="/portal/requests/$issueKey" params={{ issueKey: r.key }} className="block min-w-48 truncate text-ink group-hover:text-accent">
                {r.summary}
              </Link>
            </Td>
            {showReporter && <Td className="text-sm text-ink-muted">{r.reporter?.name ?? ""}</Td>}
            <Td className="text-sm text-ink-muted">{r.requestTypeName ?? ""}</Td>
            <Td>
              <StatusBadge name={r.status.name} category={r.status.category} />
            </Td>
            <Td className="text-right text-sm whitespace-nowrap text-ink-subtle">
              <RelativeTime iso={r.updatedAt} />
            </Td>
          </tr>
        ))}
      </tbody>
    </Table>
  );
}

function RaiseRequest() {
  const { data } = useDesks();
  const raise = useRaiseRequest();
  const desks = data?.desks ?? [];
  const [deskKey, setDeskKey] = useState("");
  const [typeId, setTypeId] = useState("");
  const [summary, setSummary] = useState("");
  const [description, setDescription] = useState("");
  const [raisedKey, setRaisedKey] = useState("");
  const [share, setShare] = useState(false);
  const [files, setFiles] = useState<File[]>([]);
  const [tooBig, setTooBig] = useState("");
  const picker = useRef<HTMLInputElement>(null);
  const attach = useUploadAttachment("portal");
  // The request exists after the first call, so a file that fails to follow
  // it is reported, never a reason to send the request twice.
  const [attaching, setAttaching] = useState<Attaching | null>(null);
  const [failed, setFailed] = useState<NotAttached | null>(null);

  const desk = desks.find((d) => d.projectKey === (deskKey || desks[0]?.projectKey));
  const types = desk?.requestTypes ?? [];
  const chosen = types.find((type) => type.id === typeId) ?? types[0];
  const groups = groupByCategory(types);

  // The details start from the template of what was chosen, and a change of
  // mind swaps templates only while the box still holds one untouched.
  const [templateShown, setTemplateShown] = useState("");
  const template = chosen?.detailsTemplate ?? "";
  const limit = useUploadLimit();
  if (template !== templateShown) {
    setDescription(prefillDetails(description, templateShown, template));
    setTemplateShown(template);
  }

  function onPick(event: ChangeEvent<HTMLInputElement>) {
    const picked = Array.from(event.target.files ?? []);
    event.target.value = "";
    const large = picked.find((f) => f.size > limit);
    setTooBig(large ? t.desk.portal.tooBig(large.name, formatSize(large.size), formatSize(limit)) : "");
    setFiles((current) => [...current, ...picked.filter((f) => f.size <= limit && !current.some((c) => c.name === f.name))]);
  }

  async function onSubmit(event: FormEvent) {
    event.preventDefault();
    if (!chosen || !summary.trim()) return;
    let key: string;
    try {
      const made = await raise.mutateAsync({
        requestTypeId: chosen.id,
        summary: summary.trim(),
        description: description.trim() || undefined,
        shareWithCompany: Boolean(desk?.company) && share,
      });
      key = made.request.key;
    } catch {
      return;
    }
    const names: string[] = [];
    for (const [i, file] of files.entries()) {
      setAttaching({ done: i, total: files.length });
      try {
        await attach.mutateAsync({ issueKey: key, file });
      } catch {
        names.push(file.name);
      }
    }
    setAttaching(null);
    if (names.length === 0) setRaisedKey(key);
    else setFailed({ key, names });
  }

  if (raisedKey) return <Navigate to="/portal/requests/$issueKey" params={{ issueKey: raisedKey }} />;

  return (
    <Page width="content">
      <PageHeader
        crumb={
          <Link to="/portal" className="hover:text-ink">
            {t.desk.portal.yourRequests}
          </Link>
        }
        title={t.desk.portal.raise}
      />
      {data && desks.length === 0 ? (
        <EmptyState title={t.desk.portal.noDesk} description={t.desk.portal.noDeskDetail} />
      ) : (
        <Card className="p-5">
          <form onSubmit={onSubmit} className="space-y-4" noValidate>
            {desks.length > 1 && (
              <Select
                label={t.desk.portal.desk}
                id="portal-desk"
                value={desk?.projectKey ?? ""}
                onChange={(e) => {
                  setDeskKey(e.target.value);
                  setTypeId("");
                }}
              >
                {desks.map((d) => (
                  <option key={d.projectKey} value={d.projectKey}>
                    {d.name}
                  </option>
                ))}
              </Select>
            )}

            <ArticleSearch projectKey={desk?.projectKey} />

            {desk?.mailAddress && (
              <p className="text-sm text-ink-muted" data-portal-mail-address={desk.mailAddress}>
                {t.desk.portal.mailAt}{" "}
                <a href={`mailto:${desk.mailAddress}`} className="text-accent hover:underline">
                  {desk.mailAddress}
                </a>
                {t.desk.portal.mailBecomes}
              </p>
            )}

            <fieldset className="space-y-2">
              <legend className="block text-sm font-medium text-ink-muted">{t.desk.portal.whatAbout}</legend>
              <div role="radiogroup" aria-label={t.desk.portal.requestType} className="space-y-3">
                {groups.map((group) => (
                  <div key={group.name} data-request-category={group.name}>
                    {groups.length > 1 && <p className="mb-1.5 text-xs font-medium tracking-wide text-ink-subtle uppercase">{categoryHeading(group.name)}</p>}
                    <div className="grid gap-2 sm:grid-cols-3">
                      {group.types.map((type: RequestType) => (
                        <OptionCard
                          key={type.id}
                          checked={type.id === chosen?.id}
                          data-request-type={type.name}
                          onSelect={() => setTypeId(type.id)}
                          title={type.name}
                          description={type.description || undefined}
                        />
                      ))}
                    </div>
                  </div>
                ))}
              </div>
            </fieldset>

            <Field
              label={t.desk.portal.summary}
              required
              autoFocus
              value={summary}
              onChange={(e) => setSummary(e.target.value)}
              placeholder={t.desk.portal.summaryPlaceholder}
            />
            <Field
              label={t.desk.portal.details}
              id="portal-description"
              rows={4}
              value={description}
              onChange={(e) => setDescription(e.target.value)}
              placeholder={t.desk.portal.detailsPlaceholder}
            />

            {desk?.company && (
              <div>
                <Checkbox
                  label={t.desk.portal.shareWith(desk.company.name)}
                  checked={share}
                  onChange={(e) => setShare(e.target.checked)}
                  data-share-with-company={desk.company.name}
                />
                <p className="mt-1 text-xs text-ink-subtle">{t.desk.portal.shareDetail}</p>
              </div>
            )}

            <div className="space-y-2">
              <div className="flex items-center gap-3">
                <input ref={picker} type="file" multiple className="sr-only" aria-label={t.desk.portal.attachFiles} onChange={onPick} />
                <Button type="button" size="sm" variant="secondary" onClick={() => picker.current?.click()}>
                  {t.desk.portal.attachFiles}
                </Button>
                <span className="text-xs text-ink-subtle">{t.desk.portal.attachHint(formatSize(limit))}</span>
              </div>
              {tooBig && <ErrorBanner>{tooBig}</ErrorBanner>}
              {files.length > 0 && (
                <ul className="divide-y divide-border rounded-md border border-border" data-attach-queued>
                  {files.map((f) => (
                    <li key={f.name} className="flex items-center gap-3 px-3 py-1.5 text-sm" data-attach-queued-file={f.name}>
                      <span className="min-w-0 flex-1 truncate text-ink">{f.name}</span>
                      <span className="text-ink-subtle tabular-nums">{formatSize(f.size)}</span>
                      <Button
                        type="button"
                        size="sm"
                        variant="ghost"
                        aria-label={t.desk.portal.doNotAttach(f.name)}
                        onClick={() => setFiles((current) => current.filter((c) => c !== f))}
                      >
                        {t.desk.portal.remove}
                      </Button>
                    </li>
                  ))}
                </ul>
              )}
            </div>

            <PortalSendRow
              refusal={(raise.error as Error | null)?.message}
              failed={failed}
              sending={raise.isPending || attaching !== null}
              attaching={attaching}
              ready={Boolean(chosen && summary.trim())}
            />
          </form>
        </Card>
      )}
    </Page>
  );
}

function RequestPage() {
  const { issueKey } = portalRequestRoute.useParams();
  const { data, isLoading, error } = useRequest(issueKey);
  const { data: deskData } = useDesks();
  const repliesByMail = Boolean(deskData?.desks.some((d) => d.repliesByMail));
  const reply = useReply();
  const [text, setText] = useState("");

  if (isLoading) return null;
  if (error) return <ErrorBanner>{(error as Error).message}</ErrorBanner>;
  if (!data) return null;

  const { request: issue, comments } = data;
  const waiting = issue.status.name === "Waiting for customer";

  function onSubmit(event: FormEvent) {
    event.preventDefault();
    if (!text.trim()) return;
    reply.mutate({ key: issueKey, text: text.trim() }, { onSuccess: () => setText("") });
  }

  return (
    <Page width="content">
      <PageHeader
        crumb={
          <Link to="/portal" className="hover:text-ink">
            {t.desk.portal.yourRequests}
          </Link>
        }
        title={issue.summary}
        meta={
          <span className="flex items-center gap-2">
            <span className="font-mono">{issue.key}</span>
            {issue.requestTypeName && <span>· {issue.requestTypeName}</span>}
            <span>·</span>
            <StatusBadge name={issue.status.name} category={issue.status.category} />
          </span>
        }
      />

      <RaisedBy issue={issue} />

      <p className="mb-4 flex items-center gap-2 text-sm text-ink-muted" data-request-assignee={issue.assignee?.name ?? ""}>
        {issue.assignee ? (
          <>
            <Avatar name={issue.assignee.name} src={issue.assignee.avatarUrl} size="sm" />
            <span>
              {t.desk.portal.handledBy} <span className="font-medium text-ink">{issue.assignee.name}</span>
            </span>
          </>
        ) : (
          <span>{t.desk.portal.notPickedUp}</span>
        )}
        {issue.team && <span data-request-team={issue.team.name}>{t.desk.portal.withTeam(issue.team.name)}</span>}
      </p>

      {waiting && (
        <div className="mb-4 rounded-md border border-warning/40 bg-warning/10 px-3 py-2 text-sm text-ink" data-testid="waiting-on-you">
          {t.desk.portal.waitingOnYou}
        </div>
      )}

      {issue.description && <DocView doc={issue.description} size="sm" className="mb-6" />}

      {!data.throughCompany && <Followers issueKey={issueKey} reporterId={issue.reporter?.id} />}

      <section>
        <SectionTitle className="mb-2">{t.desk.portal.conversation}</SectionTitle>
        {comments.length === 0 ? (
          <p className="mb-4 text-sm text-ink-subtle">{t.desk.portal.noReplies}</p>
        ) : (
          <ul className="mb-4 space-y-3">
            {comments.map((c) => (
              <li key={c.id} className="flex gap-3" data-reply={c.id}>
                <Avatar name={c.author?.name} src={c.author?.avatarUrl} />
                <div className="min-w-0 flex-1">
                  <p className="text-xs text-ink-muted">
                    <span className="font-medium text-ink">{c.author?.name ?? t.desk.portal.theDesk}</span> · <RelativeTime iso={c.createdAt} />
                  </p>
                  <DocView doc={c.body} size="sm" className="mt-1" />
                </div>
              </li>
            ))}
          </ul>
        )}

        <div className="mb-4">
          <AttachmentPanel issueKey={issueKey} editable source="portal" />
        </div>

        <form onSubmit={onSubmit} className="space-y-2">
          {reply.error && <ErrorBanner>{(reply.error as Error).message}</ErrorBanner>}
          <Textarea
            id="portal-reply"
            aria-label={t.desk.portal.reply}
            value={text}
            onChange={(e) => setText(e.target.value)}
            rows={3}
            placeholder={t.desk.portal.replyPlaceholder}
          />
          <div className="flex items-center gap-3">
            <Button type="submit" size="sm" loading={reply.isPending} disabled={!text.trim()}>
              {t.desk.portal.reply}
            </Button>
            {repliesByMail && <span className="text-xs text-ink-subtle">{t.desk.portal.replyByMail}</span>}
          </div>
        </form>
      </section>
    </Page>
  );
}

/** Who raised a request somebody else reads, and whom it is shared with. */
function RaisedBy({ issue }: { issue: PortalRequest }) {
  const { data: session } = useMe();
  const mine = issue.reporter?.id === session?.principal?.user.id;
  if (!issue.sharedWithCompany || !issue.company) return null;
  return (
    <p className="mb-2 text-sm text-ink-muted" data-request-raised-by={issue.reporter?.name ?? ""}>
      {mine
        ? t.desk.portal.sharedWith(issue.company.name)
        : t.desk.portal.raisedAndShared(issue.reporter?.name ?? t.desk.portal.aColleague, issue.company.name)}
    </p>
  );
}

/**
 * Who follows the request. The requester names people by address and may
 * remove anyone; a follower may only remove themselves.
 */
function Followers({ issueKey, reporterId }: { issueKey: string; reporterId?: string }) {
  const { data: session } = useMe();
  const { data } = useRequestWatchers(issueKey);
  const follow = useFollow();
  const unfollow = useUnfollow();
  const [email, setEmail] = useState("");
  const me = session?.principal?.user.id;
  const mine = Boolean(me && me === reporterId);
  const watchers = data?.watchers ?? [];

  function onSubmit(event: FormEvent) {
    event.preventDefault();
    if (!email.trim()) return;
    follow.mutate({ key: issueKey, email: email.trim() }, { onSuccess: () => setEmail("") });
  }

  return (
    <section className="mb-6" data-testid="followers">
      <SectionTitle className="mb-2">{t.desk.portal.followers}</SectionTitle>
      {watchers.length === 0 ? (
        <p className="mb-2 text-sm text-ink-subtle">{t.desk.portal.noFollowers}</p>
      ) : (
        <ul className="mb-2 divide-y divide-border rounded-md border border-border">
          {watchers.map((w) => (
            <li key={w.userId} className="flex items-center gap-2 px-3 py-1.5 text-sm" data-request-watcher={w.email ?? w.name}>
              <Avatar name={w.name} size="sm" />
              <span className="min-w-0 flex-1 truncate text-ink">{w.email ?? w.name}</span>
              {(mine || w.userId === me) && (
                <Button
                  variant="link"
                  className="text-xs text-ink-subtle hover:text-danger"
                  onClick={() => unfollow.mutate({ key: issueKey, userId: w.userId })}
                >
                  {w.userId === me ? t.desk.portal.stopFollowing : t.desk.portal.remove}
                </Button>
              )}
            </li>
          ))}
        </ul>
      )}
      {(follow.error ?? unfollow.error) && <ErrorBanner>{((follow.error ?? unfollow.error) as Error).message}</ErrorBanner>}
      {mine && (
        <form onSubmit={onSubmit} className="flex items-center gap-2" noValidate>
          <Input
            id="follower-address"
            type="email"
            aria-label={t.desk.portal.followerLabel}
            value={email}
            onChange={(e) => setEmail(e.target.value)}
            placeholder={t.desk.portal.followerPlaceholder}
            data-follower-address
            className="w-72"
          />
          <Button type="submit" size="sm" variant="secondary" loading={follow.isPending} disabled={!email.trim()}>
            {t.desk.portal.addFollower}
          </Button>
        </form>
      )}
    </section>
  );
}
