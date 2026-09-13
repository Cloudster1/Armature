import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { request } from "./client";
import { META_STALE_MS } from "@/config";

export type ProjectKind = "software" | "service" | "business";

/** A page a project may have, named by its route slug. */
export type Feature =
  | "board"
  | "sprints"
  | "plan"
  | "calendar"
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
  issueCount: number;
  openIssueCount: number;
  /** The latest status update, absent until somebody posts one. */
  status?: StatusUpdate;
  /** Whether the desk's door asks for a code by mail; always true off a desk. */
  portalVerifies: boolean;
  /** The address domains the desk takes requests from; empty means everyone. */
  trustedDomains: string[];
  /** The pages the project has, from its template and its administrators. */
  features: Feature[];
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

export function useProjects(includeArchived = false) {
  return useQuery({
    queryKey: [...projectsQueryKey, { includeArchived }],
    queryFn: () =>
      request<{ projects: Project[] }>(`/projects${includeArchived ? "?archived=true" : ""}`),
  });
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
  features?: Feature[];
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
