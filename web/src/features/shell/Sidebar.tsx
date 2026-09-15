import { useState } from "react";
import { Link, useLocation, useNavigate, useParams } from "@tanstack/react-router";
import { MY_DATA_HREF, useEraseMe, useLogout, useMe, useSwitchOrg } from "@/api/auth";
import { useProject, useProjects, type Feature, type Project } from "@/api/projects";
import { hasFeature } from "@/features/projects/features";
import { Avatar, Button, IconButton, Input, Menu, Tooltip } from "@/components/ui";
import { Icon, type IconName } from "@/components/icons";
import { SIDEBAR_WIDTH } from "@/config";
import { applyTheme, readTheme, type Theme } from "@/lib/theme";
import { useLastProject, useSidebarGroups, useSidebarMode } from "./state";
import { useConfirm } from "./ConfirmProvider";
import { InboxBell, NavItem } from "./nav";
import { Rail } from "./Rail";
import { SidebarGroup } from "./SidebarGroup";

// The sidebar is the map: where you are is highlighted, where you can go is
// listed, and a project's pages are always there once a project is chosen.

interface PageLink {
  path: string;
  label: string;
  icon: IconName;
  exact?: boolean;
  /** The feature the page belongs to; absent for a page every project has. */
  feature?: Feature;
}

export const projectWorkPages: PageLink[] = [
  { path: "", label: "Issues", icon: "Issue", exact: true },
  { path: "/board", label: "Board", icon: "Board", feature: "board" },
  { path: "/sprints", label: "Sprints", icon: "Sprint", feature: "sprints" },
  { path: "/plan", label: "Plan", icon: "Plan", feature: "plan" },
  { path: "/calendar", label: "Calendar", icon: "Calendar", feature: "calendar" },
  { path: "/milestones", label: "Milestones", icon: "Milestone", feature: "milestones" },
  { path: "/releases", label: "Releases", icon: "Ship", feature: "releases" },
  { path: "/dashboard", label: "Dashboard", icon: "Dashboard", feature: "dashboard" },
  { path: "/hierarchy", label: "Hierarchy", icon: "Hierarchy", feature: "hierarchy" },
  { path: "/queues", label: "Queues", icon: "Queue", feature: "queues" },
  { path: "/service-desk", label: "Service desk", icon: "Desk", feature: "desk" },
];

export const projectSetupPages: PageLink[] = [
  { path: "/teams", label: "Teams", icon: "Team", feature: "teams" },
  { path: "/workflows", label: "Workflow", icon: "Workflow" },
  { path: "/fields", label: "Fields", icon: "Field" },
  { path: "/issue-view", label: "Issue view", icon: "Issue" },
  { path: "/components", label: "Components", icon: "Component", feature: "components" },
  { path: "/repositories", label: "Repositories", icon: "Repository", feature: "repositories" },
  { path: "/automation", label: "Automation", icon: "Bolt", feature: "automation" },
  { path: "/import", label: "Import", icon: "Upload", feature: "import" },
  { path: "/settings", label: "Settings", icon: "Settings" },
];

const settingsPages: Array<{ to: string; label: string; icon: IconName }> = [
  { to: "/settings/organization", label: "Organization", icon: "Settings" },
  { to: "/settings/access", label: "Access", icon: "Key" },
  { to: "/settings/users", label: "Users", icon: "Users" },
  { to: "/settings/workflows", label: "Workflows", icon: "Workflow" },
  { to: "/settings/labels", label: "Labels", icon: "Tag" },
  { to: "/settings/automation", label: "Automation", icon: "Bolt" },
  { to: "/settings/webhooks", label: "Webhooks", icon: "Hook" },
  { to: "/settings/fields", label: "Fields", icon: "Field" },
  { to: "/settings/issue-view", label: "Issue view", icon: "Issue" },
  { to: "/settings/audit", label: "Audit log", icon: "Shield" },
  { to: "/settings/tokens", label: "API tokens", icon: "Command" },
];

/** The pages of the list the project has; a project still loading shows them all. */
export function pagesFor<T extends { feature?: Feature }>(project: Project | undefined, list: T[]): T[] {
  return list.filter((page) => hasFeature(project, page.feature));
}

