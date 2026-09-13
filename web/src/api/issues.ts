import { keepPreviousData, useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { request } from "./client";
import { META_STALE_MS } from "@/config";

export type StatusCategory = "todo" | "in_progress" | "done";
export type Priority = "lowest" | "low" | "medium" | "high" | "highest";

/** Most urgent first, the order every picker offers them in. */
export const PRIORITIES: Priority[] = ["highest", "high", "medium", "low", "lowest"];

export interface Status {
  id: string;
  name: string;
  category: StatusCategory;
  description?: string;
  position: number;
}

export interface IssueType {
  id: string;
  name: string;
  description?: string;
  icon: string;
  /** Where the type sits in the hierarchy: 0 is the ordinary working level. */
  level: number;
  isSubtask: boolean;
  position: number;
}

export const LEVEL_SUBTASK = -1;
export const LEVEL_STANDARD = 0;
export const LEVEL_EPIC = 1;

export interface TypeRef {
  id: string;
  name: string;
  icon: string;
  level: number;
  isSubtask: boolean;
}

export interface ParentRef {
  id: string;
  key: string;
  summary: string;
  type: TypeRef;
}

export interface UserRef {
  id: string;
  name: string;
  email?: string;
  /** Where the picture is served from; absent means initials. */
  avatarUrl?: string;
}

/** A rich text document, in the shape the editor and the API agree on. */
export type Doc = { type: "doc"; content: unknown[] };

/** A label as it appears on an issue. */
export interface LabelRef {
  id: string;
  name: string;
  color: LabelColor;
}

export type LabelColor = "gray" | "red" | "orange" | "amber" | "green" | "teal" | "blue" | "purple" | "pink";

/** One stretch of somebody's time on an issue. */
export interface Worklog {
  id: string;
  issueId: string;
  author?: UserRef;
  minutes: number;
  /** The day the work was done. */
  startedOn: string;
  note: string;
  createdAt: string;
  updatedAt: string;
}

/** The shape a sprint takes when it hangs off an issue. */
export interface SprintRef {
  id: string;
  name: string;
  state: "future" | "active" | "closed";
}

export interface Issue {
  id: string;
  key: string;
  type: TypeRef;
  projectId: string;
  projectKey: string;
  summary: string;
  description?: Doc;
  status: Status;
  priority: Priority;
  assignee?: UserRef;
  reporter?: UserRef;
  parentId?: string;
  parentKey?: string;
  parent?: ParentRef;
  /** The two ends of the stretch of time this occupies, if it is scheduled. */
  startDate?: string;
  dueDate?: string;
  /** The sprint this is committed to; absent means the backlog. */
  sprintId?: string;
  sprint?: SprintRef;
  /** The milestone this counts towards, if any. */
  milestoneId?: string;
  milestone?: { id: string; name: string };
  /** How much work this is. Absent is unestimated, which is not zero. */
  estimate?: number;
  /** The team carrying this; absent is work the project has not handed out. */
  teamId?: string;
  team?: { id: string; name: string };
  /** Set on a request raised through the customer portal. */
  requestTypeId?: string;
  requestTypeName?: string;
  /** Minutes: how long it was thought to take, how long is left, and the work logged. */
  timeEstimateMinutes?: number;
  timeRemainingMinutes?: number;
  timeSpentMinutes: number;
  /** The organization's words on this issue. */
  labels: LabelRef[];
  /** What ships this, and where it was found. */
  fixVersions: Array<{ id: string; name: string; released: boolean }>;
  affectsVersions: Array<{ id: string; name: string; released: boolean }>;
  components: Array<{ id: string; name: string }>;
  createdAt: string;
  updatedAt: string;
  resolvedAt?: string;
  childCount?: number;
  commentCount?: number;
}

/** How the work directly underneath an issue is getting on. */
export interface Progress {
  total: number;
  done: number;
  inProgress: number;
  todo: number;
}

export interface TreeNode {
  issue: Issue;
  progress: Progress;
  children: TreeNode[];
}

export interface Hierarchy {
  ancestors: Issue[];
  issue: Issue;
  children: TreeNode[];
  progress: Progress;
  childTypes: Array<{ id: string; name: string; icon: string; level: number }>;
}

export interface Transition {
  id: string;
  name: string;
  fromStepId?: string;
  toStepId: string;
  description?: string;
  position: number;
}

export interface Comment {
  id: string;
  issueId: string;
  /** A note between agents, never shown to a customer. */
  internal?: boolean;
  author?: UserRef;
  body: Doc;
  createdAt: string;
  editedAt?: string;
}

export interface Change {
  field: string;
  from?: string;
  to?: string;
}

export interface HistoryEntry {
  id: string;
  issueId: string;
  actor?: UserRef;
  changes: Change[];
  createdAt: string;
}

export interface IssueQuery {
  project?: string;
  assignee?: string;
  /** Label ids; issues carrying any of them. */
  label?: string[];
  /** Only the issues counting towards this milestone. */
  milestone?: string;
  category?: StatusCategory[];
  text?: string;
  /** An NQL query, ANDed with the rest; its ORDER BY outranks orderBy. */
  q?: string;
  orderBy?: string;
  limit?: number;
  offset?: number;
}

function issueSearch(query: IssueQuery): string {
  const search = new URLSearchParams();
  if (query.assignee) search.set("assignee", query.assignee);
  if (query.text) search.set("text", query.text);
  if (query.q) search.set("q", query.q);
  if (query.orderBy) search.set("orderBy", query.orderBy);
  if (query.limit) search.set("limit", String(query.limit));
  if (query.offset) search.set("offset", String(query.offset));
  for (const category of query.category ?? []) search.append("category", category);
  for (const label of query.label ?? []) search.append("label", label);
  if (query.milestone) search.set("milestone", query.milestone);
  const encoded = search.toString();
  return encoded ? `?${encoded}` : "";
}

export interface IssuePage {
  issues: Issue[];
  total: number;
  limit: number;
  offset: number;
}

export const issuesQueryKey = ["issues"] as const;

export function useIssues(query: IssueQuery) {
  const path = query.project
    ? `/projects/${query.project}/issues${issueSearch(query)}`
    : `/issues${issueSearch(query)}`;

  return useQuery({
    queryKey: [...issuesQueryKey, query],
    queryFn: () => request<IssuePage>(path),
  });
}

/** Where the caret is in a query and what could go there, as the server reads it. */
export interface Completion {
  slot: "field" | "operator" | "value" | "keyword" | "none";
  field?: string;
  custom?: boolean;
  prefix: string;
  from: number;
  to: number;
  quoted: boolean;
  words: string[];
}

export interface SuggestWord {
  text: string;
  detail: string;
}

export interface Suggestions {
  completion: Completion;
  words: SuggestWord[];
  issues: Issue[];
}

/**
 * What the search bar could say next. The last answer stays on screen while
 * the next is fetched, so the list does not flicker with every keystroke.
 */
export function useSuggestions(query: { q: string; at: number; project?: string }, enabled: boolean) {
  const params = new URLSearchParams({ q: query.q, at: String(query.at) });
  if (query.project) params.set("project", query.project);
  return useQuery({
    queryKey: [...issuesQueryKey, "suggest", query],
    queryFn: () => request<Suggestions>(`/issues/suggest?${params.toString()}`),
    enabled,
    placeholderData: keepPreviousData,
  });
}

export function useIssue(key: string) {
  return useQuery({
    queryKey: [...issuesQueryKey, "detail", key],
    queryFn: () => request<{ issue: Issue }>(`/issues/${key}`),
    enabled: Boolean(key),
  });
}

export function useTransitions(key: string) {
  return useQuery({
    queryKey: [...issuesQueryKey, "transitions", key],
    queryFn: () => request<{ transitions: Transition[] }>(`/issues/${key}/transitions`),
    enabled: Boolean(key),
  });
}

export function useComments(key: string) {
  return useQuery({
    queryKey: [...issuesQueryKey, "comments", key],
    queryFn: () => request<{ comments: Comment[] }>(`/issues/${key}/comments`),
    enabled: Boolean(key),
  });
}

export function useHistory(key: string) {
  return useQuery({
    queryKey: [...issuesQueryKey, "history", key],
    queryFn: () => request<{ history: HistoryEntry[] }>(`/issues/${key}/history`),
    enabled: Boolean(key),
  });
}

export interface CreateIssueInput {
  projectKey: string;
  summary: string;
  description?: Doc;
  typeId?: string;
  parentKey?: string;
  priority?: Priority;
  assigneeId?: string;
  /** Where the issue lands as it is made, so the plan can file it where it will sit. */
  startDate?: string;
  dueDate?: string;
  teamId?: string;
  estimate?: number;
  sprintId?: string;
  /** The project's parts the issue is filed into; the first with a default assignee takes it. */
  componentIds?: string[];
}

export function useCreateIssue() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({ projectKey, ...body }: CreateIssueInput) =>
      request<{ issue: Issue }>(`/projects/${projectKey}/issues`, { method: "POST", body }),
    // Creating an issue changes the project's counts as well as its list.
    onSuccess: () => queryClient.invalidateQueries(),
  });
}

