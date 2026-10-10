import { keepPreviousData, useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { request } from "./client";
import type { components } from "./schema";

export type ResourcePlan = components["schemas"]["ResourcePlan"];
export type ResourceRow = components["schemas"]["ResourceRow"];
export type ResourceWeek = components["schemas"]["ResourceWeek"];
export type ResourceIssue = components["schemas"]["ResourceIssue"];
export type Allocation = components["schemas"]["Allocation"];

export const resourcesQueryKey = ["resources"] as const;

/** The work per team or person per week, in hours, over whole weeks from one day to another. */
export function useResources(projectKey: string, from: string, to: string) {
  return useQuery({
    queryKey: [...resourcesQueryKey, projectKey, from, to],
    queryFn: () => request<ResourcePlan>(`/projects/${projectKey}/resources?from=${from}&to=${to}`),
    enabled: Boolean(projectKey),
    placeholderData: keepPreviousData,
  });
}

export const allocationsQueryKey = ["allocations"] as const;

/** The share of each of a project's people's weeks the project has. */
export function useAllocations(projectKey: string) {
  return useQuery({
    queryKey: [...allocationsQueryKey, projectKey],
    queryFn: () => request<{ allocations: Allocation[] }>(`/projects/${projectKey}/allocations`),
    enabled: Boolean(projectKey),
  });
}

/** Sets a person's share of the week; the resources read at it, so they are read again. */
export function useSetAllocation(projectKey: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({ userId, percent }: { userId: string; percent: number }) =>
      request<{ allocation: Allocation }>(`/projects/${projectKey}/allocations/${userId}`, { method: "PUT", body: { percent } }),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: allocationsQueryKey });
      void queryClient.invalidateQueries({ queryKey: resourcesQueryKey });
    },
  });
}
