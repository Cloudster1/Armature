import { keepPreviousData, useQuery, useQueryClient } from "@tanstack/react-query";
import { useMutation } from "./mutation";
import { request } from "./client";
import type { Status } from "./issues";
import { LIBRARY_PAGE_SIZE, META_STALE_MS } from "@/config";

/**
 * Workflows are configured twice over. The organization keeps one scheme that
 * answers for every project; a project may name its own, which is consulted
 * first and need only cover the issue types it disagrees about.
 */
export type Scope = "project" | "tenant";

export interface WorkflowSummary {
  id: string;
  name: string;
  description?: string;
  stepCount: number;
  transitionCount: number;
  schemeCount: number;
  /** Mapped by the organization's own scheme, so every project inherits it. */
  inDefault: boolean;
}

/** Where the designer drew a status, in canvas pixels. */
export interface Point {
  x: number;
  y: number;
}

export interface Step {
  id: string;
  status: Status;
  isInitial: boolean;
  position: number;
  /** Absent until somebody has placed the status on the canvas. */
  layout?: Point;
}

export type RuleKind = "condition" | "validator" | "postfunction";

/** One configured rule on a transition, as the server stores it. */
export interface Rule {
  id: string;
  kind: RuleKind;
  type: string;
  config?: Record<string, unknown>;
  position: number;
}

export type OptionKind = "text" | "roles";

/** One configurable value of a rule type. */
export interface RuleOption {
  name: string;
  label: string;
  kind: OptionKind;
  required: boolean;
  /** The values a roles option may take. */
  choices: string[];
}

/** A rule the engine can run, described for the designer's picker. */
export interface RuleType {
  kind: RuleKind;
  type: string;
  label: string;
  description: string;
  options: RuleOption[];
}

export interface Transition {
  id: string;
  name: string;
  description?: string;
  /** Absent for a transition available from every status. */
  fromStepId?: string;
  toStepId: string;
  position: number;
}

export interface Workflow {
  id: string;
  name: string;
  description?: string;
  steps: Step[];
  transitions: Transition[];
}

export interface SchemeItem {
  issueTypeId?: string;
  issueTypeName?: string;
  workflowId: string;
  workflowName: string;
}

export interface Scheme {
  id: string;
  name: string;
  isDefault: boolean;
  items: SchemeItem[];
  projectKeys: string[];
}

export interface Origin {
  scope: Scope;
  schemeId: string;
  schemeName: string;
  /** The scheme maps this issue type by name rather than by its fallback. */
  named: boolean;
}

export interface Assignment {
  issueTypeId: string;
  issueTypeName: string;
  workflowId: string;
  workflowName: string;
  origin: Origin;
}

export interface ProjectWorkflows {
  projectKey: string;
  /** The project's own scheme, absent when it follows the organization. */
  schemeId?: string | null;
  /** What the project's own scheme is called, absent when it has none. */
  schemeName?: string;
  defaultSchemeName: string;
  assignments: Assignment[];
}

/** One page of a library listing, with how many matched in all. */
interface Paged {
  total: number;
  limit: number;
  offset: number;
}

export interface WorkflowPage extends Paged {
  workflows: WorkflowSummary[];
}

export interface SchemePage extends Paged {
  schemes: Scheme[];
}

export interface LibraryParams {
  /** Text the name contains. */
  q?: string;
  offset?: number;
  limit?: number;
}

export const workflowsQueryKey = ["workflows"] as const;

function libraryQuery(path: string, { q = "", offset = 0, limit = LIBRARY_PAGE_SIZE }: LibraryParams) {
  const params = new URLSearchParams({ limit: String(limit) });
  if (q.trim()) params.set("q", q.trim());
  if (offset) params.set("offset", String(offset));
  return `${path}?${params}`;
}

/** A page of the workflows, searched by name; a page stays on screen while the next loads. */
export function useWorkflows(params: LibraryParams = {}) {
  const { q = "", offset = 0, limit = LIBRARY_PAGE_SIZE } = params;
  return useQuery({
    queryKey: [...workflowsQueryKey, "list", { q: q.trim(), offset, limit }],
    queryFn: () => request<WorkflowPage>(libraryQuery("/workflows", params)),
    placeholderData: keepPreviousData,
  });
}

/** A workflow with its rules, keyed by transition id. */
export interface WorkflowDetail {
  workflow: Workflow;
  rules: Record<string, Rule[]>;
}

/** One workflow's query, shared by the hook and a caller that needs it only on demand. */
export function workflowQuery(id: string) {
  return {
    queryKey: [...workflowsQueryKey, id],
    queryFn: () => request<WorkflowDetail>(`/workflows/${id}`),
  };
}

