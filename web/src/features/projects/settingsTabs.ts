import type { Feature, Project } from "@/api/projects";
import { hasFeature } from "@/features/projects/features";
import type { IconName } from "@/components/icons";
import { t } from "@/i18n";

export interface PageLink {
  path: string;
  label: string;
  icon: IconName;
  exact?: boolean;
  /** The feature the page belongs to; absent for a page every project has. */
  feature?: Feature;
}

/** The suite's sections of a project's settings, in their order. */
export type SettingsSection = "general" | "work" | "workflow" | "integrations" | "import";

export interface SetupPage extends PageLink {
  tab: string;
  section: SettingsSection;
  /** Only its administrators use it, so the others are not shown it. */
  adminOnly?: boolean;
}

/**
 * A project's settings, one tab per page, in the order of the suite's sections:
 * General, Work items, Workflow, Integrations, Import and export. Every member
 * opens them; what only administrators use is marked so.
 */
export const projectSetupPages: SetupPage[] = [
  {
    path: "/settings",
    section: "general",
    get label() {
      return t.shell.projectPages.settings;
    },
    get tab() {
      return t.projectSetup.settings.general;
    },
    icon: "Settings",
  },
  {
    path: "/issue-types",
    section: "work",
    get label() {
      return t.shell.projectPages.issueTypes;
    },
    get tab() {
      return t.shell.projectPages.issueTypes;
    },
    icon: "Tag",
  },
  {
    path: "/fields",
    section: "work",
    get label() {
      return t.shell.projectPages.fields;
    },
    get tab() {
      return t.shell.projectPages.fields;
    },
    icon: "Field",
  },
  {
    path: "/issue-view",
    section: "work",
    get label() {
      return t.shell.projectPages.issueView;
    },
    get tab() {
      return t.shell.projectPages.issueView;
    },
    icon: "Issue",
  },
  {
    path: "/workflows",
    section: "workflow",
    get label() {
      return t.shell.projectPages.workflow;
    },
    get tab() {
      return t.shell.projectPages.workflow;
    },
    icon: "Workflow",
  },
  {
    path: "/components",
    section: "integrations",
    get label() {
      return t.shell.projectPages.components;
    },
    get tab() {
      return t.shell.projectPages.components;
    },
    icon: "Component",
    feature: "components",
  },
  {
    path: "/repositories",
    section: "integrations",
    get label() {
      return t.shell.projectPages.repositories;
    },
    get tab() {
      return t.shell.projectPages.repositories;
    },
    icon: "Repository",
    feature: "repositories",
  },
  {
    path: "/automation",
    section: "integrations",
    adminOnly: true,
    get label() {
      return t.shell.projectPages.automation;
    },
    get tab() {
      return t.shell.projectPages.automation;
    },
    icon: "Bolt",
    feature: "automation",
  },
  {
    path: "/import",
    section: "import",
    adminOnly: true,
    get label() {
      return t.shell.projectPages.import;
    },
    get tab() {
      return t.shell.projectPages.import;
    },
    icon: "Upload",
    feature: "import",
  },
];

/** The entry among a project's pages that opens its settings. */
export const projectSettingsPage = projectSetupPages[0]!;

/** Where a setup page lives under the project: General is the settings page itself, the rest are tabs of it. */
export function setupAddress(path: string): string {
  return path === projectSettingsPage.path ? path : `/settings${path}`;
}

/** The settings pages somebody is shown: those the project's features include, and the administrators' only to them. */
export function settingsPagesFor(project: Project | undefined, administers: boolean): SetupPage[] {
  return projectSetupPages.filter((page) => hasFeature(project, page.feature) && (administers || !page.adminOnly));
}

/** The old project addresses of the pages that became tabs. */
export const MOVED_PROJECT_PAGES: string[] = projectSetupPages.map((page) => page.path).filter((path) => path !== projectSettingsPage.path);

/** Where an old address such as /projects/KEY/teams now lives, with its search and hash kept. */
export function movedProjectAddress(projectKey: string, slug: string, searchStr = "", hash = ""): string | undefined {
  if (!MOVED_PROJECT_PAGES.includes(`/${slug}`)) return undefined;
  return `/projects/${projectKey}${setupAddress(`/${slug}`)}${searchStr}${hash ? `#${hash}` : ""}`;
}
