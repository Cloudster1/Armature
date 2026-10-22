import type { IconName } from "@/components/icons";

/**
 * A page the shell leads to. The sidebar, the avatar menu, the settings area,
 * its hub and the palette all read these lists, so they cannot disagree.
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
  { to: "/", label: "Home", icon: "Home" },
  { to: "/search", label: "Search", icon: "Search" },
  { to: "/inbox", label: "Notifications", icon: "Bell" },
  { to: "/filters", label: "Saved searches", icon: "Filter" },
  { to: "/projects", label: "Projects", icon: "Board" },
];

/** The person's own pages: the avatar menu and the settings area's "You", in this order. */
export const personalPlaces: Place[] = [
  { to: "/settings/profile", label: "Your profile", icon: "User", detail: "Your name, your picture, your working week, and how dates are written for you." },
  { to: "/settings/profile", hash: "notifications", label: "Notification settings", icon: "Bell", detail: "What you are told about, here and by mail." },
  { to: "/search", search: { q: WATCHING_QUERY }, label: "Watching", icon: "Eye", detail: "The issues you watch, in every project." },
  { to: "/settings/themes", label: "Themes", icon: "Palette", detail: "Colours, type, cursors and icons, yours or shared with everyone." },
  { to: "/settings/tokens", label: "Access tokens", icon: "Command", detail: "Call the API from a script or a CI job, and the apps you connected." },
];

/** The installation's settings, for its administrators only, in the order the settings area lists them. */
export const settingsPlaces: Place[] = [
  { to: "/settings/installation", label: "Installation", icon: "Settings", detail: "What everybody sees it called, and the demo project to show it with." },
  { to: "/settings/access", label: "Access", icon: "Key", detail: "Who holds which role, the groups, and single sign-on." },
  { to: "/settings/users", label: "Users", icon: "Users", detail: "Accounts that sign in with a password here: make one, reset it, switch it off." },
  { to: "/settings/holidays", label: "Holidays", icon: "Calendar", detail: "Calendars of days off, and which one everybody keeps by default." },
  { to: "/settings/workflows", label: "Workflows", icon: "Workflow", detail: "The workflows every project follows and the schemes that hand them to issue types." },
  { to: "/settings/labels", label: "Labels", icon: "Tag", detail: "Words shared by every project." },
  { to: "/settings/automation", label: "Automation", icon: "Bolt", detail: "Rules that watch every project: when something happens, check it, do things." },
  { to: "/settings/fields", label: "Fields", icon: "Field", detail: "What every project records about an issue beyond the standard fields." },
  { to: "/settings/issue-view", label: "Issue view", icon: "Issue", detail: "How an issue's fields are arranged in every project that does not arrange its own." },
  { to: "/settings/connected-apps", label: "Connected apps", icon: "Link", detail: "Partner tools people connect in one click, with their consent." },
  { to: "/settings/webhooks", label: "Webhooks", icon: "Hook", detail: "Where events are posted, signed, with a log of every try." },
  { to: "/settings/audit", label: "Audit log", icon: "Shield", detail: "Who did what, and when, kept for a year." },
];

/** The settings a person may open: all of them for an administrator, none for anybody else. */
export function settingsFor(canAdministerOrg: boolean): Place[] {
  return canAdministerOrg ? settingsPlaces : [];
}

/** Whether a path lies in the settings area, which swaps the sidebar for its own. */
export function inSettingsArea(pathname: string): boolean {
  return pathname === SETTINGS_PATH || pathname.startsWith(`${SETTINGS_PATH}/`);
}

/** A key for a place that tells apart two on one path, such as the profile and its notifications. */
export function placeKey(place: Place): string {
  const query = place.search ? `?${new URLSearchParams(place.search)}` : "";
  return `${place.to}${query}${place.hash ? `#${place.hash}` : ""}`;
}
