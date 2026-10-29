import { useRef, useState, type MouseEvent, type ReactNode } from "react";
import { createPortal } from "react-dom";
import { useLocation, useNavigate, useParams, useRouter } from "@tanstack/react-router";
import { useAccess } from "@/api/access";
import { MY_DATA_HREF, useEraseMe, useLogout, useMe } from "@/api/auth";
import { useProject, useProjectSearch, type Feature, type Project } from "@/api/projects";
import { hasFeature } from "@/features/projects/features";
import { projectSettingsPage, type PageLink } from "@/features/projects/settingsTabs";
import { Avatar, Button, IconButton, Input, Menu, Tooltip, type MenuItem } from "@/components/ui";
import { useEscape, useFocusReturn } from "@/components/ui/overlay";
import { Icon } from "@/components/icons";
import { PROJECT_SEARCH_DELAY_MS, SIDEBAR_RAIL_WIDTH, SIDEBAR_WIDTH } from "@/config";
import { useDebounced } from "@/lib/debounce";
import { DRAWER_ID, SIDEBAR_ID, lastOutsideSettings, useLastProject, useNarrow, useSidebarGroups, useSidebarMode } from "./state";
import { useConfirm } from "./ConfirmProvider";
import { NavButton, NavItem, NavLinkOut, UnreadBadge } from "./nav";
import { Rail } from "./Rail";
import { BuildStamp } from "./BuildStamp";
import { LogoMark } from "@/components/Logo";
import { SidebarGroup } from "./SidebarGroup";
import { ThemeButton } from "./ThemeButton";
import { roleWord, useAccountItems } from "./AccountMenu";
import { adminEntry, inSettingsArea, personalPlaces, workPlaces, type Place } from "./places";
import { t } from "@/i18n";

export { projectSetupPages } from "@/features/projects/settingsTabs";

// The sidebar is the map: where you are is highlighted, where you can go is
// listed, and a project's pages are always there once a project is chosen.

export const projectWorkPages: PageLink[] = [
  {
    path: "",
    get label() {
      return t.shell.projectPages.issues;
    },
    icon: "Issue",
    exact: true,
  },
  {
    path: "/board",
    get label() {
      return t.shell.projectPages.board;
    },
    icon: "Board",
    feature: "board",
  },
  {
    path: "/sprints",
    get label() {
      return t.shell.projectPages.sprints;
    },
    icon: "Sprint",
    feature: "sprints",
  },
  {
    path: "/plan",
    get label() {
      return t.shell.projectPages.plan;
    },
    icon: "Plan",
    feature: "plan",
  },
  {
    path: "/calendar",
    get label() {
      return t.shell.projectPages.calendar;
    },
    icon: "Calendar",
    feature: "calendar",
  },
  {
    path: "/resources",
    get label() {
      return t.shell.projectPages.resources;
    },
    icon: "Capacity",
    feature: "resources",
  },
  {
    path: "/teams",
    get label() {
      return t.shell.projectPages.teams;
    },
    icon: "Team",
    feature: "teams",
  },
  {
    path: "/milestones",
    get label() {
      return t.shell.projectPages.milestones;
    },
    icon: "Milestone",
    feature: "milestones",
  },
  {
    path: "/releases",
    get label() {
      return t.shell.projectPages.releases;
    },
    icon: "Ship",
    feature: "releases",
  },
  {
    path: "/dashboard",
    get label() {
      return t.shell.projectPages.dashboard;
    },
    icon: "Dashboard",
    feature: "dashboard",
  },
  {
    path: "/hierarchy",
    get label() {
      return t.shell.projectPages.hierarchy;
    },
    icon: "Hierarchy",
    feature: "hierarchy",
  },
  {
    path: "/queues",
    get label() {
      return t.shell.projectPages.queues;
    },
    icon: "Queue",
    feature: "queues",
  },
  {
    path: "/service-desk",
    get label() {
      return t.shell.projectPages.serviceDesk;
    },
    icon: "Desk",
    feature: "desk",
  },
];

