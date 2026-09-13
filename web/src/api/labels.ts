import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { request } from "./client";
import { META_STALE_MS } from "@/config";
import { issuesQueryKey, type LabelColor, type LabelRef } from "./issues";

/** A label as the organization keeps it, with how often it is used. */
export interface Label extends LabelRef {
  issueCount: number;
  createdAt: string;
  updatedAt: string;
}

export const LABEL_COLORS: LabelColor[] = ["gray", "red", "orange", "amber", "green", "teal", "blue", "purple", "pink"];

export const labelsQueryKey = ["labels"] as const;

export function useLabels() {
  return useQuery({
    queryKey: labelsQueryKey,
    queryFn: () => request<{ labels: Label[] }>("/labels"),
    staleTime: META_STALE_MS,
  });
}

/** Makes the issue carry exactly these names; a new word is coined on the spot. */
export function useSetIssueLabels() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({ key, labels }: { key: string; labels: string[] }) =>
      request<{ labels: LabelRef[] }>(`/issues/${key}/labels`, { method: "PUT", body: { labels } }),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: issuesQueryKey });
      void queryClient.invalidateQueries({ queryKey: labelsQueryKey });
    },
  });
}

export function useUpdateLabel() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({ id, ...body }: { id: string; name?: string; color?: LabelColor }) =>
      request<{ label: Label }>(`/labels/${id}`, { method: "PATCH", body }),
    // A rename shows on every issue that carries the word.
    onSuccess: () => queryClient.invalidateQueries(),
  });
}

export function useDeleteLabel() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (id: string) => request<void>(`/labels/${id}`, { method: "DELETE" }),
    onSuccess: () => queryClient.invalidateQueries(),
  });
}
