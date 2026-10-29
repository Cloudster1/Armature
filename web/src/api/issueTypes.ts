import { useQueryClient } from "@tanstack/react-query";
import { useMutation } from "./mutation";
import { request } from "./client";
import type { IssueType, IssueTypeIcon } from "./issues";

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
