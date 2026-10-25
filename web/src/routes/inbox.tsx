import { useState } from "react";
import { Link, createRoute } from "@tanstack/react-router";
import { appRoute } from "./app";
import { useInbox, useMarkRead, type Notification } from "@/api/notifications";
import { Button, Card, EmptyState, Page, PageHeader, Segmented } from "@/components/ui";
import { Icon } from "@/components/icons";
import { useFormat } from "@/lib/format";
import { t } from "@/i18n";

/** What you were told, newest first; opening one marks it read and goes there. */
export const inboxRoute = createRoute({
  getParentRoute: () => appRoute,
  path: "/inbox",
  component: InboxPage,
});

type View = "unread" | "all";

function InboxPage() {
  const [view, setView] = useState<View>("unread");
  const { data, isLoading } = useInbox(view === "unread");
  const markRead = useMarkRead();
  const format = useFormat();
  const items = data?.notifications ?? [];

  // The link does the going; opening is what marks it read.
  function open(n: Notification) {
    if (!n.readAt) markRead.mutate({ ids: [n.id] });
  }

  return (
    <Page width="narrow">
      <PageHeader
        title={t.search.inbox.title}
        meta={items.length ? (view === "unread" ? t.search.inbox.unreadCount(items.length) : t.search.inbox.recentCount(items.length)) : undefined}
        actions={
          <Button variant="secondary" size="sm" onClick={() => markRead.mutate({ all: true })} loading={markRead.isPending} data-action="mark-all-read">
            {t.search.inbox.markAllRead}
          </Button>
        }
      />
      <div className="mb-4">
        <Segmented<View>
          label={t.search.inbox.show}
          value={view}
          onChange={setView}
          options={[
            { value: "unread", label: t.search.inbox.unread, attrs: { "data-inbox-view": "unread" } },
            { value: "all", label: t.search.inbox.everything, attrs: { "data-inbox-view": "all" } },
          ]}
        />
      </div>
      {isLoading ? null : items.length === 0 ? (
        <EmptyState
          icon={<Icon.Bell />}
          title={view === "unread" ? t.search.inbox.nothingUnread : t.search.inbox.nothingYet}
          description={t.search.inbox.emptyHint}
          action={
            <Link to="/settings/profile" className="text-sm text-accent hover:underline">
              {t.search.inbox.settings}
            </Link>
          }
        />
      ) : (
        <Card className="divide-y divide-border">
          {items.map((n) => {
            const row = {
              onClick: () => open(n),
              className: "flex items-start gap-3 px-4 py-3 hover:bg-surface-raised",
              "data-notification": n.kind,
              "data-notification-read": n.readAt ? "true" : "false",
            };
            const content = (
              <>
                <span className={n.readAt ? "mt-2 size-2 shrink-0 rounded-full bg-transparent" : "mt-2 size-2 shrink-0 rounded-full bg-accent"} aria-hidden />
                <span className="min-w-0 flex-1">
                  <span className={n.readAt ? "block text-sm text-ink-muted" : "block text-sm font-medium text-ink"}>{n.title}</span>
                  {n.body && <span className="block truncate text-sm text-ink-muted">{n.body}</span>}
                </span>
                <span className="shrink-0 text-xs text-ink-subtle">{format.relative(n.createdAt)}</span>
              </>
            );
            // A notice about your absences opens your profile, where they are listed.
            return n.kind === "absence" ? (
              <Link key={n.id} to="/settings/profile" {...row}>
                {content}
              </Link>
            ) : (
              <Link key={n.id} to="/issues/$issueKey" params={{ issueKey: n.issueKey ?? "" }} {...row}>
                {content}
              </Link>
            );
          })}
        </Card>
      )}
    </Page>
  );
}
