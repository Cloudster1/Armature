import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { request } from "./client";
import { META_STALE_MS } from "@/config";
import type { components } from "./schema";

/**
 * How an issue's fields are arranged, per issue type. The vocabulary comes
 * from the generated document, so a slot the server knows must be drawn here.
 */
export type Arrangement = components["schemas"]["Arrangement"];
export type Placement = components["schemas"]["Placement"];
export type Slot = NonNullable<Placement["slot"]>;
export type Area = Placement["area"];

export const arrangementsQueryKey = ["issue-arrangement"] as const;

/** Every issue type's arrangement in one read, since a page needs one of them. */
export function useArrangements(projectKey: string) {
  return useQuery({
    queryKey: [...arrangementsQueryKey, "project", projectKey],
    queryFn: () => request<{ arrangements: Arrangement[] }>(`/projects/${projectKey}/issue-arrangement`),
    enabled: Boolean(projectKey),
    staleTime: META_STALE_MS,
  });
}

/** What the organization arranges, which every project follows until it disagrees. */
export function useOrgArrangements(enabled = true) {
  return useQuery({
    queryKey: [...arrangementsQueryKey, "org"],
    queryFn: () => request<{ arrangements: Arrangement[] }>("/issue-arrangement"),
    staleTime: META_STALE_MS,
    enabled,
  });
}

/** An arrangement is sent whole; null places hand the type back. */
export interface ArrangementInput {
  issueTypeId?: string;
  places: Placement[] | null;
}

export function useSetProjectArrangement(projectKey: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (body: ArrangementInput) => request<{ arrangements: Arrangement[] }>(`/projects/${projectKey}/issue-arrangement`, { method: "PUT", body }),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: arrangementsQueryKey }),
  });
}

export function useSetOrgArrangement() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (body: ArrangementInput) => request<{ arrangements: Arrangement[] }>("/issue-arrangement", { method: "PUT", body }),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: arrangementsQueryKey }),
  });
}
