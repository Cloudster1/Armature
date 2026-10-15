import { keepPreviousData, useQuery, useQueryClient } from "@tanstack/react-query";
import { useMutation } from "./mutation";
import { request } from "./client";
import { META_STALE_MS, PROJECT_PAGE_SIZE, PROJECT_PICKER_RESULTS } from "@/config";

export type ProjectKind = "software" | "service" | "business";

/** How a project plans: in sprints, by team, or flowing, by team or by person. */
export type PlanningMethod = "scrum" | "kanban";
export type ResourceGrouping = "team" | "person";

/** A page a project may have, named by its route slug. */
export type Feature =
  | "board"
  | "sprints"
  | "plan"
  | "calendar"
  | "resources"
  | "milestones"
  | "releases"
  | "components"
  | "hierarchy"
  | "dashboard"
  | "queues"
  | "desk"
  | "teams"
  | "repositories"
  | "automation"
  | "import";

export interface Project {
  id: string;
  key: string;
  name: string;
  description: string;
  kind: ProjectKind;
  leadId?: string;
  leadName?: string;
  /** The project's own scheme; absent when it follows the organization's. */
  workflowSchemeId?: string;
  /** The template the project was made from, or absent for one made without. */
  template?: string;
  /** Where the project's documentation lives, shown in its sidebar; absent when it has none. */
  docsUrl?: string;
  /** What the sidebar calls that link; "Docs" when absent. */
  docsLabel?: string;
  issueCount: number;
  openIssueCount: number;
  /** The latest status update, absent until somebody posts one. */
  status?: StatusUpdate;
  /** Whether the desk's door asks for a code by mail; always true off a desk. */
  portalVerifies: boolean;
  /** The address domains the desk takes requests from; empty means everyone. */
  trustedDomains: string[];
  /** What a new mail to the desk's own address becomes; absent when mail raises nothing. */
  mailRequestTypeId?: string;
  /** The pages the project has, from its template and its administrators. */
  features: Feature[];
  /** What the resource view sets the work against; a scrum project always plans by team. */
  planningMethod: PlanningMethod;
  resourceGrouping: ResourceGrouping;
  createdAt: string;
  updatedAt: string;
  archivedAt?: string;
}

/** How a project says it is doing, in the three words everybody uses. */
export type Health = "on_track" | "at_risk" | "off_track";

export interface StatusUpdate {
  id: string;
  projectKey: string;
  status: Health;
  note: string;
  targetOn?: string;
  authorId?: string;
  authorName: string;
  createdAt: string;
}

export function useStatusUpdates(projectKey: string) {
  return useQuery({
    queryKey: [...projectsQueryKey, projectKey, "status-updates"],
    queryFn: () => request<{ updates: StatusUpdate[] }>(`/projects/${projectKey}/status-updates`),
    enabled: Boolean(projectKey),
  });
}

export function usePostStatusUpdate() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({ projectKey, ...body }: { projectKey: string; status: Health; note?: string; targetOn?: string }) =>
      request<{ update: StatusUpdate }>(`/projects/${projectKey}/status-updates`, { method: "POST", body }),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: projectsQueryKey }),
  });
}

export const projectsQueryKey = ["projects"] as const;

/** One page of the projects somebody can see, and how many match in all. */
export interface ProjectPage {
  projects: Project[];
  total: number;
  limit: number;
  offset: number;
}

export interface ProjectListParams {
  /** Text the key or the name contains. */
  q?: string;
  offset?: number;
  limit?: number;
  includeArchived?: boolean;
}

/** A page of projects by key; the server never sends them all at once. */
export function useProjects({ q = "", offset = 0, limit = PROJECT_PAGE_SIZE, includeArchived = false }: ProjectListParams = {}) {
  const text = q.trim();
  return useQuery({
    queryKey: [...projectsQueryKey, { q: text, offset, limit, includeArchived }],
    queryFn: () => {
      const params = new URLSearchParams({ limit: String(limit) });
      if (text) params.set("q", text);
      if (offset) params.set("offset", String(offset));
      if (includeArchived) params.set("archived", "true");
      return request<ProjectPage>(`/projects?${params}`);
    },
    placeholderData: keepPreviousData,
  });
}

/** The first projects whose key or name holds the text, for a picker or a switcher. */
export function useProjectSearch(q: string, limit = PROJECT_PICKER_RESULTS) {
  return useProjects({ q, limit });
}

export function useProject(key: string) {
  return useQuery({
    queryKey: [...projectsQueryKey, key],
    queryFn: () => request<{ project: Project }>(`/projects/${key}`),
    enabled: Boolean(key),
  });
}

export interface CreateProjectInput {
  name: string;
  key?: string;
  description?: string;
  kind?: ProjectKind;
  /** Which template to set the project up from; the server has a default. */
  template?: string;
}

/**
 * A way to set a project up: what kind it is, what its first board is, and
 * whether it brings a workflow of its own or follows the organization's.
 */
export interface ProjectTemplate {
  key: string;
  name: string;
  description: string;
  kind: ProjectKind;
  boardType: "scrum" | "kanban";
  planningMethod: PlanningMethod;
  workflowName?: string;
  /** The pages a project made from it starts with. */
  features: Feature[];
}

export function useProjectTemplates() {
  return useQuery({
    queryKey: ["project-templates"],
    queryFn: () => request<{ templates: ProjectTemplate[] }>("/project-templates"),
    staleTime: META_STALE_MS,
  });
}

export function useCreateProject() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (input: CreateProjectInput) =>
      request<{ project: Project }>("/projects", { method: "POST", body: input }),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: projectsQueryKey }),
  });
}

export interface KeyCheck {
  key?: string;
  valid?: boolean;
  available?: boolean;
  suggestion?: string;
}

/**
 * Asks the server for a key suggestion, or whether one is free. Kept as a
 * query rather than computed client side so the two never disagree about what
 * a valid key is.
 */
export function useKeyCheck(params: { name?: string; key?: string }) {
  const search = new URLSearchParams();
  if (params.key) search.set("key", params.key);
  if (params.name) search.set("name", params.name);

  return useQuery({
    queryKey: ["projects", "key-check", params.key ?? "", params.name ?? ""],
    queryFn: () => request<KeyCheck>(`/projects/key-check?${search.toString()}`),
    enabled: Boolean(params.key || params.name),
    staleTime: 5_000,
  });
}

export interface UpdateProjectInput {
  key: string;
  name?: string;
  description?: string;
  /** Null clears the lead; absent leaves it. */
  leadId?: string | null;
  portalVerifies?: boolean;
  trustedDomains?: string[];
  /** Null stops mail raising requests; absent leaves it. */
  mailRequestTypeId?: string | null;
  features?: Feature[];
  /** An empty string clears the link and its label. */
  docsUrl?: string;
  docsLabel?: string;
  /** Turning to scrum takes the grouping back to teams. */
  planningMethod?: PlanningMethod;
  resourceGrouping?: ResourceGrouping;
}

export function useUpdateProject() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({ key, ...body }: UpdateProjectInput) => request<{ project: Project }>(`/projects/${key}`, { method: "PATCH", body }),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: projectsQueryKey }),
  });
}

/** Archiving hides a project and its issues; nothing is deleted. */
export function useArchiveProject() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (key: string) => request<void>(`/projects/${key}`, { method: "DELETE" }),
    onSuccess: () => queryClient.invalidateQueries(),
  });
}

/** Restoring brings an archived project and its issues back into every list. */
export function useRestoreProject() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (key: string) => request<void>(`/projects/${key}/restore`, { method: "POST" }),
    onSuccess: () => queryClient.invalidateQueries(),
  });
}
