import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useMutation } from "./mutation";
import { request, upload } from "./client";
import type { Issue, IssuePage, Priority } from "./issues";
import { BULK_MAX_SELECTION } from "@/config";

export interface FilterSubscription {
  schedule: "daily" | "weekly";
  hour: number;
  weekday?: number;
  lastSentAt?: string;
}

/** A saved query: the owner's, shared when they say so. Starred and subscription are the reader's own. */
export interface SavedFilter {
  id: string;
  ownerId: string;
  ownerName: string;
  name: string;
  query: string;
  shared: boolean;
  columns: string[];
  starred: boolean;
  subscription?: FilterSubscription;
  createdAt: string;
  updatedAt: string;
}

export interface SavedFilterInput {
  name?: string;
  query?: string;
  shared?: boolean;
  columns?: string[];
}

export const filtersQueryKey = ["filters"] as const;

export function useSavedFilters() {
  return useQuery({ queryKey: filtersQueryKey, queryFn: () => request<{ filters: SavedFilter[] }>("/filters") });
}

/** One saved search, which answers not found when the reader may not see it. */
export function useSavedFilter(id: string) {
  return useQuery({ queryKey: [...filtersQueryKey, id], queryFn: () => request<{ filter: SavedFilter }>(`/filters/${id}`) });
}

function useFilterMutation<TArgs, TResult>(run: (args: TArgs) => Promise<TResult>) {
  const queryClient = useQueryClient();
  return useMutation({ mutationFn: run, onSuccess: () => queryClient.invalidateQueries({ queryKey: filtersQueryKey }) });
}

export function useCreateSavedFilter() {
  return useFilterMutation((input: SavedFilterInput) => request<{ filter: SavedFilter }>("/filters", { method: "POST", body: input }));
}

export function useUpdateSavedFilter() {
  return useFilterMutation(({ id, ...input }: SavedFilterInput & { id: string }) =>
    request<{ filter: SavedFilter }>(`/filters/${id}`, { method: "PATCH", body: input }),
  );
}

export function useDeleteSavedFilter() {
  return useFilterMutation((id: string) => request<void>(`/filters/${id}`, { method: "DELETE" }));
}

export function useStarFilter() {
  return useFilterMutation((input: { id: string; on: boolean }) =>
    request<{ filter: SavedFilter }>(`/filters/${input.id}/star`, { method: input.on ? "PUT" : "DELETE" }),
  );
}

export function useSubscribeFilter() {
  return useFilterMutation((input: { id: string; subscription: FilterSubscription | null }) =>
    input.subscription
      ? request<{ filter: SavedFilter }>(`/filters/${input.id}/subscription`, { method: "PUT", body: input.subscription })
      : request<{ filter: SavedFilter }>(`/filters/${input.id}/subscription`, { method: "DELETE" }),
  );
}

// Bulk: many issues, one request; the answer names what was refused and why.

/** Members of a set to add and to take away; the rest of each issue's set stays. */
export interface BulkSetEdit {
  add?: string[];
  remove?: string[];
}

export interface BulkChange {
  priority?: Priority;
  assignee?: string | null;
  sprintId?: string | null;
  milestoneId?: string | null;
  teamId?: string | null;
  addLabels?: string[];
  removeLabels?: string[];
  fixVersions?: BulkSetEdit;
  components?: BulkSetEdit;
  /** Answers by field id; null clears one. */
  fields?: Record<string, unknown>;
  transition?: string;
  /** A move and a delete each go alone. */
  moveTo?: { projectKey: string; statuses?: Record<string, string> };
  delete?: boolean;
}

export interface BulkResult {
  applied: string[];
  refused: Array<{ key: string; reason: string }>;
  /** Every key a move changed, those carried under a moved issue included. */
  moved: Array<{ from: string; to: string }>;
}

export function useBulkEdit() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (input: { keys: string[]; change: BulkChange }) => request<BulkResult>("/issues/bulk", { method: "POST", body: input }),
    onSuccess: () => queryClient.invalidateQueries(),
  });
}