/** The project whose pages are on screen, read off the route; an issue key carries its project. */
export function currentProjectKey(): string | undefined {
  const params = useParams({ strict: false }) as { projectKey?: string; issueKey?: string };
  if (params.projectKey) return params.projectKey;
  if (params.issueKey) return params.issueKey.slice(0, params.issueKey.lastIndexOf("-")) || undefined;
  return undefined;
}

export function Sidebar({ onNewIssue, onAsk }: { onNewIssue: () => void; onAsk?: () => void }) {
  const { data } = useMe();
  const principal = data?.principal;
  const organizations = data?.organizations ?? [];
  const switchOrg = useSwitchOrg();
  const [mode, toggleMode] = useSidebarMode();
  const open = mode === "open";
  const projectKey = useLastProject(currentProjectKey());
  const [groups, toggleGroup] = useSidebarGroups();
  const isOpen = (id: string, defaultOpen: boolean) => groups[id] ?? defaultOpen;

  return (
    <>
      <Rail open={open} onToggle={toggleMode} onNewIssue={onNewIssue} onAsk={onAsk} userName={principal?.user.name ?? ""} avatar={principal?.user.avatarUrl} />
      {open && (
        <nav className="flex shrink-0 flex-col border-r border-border bg-surface" style={{ width: SIDEBAR_WIDTH }} data-sidebar={mode} aria-label="Where you are">
          <div className="flex h-12 items-center gap-1 border-b border-border px-3">
            {organizations.length > 1 ? (
              <Menu
                label="Organization"
                trigger={(props) => (
                  <Button
                    variant="ghost"
                    size="sm"
                    onClick={props.toggle}
                    aria-haspopup={props["aria-haspopup"]}
                    aria-expanded={props["aria-expanded"]}
                    className="min-w-0 flex-1 justify-start px-1! font-semibold"
                    iconRight={<Icon.ChevronDown className="shrink-0 text-ink-subtle" />}
                  >
                    <span className="truncate text-ink">{principal?.org?.name ?? "No organization"}</span>
                  </Button>
                )}
                items={organizations.map((org) => ({ label: org.orgName, onSelect: () => switchOrg.mutate(org.orgSlug) }))}
              />
            ) : (
              <span className="min-w-0 flex-1 truncate text-sm font-semibold text-ink">{principal?.org?.name ?? "No organization"}</span>
            )}
          </div>

          <div className="flex-1 overflow-y-auto px-2 py-3">
            <SidebarGroup id="work" title="Work & Plan" open={isOpen("work", true)} onToggle={() => toggleGroup("work", true)}>
              <NavItem to="/" exact icon="Home" rail={false}>
                Home
              </NavItem>
              <NavItem to="/search" icon="Search" rail={false} trailing={<kbd className="font-mono text-2xs text-ink-subtle">Ctrl K</kbd>}>
                Search
              </NavItem>
              <NavItem to="/inbox" icon="Bell" rail={false}>
                Inbox
              </NavItem>
              <NavItem to="/projects" icon="Board" rail={false}>
                Projects
              </NavItem>
            </SidebarGroup>

            <SidebarGroup id="project" title="Project" open={isOpen("project", true)} onToggle={() => toggleGroup("project", true)}>
              <ProjectSwitcher projectKey={projectKey} />
              {projectKey && <ProjectPages projectKey={projectKey} adminOpen={isOpen("project-admin", false)} onToggleAdmin={() => toggleGroup("project-admin", false)} />}
            </SidebarGroup>

            <SidebarGroup id="settings" title="Settings" open={isOpen("settings", true)} onToggle={() => toggleGroup("settings", true)}>
              {settingsPages.map((page) => (
                <NavItem key={page.to} to={page.to} icon={page.icon} rail={false}>
                  {page.label}
                </NavItem>
              ))}
            </SidebarGroup>
          </div>

          <SidebarUser userName={principal?.user.name ?? ""} role={principal?.role ?? ""} avatar={principal?.user.avatarUrl} />
        </nav>
      )}
    </>
  );
}

