import { useState } from "react";
import { Link, createRoute } from "@tanstack/react-router";
import { appRoute } from "./app";
import { useInbox, useMarkRead, type Notification } from "@/api/notifications";
import { Button, Card, EmptyState, Page, PageHeader, Segmented } from "@/components/ui";
import { Icon } from "@/components/icons";
import { useFormat } from "@/lib/format";

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
        title="Inbox"
        meta={items.length ? `${items.length} ${view === "unread" ? "unread" : "recent"}` : undefined}
        actions={
          <Button variant="secondary" size="sm" onClick={() => markRead.mutate({ all: true })} loading={markRead.isPending} data-action="mark-all-read">
            Mark all read
          </Button>
        }
      />
      <div className="mb-4">
        <Segmented<View>
          label="Show"
          value={view}
          onChange={setView}
          options={[
            { value: "unread", label: "Unread", attrs: { "data-inbox-view": "unread" } },
            { value: "all", label: "Everything", attrs: { "data-inbox-view": "all" } },
          ]}
        />
      </div>
      {isLoading ? null : items.length === 0 ? (
        <EmptyState
          icon={<Icon.Bell />}
          title={view === "unread" ? "Nothing unread" : "Nothing yet"}
          description="You are told here when an issue of yours moves, somebody mentions you, or work is assigned to you. How you are told is yours to set."
          action={
            <Link to="/settings/profile" className="text-sm text-accent hover:underline">
              Notification settings
            </Link>
          }
        />
      ) : (
        <Card className="divide-y divide-border">
          {items.map((n) => (
            <Link
              key={n.id}
              to="/issues/$issueKey"
              params={{ issueKey: n.issueKey ?? "" }}
              onClick={() => open(n)}
              className="flex items-start gap-3 px-4 py-3 hover:bg-surface-raised"
              data-notification={n.kind}
              data-notification-read={n.readAt ? "true" : "false"}
            >
              <span className={n.readAt ? "mt-2 size-2 shrink-0 rounded-full bg-transparent" : "mt-2 size-2 shrink-0 rounded-full bg-accent"} aria-hidden />
              <span className="min-w-0 flex-1">
                <span className={n.readAt ? "block text-sm text-ink-muted" : "block text-sm font-medium text-ink"}>{n.title}</span>
                {n.body && <span className="block truncate text-sm text-ink-muted">{n.body}</span>}
              </span>
              <span className="shrink-0 text-xs text-ink-subtle">{format.relative(n.createdAt)}</span>
            </Link>
          ))}
        </Card>
      )}
    </Page>
  );
}