/** The pages of the list the project has; a project still loading shows them all. */
export function pagesFor<T extends { feature?: Feature }>(project: Project | undefined, list: T[]): T[] {
  return list.filter((page) => hasFeature(project, page.feature));
}

/** The project whose pages are on screen, read off the route; an issue key carries its project. */
export function useCurrentProjectKey(): string | undefined {
  const params = useParams({ strict: false }) as { projectKey?: string; issueKey?: string };
  if (params.projectKey) return params.projectKey;
  if (params.issueKey) return params.issueKey.slice(0, params.issueKey.lastIndexOf("-")) || undefined;
  return undefined;
}

/**
 * The rail and the sidebar beside it, or on a narrow screen nothing until the
 * strip's button opens them as a drawer. In the settings area the sidebar is
 * the settings' own.
 */
export function Sidebar({
  onNewIssue,
  onAsk,
  drawerOpen = false,
  onCloseDrawer = () => {},
}: {
  onNewIssue: () => void;
  onAsk?: () => void;
  drawerOpen?: boolean;
  onCloseDrawer?: () => void;
}) {
  const { data } = useMe();
  const principal = data?.principal;
  const [mode, toggleMode] = useSidebarMode();
  const narrow = useNarrow();
  const { pathname } = useLocation();
  const settings = inSettingsArea(pathname);
  if (narrow) return drawerOpen ? <NavDrawer settings={settings} onClose={onCloseDrawer} onNewIssue={onNewIssue} /> : null;
  const open = mode === "open";

  return (
    <>
      <Rail open={open} onToggle={toggleMode} onNewIssue={onNewIssue} onAsk={onAsk} userName={principal?.user.name ?? ""} avatar={principal?.user.avatarUrl} />
      {open && (
        <nav
          id={SIDEBAR_ID}
          className="flex shrink-0 flex-col border-r border-border bg-surface"
          style={{ width: SIDEBAR_WIDTH }}
          data-sidebar={mode}
          data-print-hide
          data-sidebar-area={settings ? "settings" : "work"}
          aria-label={t.shell.nav.whereYouAre}
        >
          <div className="flex h-12 items-center gap-1 border-b border-border px-3">{settings ? <BackToArmature /> : <InstallationName />}</div>
          <div className="flex-1 overflow-y-auto px-2 py-3">{settings ? <SettingsGroups /> : <WorkGroups />}</div>
          <BuildStamp />
        </nav>
      )}
    </>
  );
}

function InstallationName() {
  const { data } = useMe();
  return (
    <>
      <LogoMark className="mr-1 shrink-0 rounded-control" />
      <span className="min-w-0 flex-1 truncate text-sm font-semibold text-ink" data-installation-name>
        {data?.principal?.installation?.name}
      </span>
    </>
  );
}

/** Heads the settings area's sidebar, and leaves it for the last page outside it. */
function BackToArmature() {
  const router = useRouter();
  const back = lastOutsideSettings();
  function leave(event: MouseEvent<HTMLAnchorElement>) {
    if (event.metaKey || event.ctrlKey || event.shiftKey || event.button !== 0) return;
    event.preventDefault();
    router.history.push(back);
  }
  return (
    <a
      href={back}
      onClick={leave}
      className="flex min-w-0 flex-1 items-center gap-2 rounded-control px-1 py-1 text-sm font-semibold text-ink hover:bg-surface-raised"
      data-action="back-from-settings"
    >
      <Icon.ChevronLeft className="shrink-0 text-ink-muted" />
      <span className="truncate">{t.shell.nav.backToArmature}</span>
    </a>
  );
}

