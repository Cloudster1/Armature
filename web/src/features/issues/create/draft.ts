import type { Area, Placement } from "@/api/arrange";
import type { CreateIssueInput, Priority } from "@/api/issues";
import { markdownToDoc } from "@/features/editor/markdown";

/** One custom field's answer, carrying the name a refusal would be about. */
export interface Answer {
  name: string;
  value: unknown;
}

/**
 * Every answer the form can take, whether or not the arrangement draws the
 * slot that takes it. A slot nobody placed simply leaves its answer empty.
 */
export interface Draft {
  summary: string;
  description: string;
  priority: Priority | "";
  assigneeId: string;
  reporterId: string;
  parentKey: string;
  startDate: string;
  dueDate: string;
  sprintId: string;
  milestoneId: string;
  teamId: string;
  estimate: string;
  timeEstimate: string;
  componentIds: string[];
  fixVersionIds: string[];
  affectsVersionIds: string[];
  labels: string[];
  /** One answer per custom field, by field id, in the shape the API stores. */
  values: Record<string, Answer>;
}

export function emptyDraft(): Draft {
  return {
    summary: "",
    description: "",
    priority: "",
    assigneeId: "",
    reporterId: "",
    parentKey: "",
    startDate: "",
    dueDate: "",
    sprintId: "",
    milestoneId: "",
    teamId: "",
    estimate: "",
    timeEstimate: "",
    componentIds: [],
    fixVersionIds: [],
    affectsVersionIds: [],
    labels: [],
    values: {},
  };
}

// The page's areas in the order it draws them. What the arrangement hid is not
// here, so a field nobody shows is not a field anybody is asked for.
const ASKED_AREAS: Area[] = ["main", "people", "planning", "tracking", "more"];

/** The places the form asks about, flattened in the order the page draws them. */
export function askedPlaces(places: Placement[]): Placement[] {
  return ASKED_AREAS.flatMap((area) => places.filter((place) => place.area === area));
}

/** What the create request itself can carry. The rest follows it. */
export function createBody(draft: Draft, projectKey: string, typeId: string): CreateIssueInput {
  const body: CreateIssueInput = { projectKey, summary: draft.summary.trim() };
  if (typeId) body.typeId = typeId;
  const description = markdownToDoc(draft.description);
  if (description) body.description = description;
  if (draft.priority) body.priority = draft.priority;
  if (draft.assigneeId) body.assigneeId = draft.assigneeId;
  if (draft.parentKey.trim()) body.parentKey = draft.parentKey.trim();
  if (draft.startDate) body.startDate = draft.startDate;
  if (draft.dueDate) body.dueDate = draft.dueDate;
  if (draft.sprintId) body.sprintId = draft.sprintId;
  if (draft.teamId) body.teamId = draft.teamId;
  if (draft.estimate.trim()) body.estimate = Number(draft.estimate);
  if (draft.componentIds.length > 0) body.componentIds = draft.componentIds;
  return body;
}
