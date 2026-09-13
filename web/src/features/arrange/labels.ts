import type { Area, Slot } from "@/api/arrange";

/** What each part of the page is called to somebody arranging it. */
export const AREA_LABELS: { area: Area; title: string; note: string }[] = [
  { area: "main", title: "Beside the description", note: "Under the description, the full width of the page." },
  { area: "people", title: "People", note: "" },
  { area: "planning", title: "Planning", note: "" },
  { area: "tracking", title: "Tracking", note: "" },
  { area: "more", title: "Fields", note: "" },
  { area: "hidden", title: "Not shown", note: "Kept on the issue, shown nowhere." },
];

/** What each field this tracker knows is called on the page. */
export const SLOT_LABELS: Record<Slot, string> = {
  description: "Description",
  assignee: "Assignee",
  reporter: "Reporter",
  parent: "Parent",
  schedule: "Scheduled",
  sprint: "Sprint",
  milestone: "Milestone",
  fixVersions: "Fix versions",
  affectsVersions: "Affects versions",
  components: "Components",
  estimate: "Estimate",
  team: "Team",
  request: "Request",
  goals: "Goals",
  priority: "Priority",
  labels: "Labels",
  time: "Time",
  otherFields: "The project's other fields",
  created: "Created",
  resolved: "Resolved",
};

/** Who decided the arrangement being looked at. */
export const ORIGIN_WORDS: Record<string, string> = {
  builtin: "As this tracker draws it",
  organization: "The organization's",
  project: "This project's own",
};