function PlaceLink({ place, trailing }: { place: Place; trailing?: ReactNode }) {
  // Your profile and its notification settings are one page; only the section tells them apart.
  const shared = personalPlaces.some((other) => other !== place && other.to === place.to);
  return (
    <NavItem
      to={place.to}
      search={place.search}
      hash={place.hash}
      matchHash={shared}
      exact={place.to === "/"}
      icon={place.icon}
      rail={false}
      trailing={trailing}
      data-place={place.label}
    >
      {place.label}
    </NavItem>
  );
}

/** The work: the tool's places, then the project on screen or the last one visited, then Admin for an administrator. */
function WorkGroups() {
  const { data: access } = useAccess();
  const projectKey = useLastProject(useCurrentProjectKey());
  const [groups, toggleGroup] = useSidebarGroups();
  const isOpen = (id: string, defaultOpen: boolean) => groups[id] ?? defaultOpen;
  const trailing = (place: Place) =>
    place.to === "/search" ? <kbd className="font-mono text-2xs text-ink-subtle">Ctrl K</kbd> : place.to === "/inbox" ? <UnreadBadge /> : undefined;
  return (
    <>
      <SidebarGroup id="work" title={t.shell.groups.work} open={isOpen("work", true)} onToggle={() => toggleGroup("work", true)}>
        {workPlaces.map((place) => (
          <PlaceLink key={place.to} place={place} trailing={trailing(place)} />
        ))}
      </SidebarGroup>

      <SidebarGroup id="project" title={t.shell.groups.project} open={isOpen("project", true)} onToggle={() => toggleGroup("project", true)}>
        <ProjectSwitcher projectKey={projectKey} />
        {projectKey && <ProjectPages projectKey={projectKey} />}
      </SidebarGroup>

      {access?.canAdministerOrg && (
        <div className="mt-3">
          <PlaceLink place={adminEntry} />
        </div>
      )}
    </>
  );
}

/** The settings area's own group: the person's pages. */
function SettingsGroups() {
  const [groups, toggleGroup] = useSidebarGroups();
  const isOpen = (id: string) => groups[id] ?? true;
  return (
    <>
      <SidebarGroup id="you" title={t.shell.groups.you} open={isOpen("you")} onToggle={() => toggleGroup("you", true)}>
        {personalPlaces.map((place) => (
          <PlaceLink key={place.label} place={place} />
        ))}
      </SidebarGroup>
    </>
  );
}

// A modal panel from the left, as in Stator and Exciter: the page stays in
// view behind the scrim but out of reach, and focus is held inside.
function NavDrawer({ settings, onClose, onNewIssue }: { settings: boolean; onClose: () => void; onNewIssue: () => void }) {
  const ref = useRef<HTMLDivElement>(null);
  useEscape(true, onClose);
  useFocusReturn(true, ref, true);
  // Closing on the press would let the rest of the click land on the page.
  return createPortal(
    <div className="fixed inset-0 z-50 flex bg-ink/30" onClick={(e) => e.target === e.currentTarget && onClose()} data-drawer-backdrop>
      <div
        ref={ref}
        id={DRAWER_ID}
        role="dialog"
        aria-modal="true"
        aria-label={t.shell.nav.navigation}
        // A strip of the page as wide as the rail stays uncovered, so there
        // is always somewhere to tap the drawer shut.
        style={{ width: `min(${SIDEBAR_WIDTH}px, calc(100vw - ${SIDEBAR_RAIL_WIDTH}px))` }}
        className="flex h-full flex-col border-r border-border bg-surface shadow-2 motion-safe:animate-drawer-in"
        data-sidebar="drawer"
        data-sidebar-area={settings ? "settings" : "work"}
      >
        <div className="flex h-12 shrink-0 items-center gap-1 border-b border-border px-3">
          {settings ? <BackToArmature /> : <InstallationName />}
          <IconButton icon={<Icon.X />} label={t.shell.nav.closeNavigation} size="sm" onClick={onClose} data-focus-last="" data-action="close-drawer" />
        </div>
        <nav aria-label={t.shell.nav.whereYouAre} className="flex-1 overflow-y-auto px-2 py-3">
          {!settings && (
            <div className="mb-3">
              <NavButton
                icon="Plus"
                rail={false}
                onClick={() => {
                  onClose();
                  onNewIssue();
                }}
                data-action="new-issue"
              >
                {t.shell.nav.newIssue}
              </NavButton>
            </div>
          )}
          {settings ? <SettingsGroups /> : <WorkGroups />}
        </nav>
        <DrawerFoot />
      </div>
    </div>,
    document.body,
  );
}

