import { useState } from "react";
import { Link, useNavigate } from "@tanstack/react-router";
import { useLogout } from "@/api/auth";
import { Avatar, IconButton, Menu, Tooltip } from "@/components/ui";
import { Icon } from "@/components/icons";
import { SIDEBAR_RAIL_WIDTH } from "@/config";
import { applyTheme, readTheme, type Theme } from "@/lib/theme";
import { InboxBell, NavButton, NavItem } from "./nav";

const themeLabels: Record<Theme, string> = { system: "Auto", light: "Light", dark: "Dark" };

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
  const navigate = useNavigate();
  const logout = useLogout();
  const [theme, setTheme] = useState<Theme>(readTheme);

  function cycleTheme() {
    const next: Theme = theme === "system" ? "light" : theme === "light" ? "dark" : "system";
    setTheme(next);
    applyTheme(next);
  }
  const themeIcon = theme === "system" ? <Icon.Monitor /> : theme === "light" ? <Icon.Sun /> : <Icon.Moon />;

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
        <InboxBell side="right" />
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
        <Tooltip text={`Theme: ${themeLabels[theme]}`} side="right">
          <IconButton icon={themeIcon} label={themeLabels[theme]} size="sm" onClick={cycleTheme} data-action="theme" data-guide="theme" />
        </Tooltip>
        {open ? (
          <Tooltip text="Your profile" side="right">
            <Link to="/settings/profile" className="inline-flex rounded-full" data-action="profile">
              <Avatar name={userName} src={avatar} size="sm" />
              <span className="sr-only">Your profile</span>
            </Link>
          </Tooltip>
        ) : (
          // With the sidebar folded the person's block is gone, so the way out
          // hangs off the picture instead.
          <Menu
            label="Your account"
            align="start"
            trigger={(props) => (
              <IconButton icon={<Avatar name={userName} src={avatar} size="sm" />} label="Your account" size="sm" onClick={props.toggle} aria-haspopup={props["aria-haspopup"]} aria-expanded={props["aria-expanded"]} className="rounded-full" data-action="profile" />
            )}
            items={[
              { label: "Your profile", icon: <Icon.User />, onSelect: () => navigate({ to: "/settings/profile" }) },
              { label: "Sign out", icon: <Icon.External />, onSelect: () => logout.mutate(undefined, { onSuccess: () => navigate({ to: "/login" }) }), attrs: { "data-action": "sign-out" } },
            ]}
          />
        )}
      </div>
    </aside>
  );
}
