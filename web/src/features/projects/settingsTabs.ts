import type { Feature } from "@/api/projects";
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

/** A project's setup pages, for its administrators: General first, then one tab for each thing the project is set up with. */
export const projectSetupPages: Array<PageLink & { tab: string }> = [
  {
    path: "/settings",
    get label() {
      return t.shell.projectPages.settings;
    },
    get tab() {
      return t.projectSetup.settings.general;
    },
    icon: "Settings",
  },
  {
    path: "/workflows",
    get label() {
      return t.shell.projectPages.workflow;
    },
    get tab() {
      return t.shell.projectPages.workflow;
    },
    icon: "Workflow",
  },
  {
    path: "/fields",
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
    get label() {
      return t.shell.projectPages.issueView;
    },
    get tab() {
      return t.shell.projectPages.issueView;
    },
    icon: "Issue",
  },
  {
    path: "/components",
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

/** The old project addresses of the pages that became tabs. */
export const MOVED_PROJECT_PAGES: string[] = projectSetupPages.map((page) => page.path).filter((path) => path !== projectSettingsPage.path);

/** Where an old address such as /projects/KEY/teams now lives, with its search and hash kept. */
export function movedProjectAddress(projectKey: string, slug: string, searchStr = "", hash = ""): string | undefined {
  if (!MOVED_PROJECT_PAGES.includes(`/${slug}`)) return undefined;
  return `/projects/${projectKey}${setupAddress(`/${slug}`)}${searchStr}${hash ? `#${hash}` : ""}`;
}