/** The drawer's foot: the avatar menu's entries, the theme switch and the build stamp. */
function DrawerFoot() {
  const { data } = useMe();
  const principal = data?.principal;
  const items = useAccountItems();
  return (
    <div className="max-h-[45vh] shrink-0 overflow-y-auto border-t border-border px-2 pt-2" data-drawer-foot>
      <div className="flex items-center gap-2 px-1 pb-2">
        <Avatar name={principal?.user.name ?? ""} src={principal?.user.avatarUrl} size="sm" />
        <span className="min-w-0 flex-1">
          <span className="block truncate text-sm text-ink">{principal?.user.name}</span>
          <span className="block text-2xs text-ink-subtle">{roleWord(principal?.role)}</span>
        </span>
        <ThemeButton side="top" />
      </div>
      <ul aria-label={t.shell.account.yours}>
        {items.map((item, i) => (
          <li key={i}>
            {item.section && <p className="px-2 pt-2 pb-1 text-2xs font-medium tracking-wide text-ink-subtle uppercase">{item.section}</p>}
            {item.separated && !item.section && <div role="separator" className="my-1 border-t border-border" />}
            <DrawerEntry item={item} />
          </li>
        ))}
      </ul>
      <BuildStamp />
    </div>
  );
}

function DrawerEntry({ item }: { item: MenuItem }) {
  const className = "flex h-8 w-full items-center gap-2 rounded-control px-2 text-left text-sm text-ink-muted hover:bg-surface-raised hover:text-ink";
  const content = (
    <>
      {item.icon && <span className="shrink-0 [&_svg]:size-4">{item.icon}</span>}
      <span className="min-w-0 flex-1 truncate">{item.label}</span>
    </>
  );
  return item.href ? (
    <a href={item.href} className={className} {...item.attrs}>
      {content}
    </a>
  ) : (
    <Button variant="ghost" onClick={item.onSelect} className="h-8 w-full justify-start gap-2 px-2! font-normal text-ink-muted" {...item.attrs}>
      {content}
    </Button>
  );
}

function ProjectPages({ projectKey }: { projectKey: string }) {
  const { data } = useProject(projectKey);
  const pages = (list: PageLink[]) => pagesFor(data?.project, list);
  return (
    <>
      {pages(projectWorkPages).map((page) => (
        <NavItem key={page.path} to={`/projects/$projectKey${page.path}`} params={{ projectKey }} exact={page.exact} icon={page.icon} rail={false}>
          {page.label}
        </NavItem>
      ))}
      {data?.project.docsUrl && (
        <NavLinkOut href={data.project.docsUrl} icon="External" data-project-docs={data.project.docsUrl}>
          {data.project.docsLabel || t.shell.projectPages.docs}
        </NavLinkOut>
      )}
      <NavItem to={`/projects/$projectKey${projectSettingsPage.path}`} params={{ projectKey }} icon={projectSettingsPage.icon} rail={false}>
        {projectSettingsPage.label}
      </NavItem>
    </>
  );
}

/** The page's kind under the project, such as "/board"; anything deeper names
 * something of this project, like a sprint, and has no meaning in another. */
function pageKind(pathname: string, projectKey: string): string {
  const prefix = `/projects/${projectKey}`;
  if (!pathname.startsWith(`${prefix}/`)) return "";
  const kind = pathname.slice(prefix.length + 1).split("/")[0];
  return kind ? `/${kind}` : "";
}