// The person, at the bottom of the map: who they are here, and the way out.
function SidebarUser({ userName, role, avatar }: { userName: string; role: string; avatar?: string }) {
  const navigate = useNavigate();
  const logout = useLogout();
  return (
    <div className="flex items-center gap-2 border-t border-border px-3 py-2.5">
      <Link to="/settings/profile" className="flex min-w-0 flex-1 items-center gap-2 rounded-control hover:bg-surface-raised" title="Your profile">
        <Avatar name={userName} src={avatar} size="sm" />
        <span className="min-w-0 flex-1 truncate text-sm text-ink">{userName}</span>
      </Link>
      {/* The seat this person holds here, which decides what the column offers them. */}
      <span className="shrink-0 text-2xs text-ink-subtle">{role ? role[0]!.toUpperCase() + role.slice(1) : ""}</span>
      <Tooltip text="Sign out">
        <IconButton
          icon={<Icon.External />}
          label="Sign out"
          size="sm"
          onClick={() => logout.mutate(undefined, { onSuccess: () => navigate({ to: "/login" }) })}
          data-action="sign-out"
          aria-busy={logout.isPending}
        />
      </Tooltip>
    </div>
  );
}

function ProjectPages({ projectKey, adminOpen, onToggleAdmin }: { projectKey: string; adminOpen: boolean; onToggleAdmin: () => void }) {
  const { data } = useProject(projectKey);
  const pages = (list: PageLink[]) => pagesFor(data?.project, list);
  return (
    <>
      {pages(projectWorkPages).map((page) => (
        <NavItem key={page.path} to={`/projects/$projectKey${page.path}`} params={{ projectKey }} exact={page.exact} icon={page.icon} rail={false}>
          {page.label}
        </NavItem>
      ))}
      <SidebarGroup id="project-admin" title="Project Admin" open={adminOpen} onToggle={onToggleAdmin} nested>
        {pages(projectSetupPages).map((page) => (
          <NavItem key={page.path} to={`/projects/$projectKey${page.path}`} params={{ projectKey }} icon={page.icon} rail={false}>
            {page.label}
          </NavItem>
        ))}
      </SidebarGroup>
    </>
  );
}

// Switching keeps the page kind: Board stays Board in the other project, so
// comparing two projects is two clicks and not a search through the sidebar.
function ProjectSwitcher({ projectKey }: { projectKey: string | undefined }) {
  const { data } = useProjects();
  const { data: current } = useProject(projectKey ?? "");
  const navigate = useNavigate();
  const { pathname } = useLocation();
  const [filter, setFilter] = useState("");
  const projects = (data?.projects ?? []).filter((p) => !filter || `${p.name} ${p.key}`.toLowerCase().includes(filter.toLowerCase()));
  const suffix = projectKey && pathname.startsWith(`/projects/${projectKey}`) ? pathname.slice(`/projects/${projectKey}`.length) : "";
  const name = current?.project?.name ?? projectKey ?? "Choose a project";
  return (
    <Menu
      label="Switch project"
      className="mb-1 w-full"
      trigger={(props) => (
        <Button
          variant="secondary"
          onClick={props.toggle}
          aria-haspopup={props["aria-haspopup"]}
          aria-expanded={props["aria-expanded"]}
          className="w-full justify-start gap-2 px-2! text-left"
          icon={<Icon.ChevronDown className="shrink-0 text-ink-subtle" />}
          data-project-switcher
        >
          <span className="min-w-0 flex-1 truncate font-medium text-ink">{name}</span>
          {projectKey && <span className="font-mono text-2xs font-normal text-ink-subtle">{projectKey}</span>}
        </Button>
      )}
      items={[
        {
          label: <Input aria-label="Filter projects" value={filter} onChange={(e) => setFilter(e.target.value)} controlSize="sm" placeholder="Filter" onKeyDown={(e) => e.stopPropagation()} />,
          onSelect: () => {},
          attrs: { "data-project-filter": "" },
        },
        ...projects.map((p) => ({
          label: (
            <>
              <span className="min-w-0 flex-1 truncate">{p.name}</span>
              <span className="font-mono text-2xs text-ink-subtle">{p.key}</span>
            </>
          ),
          onSelect: () => navigate({ to: `/projects/${p.key}${suffix}` as never }),
          attrs: { "data-project-option": p.key },
        })),
      ]}
    />
  );
}

const themeLabels: Record<Theme, string> = { system: "Auto", light: "Light", dark: "Dark" };

