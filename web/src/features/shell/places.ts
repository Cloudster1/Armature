import type { IconName } from "@/components/icons";
import { t } from "@/i18n";

/**
 * A page the shell leads to. The sidebar, the avatar menu, the settings area,
 * its hub, the Admin page and the palette all read these lists, so they cannot disagree.
 */
export interface Place {
  to: string;
  label: string;
  icon: IconName;
  /** What the settings hub says the page is for. */
  detail?: string;
  /** The query the page opens with, such as Watching's search. */
  search?: Record<string, string>;
  /** The section of the page it opens at. */
  hash?: string;
}

/** The search Watching opens: every issue the person watches, in any project. */
export const WATCHING_QUERY = "watcher = currentUser()";

/** Where the settings area begins; every page under it shares its sidebar. */
export const SETTINGS_PATH = "/settings";

/** The tool's own places, the first group of the sidebar. */
export const workPlaces: Place[] = [
  {
    to: "/",
    get label() {
      return t.shell.places.home;
    },
    icon: "Home",
  },
  {
    to: "/search",
    get label() {
      return t.shell.places.search;
    },
    icon: "Search",
  },
  {
    to: "/inbox",
    get label() {
      return t.shell.places.notifications;
    },
    icon: "Bell",
  },
  {
    to: "/filters",
    get label() {
      return t.shell.places.savedSearches;
    },
    icon: "Filter",
  },
  {
    to: "/projects",
    get label() {
      return t.shell.places.projects;
    },
    icon: "Board",
  },
];

/** The person's own pages: the avatar menu and the settings area's "You", in this order. */
export const personalPlaces: Place[] = [
  {
    to: "/settings/profile",
    get label() {
      return t.shell.places.profile;
    },
    icon: "User",
    get detail() {
      return t.shell.places.profileDetail;
    },
  },
  {
    to: "/settings/profile",
    hash: "notifications",
    get label() {
      return t.shell.places.notificationSettings;
    },
    icon: "Bell",
    get detail() {
      return t.shell.places.notificationSettingsDetail;
    },
  },
  {
    to: "/search",
    search: { q: WATCHING_QUERY },
    get label() {
      return t.shell.places.watching;
    },
    icon: "Eye",
    get detail() {
      return t.shell.places.watchingDetail;
    },
  },
  {
    to: "/settings/themes",
    get label() {
      return t.shell.places.themes;
    },
    icon: "Palette",
    get detail() {
      return t.shell.places.themesDetail;
    },
  },
  {
    to: "/settings/tokens",
    get label() {
      return t.shell.places.accessTokens;
    },
    icon: "Command",
    get detail() {
      return t.shell.places.accessTokensDetail;
    },
  },
];

/** The Admin page's groups, in the order it lists them. */
export const ADMIN_SECTIONS = ["people", "work", "integrations", "records"] as const;
export type AdminSection = (typeof ADMIN_SECTIONS)[number];

/** A place on the Admin page, which files it under one of its sections. */
export interface AdminPlace extends Place {
  section: AdminSection;
}

/** Where the Admin page begins; every page under it is the installation's, for its administrators. */
export const ADMIN_PATH = "/admin";

/** The one Admin entry the work sidebar and the account menu give an administrator. */
export const adminEntry: Place = {
  to: ADMIN_PATH,
  get label() {
    return t.shell.admin.title;
  },
  icon: "Settings",
};

/** What the installation's administrators set, by section, in the order the Admin page lists them. */
export const adminPlaces: AdminPlace[] = [
  {
    to: "/admin/installation",
    section: "people",
    get label() {
      return t.shell.places.installation;
    },
    icon: "Settings",
    get detail() {
      return t.shell.places.installationDetail;
    },
  },
  {
    to: "/admin/access",
    section: "people",
    get label() {
      return t.shell.places.access;
    },
    icon: "Key",
    get detail() {
      return t.shell.places.accessDetail;
    },
  },
  {
    to: "/admin/users",
    section: "people",
    get label() {
      return t.shell.places.users;
    },
    icon: "Users",
    get detail() {
      return t.shell.places.usersDetail;
    },
  },
  {
    to: "/admin/holidays",
    section: "work",
    get label() {
      return t.shell.places.holidays;
    },
    icon: "Calendar",
    get detail() {
      return t.shell.places.holidaysDetail;
    },
  },
  {
    to: "/admin/workflows",
    section: "work",
    get label() {
      return t.shell.places.workflows;
    },
    icon: "Workflow",
    get detail() {
      return t.shell.places.workflowsDetail;
    },
  },
  {
    to: "/admin/issue-types",
    section: "work",
    get label() {
      return t.shell.places.issueTypes;
    },
    icon: "Issue",
    get detail() {
      return t.shell.places.issueTypesDetail;
    },
  },
  {
    to: "/admin/labels",
    section: "work",
    get label() {
      return t.shell.places.labels;
    },
    icon: "Tag",
    get detail() {
      return t.shell.places.labelsDetail;
    },
  },
  {
    to: "/admin/automation",
    section: "work",
    get label() {
      return t.shell.places.automation;
    },
    icon: "Bolt",
    get detail() {
      return t.shell.places.automationDetail;
    },
  },
  {
    to: "/admin/fields",
    section: "work",
    get label() {
      return t.shell.places.fields;
    },
    icon: "Field",
    get detail() {
      return t.shell.places.fieldsDetail;
    },
  },
  {
    to: "/admin/issue-view",
    section: "work",
    get label() {
      return t.shell.places.issueView;
    },
    icon: "Issue",
    get detail() {
      return t.shell.places.issueViewDetail;
    },
  },
  {
    to: "/admin/connected-apps",
    section: "integrations",
    get label() {
      return t.shell.places.connectedApps;
    },
    icon: "Link",
    get detail() {
      return t.shell.places.connectedAppsDetail;
    },
  },
  {
    to: "/admin/webhooks",
    section: "integrations",
    get label() {
      return t.shell.places.webhooks;
    },
    icon: "Hook",
    get detail() {
      return t.shell.places.webhooksDetail;
    },
  },
  {
    to: "/admin/audit",
    section: "records",
    get label() {
      return t.shell.places.audit;
    },
    icon: "Shield",
    get detail() {
      return t.shell.places.auditDetail;
    },
  },
];

/** The admin places a person may open: all of them for an administrator, none for anybody else. */
export function adminFor(canAdministerOrg: boolean): AdminPlace[] {
  return canAdministerOrg ? adminPlaces : [];
}

/** Whether a path lies in the settings area (the person's own pages), which swaps the sidebar for its own. */
export function inSettingsArea(pathname: string): boolean {
  return pathname === SETTINGS_PATH || pathname.startsWith(`${SETTINGS_PATH}/`);
}

/** A key for a place that tells apart two on one path, such as the profile and its notifications. */
export function placeKey(place: Place): string {
  const query = place.search ? `?${new URLSearchParams(place.search)}` : "";
  return `${place.to}${query}${place.hash ? `#${place.hash}` : ""}`;
}
