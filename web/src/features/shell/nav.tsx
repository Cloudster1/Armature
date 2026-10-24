import { useState, type ReactNode } from "react";
import { Link } from "@tanstack/react-router";
import { useInbox, useMarkRead, useUnreadCount, type Notification } from "@/api/notifications";
import { Button, IconButton, Popover, Tooltip, cx } from "@/components/ui";
import { Icon, type IconName } from "@/components/icons";
import { BELL_PANEL_SIZE } from "@/config";
import { useFormat } from "@/lib/format";

/** A destination in the rail or the sidebar: an icon with its word, or the icon alone with the word in a tooltip. */
export function NavItem({
  to,
  params,
  search,
  hash,
  exact = false,
  matchHash,
  icon,
  rail,
  trailing,
  children,
  ...rest
}: {
  to: string;
  params?: Record<string, string>;
  /** The query the page opens with; the link is lit only when the page has it. */
  search?: Record<string, string>;
  /** The section the page opens at; the link is lit only on it, so two links to one page tell apart. */
  hash?: string;
  exact?: boolean;
  /** Lit only when the page's section is the link's too, for a page that two links open at different places. */
  matchHash?: boolean;
  icon: IconName;
  rail: boolean;
  trailing?: ReactNode;
  children: ReactNode;
} & Record<`data-${string}`, string>) {
  const Glyph = Icon[icon];
  const link = (
    <Link
      to={to}
      params={params}
      search={search as never}
      hash={hash}
      {...rest}
      activeOptions={{ exact: exact || hash !== undefined || search !== undefined, includeHash: matchHash ?? hash !== undefined, includeSearch: search !== undefined }}
      className={cx(
        "flex h-8 items-center gap-2 rounded-control text-sm text-ink-muted hover:bg-surface-raised hover:text-ink",
        rail ? "w-8 justify-center px-0" : "px-2",
      )}
      activeProps={{ className: "bg-accent-subtle text-accent font-medium hover:bg-accent-subtle hover:text-accent" }}
    >
      <Glyph className="shrink-0" />
      {!rail && <span className="min-w-0 flex-1 truncate">{children}</span>}
      {!rail && trailing}
      {rail && <span className="sr-only">{children}</span>}
    </Link>
  );
  return rail ? (
    <Tooltip text={String(children)} side="right">
      {link}
    </Tooltip>
  ) : (
    link
  );
}

/** A destination outside Armature, drawn like one inside it and opened in a new tab. */
export function NavLinkOut({ href, icon, children, ...rest }: { href: string; icon: IconName; children: string } & Record<`data-${string}`, string>) {
  const Glyph = Icon[icon];
  return (
    <a
      href={href}
      target="_blank"
      rel="noopener noreferrer"
      className="flex h-8 items-center gap-2 rounded-control px-2 text-sm text-ink-muted hover:bg-surface-raised hover:text-ink"
      {...rest}
    >
      <Glyph className="shrink-0" />
      <span className="min-w-0 flex-1 truncate">{children}</span>
    </a>
  );
}

/** An action in the rail or the sidebar, drawn like a destination. */
export function NavButton({ icon, rail, children, ...rest }: { icon: IconName; rail: boolean; children: string; onClick: () => void } & Record<`data-${string}`, string>) {
  const Glyph = Icon[icon];
  const button = (
    <Button variant="ghost" {...rest} className={cx("gap-2 font-normal", rail ? "w-8 px-0!" : "w-full justify-start px-2!")} icon={<Glyph className="shrink-0" />}>
      {rail ? <span className="sr-only">{children}</span> : <span className="min-w-0 flex-1 truncate text-left">{children}</span>}
    </Button>
  );
  return rail ? (
    <Tooltip text={children} side="right">
      {button}
    </Tooltip>
  ) : (
    button
  );
}

/** How many notifications are unread, beside the sidebar's Notifications; nothing when none are. */
export function UnreadBadge() {
  const { data } = useUnreadCount();
  const unread = data?.unread ?? 0;
  if (unread === 0) return null;
  return (
    <span className="min-w-5 rounded-full bg-accent px-1.5 text-center text-2xs leading-5 font-medium text-on-accent" data-unread-badge={unread}>
      {unread > 99 ? "99+" : unread}
      <span className="sr-only"> unread</span>
    </span>
  );
}