/** The transitions every selected issue offers, so one name can be picked for all. */
export function useBulkTransitions(keys: string[]) {
  return useQuery({
    queryKey: ["bulk-transitions", [...keys].sort()],
    queryFn: () => request<{ names: string[] }>(`/issues/bulk/transitions?keys=${encodeURIComponent(keys.join(","))}`),
    enabled: keys.length > 0 && keys.length <= BULK_MAX_SELECTION,
  });
}

/** Where a query's result is fetched as a file. */
export function exportHref(query: string, columns: string[], project?: string): string {
  const params = new URLSearchParams();
  if (query) params.set("q", query);
  if (project) params.set("project", project);
  if (columns.length) params.set("columns", columns.join(","));
  return `/api/v1/issues/export?${params.toString()}`;
}

// Import: a file in, previewed, mapped, tried dry first.

/** A column of the file, by position: two columns may share a name. */
export interface ImportColumn {
  at: number;
  name: string;
  samples: string[];
}

/** One distinct value of a mapped column, and what it becomes here. */
export interface ImportWord {
  target: string;
  value: string;
  count: number;
  means: string;
}

/** One person the file names, and the member they look like. */
export interface ImportPerson {
  name: string;
  count: number;
  member?: string;
  email?: string;
}

export interface ImportPreview {
  preview: { columns: ImportColumn[]; total: number; words: ImportWord[]; people: ImportPerson[] };
  mapping: Record<string, number[]>;
  targets: string[];
}

export interface ImportReport {
  dryRun: boolean;
  imported: string[];
  updated: string[];
  refused: Array<{ row: number; reason: string }>;
  partial: Array<{ row: number; reason: string }>;
  notes: string[];
  rows: number;
  mapping: Record<string, number[]>;
}

/** What one name in the file becomes: a member, or an account made for them. */
export interface PersonChoice {
  member?: string;
  create?: boolean;
}

export interface ImportPeople {
  domain?: string;
  choices?: Record<string, PersonChoice>;
}

/** Everything decided about a file before it is read. */
export interface ImportDecisions {
  mapping: Record<string, number[]>;
  values?: Record<string, Record<string, string>>;
  people?: ImportPeople;
}

function fileForm(file: File, decisions?: ImportDecisions): FormData {
  const form = new FormData();
  form.append("file", file, file.name);
  if (decisions) {
    form.append("mapping", JSON.stringify(decisions.mapping));
    if (decisions.values) form.append("values", JSON.stringify(decisions.values));
    if (decisions.people) form.append("people", JSON.stringify(decisions.people));
  }
  return form;
}

export function useImportPreview(projectKey: string) {
  return useMutation({
    mutationFn: (input: { file: File; mapping?: Record<string, number[]> }) =>
      upload<ImportPreview>(`/projects/${projectKey}/import/preview`, fileForm(input.file, input.mapping ? { mapping: input.mapping } : undefined)),
  });
}

export function useImportIssues(projectKey: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (input: { file: File; decisions: ImportDecisions; dryRun: boolean }) =>
      upload<{ report: ImportReport }>(`/projects/${projectKey}/import${input.dryRun ? "?dryRun=true" : ""}`, fileForm(input.file, input.decisions)),
    onSuccess: (_result, input) => {
      if (!input.dryRun) queryClient.invalidateQueries();
    },
  });
}

// Clone and move.

export function useCloneIssue() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (input: { key: string; links: boolean; subtasks: boolean; summary?: string }) =>
      request<{ issue: Issue }>(`/issues/${input.key}/clone`, {
        method: "POST",
        body: { links: input.links, subtasks: input.subtasks, summary: input.summary },
      }),
    onSuccess: () => queryClient.invalidateQueries(),
  });
}

export function useMoveIssue() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (input: { key: string; projectKey: string; statusId?: string }) =>
      request<{ issue: Issue }>(`/issues/${input.key}/move`, { method: "POST", body: { projectKey: input.projectKey, statusId: input.statusId } }),
    onSuccess: () => queryClient.invalidateQueries(),
  });
}

export function useFilterIssues(filterId: string | null) {
  return useQuery({
    queryKey: [...filtersQueryKey, "issues", filterId],
    queryFn: () => request<IssuePage>(`/filters/${filterId}/issues?limit=50`),
    enabled: filterId !== null,
  });
}
