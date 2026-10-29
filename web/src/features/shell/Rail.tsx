import { IconButton, Tooltip } from "@/components/ui";
import { Icon } from "@/components/icons";
import { SIDEBAR_RAIL_WIDTH } from "@/config";
import { NavButton, NavItem, NotificationsBell } from "./nav";
import { AccountMenu } from "./AccountMenu";
import { ThemeButton } from "./ThemeButton";
import { SIDEBAR_ID } from "./state";
import { t } from "@/i18n";

/**
 * The rail is what is always there: the places every page reaches from, and
 * the person at the bottom. The sidebar beside it is the map of where you
 * are; when the map is folded away, the rail is the whole chrome.
 */
export function Rail({
  open,
  onToggle,
  onNewIssue,
  onAsk,
  userName,
  avatar,
}: {
  open: boolean;
  onToggle: () => void;
  onNewIssue: () => void;
  onAsk?: () => void;
  userName: string;
  avatar?: string;
}) {
  return (
    <aside className="flex shrink-0 flex-col items-center border-r border-border bg-surface" style={{ width: SIDEBAR_RAIL_WIDTH }} data-rail data-print-hide>
      {/* The toggle opens and closes the navigation, so it sits inside it, not in a banner of its own. */}
      <nav aria-label={t.shell.nav.everywhere} className="flex w-full flex-1 flex-col items-center">
        <div className="flex h-12 w-full items-center justify-center border-b border-border" data-rail-top>
          <IconButton
            icon={open ? <Icon.Collapse /> : <Icon.Expand />}
            label={open ? t.shell.nav.collapseSidebar : t.shell.nav.expandSidebar}
            size="sm"
            onClick={onToggle}
            aria-expanded={open}
            // Folded, the sidebar is not in the document, so there is nothing to name.
            aria-controls={open ? SIDEBAR_ID : undefined}
            data-action="sidebar"
          />
        </div>
        <div className="flex flex-1 flex-col items-center gap-1 py-3">
          <NavItem to="/" exact icon="Home" rail>
            {t.shell.places.home}
          </NavItem>
          <NavItem to="/search" icon="Search" rail>
            {t.shell.places.search}
          </NavItem>
          <NotificationsBell side="right" />
          <NavItem to="/projects" icon="Folder" rail>
            {t.shell.places.projects}
          </NavItem>
          <NavButton icon="Plus" rail onClick={onNewIssue} data-action="new-issue" data-guide="new-issue">
            {t.shell.nav.newIssue}
          </NavButton>
        </div>
      </nav>
      {/* As wide as the rail, unlike its centred controls, so the account menu's anchor reaches the rail's edge. */}
      <div className="flex w-full flex-col items-center gap-1 border-t border-border py-2">
        {onAsk && (
          <Tooltip text={t.shell.nav.whereIsSomething} side="right">
            <IconButton icon={<Icon.Help />} label={t.shell.nav.whereIsSomething} size="sm" onClick={onAsk} data-action="guide" />
          </Tooltip>
        )}
        <ThemeButton side="right" />
        <AccountMenu userName={userName} avatar={avatar} />
      </div>
    </aside>
  );
}
