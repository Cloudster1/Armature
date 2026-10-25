import type { Feature, Project } from "@/api/projects";
import { t } from "@/i18n";

/** Every feature in the order pages are listed, with the word a sentence uses. */
export const FEATURES: Array<{ key: Feature; label: string; word: string; listed: string; deskOnly?: boolean }> = [
  {
    key: "board",
    get label() {
      return t.projectSetup.features.board.label;
    },
    get word() {
      return t.projectSetup.features.board.word;
    },
    get listed() {
      return t.projectSetup.features.board.listed;
    },
  },
  {
    key: "sprints",
    get label() {
      return t.projectSetup.features.sprints.label;
    },
    get word() {
      return t.projectSetup.features.sprints.word;
    },
    get listed() {
      return t.projectSetup.features.sprints.listed;
    },
  },
  {
    key: "plan",
    get label() {
      return t.projectSetup.features.plan.label;
    },
    get word() {
      return t.projectSetup.features.plan.word;
    },
    get listed() {
      return t.projectSetup.features.plan.listed;
    },
  },
  {
    key: "calendar",
    get label() {
      return t.projectSetup.features.calendar.label;
    },
    get word() {
      return t.projectSetup.features.calendar.word;
    },
    get listed() {
      return t.projectSetup.features.calendar.listed;
    },
  },
  {
    key: "resources",
    get label() {
      return t.projectSetup.features.resources.label;
    },
    get word() {
      return t.projectSetup.features.resources.word;
    },
    get listed() {
      return t.projectSetup.features.resources.listed;
    },
  },
  {
    key: "milestones",
    get label() {
      return t.projectSetup.features.milestones.label;
    },
    get word() {
      return t.projectSetup.features.milestones.word;
    },
    get listed() {
      return t.projectSetup.features.milestones.listed;
    },
  },
  {
    key: "releases",
    get label() {
      return t.projectSetup.features.releases.label;
    },
    get word() {
      return t.projectSetup.features.releases.word;
    },
    get listed() {
      return t.projectSetup.features.releases.listed;
    },
  },
  {
    key: "components",
    get label() {
      return t.projectSetup.features.components.label;
    },
    get word() {
      return t.projectSetup.features.components.word;
    },
    get listed() {
      return t.projectSetup.features.components.listed;
    },
  },
  {
    key: "hierarchy",
    get label() {
      return t.projectSetup.features.hierarchy.label;
    },
    get word() {
      return t.projectSetup.features.hierarchy.word;
    },
    get listed() {
      return t.projectSetup.features.hierarchy.listed;
    },
  },
  {
    key: "dashboard",
    get label() {
      return t.projectSetup.features.dashboard.label;
    },
    get word() {
      return t.projectSetup.features.dashboard.word;
    },
    get listed() {
      return t.projectSetup.features.dashboard.listed;
    },
  },
  {
    key: "queues",
    get label() {
      return t.projectSetup.features.queues.label;
    },
    get word() {
      return t.projectSetup.features.queues.word;
    },
    get listed() {
      return t.projectSetup.features.queues.listed;
    },
    deskOnly: true,
  },
  {
    key: "desk",
    get label() {
      return t.projectSetup.features.desk.label;
    },
    get word() {
      return t.projectSetup.features.desk.word;
    },
    get listed() {
      return t.projectSetup.features.desk.listed;
    },
    deskOnly: true,
  },
  {
    key: "teams",
    get label() {
      return t.projectSetup.features.teams.label;
    },
    get word() {
      return t.projectSetup.features.teams.word;
    },
    get listed() {
      return t.projectSetup.features.teams.listed;
    },
  },
  {
    key: "repositories",
    get label() {
      return t.projectSetup.features.repositories.label;
    },
    get word() {
      return t.projectSetup.features.repositories.word;
    },
    get listed() {
      return t.projectSetup.features.repositories.listed;
    },
  },
  {
    key: "automation",
    get label() {
      return t.projectSetup.features.automation.label;
    },
    get word() {
      return t.projectSetup.features.automation.word;
    },
    get listed() {
      return t.projectSetup.features.automation.listed;
    },
  },
  {
    key: "import",
    get label() {
      return t.projectSetup.features.import.label;
    },
    get word() {
      return t.projectSetup.features.import.word;
    },
    get listed() {
      return t.projectSetup.features.import.listed;
    },
  },
];

export function featureWord(feature: Feature): string {
  return FEATURES.find((f) => f.key === feature)?.word ?? feature;
}

/** The route slug under a project that each feature owns; desk pages differ from their names. */
const slugs: Record<string, Feature> = {
  board: "board",
  sprints: "sprints",
  plan: "plan",
  calendar: "calendar",
  resources: "resources",
  milestones: "milestones",
  releases: "releases",
  components: "components",
  hierarchy: "hierarchy",
  dashboard: "dashboard",
  queues: "queues",
  "service-desk": "desk",
  teams: "teams",
  repositories: "repositories",
  automation: "automation",
  import: "import",
};

/** Which feature a path under a project belongs to, or undefined for a page every project has. */
export function featureOfPath(projectKey: string, pathname: string): Feature | undefined {
  const prefix = `/projects/${projectKey}/`;
  if (!pathname.startsWith(prefix)) return undefined;
  const slug = pathname.slice(prefix.length).split("/")[0] ?? "";
  return slugs[slug];
}

/** Whether the project has the feature; a project still loading is given the benefit of the doubt. */
export function hasFeature(project: Project | undefined, feature: Feature | undefined): boolean {
  if (!feature || !project) return true;
  return project.features.includes(feature);
}

/** The words for what a feature list leaves out, against everything a non-desk project could have. */
export function leftOut(features: Feature[]): string[] {
  return FEATURES.filter((f) => !f.deskOnly && !features.includes(f.key)).map((f) => f.listed);
}
