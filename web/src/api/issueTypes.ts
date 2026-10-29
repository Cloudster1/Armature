import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useMutation } from "./mutation";
import { request } from "./client";
import { issueTypesQueryKey, type IssueType, type IssueTypeIcon } from "./issues";
import { META_STALE_MS } from "@/config";
import type { components } from "./schema";

/** A type as a project keeps it: the library's type, and how many of the project's issues have it. */
export type ProjectIssueType = components["schemas"]["ProjectIssueType"];

/**
 * The types a project allows, in its order. Anything that offers a type for
 * new or changed work in one project reads this, not the library; a reading
 * of what exists asks for the retired ones the project keeps too.
 */
export function useProjectIssueTypes(projectKey: string, { retired = false }: { retired?: boolean } = {}) {
  return useQuery({
    queryKey: [...issueTypesQueryKey, "project", projectKey, { retired }],
    queryFn: () => request<{ issueTypes: ProjectIssueType[] }>(`/projects/${projectKey}/issue-types${retired ? "?retired=true" : ""}`),
    staleTime: META_STALE_MS,
    enabled: Boolean(projectKey),
  });
}

/** What an administrator writes about a type; on an edit, what is left out stays. */
export interface IssueTypeInput {
  name?: string;
  description?: string;
  icon?: IssueTypeIcon;
  level?: number;
}

// Every picker reads the library, and an issue's own type travels with it, so a change refreshes everything.
function useLibraryMutation<Vars, Data>(send: (vars: Vars) => Promise<Data>) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: send,
    onSuccess: () => queryClient.invalidateQueries(),
  });
}

export function useCreateIssueType() {
  return useLibraryMutation((body: IssueTypeInput) => request<{ issueType: IssueType }>("/issue-types", { method: "POST", body }));
}

export function useUpdateIssueType() {
  return useLibraryMutation(({ id, ...body }: IssueTypeInput & { id: string }) =>
    request<{ issueType: IssueType }>(`/issue-types/${id}`, { method: "PATCH", body }),
  );
}

export function useRetireIssueType() {
  return useLibraryMutation((id: string) => request<{ issueType: IssueType }>(`/issue-types/${id}/retire`, { method: "POST" }));
}

export function useReinstateIssueType() {
  return useLibraryMutation((id: string) => request<{ issueType: IssueType }>(`/issue-types/${id}/reinstate`, { method: "POST" }));
}

export function useDeleteIssueType() {
  return useLibraryMutation((id: string) => request<void>(`/issue-types/${id}`, { method: "DELETE" }));
}

/** Choose the types a project allows, in order. A library edit and this both refresh every reading of either. */
export function useSetProjectIssueTypes(projectKey: string) {
  return useLibraryMutation((typeIds: string[]) =>
    request<{ issueTypes: ProjectIssueType[] }>(`/projects/${projectKey}/issue-types`, { method: "PUT", body: { typeIds } }),
  );
}