/**
 * The bell: how much is unread, and a panel with the latest notifications, a
 * way to mark them all read, the whole list and the settings for them.
 */
export function NotificationsBell({ side = "bottom" }: { side?: "top" | "bottom" | "right" }) {
  const { data } = useUnreadCount();
  const [open, setOpen] = useState(false);
  const unread = data?.unread ?? 0;
  const label = unread > 0 ? `Notifications, ${unread} unread` : "Notifications";
  return (
    <Popover
      open={open}
      onClose={() => setOpen(false)}
      label="Notifications"
      align="start"
      className="w-80 p-0!"
      trigger={
        <Tooltip text={label} side={side}>
          <span className="relative inline-flex">
            <IconButton icon={<Icon.Bell />} label={label} size="sm" onClick={() => setOpen((o) => !o)} aria-expanded={open} data-action="notifications" data-guide="inbox" data-unread-count={unread} />
            {unread > 0 && (
              <span aria-hidden className="pointer-events-none absolute -top-0.5 -right-0.5 min-w-4 rounded-full bg-accent px-1 text-center text-2xs leading-4 font-medium text-on-accent">
                {unread > 99 ? "99+" : unread}
              </span>
            )}
          </span>
        </Tooltip>
      }
    >
      {open && <NotificationsPanel unread={unread} onDone={() => setOpen(false)} />}
    </Popover>
  );
}

function NotificationsPanel({ unread, onDone }: { unread: number; onDone: () => void }) {
  const { data, isLoading } = useInbox(false);
  const markRead = useMarkRead();
  const format = useFormat();
  const latest = (data?.notifications ?? []).slice(0, BELL_PANEL_SIZE);
  // The link does the going; opening is what marks it read, as on the page.
  const open = (n: Notification) => {
    if (!n.readAt) markRead.mutate({ ids: [n.id] });
    onDone();
  };
  return (
    <div data-notifications-panel>
      <div className="flex items-center justify-between gap-2 border-b border-border px-3 py-2">
        <span className="text-sm font-semibold text-ink">Notifications</span>
        <Button variant="ghost" size="sm" onClick={() => markRead.mutate({ all: true })} loading={markRead.isPending} disabled={unread === 0} data-action="mark-all-read">
          Mark all as read
        </Button>
      </div>
      <div className="max-h-96 overflow-y-auto">
        {isLoading ? null : latest.length === 0 ? (
          <p className="px-3 py-6 text-center text-sm text-ink-subtle">Nothing yet. You are told here when an issue of yours moves or somebody mentions you.</p>
        ) : (
          latest.map((n) => {
            const row = {
              onClick: () => open(n),
              className: "flex items-start gap-2 px-3 py-2 hover:bg-surface-raised",
              "data-notification": n.kind,
              "data-notification-read": n.readAt ? "true" : "false",
            };
            const content = (
              <>
                <span className={n.readAt ? "mt-1.5 size-2 shrink-0 rounded-full bg-transparent" : "mt-1.5 size-2 shrink-0 rounded-full bg-accent"} aria-hidden />
                <span className="min-w-0 flex-1">
                  <span className={cx("block text-sm", n.readAt ? "text-ink-muted" : "font-medium text-ink")}>{n.title}</span>
                  <span className="block text-xs text-ink-subtle">{format.relative(n.createdAt)}</span>
                </span>
              </>
            );
            // A notice about your absences opens your profile, where they are listed.
            return n.kind === "absence" || !n.issueKey ? (
              <Link key={n.id} to="/settings/profile" {...row}>
                {content}
              </Link>
            ) : (
              <Link key={n.id} to="/issues/$issueKey" params={{ issueKey: n.issueKey }} {...row}>
                {content}
              </Link>
            );
          })
        )}
      </div>
      <div className="flex items-center justify-between gap-2 border-t border-border px-3 py-2 text-sm">
        <Link to="/inbox" onClick={onDone} className="text-accent hover:underline" data-action="see-all-notifications">
          See all
        </Link>
        <Link to="/settings/profile" hash="notifications" onClick={onDone} className="text-ink-muted hover:text-ink hover:underline" data-action="notification-settings">
          Notification settings
        </Link>
      </div>
    </div>
  );
}
