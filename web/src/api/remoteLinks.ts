import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { request } from "./client";
import type { components } from "./schema";

/** A page in another application that is about an issue, kept in step by its address. */
export type RemoteLink = components["schemas"]["RemoteLink"];

export const remoteLinksQueryKey = ["remote-links"] as const;

export function useRemoteLinks(issueKey: string) {
  return useQuery({
    queryKey: [...remoteLinksQueryKey, issueKey],
    queryFn: () => request<{ remoteLinks: RemoteLink[] }>(`/issues/${issueKey}/remote-links`),
    enabled: Boolean(issueKey),
  });
}

export function useRemoveRemoteLink() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({ key, id }: { key: string; id: string }) => request<void>(`/issues/${key}/remote-links/${id}`, { method: "DELETE" }),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: remoteLinksQueryKey }),
  });
}