export interface UpdateIssueInput {
  key: string;
  summary?: string;
  /** A document replaces the description; null clears it. */
  description?: Doc | null;
  priority?: Priority;
  assigneeId?: string | null;
  reporterId?: string;
  /** Minutes; null clears. */
  timeEstimateMinutes?: number | null;
  timeRemainingMinutes?: number | null;
}

export function useUpdateIssue() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({ key, ...body }: UpdateIssueInput) =>
      request<{ issue: Issue }>(`/issues/${key}`, { method: "PATCH", body }),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: issuesQueryKey }),
  });
}

export function useTransitionIssue() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({ key, transitionId, comment }: { key: string; transitionId: string; comment?: string }) =>
      request<{ issue: Issue }>(`/issues/${key}/transitions`, {
        method: "POST",
        body: { transitionId, comment },
      }),
    // A transition can change the status, the assignee, the resolution, the
    // comments and the history at once, so everything about this issue is stale.
    onSuccess: () => queryClient.invalidateQueries(),
  });
}

export function useAddComment() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({ key, body }: { key: string; body: Doc }) =>
      request<{ comment: Comment }>(`/issues/${key}/comments`, { method: "POST", body: { body } }),
    // A reply can complete a desk clock, so nothing about the issue is trusted after one.
    onSuccess: () => queryClient.invalidateQueries(),
  });
}

