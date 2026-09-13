import type { Feature, Project } from "@/api/projects";

/** Every feature in the order pages are listed, with the word a sentence uses. */
export const FEATURES: Array<{ key: Feature; label: string; word: string; deskOnly?: boolean }> = [
  { key: "board", label: "Board", word: "the board" },
  { key: "sprints", label: "Sprints", word: "sprints" },
  { key: "plan", label: "Plan", word: "the plan" },
  { key: "calendar", label: "Calendar", word: "the calendar" },
  { key: "milestones", label: "Milestones", word: "milestones" },
  { key: "releases", label: "Releases", word: "releases" },
  { key: "components", label: "Components", word: "components" },
  { key: "hierarchy", label: "Hierarchy", word: "the hierarchy" },
  { key: "dashboard", label: "Dashboard", word: "dashboards" },
  { key: "queues", label: "Queues", word: "queues", deskOnly: true },
  { key: "desk", label: "Service desk", word: "the service desk", deskOnly: true },
  { key: "teams", label: "Teams", word: "teams" },
  { key: "repositories", label: "Repositories", word: "repositories" },
  { key: "automation", label: "Automation", word: "automation" },
  { key: "import", label: "Import", word: "imports" },
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
  return FEATURES.filter((f) => !f.deskOnly && !features.includes(f.key)).map((f) => f.label.toLowerCase());
}
