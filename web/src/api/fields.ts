import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { request } from "./client";
import { META_STALE_MS } from "@/config";
import { issuesQueryKey } from "./issues";

/**
 * Custom fields: what a project records about its issues beyond the standard
 * columns. A field is defined per project; a value is one issue's answer.
 */
export type FieldKind = "text" | "number" | "date" | "select" | "checkbox" | "url";

export interface KindInfo {
  kind: FieldKind;
  title: string;
  description: string;
  /** The field is defined with a list of choices. */
  hasOptions: boolean;
}

export interface Field {
  id: string;
  /** Absent on a field the whole organization shares. */
  projectId?: string;
  projectKey?: string;
  /** The field belongs to every project of the organization. */
  org: boolean;
  name: string;
  kind: FieldKind;
  options: string[];
  position: number;
  createdAt: string;
  updatedAt: string;
}

/** One issue's answer to one field. Absent value means unanswered. */
export interface FieldValue {
  field: Field;
  value?: string | number | boolean;
  display?: string;
}

export const fieldsQueryKey = ["fields"] as const;

export function useFieldKinds() {
  return useQuery({
    queryKey: [...fieldsQueryKey, "kinds"],
    queryFn: () => request<{ kinds: KindInfo[] }>("/field-kinds"),
    staleTime: META_STALE_MS,
  });
}

export function useProjectFields(projectKey: string) {
  return useQuery({
    queryKey: [...fieldsQueryKey, "project", projectKey],
    queryFn: () => request<{ fields: Field[] }>(`/projects/${projectKey}/fields`),
    enabled: Boolean(projectKey),
  });
}

/** The fields every project of the organization has. */
export function useOrgFields() {
  return useQuery({
    queryKey: [...fieldsQueryKey, "org"],
    queryFn: () => request<{ fields: Field[] }>("/fields"),
  });
}

export function useCreateOrgField() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (body: { name: string; kind: FieldKind; options?: string[] }) => request<{ field: Field }>("/fields", { method: "POST", body }),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: fieldsQueryKey }),
  });
}

/** Makes a project's field the organization's, folding same-named fields into it. */
export function usePromoteField() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (id: string) => request<{ field: Field }>(`/fields/${id}/promote`, { method: "POST" }),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: fieldsQueryKey }),
  });
}

export function useCreateField() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({ projectKey, ...body }: { projectKey: string; name: string; kind: FieldKind; options?: string[] }) =>
      request<{ field: Field }>(`/projects/${projectKey}/fields`, { method: "POST", body }),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: fieldsQueryKey }),
  });
}

export function useUpdateField() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({ id, ...body }: { id: string; name?: string; options?: string[] }) =>
      request<{ field: Field }>(`/fields/${id}`, { method: "PATCH", body }),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: fieldsQueryKey }),
  });
}

export function useDeleteField() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (id: string) => request<void>(`/fields/${id}`, { method: "DELETE" }),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: fieldsQueryKey }),
  });
}

export function useIssueFields(issueKey: string) {
  return useQuery({
    queryKey: [...fieldsQueryKey, "issue", issueKey],
    queryFn: () => request<{ values: FieldValue[] }>(`/issues/${issueKey}/fields`),
    enabled: Boolean(issueKey),
  });
}

/** Records an answer; null clears it. */
export function useSetFieldValue() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({ issueKey, fieldId, value }: { issueKey: string; fieldId: string; value: unknown }) =>
      request<{ value: FieldValue }>(`/issues/${issueKey}/fields/${fieldId}`, { method: "PUT", body: { value } }),
    // The answer is in the changelog too.
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: fieldsQueryKey });
      void queryClient.invalidateQueries({ queryKey: issuesQueryKey });
    },
  });
}
