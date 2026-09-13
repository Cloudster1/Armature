import { DocView } from "@/features/editor/DocView";
import { useRef, useState, type ChangeEvent, type FormEvent } from "react";
import { Link, Navigate, createRoute } from "@tanstack/react-router";
import { appRoute } from "./app";
import { useMe } from "@/api/auth";
import { formatSize, useUploadAttachment } from "@/api/attachments";
import { AttachmentPanel } from "@/features/attachments/AttachmentPanel";
import { ATTACHMENT_MAX_BYTES } from "@/config";
import { useDesks, useFollow, useMyRequests, usePortalArticle, usePortalArticles, useRaiseRequest, useReply, useRequest, useRequestWatchers, useUnfollow, type RequestType } from "@/api/desk";
import { Button, Card, EmptyState, ErrorBanner, Field, Input, OptionCard, Page, PageHeader, SectionTitle, Select, Table, Td, Th, Textarea } from "@/components/ui";
import { Avatar, StatusBadge, relativeTime } from "@/features/issues/badges";
import { groupByCategory, prefillDetails } from "@/features/desk/templates";

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
      <Field label="Is it one of these?" id="field-article-search" type="search" value={query} onChange={(e) => setQuery(e.target.value)} placeholder="A word or two about it" hint="What the desk has written down; it may answer you now." />
      {articles.length > 0 ? (
        <ul className="grid gap-2 sm:grid-cols-2">
          {articles.map((a) => (
            <li key={a.id}>
              <Link to="/portal/articles/$articleId" params={{ articleId: a.id }} className="block rounded-control border border-border p-3 hover:bg-surface-raised" data-article={a.title}>
                <span className="block text-sm font-medium text-ink">{a.title}</span>
                <span className="block truncate text-xs text-ink-muted">{a.body.slice(0, 100)}</span>
              </Link>
            </li>
          ))}
        </ul>
      ) : (
        query && <p className="text-sm text-ink-subtle">Nothing written about that; raise the request below.</p>
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
            Raise a request
          </Link>
        }
        title={article?.title ?? "Article"}
      />
      {error && <ErrorBanner>{(error as Error).message}</ErrorBanner>}
      {article && (
        <Card className="p-5" data-portal-article={article.title}>
          <p className="whitespace-pre-wrap text-sm text-ink">{article.body}</p>
          <p className="mt-4 text-sm text-ink-muted">
            Did this not help?{" "}
            <Link to="/portal/new" className="text-accent hover:underline">
              Raise a request.
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
  // Somebody who came in without a code sees one desk; the heading says which,
  // so a short list is not a surprise.
  const desk = session?.principal?.portalDesk;

  return (
    <Page width="content">
      <PageHeader
        title={desk ? `Your requests at ${desk}` : "Your requests"}
        meta={data ? `${requests.length} request${requests.length === 1 ? "" : "s"}` : undefined}
        actions={
          <Link
            to="/portal/new"
            className="inline-flex h-8 items-center rounded-md bg-primary px-3 text-sm font-medium text-on-primary hover:bg-primary-hover"
          >
            Raise a request
          </Link>
        }
      />
      {isLoading ? null : requests.length === 0 ? (
        <EmptyState
          title="Nothing raised yet"
          description="When you raise a request it appears here, with every reply the desk sends you."
          action={
            <Link to="/portal/new" className="text-sm font-medium text-accent hover:underline">
              Raise a request
            </Link>
          }
        />
      ) : (
        <Table>
          <thead>
            <tr>
              <Th className="w-24">Reference</Th>
              <Th>Request</Th>
              <Th className="w-40">Kind</Th>
              <Th className="w-40">Status</Th>
              <Th className="w-24 text-right">Updated</Th>
            </tr>
          </thead>
          <tbody>
            {requests.map((r) => (
              <tr key={r.id} className="group hover:bg-surface-raised/60" data-request={r.key}>
                <Td className="font-mono text-sm text-ink-muted">{r.key}</Td>
                <Td className="max-w-0">
                  <Link
                    to="/portal/requests/$issueKey"
                    params={{ issueKey: r.key }}
                    className="block truncate text-ink group-hover:text-accent"
                  >
                    {r.summary}
                  </Link>
                </Td>
                <Td className="text-sm text-ink-muted">{r.requestTypeName ?? ""}</Td>
                <Td>
                  <StatusBadge name={r.status.name} category={r.status.category} />
                </Td>
                <Td className="text-right text-sm whitespace-nowrap text-ink-subtle">{relativeTime(r.updatedAt)}</Td>
              </tr>
            ))}
          </tbody>
        </Table>
      )}
    </Page>
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
  const [files, setFiles] = useState<File[]>([]);
  const [tooBig, setTooBig] = useState("");
  const picker = useRef<HTMLInputElement>(null);
  const attach = useUploadAttachment("portal");
  // The request exists after the first call, so a file that fails to follow
  // it is reported, never a reason to send the request twice.
  const [attaching, setAttaching] = useState<{ done: number; total: number } | null>(null);
  const [failed, setFailed] = useState<{ key: string; names: string[] } | null>(null);

  const desk = desks.find((d) => d.projectKey === (deskKey || desks[0]?.projectKey));
  const types = desk?.requestTypes ?? [];
  const chosen = types.find((t) => t.id === typeId) ?? types[0];
  const groups = groupByCategory(types);

  // The details start from the template of what was chosen, and a change of
  // mind swaps templates only while the box still holds one untouched.
  const [templateShown, setTemplateShown] = useState("");
  const template = chosen?.detailsTemplate ?? "";
  if (template !== templateShown) {
    setDescription(prefillDetails(description, templateShown, template));
    setTemplateShown(template);
  }

  function onPick(event: ChangeEvent<HTMLInputElement>) {
    const picked = Array.from(event.target.files ?? []);
    event.target.value = "";
    const large = picked.find((f) => f.size > ATTACHMENT_MAX_BYTES);
    setTooBig(large ? `${large.name} is ${formatSize(large.size)}; the limit is ${formatSize(ATTACHMENT_MAX_BYTES)}.` : "");
    setFiles((current) => [...current, ...picked.filter((f) => f.size <= ATTACHMENT_MAX_BYTES && !current.some((c) => c.name === f.name))]);
  }

  async function onSubmit(event: FormEvent) {
    event.preventDefault();
    if (!chosen || !summary.trim()) return;
    let key: string;
    try {
      const made = await raise.mutateAsync({ requestTypeId: chosen.id, summary: summary.trim(), description: description.trim() || undefined });
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
            Your requests
          </Link>
        }
        title="Raise a request"
      />
      {data && desks.length === 0 ? (
        <EmptyState title="No desk to raise it with" description="Nobody has set up a service desk here yet." />
      ) : (
        <Card className="p-5">
          <form onSubmit={onSubmit} className="space-y-4" noValidate>
            {raise.error && <ErrorBanner>{(raise.error as Error).message}</ErrorBanner>}

            {desks.length > 1 && (
              <Select
                label="Desk"
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

            <fieldset className="space-y-2">
              <legend className="block text-sm font-medium text-ink-muted">What is it about?</legend>
              <div role="radiogroup" aria-label="Request type" className="space-y-3">
                {groups.map((group) => (
                  <div key={group.name} data-request-category={group.name}>
                    {groups.length > 1 && (
                      <p className="mb-1.5 text-xs font-medium tracking-wide text-ink-subtle uppercase">{group.name}</p>
                    )}
                    <div className="grid gap-2 sm:grid-cols-3">
                      {group.types.map((t: RequestType) => (
                        <OptionCard key={t.id} checked={t.id === chosen?.id} data-request-type={t.name} onSelect={() => setTypeId(t.id)} title={t.name} description={t.description || undefined} />
                      ))}
                    </div>
                  </div>
                ))}
              </div>
            </fieldset>

            <Field
              label="Summary"
              required
              autoFocus
              value={summary}
              onChange={(e) => setSummary(e.target.value)}
              placeholder="What do you need?"
            />
            <Field
              label="Details"
              id="portal-description"
              rows={4}
              value={description}
              onChange={(e) => setDescription(e.target.value)}
              placeholder="Anything that helps: what you did, what happened, what you expected."
            />

            <div className="space-y-2">
              <div className="flex items-center gap-3">
                <input ref={picker} type="file" multiple className="sr-only" aria-label="Attach files" onChange={onPick} />
                <Button type="button" size="sm" variant="secondary" onClick={() => picker.current?.click()}>
                  Attach files
                </Button>
                <span className="text-xs text-ink-subtle">Screenshots, logs and documents up to {formatSize(ATTACHMENT_MAX_BYTES)} each.</span>
              </div>
              {tooBig && <ErrorBanner>{tooBig}</ErrorBanner>}
              {files.length > 0 && (
                <ul className="divide-y divide-border rounded-md border border-border" data-attach-queued>
                  {files.map((f) => (
                    <li key={f.name} className="flex items-center gap-3 px-3 py-1.5 text-sm" data-attach-queued-file={f.name}>
                      <span className="min-w-0 flex-1 truncate text-ink">{f.name}</span>
                      <span className="text-ink-subtle tabular-nums">{formatSize(f.size)}</span>
                      <Button type="button" size="sm" variant="ghost" aria-label={`Do not attach ${f.name}`} onClick={() => setFiles((current) => current.filter((c) => c !== f))}>
                        Remove
                      </Button>
                    </li>
                  ))}
                </ul>
              )}
            </div>

            {failed ? (
              <ErrorBanner>
                Your request {failed.key} was sent, but {failed.names.length === 1 ? "one file" : `${failed.names.length} files`} could not be attached: {failed.names.join(", ")}.{" "}
                <Link to="/portal/requests/$issueKey" params={{ issueKey: failed.key }} className="underline">
                  Open the request
                </Link>{" "}
                and attach {failed.names.length === 1 ? "it" : "them"} there.
              </ErrorBanner>
            ) : (
              <div className="flex gap-2">
                <Button type="submit" loading={raise.isPending || attaching !== null} disabled={!chosen || !summary.trim()}>
                  {attaching ? `Attaching ${attaching.done + 1} of ${attaching.total}` : "Send request"}
                </Button>
                <Link to="/portal" className="inline-flex h-8 items-center px-3 text-sm text-ink-muted hover:text-ink">
                  Cancel
                </Link>
              </div>
            )}
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

  const { issue, comments } = data;
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
            Your requests
          </Link>
        }
        title={issue.summary}
        meta={
          <span className="flex items-center gap-2">
            <span className="font-mono">{issue.key}</span>
            {data.requestTypeName && <span>· {data.requestTypeName}</span>}
            <span>·</span>
            <StatusBadge name={issue.status.name} category={issue.status.category} />
          </span>
        }
      />

      <p className="mb-4 flex items-center gap-2 text-sm text-ink-muted" data-request-assignee={issue.assignee?.name ?? ""}>
        {issue.assignee ? (
          <>
            <Avatar name={issue.assignee.name} src={issue.assignee.avatarUrl} size="sm" />
            <span>
              Being handled by <span className="font-medium text-ink">{issue.assignee.name}</span>
            </span>
          </>
        ) : (
          <span>Not picked up yet</span>
        )}
        {issue.team && <span data-request-team={issue.team.name}>· with {issue.team.name}</span>}
      </p>

      {waiting && (
        <div className="mb-4 rounded-md border border-warning/40 bg-warning/10 px-3 py-2 text-sm text-ink" data-testid="waiting-on-you">
          The desk is waiting on you. Reply below and it goes back to them.
        </div>
      )}

      {issue.description && <DocView doc={issue.description} size="sm" className="mb-6" />}

      <Followers issueKey={issueKey} reporterId={issue.reporter?.id} />

      <section>
        <SectionTitle className="mb-2">Conversation</SectionTitle>
        {comments.length === 0 ? (
          <p className="mb-4 text-sm text-ink-subtle">No replies yet.</p>
        ) : (
          <ul className="mb-4 space-y-3">
            {comments.map((c) => (
              <li key={c.id} className="flex gap-3" data-reply={c.id}>
                <Avatar name={c.author?.name} src={c.author?.avatarUrl} />
                <div className="min-w-0 flex-1">
                  <p className="text-xs text-ink-muted">
                    <span className="font-medium text-ink">{c.author?.name ?? "The desk"}</span> · {relativeTime(c.createdAt)}
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
          <Textarea id="portal-reply" aria-label="Reply" value={text} onChange={(e) => setText(e.target.value)} rows={3} placeholder="Reply to the desk" />
          <div className="flex items-center gap-3">
            <Button type="submit" size="sm" loading={reply.isPending} disabled={!text.trim()}>
              Reply
            </Button>
            {repliesByMail && <span className="text-xs text-ink-subtle">You can also answer the mail the desk sends you.</span>}
          </div>
        </form>
      </section>
    </Page>
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
      <SectionTitle className="mb-2">Followers</SectionTitle>
      {watchers.length === 0 ? (
        <p className="mb-2 text-sm text-ink-subtle">Nobody else is following this request.</p>
      ) : (
        <ul className="mb-2 divide-y divide-border rounded-md border border-border">
          {watchers.map((w) => (
            <li key={w.userId} className="flex items-center gap-2 px-3 py-1.5 text-sm" data-request-watcher={w.email}>
              <Avatar name={w.name} size="sm" />
              <span className="min-w-0 flex-1 truncate text-ink">{w.email}</span>
              {(mine || w.userId === me) && (
                <Button variant="link" className="text-xs text-ink-subtle hover:text-danger" onClick={() => unfollow.mutate({ key: issueKey, userId: w.userId })}>
                  {w.userId === me ? "Stop following" : "Remove"}
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
            aria-label="Somebody to follow this request"
            value={email}
            onChange={(e) => setEmail(e.target.value)}
            placeholder="Add somebody by mail address"
            data-follower-address
            className="w-72"
          />
          <Button type="submit" size="sm" variant="secondary" loading={follow.isPending} disabled={!email.trim()}>
            Add follower
          </Button>
        </form>
      )}
    </section>
  );
}