export function useWorkflow(id: string) {
  return useQuery({ ...workflowQuery(id), enabled: Boolean(id) });
}

export function useRuleTypes() {
  return useQuery({
    queryKey: [...workflowsQueryKey, "rule-types"],
    queryFn: () => request<{ ruleTypes: RuleType[] }>("/workflows/rule-types"),
    staleTime: META_STALE_MS,
  });
}

/** A page of the schemes, the default first, searched by name. */
export function useSchemes(params: LibraryParams = {}) {
  const { q = "", offset = 0, limit = LIBRARY_PAGE_SIZE } = params;
  return useQuery({
    queryKey: [...workflowsQueryKey, "schemes", { q: q.trim(), offset, limit }],
    queryFn: () => request<SchemePage>(libraryQuery("/workflow-schemes", params)),
    placeholderData: keepPreviousData,
  });
}

/** Which workflow each type follows in one project, for the hook and an on-demand caller alike. */
export function projectWorkflowsQuery(projectKey: string) {
  return {
    queryKey: [...workflowsQueryKey, "project", projectKey],
    queryFn: () => request<ProjectWorkflows>(`/projects/${projectKey}/workflows`),
  };
}

export function useProjectWorkflows(projectKey: string) {
  return useQuery({ ...projectWorkflowsQuery(projectKey), enabled: Boolean(projectKey) });
}

export interface RuleInput {
  kind: RuleKind;
  type: string;
  config?: Record<string, unknown>;
}

export interface GraphInput {
  name: string;
  description?: string;
  steps: Array<{ statusId: string; isInitial: boolean; layout?: Point }>;
  transitions: Array<{
    id?: string;
    name: string;
    description?: string;
    fromStatusId: string | null;
    toStatusId: string;
    /** Sent, these replace the transition's rules; left out, it keeps them. */
    rules?: RuleInput[];
  }>;
}

export interface SchemeInput {
  name: string;
  items: Array<{ issueTypeId: string | null; workflowId: string }>;
}

/**
 * Every write here can change which workflow an issue is in, and so which moves
 * it offers and which swimlane its board puts it in. Nothing is spared, because
 * a stale transition menu is a button that fails when it is pressed.
 */
function useConfigMutation<TArgs, TResult>(run: (args: TArgs) => Promise<TResult>) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: run,
    onSuccess: () => queryClient.invalidateQueries(),
  });
}

export function useCreateWorkflow() {
  return useConfigMutation((input: GraphInput) =>
    request<{ workflow: Workflow }>("/workflows", { method: "POST", body: input }),
  );
}

export function useSaveWorkflow() {
  return useConfigMutation(({ id, ...input }: GraphInput & { id: string }) =>
    request<{ workflow: Workflow }>(`/workflows/${id}`, { method: "PUT", body: input }),
  );
}

export function useCopyWorkflow() {
  return useConfigMutation(({ id, name }: { id: string; name: string }) =>
    request<{ workflow: Workflow }>(`/workflows/${id}/copy`, { method: "POST", body: { name } }),
  );
}

export function useDeleteWorkflow() {
  return useConfigMutation((id: string) => request<void>(`/workflows/${id}`, { method: "DELETE" }));
}

export function useCreateScheme() {
  return useConfigMutation((input: SchemeInput) =>
    request<{ scheme: Scheme }>("/workflow-schemes", { method: "POST", body: input }),
  );
}

export function useSaveScheme() {
  return useConfigMutation(({ id, ...input }: SchemeInput & { id: string }) =>
    request<{ scheme: Scheme }>(`/workflow-schemes/${id}`, { method: "PUT", body: input }),
  );
}

export function useSetDefaultScheme() {
  return useConfigMutation((id: string) =>
    request<void>(`/workflow-schemes/${id}/default`, { method: "PUT", body: {} }),
  );
}

export function useDeleteScheme() {
  return useConfigMutation((id: string) =>
    request<void>(`/workflow-schemes/${id}`, { method: "DELETE" }),
  );
}

/** One row of the project's table: a workflow for the type, or null to follow the organization. */
export function useSetProjectAssignment() {
  return useConfigMutation(
    ({ projectKey, issueTypeId, workflowId }: { projectKey: string; issueTypeId: string; workflowId: string | null }) =>
      request<ProjectWorkflows>(`/projects/${projectKey}/workflow-assignments/${issueTypeId}`, {
        method: "PUT",
        body: { workflowId },
      }),
  );
}

/** A null scheme hands the decision back to the organization. */
export function useSetProjectScheme() {
  return useConfigMutation(({ projectKey, schemeId }: { projectKey: string; schemeId: string | null }) =>
    request(`/projects/${projectKey}/workflow-scheme`, { method: "PUT", body: { schemeId } }),
  );
}
