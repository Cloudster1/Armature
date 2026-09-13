import type { ReactNode } from "react";
import { Link, useNavigate } from "@tanstack/react-router";
import { useUnreadCount } from "@/api/notifications";
import { Button, IconButton, Tooltip, cx } from "@/components/ui";
import { Icon, type IconName } from "@/components/icons";

/** A destination in the rail or the sidebar: an icon with its word, or the icon alone with the word in a tooltip. */
export function NavItem({
  to,
  params,
  exact = false,
  icon,
  rail,
  trailing,
  children,
}: {
  to: string;
  params?: Record<string, string>;
  exact?: boolean;
  icon: IconName;
  rail: boolean;
  trailing?: ReactNode;
  children: ReactNode;
}) {
  const Glyph = Icon[icon];
  const link = (
    <Link
      to={to}
      params={params}
      activeOptions={{ exact }}
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

/** The bell: the inbox, with how much of it is unread. */
export function InboxBell({ side = "bottom" }: { side?: "top" | "bottom" | "right" }) {
  const { data } = useUnreadCount();
  const navigate = useNavigate();
  const unread = data?.unread ?? 0;
  const label = unread > 0 ? `Inbox, ${unread} unread` : "Inbox";
  return (
    <Tooltip text={label} side={side}>
      <span className="relative inline-flex">
        <IconButton icon={<Icon.Bell />} label={label} size="sm" onClick={() => navigate({ to: "/inbox" })} data-action="inbox" data-guide="inbox" data-unread-count={unread} />
        {unread > 0 && (
          <span aria-hidden className="pointer-events-none absolute -top-0.5 -right-0.5 min-w-4 rounded-full bg-primary px-1 text-center text-2xs font-semibold leading-4 text-on-primary">
            {unread > 99 ? "99+" : unread}
          </span>
        )}
      </span>
    </Tooltip>
  );
}
