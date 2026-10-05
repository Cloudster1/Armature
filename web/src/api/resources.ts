import { keepPreviousData, useQuery } from "@tanstack/react-query";
import { request } from "./client";
import type { components } from "./schema";

export type ResourcePlan = components["schemas"]["ResourcePlan"];
export type ResourceRow = components["schemas"]["ResourceRow"];
export type ResourceWeek = components["schemas"]["ResourceWeek"];
export type ResourceIssue = components["schemas"]["ResourceIssue"];

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
