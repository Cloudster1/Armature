import { keepPreviousData, useQuery } from "@tanstack/react-query";
import { request } from "./client";
import { issuesQueryKey, type IssuePage, type StatusCategory } from "./issues";
import { MAX_PAGE_SIZE, PARENT_PICKER_RESULTS } from "@/config";

/** The work that is not finished, which is all a backlog is ever made of. */
const UNFINISHED: StatusCategory[] = ["todo", "in_progress"];

/**
 * Reads every page a project's issue list has for these parameters. A sprint or
 * a backlog cut short at one page hides work, which is worse than a slow read.
 */
async function everyPage(projectKey: string, params: URLSearchParams): Promise<IssuePage> {
  const issues: IssuePage["issues"] = [];
  let total = 0;
  do {
    const search = new URLSearchParams(params);
    search.set("limit", String(MAX_PAGE_SIZE));
    search.set("offset", String(issues.length));
    const page = await request<IssuePage>(`/projects/${projectKey}/issues?${search}`);
    total = page.total;
    issues.push(...page.issues);
    if (page.issues.length === 0) break;
  } while (issues.length < total);
  return { issues, total, limit: issues.length, offset: 0 };
}

/** Everything committed to one sprint, in rank order, whatever its age. */
export function useSprintIssues(projectKey: string, sprintId: string) {
  return useQuery({
    queryKey: [...issuesQueryKey, "sprint", projectKey, sprintId],
    queryFn: () => everyPage(projectKey, new URLSearchParams({ sprint: sprintId, orderBy: "rank", order: "asc" })),
    enabled: Boolean(projectKey && sprintId),
  });
}

/**
 * One stream's backlog: the unfinished work in no sprint, ranked. An empty team
 * is the project's own stream, the work no team has taken on.
 */
export function useBacklogIssues(projectKey: string, teamId: string) {
  return useQuery({
    queryKey: [...issuesQueryKey, "backlog", projectKey, teamId],
    queryFn: () => {
      const params = new URLSearchParams({ sprint: "none", team: teamId || "none", orderBy: "rank", order: "asc" });
      for (const category of UNFINISHED) params.append("category", category);
      return everyPage(projectKey, params);
    },
    enabled: Boolean(projectKey),
  });
}

/** The issues of these types whose summary holds the words, for picking a parent. */
export function useParentCandidates(projectKey: string, typeIds: string[], words: string) {
  return useQuery({
    queryKey: [...issuesQueryKey, "parents", projectKey, typeIds, words],
    queryFn: () => {
      const params = new URLSearchParams({ limit: String(PARENT_PICKER_RESULTS), orderBy: "updated" });
      if (words) params.set("text", words);
      for (const id of typeIds) params.append("type", id);
      return request<IssuePage>(`/projects/${projectKey}/issues?${params}`);
    },
    enabled: Boolean(projectKey) && typeIds.length > 0,
    placeholderData: keepPreviousData,
  });
}
