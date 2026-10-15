import { keepPreviousData, useQuery, useQueryClient } from "@tanstack/react-query";
import { useMutation } from "./mutation";
import { request } from "./client";
import { LABEL_PAGE_SIZE, LABEL_SUGGESTIONS } from "@/config";
import type { LabelColor, LabelRef } from "./issues";

/** A label as the organization keeps it, with how often it is used. */
export interface Label extends LabelRef {
  issueCount: number;
  createdAt: string;
  updatedAt: string;
}

export const LABEL_COLORS: LabelColor[] = ["gray", "red", "orange", "amber", "green", "teal", "blue", "purple", "pink"];

export const labelsQueryKey = ["labels"] as const;

/** One page of the labels and how many matched in all. */
export interface LabelPage {
  labels: Label[];
  total: number;
  limit: number;
  offset: number;
}

/** A page of the labels by name, or of those holding the text with those starting with it first. */
export function useLabels({ q = "", offset = 0 }: { q?: string; offset?: number } = {}) {
  const text = q.trim();
  return useQuery({
    queryKey: [...labelsQueryKey, "page", { q: text, offset }],
    queryFn: () => {
      const params = new URLSearchParams({ limit: String(LABEL_PAGE_SIZE) });
      if (text) params.set("q", text);
      if (offset) params.set("offset", String(offset));
      return request<LabelPage>(`/labels?${params}`);
    },
    placeholderData: keepPreviousData,
  });
}

/** The few labels holding the text, those starting with it first; nothing is asked for an empty box. */
export function useLabelSearch(q: string, limit = LABEL_SUGGESTIONS) {
  const text = q.trim();
  return useQuery({
    queryKey: [...labelsQueryKey, "search", { q: text, limit }],
    queryFn: () => request<{ labels: Label[] }>(`/labels?${new URLSearchParams({ q: text, limit: String(limit) })}`),
    enabled: text !== "",
    placeholderData: keepPreviousData,
  });
}

/** Makes the issue carry exactly these names; a new word is coined on the spot. */
export function useSetIssueLabels() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({ key, labels }: { key: string; labels: string[] }) =>
      request<{ labels: LabelRef[] }>(`/issues/${key}/labels`, { method: "PUT", body: { labels } }),
    // Cards on the board and rows in the plan carry the labels, as well as the issue.
    onSuccess: () => queryClient.invalidateQueries(),
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
