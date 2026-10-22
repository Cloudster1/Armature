import { IconButton, Tooltip } from "@/components/ui";
import { Icon } from "@/components/icons";
import { SIDEBAR_RAIL_WIDTH } from "@/config";
import { NavButton, NavItem, NotificationsBell } from "./nav";
import { AccountMenu } from "./AccountMenu";
import { ThemeButton } from "./ThemeButton";

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
    <aside className="flex shrink-0 flex-col items-center border-r border-border bg-surface" style={{ width: SIDEBAR_RAIL_WIDTH }} data-rail>
      <header className="flex h-12 w-full items-center justify-center border-b border-border">
        <IconButton icon={open ? <Icon.Collapse /> : <Icon.Expand />} label={open ? "Collapse the sidebar" : "Expand the sidebar"} size="sm" onClick={onToggle} data-action="sidebar" />
      </header>
      <nav aria-label="Everywhere" className="flex flex-1 flex-col items-center gap-1 py-3">
        <NavItem to="/" exact icon="Home" rail>
          Home
        </NavItem>
        <NavItem to="/search" icon="Search" rail>
          Search
        </NavItem>
        <NotificationsBell side="right" />
        <NavItem to="/projects" icon="Board" rail>
          Projects
        </NavItem>
        <NavButton icon="Plus" rail onClick={onNewIssue} data-action="new-issue" data-guide="new-issue">
          New issue
        </NavButton>
      </nav>
      <div className="flex flex-col items-center gap-1 border-t border-border py-2">
        {onAsk && (
          <Tooltip text="Where is something?" side="right">
            <IconButton icon={<Icon.Help />} label="Where is something?" size="sm" onClick={onAsk} data-action="guide" />
          </Tooltip>
        )}
        <ThemeButton side="right" />
        <AccountMenu userName={userName} avatar={avatar} />
      </div>
    </aside>
  );
}
