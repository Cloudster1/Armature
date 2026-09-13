import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { request } from "./client";
import type { Issue } from "./issues";
import type { Progress } from "./milestones";

export interface Version {
  id: string;
  projectId: string;
  projectKey: string;
  name: string;
  description?: string;
  startOn?: string;
  releaseOn?: string;
  releasedAt?: string;
  archivedAt?: string;
  position: number;
  /** The issues that fix this version, by status category. */
  progress: Progress;
  createdAt: string;
  updatedAt: string;
}

/** A version as it hangs off an issue. */
export interface VersionRef {
  id: string;
  name: string;
  released: boolean;
}

export interface VersionInput {
  name?: string;
  description?: string;
  /** Days as YYYY-MM-DD; the clear flags take a date away. */
  startOn?: string;
  releaseOn?: string;
  clearStart?: boolean;
  clearRelease?: boolean;
}

export interface ReleaseNotes {
  version: Version;
  groups: Array<{ type: string; issues: Array<{ key: string; summary: string }> }>;
  open: number;
}

export interface Component {
  id: string;
  projectId: string;
  projectKey: string;
  name: string;
  description?: string;
  lead?: { id: string; name: string };
  defaultAssignee?: { id: string; name: string };
  issues: number;
  createdAt: string;
  updatedAt: string;
}

export interface ComponentRef {
  id: string;
  name: string;
}

export interface ComponentInput {
  name?: string;
  description?: string;
  leadId?: string;
  defaultAssigneeId?: string;
  clearLead?: boolean;
  clearDefaultAssignee?: boolean;
}

/** A day the API takes: midnight UTC of the day typed. */
function asDay(day: string | undefined): string | undefined {
  return day ? `${day}T00:00:00Z` : undefined;
}

export function useVersions(projectKey: string, includeArchived = false) {
  return useQuery({
    queryKey: ["versions", projectKey, { includeArchived }],
    queryFn: () => request<{ versions: Version[] }>(`/projects/${projectKey}/versions${includeArchived ? "?archived=true" : ""}`),
    enabled: Boolean(projectKey),
  });
}

export function useReleaseNotes(versionId: string | null) {
  return useQuery({
    queryKey: ["versions", "notes", versionId],
    queryFn: () => request<{ notes: ReleaseNotes }>(`/versions/${versionId}/notes`),
    enabled: versionId !== null,
  });
}

// A version's change moves issues' progress and what the plan and the issue
// say; nothing is spared.
function useVersionMutation<TArgs, TResult>(run: (args: TArgs) => Promise<TResult>) {
  const queryClient = useQueryClient();
  return useMutation({ mutationFn: run, onSuccess: () => queryClient.invalidateQueries() });
}

export function useCreateVersion(projectKey: string) {
  return useVersionMutation((input: VersionInput) =>
    request<{ version: Version }>(`/projects/${projectKey}/versions`, { method: "POST", body: { ...input, startOn: asDay(input.startOn), releaseOn: asDay(input.releaseOn) } }),
  );
}

export function useUpdateVersion() {
  return useVersionMutation(({ id, ...input }: VersionInput & { id: string }) =>
    request<{ version: Version }>(`/versions/${id}`, { method: "PATCH", body: { ...input, startOn: asDay(input.startOn), releaseOn: asDay(input.releaseOn) } }),
  );
}

export function useVersionAction(action: "release" | "unrelease" | "archive") {
  return useVersionMutation((id: string) => request<{ version: Version }>(`/versions/${id}/${action}`, { method: "POST", body: {} }));
}

export function useDeleteVersion() {
  return useVersionMutation((id: string) => request<void>(`/versions/${id}`, { method: "DELETE" }));
}

export function useSetIssueVersions() {
  return useVersionMutation((input: { key: string; fix: string[]; affects: string[] }) =>
    request<{ issue: Issue }>(`/issues/${input.key}/versions`, { method: "PUT", body: { fix: input.fix, affects: input.affects } }),
  );
}

export function useComponents(projectKey: string) {
  return useQuery({
    queryKey: ["components", projectKey],
    queryFn: () => request<{ components: Component[] }>(`/projects/${projectKey}/components`),
    enabled: Boolean(projectKey),
  });
}

export function useCreateComponent(projectKey: string) {
  return useVersionMutation((input: ComponentInput) => request<{ component: Component }>(`/projects/${projectKey}/components`, { method: "POST", body: input }));
}

export function useUpdateComponent() {
  return useVersionMutation(({ id, ...input }: ComponentInput & { id: string }) => request<{ component: Component }>(`/components/${id}`, { method: "PATCH", body: input }));
}

export function useDeleteComponent() {
  return useVersionMutation((id: string) => request<void>(`/components/${id}`, { method: "DELETE" }));
}

export function useSetIssueComponents() {
  return useVersionMutation((input: { key: string; components: string[] }) =>
    request<{ issue: Issue }>(`/issues/${input.key}/components`, { method: "PUT", body: { components: input.components } }),
  );
}