// Switching keeps the page kind: Board stays Board in the other project, so
// comparing two projects is two clicks and not a search through the sidebar.
function ProjectSwitcher({ projectKey }: { projectKey: string | undefined }) {
  const [filter, setFilter] = useState("");
  // The server finds the matches, so an installation of thousands lists only a few here.
  const settled = useDebounced(filter.trim(), PROJECT_SEARCH_DELAY_MS);
  const { data } = useProjectSearch(settled);
  const { data: current } = useProject(projectKey ?? "");
  const navigate = useNavigate();
  const { pathname } = useLocation();
  const projects = data?.projects ?? [];
  const suffix = projectKey ? pageKind(pathname, projectKey) : "";
  const name = current?.project?.name ?? projectKey ?? t.shell.projects.choose;
  return (
    <Menu
      label={t.shell.projects.switch}
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
      header={
        <div className="px-1 pb-1" data-project-filter>
          <Input
            aria-label={t.shell.projects.filter}
            value={filter}
            onChange={(e) => setFilter(e.target.value)}
            controlSize="sm"
            placeholder={t.shell.projects.filterPlaceholder}
          />
        </div>
      }
      items={projects.map((p) => ({
        label: (
          <>
            <span className="min-w-0 flex-1 truncate">{p.name}</span>
            <span className="font-mono text-2xs text-ink-subtle">{p.key}</span>
          </>
        ),
        onSelect: () => navigate({ to: `/projects/${p.key}${suffix}` as never }),
        attrs: { "data-project-option": p.key },
      }))}
    />
  );
}

// A customer has no profile page, so their rights over their data hang off
// their name: the file, and the way out. Erasing keeps their requests, by
// "Former user", which the confirm says.
function CustomerMenu({ userName, avatar }: { userName: string; avatar?: string }) {
  const erase = useEraseMe();
  const confirm = useConfirm();
  const navigate = useNavigate();
  return (
    <Menu
      label={t.shell.account.yours}
      align="end"
      trigger={(props) => (
        <Button
          variant="ghost"
          size="sm"
          onClick={props.toggle}
          aria-haspopup={props["aria-haspopup"]}
          aria-expanded={props["aria-expanded"]}
          className="px-1!"
          data-action="customer-menu"
        >
          <Avatar name={userName} src={avatar} size="sm" />
          <span className="sr-only text-sm text-ink shell:not-sr-only">{userName}</span>
        </Button>
      )}
      items={[
        {
          label: t.desk.customer.downloadData,
          icon: <Icon.Download />,
          onSelect: () => window.open(MY_DATA_HREF, "_blank"),
          attrs: { "data-action": "export-me" },
        },
        {
          label: t.desk.customer.deleteAccount,
          icon: <Icon.Trash />,
          danger: true,
          attrs: { "data-action": "erase-me" },
          onSelect: async () => {
            if (
              await confirm({
                noun: t.kit.confirm.things.account,
                verb: t.kit.confirm.verbs.delete,
                body: t.desk.customer.deleteBody(userName),
              })
            ) {
              erase.mutate(undefined, { onSuccess: () => navigate({ to: "/login" }) });
            }
          },
        },
      ]}
    />
  );
}

/** The portal's corner: the customer's menu, the theme switch and the way out. Customers have no notifications page; the desk mails them. */
export function SidebarFoot({ userName, avatar }: { userName: string; avatar?: string }) {
  const navigate = useNavigate();
  const logout = useLogout();
  return (
    <div className="flex items-center gap-2">
      <CustomerMenu userName={userName} avatar={avatar} />
      <ThemeButton />
      <Tooltip text={t.shell.account.signOut}>
        <IconButton
          icon={<Icon.External />}
          label={t.shell.account.signOut}
          size="sm"
          onClick={() => logout.mutate(undefined, { onSuccess: () => navigate({ to: "/login" }) })}
          data-action="sign-out"
          aria-busy={logout.isPending}
        />
      </Tooltip>
    </div>
  );
}