export function useWorklogs(key: string) {
  return useQuery({
    queryKey: [...issuesQueryKey, "worklogs", key],
    queryFn: () => request<{ worklogs: Worklog[] }>(`/issues/${key}/worklogs`),
    enabled: Boolean(key),
  });
}

export interface WorklogInput {
  minutes: number;
  startedOn?: string;
  note?: string;
}

/** Logging time changes what remains on the issue, so everything about it is refetched. */
export function useLogWork() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({ key, ...body }: WorklogInput & { key: string }) =>
      request<{ worklog: Worklog }>(`/issues/${key}/worklogs`, { method: "POST", body }),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: issuesQueryKey }),
  });
}

export function useUpdateWorklog() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({ id, ...body }: WorklogInput & { id: string }) =>
      request<{ worklog: Worklog }>(`/worklogs/${id}`, { method: "PATCH", body }),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: issuesQueryKey }),
  });
}

export function useDeleteWorklog() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (id: string) => request<void>(`/worklogs/${id}`, { method: "DELETE" }),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: issuesQueryKey }),
  });
}

/** One issue in context: what is above it, what is under it, and its roll-up. */
export function useIssueHierarchy(key: string) {
  return useQuery({
    queryKey: [...issuesQueryKey, "hierarchy", key],
    queryFn: () => request<Hierarchy>(`/issues/${key}/hierarchy`),
    enabled: Boolean(key),
  });
}

export function useProjectHierarchy(projectKey: string) {
  return useQuery({
    queryKey: [...issuesQueryKey, "tree", projectKey],
    queryFn: () => request<{ tree: TreeNode[] }>(`/projects/${projectKey}/hierarchy`),
    enabled: Boolean(projectKey),
  });
}

/** Moving an issue in the tree, or out of it: null detaches. */
export function useSetParent() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({ key, parentKey }: { key: string; parentKey: string | null }) =>
      request<{ issue: Issue }>(`/issues/${key}/parent`, { method: "PUT", body: { parentKey } }),
    // A move changes the roll-ups on both ends and the board's epic chips.
    onSuccess: () => queryClient.invalidateQueries(),
  });
}

export function useIssueTypes() {
  return useQuery({
    queryKey: ["issue-types"],
    queryFn: () => request<{ issueTypes: IssueType[] }>("/issue-types"),
    staleTime: META_STALE_MS,
  });
}

/** The organization's statuses. Workflows are built out of these, and several
 * workflows can share one, which is what lets two projects agree on the meaning
 * of "In Progress" and disagree about how an issue gets there. */
export function useStatuses() {
  return useQuery({
    queryKey: ["statuses"],
    queryFn: () => request<{ statuses: Status[] }>("/statuses"),
    staleTime: META_STALE_MS,
  });
}

export interface CreateStatusInput {
  name: string;
  category: StatusCategory;
  description?: string;
}

/** Coins a status for the organization; every workflow can then be built from it. */
export function useCreateStatus() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (input: CreateStatusInput) => request<{ status: Status }>("/statuses", { method: "POST", body: input }),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ["statuses"] }),
  });
}

export function useMembers() {
  return useQuery({
    queryKey: ["members"],
    queryFn: () => request<{ members: Array<UserRef & { role: string }> }>("/members"),
    staleTime: 60_000,
  });
}

/**
 * Renders a document down to readable text, for previews and for the places
 * that show one line of it. The editor and DocView in features/editor are
 * how a document is written and read whole.
 */
export function docToText(doc?: Doc | null): string {
  if (!doc) return "";
  const parts: string[] = [];
  const walk = (node: unknown): void => {
    if (!node || typeof node !== "object") return;
    const n = node as { text?: string; content?: unknown[]; type?: string; attrs?: { label?: string } };
    if (n.type === "mention") parts.push(`@${n.attrs?.label ?? ""}`);
    if (n.type === "hardBreak") parts.push("\n");
    if (n.text) parts.push(n.text);
    for (const child of n.content ?? []) walk(child);
    if (n.type === "paragraph" || n.type === "heading") parts.push("\n");
  };
  walk(doc);
  return parts.join("").trim();
}