// A customer has no profile page, so their rights over their data hang off
// their name: the file, and the way out. Erasing keeps their requests, by
// "Former user", which the confirm says.
function CustomerMenu({ userName, avatar }: { userName: string; avatar?: string }) {
  const erase = useEraseMe();
  const confirm = useConfirm();
  const navigate = useNavigate();
  return (
    <Menu
      label="Your account"
      align="end"
      trigger={(props) => (
        <Button variant="ghost" size="sm" onClick={props.toggle} aria-haspopup={props["aria-haspopup"]} aria-expanded={props["aria-expanded"]} className="px-1!" data-action="customer-menu">
          <Avatar name={userName} src={avatar} size="sm" />
          <span className="text-sm text-ink">{userName}</span>
        </Button>
      )}
      items={[
        { label: "Download my data", icon: <Icon.Download />, onSelect: () => window.open(MY_DATA_HREF, "_blank"), attrs: { "data-action": "export-me" } },
        {
          label: "Delete my account",
          icon: <Icon.Trash />,
          danger: true,
          attrs: { "data-action": "erase-me" },
          onSelect: async () => {
            if (await confirm({ noun: "account", verb: "Delete", body: `${userName}'s name, address and sessions go for good. Your requests stay, by "Former user".` })) {
              erase.mutate(undefined, { onSuccess: () => navigate({ to: "/login" }) });
            }
          },
        },
      ]}
    />
  );
}

export function SidebarFoot({ userName, role, avatar, rail, compact = false, onAsk }: { userName: string; role: string; avatar?: string; rail?: boolean; compact?: boolean; onAsk?: () => void }) {
  const navigate = useNavigate();
  const logout = useLogout();
  const [theme, setTheme] = useState<Theme>(readTheme);

  function cycleTheme() {
    const next: Theme = theme === "system" ? "light" : theme === "light" ? "dark" : "system";
    setTheme(next);
    applyTheme(next);
  }
  const themeIcon = theme === "system" ? <Icon.Monitor /> : theme === "light" ? <Icon.Sun /> : <Icon.Moon />;
  // The buttons say their words to screen readers and to the tests, which read
  // the theme button's text as Auto, Light or Dark.
  // Customers have no inbox: the desk mails them, so the portal's foot has no bell.
  const controls = (
    <>
      {!compact && <InboxBell />}
      {onAsk && (
        <Tooltip text="Where is something?">
          <IconButton icon={<Icon.Help />} label="Where is something?" size="sm" onClick={onAsk} data-action="guide" />
        </Tooltip>
      )}
      <Tooltip text={`Theme: ${themeLabels[theme]}`}>
        <IconButton icon={themeIcon} label={themeLabels[theme]} size="sm" onClick={cycleTheme} data-action="theme" data-guide="theme" />
      </Tooltip>
      <Tooltip text="Sign out">
        <IconButton
          icon={<Icon.External />}
          label="Sign out"
          size="sm"
          onClick={() => logout.mutate(undefined, { onSuccess: () => navigate({ to: "/login" }) })}
          data-action="sign-out"
          aria-busy={logout.isPending}
        />
      </Tooltip>
    </>
  );

  if (compact) {
    return (
      <div className="flex items-center gap-2">
        <CustomerMenu userName={userName} avatar={avatar} />
        {controls}
      </div>
    );
  }
  if (rail) {
    return (
      <div className="flex flex-col items-center gap-1 border-t border-border py-2">
        <Avatar name={userName} src={avatar} size="sm" />
        {controls}
      </div>
    );
  }
  return (
    <div className="flex items-center gap-2 border-t border-border px-3 py-2.5">
      <Link to="/settings/profile" className="flex min-w-0 flex-1 items-center gap-2 rounded-control hover:bg-surface-raised" title="Your profile" data-action="profile">
        <Avatar name={userName} src={avatar} size="sm" />
        <span className="min-w-0 flex-1 truncate text-sm text-ink">{userName}</span>
      </Link>
      {/* The seat this person holds here, which decides what the column offers them. */}
      <span className="shrink-0 text-2xs text-ink-subtle">{role ? role[0]!.toUpperCase() + role.slice(1) : ""}</span>
      {controls}
    </div>
  );
}
