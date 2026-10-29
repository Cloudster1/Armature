import type { ReactNode } from "react";
import { Link, useRouterState } from "@tanstack/react-router";
import { useProject } from "@/api/projects";
import { Breadcrumbs, type Crumb } from "@/components/ui";
import { projectSettingsPage } from "@/features/projects/settingsTabs";
import { ADMIN_PATH, SETTINGS_PATH, adminPlaces, personalPlaces, workPlaces } from "./places";
import { t } from "@/i18n";

/** One crumb of the trail: a place to go back to, with where it leads. */
export interface PlaceCrumb {
  label: string;
  to: string;
}

/** What a trail needs beyond the address: the name of the project it passes, once it is known. */
export interface CrumbNames {
  projectName?: string;
}

/** The project a path is about: a project's own page, or an issue, whose key carries its project. */
export function projectOfPath(pathname: string): string | undefined {
  const project = /^\/projects\/([^/]+)/.exec(pathname);
  if (project) return decodeURIComponent(project[1]!);
  const issue = /^\/issues\/([^/]+)-\d+/.exec(pathname);
  return issue ? decodeURIComponent(issue[1]!) : undefined;
}

function labelOf(places: Array<{ to: string; label: string }>, to: string): string | undefined {
  return places.find((place) => place.to === to)?.label;
}

/**
 * The places a page lies under, as the sidebar leads to it, without the page
 * itself: Projects and the project on a project's page or an issue, Settings
 * below the person's own pages, Admin below the installation's. Home, Projects
 * and Search lie under nothing.
 */
export function crumbsFor(pathname: string, names: CrumbNames = {}): PlaceCrumb[] {
  const path = pathname.length > 1 ? pathname.replace(/\/+$/, "") : pathname;
  const parts = path.split("/").filter(Boolean);
  const key = projectOfPath(path);
  if (key) {
    const projects: PlaceCrumb = { label: t.shell.places.projects, to: "/projects" };
    if (parts[0] === "projects" && parts.length === 2) return [projects];
    const project: PlaceCrumb = { label: names.projectName ?? key, to: `/projects/${key}` };
    if (parts[0] === "projects" && parts[2] === "settings" && parts.length > 3) {
      return [projects, project, { label: projectSettingsPage.label, to: `/projects/${key}/settings` }];
    }
    return [projects, project];
  }
  const top = `/${parts[0] ?? ""}`;
  if (top === SETTINGS_PATH) {
    if (parts.length === 1) return [];
    const settings: PlaceCrumb = { label: t.settings.crumb, to: SETTINGS_PATH };
    const page = `/${parts.slice(0, 2).join("/")}`;
    const pageLabel = labelOf(personalPlaces, page);
    return parts.length > 2 && pageLabel ? [settings, { label: pageLabel, to: page }] : [settings];
  }
  if (top === ADMIN_PATH) {
    if (parts.length === 1) return [];
    const admin: PlaceCrumb = { label: t.shell.admin.crumb, to: ADMIN_PATH };
    const page = `/${parts.slice(0, 2).join("/")}`;
    const pageLabel = labelOf(adminPlaces, page);
    return parts.length > 2 && pageLabel ? [admin, { label: pageLabel, to: page }] : [admin];
  }
  if (top === "/portal") {
    if (parts[1] === "articles") return [{ label: t.desk.portal.raise, to: "/portal/new" }];
    if (parts.length > 1) return [{ label: t.desk.portal.yourRequests, to: "/portal" }];
    return [];
  }
  if (parts.length > 1) {
    const label = labelOf(workPlaces, top);
    if (label) return [{ label, to: top }];
  }
  return [];
}

/** Where the reader is, read off the address and drawn in the page's head in the sticky strip. */
export function PlaceCrumbs() {
  const pathname = useRouterState({ select: (s) => s.location.pathname });
  const key = projectOfPath(pathname);
  const { data } = useProject(key ?? "");
  const crumbs: Crumb[] = crumbsFor(pathname, { projectName: data?.project?.name }).map(({ label, to }) => ({
    label,
    render: (shown: ReactNode) => (
      <Link to={to as never} className="hover:text-ink">
        {shown}
      </Link>
    ),
  }));
  return <Breadcrumbs crumbs={crumbs} className="mb-1" />;
}
