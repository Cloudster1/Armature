import { keepPreviousData, useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { request } from "./client";
import { issuesQueryKey, type Issue, type Progress } from "./issues";
import type { SprintPlan } from "./sprints";
import type { Milestone } from "./milestones";

export interface PlanItem {
  issue: Issue;
  depth: number;
  start?: string;
  due?: string;
  /** The range came from the children rather than from the issue itself. */
  derived: boolean;
  /** How much work this is, and whether that was typed in or added up. */
  estimate?: number;
  estimateDerived?: boolean;
  progress: Progress;
  children: PlanItem[];
}

export interface Dependency {
  /** The link itself, so the dependency can be undone where it is seen. */
  linkId: string;
  blockerKey: string;
  blockedKey: string;
}

export type WarningKind =
  | "blocked-too-early"
  | "outside-parent"
  | "half-scheduled"
  | "over-capacity"
  | "outside-sprint"
  | "past-milestone"
  | "over-load";

export interface PlanWarning {
  /** Exactly one of these names what the warning is about. A sprint committed
   * beyond its capacity, or a team loaded beyond its week, is not any one
   * issue's fault. */
  issueKey?: string;
  sprint?: string;
  team?: string;
  kind: WarningKind;
  message: string;
}

/** One team's week: what is scheduled into it against what the team can take. */
export interface LoadWeek {
  start: string;
  load: number;
  capacity?: number;
  issues: number;
  unestimated: number;
}

/** A team's row of the load, the work no team carries, or the whole project. */
export interface TeamLoad {
  teamId?: string;
  team: string;
  kind: "team" | "unassigned" | "total";
  weeklyCapacity?: number;
  weeks: LoadWeek[];
  /** Work with no dates, counted rather than spread. */
  unscheduled: number;
}

export interface Load {
  weeks: string[];
  rows: TeamLoad[];
}

export interface LinkType {
  id: string;
  name: string;
  outward: string;
  inward: string;
}

export interface Plan {
  projectKey: string;
  items: PlanItem[];
  dependencies: Dependency[];
  sprints: SprintPlan[];
  /** The points the work is heading for, each with how far along it is. */
  milestones: Milestone[];
  /** The work per team per week against what each team can take. */
  load: Load;
  warnings: PlanWarning[];
  from: string;
  to: string;
  /** The two kinds of row a plan cannot say anything about, counted not hidden. */
  unscheduled: number;
  unestimated: number;
  linkTypes: LinkType[];
  /** The keys the request's query selected; empty when there was no query. */
  matched: string[];
}

export const planQueryKey = ["plan"] as const;

/**
 * The plan, whole, plus what a query selects. The last good plan stays while
 * a query is being corrected, so a typo does not blank the calendar.
 */
export function usePlan(projectKey: string, query = "") {
  const search = query ? `?q=${encodeURIComponent(query)}` : "";
  return useQuery({
    queryKey: [...planQueryKey, projectKey, query],
    queryFn: () => request<Plan>(`/projects/${projectKey}/plan${search}`),
    enabled: Boolean(projectKey),
    placeholderData: keepPreviousData,
  });
}

/** Moving an issue in time. Null clears that end of the range. */
export function useSchedule() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({
      key,
      startDate,
      dueDate,
    }: {
      key: string;
      startDate?: string | null;
      dueDate?: string | null;
    }) => request<{ issue: Issue }>(`/issues/${key}/schedule`, { method: "PUT", body: { startDate, dueDate } }),
    // One issue's dates change the roll-up of everything above it.
    onSuccess: () => queryClient.invalidateQueries(),
  });
}

export interface IssueLink {
  id: string;
  direction: "outward" | "inward";
  typeId: string;
  typeName: string;
  phrase: string;
  issue: Issue;
}

export function useLinkTypes() {
  return useQuery({
    queryKey: ["link-types"],
    queryFn: () => request<{ linkTypes: LinkType[] }>("/link-types"),
  });
}

export function useLinks(key: string) {
  return useQuery({
    queryKey: [...issuesQueryKey, "links", key],
    queryFn: () => request<{ links: IssueLink[] }>(`/issues/${key}/links`),
    enabled: Boolean(key),
  });
}

export function useAddLink() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({ key, type, targetKey }: { key: string; type?: string; targetKey: string }) =>
      request<{ link: IssueLink }>(`/issues/${key}/links`, { method: "POST", body: { type, targetKey } }),
    onSuccess: () => queryClient.invalidateQueries(),
  });
}

export function useRemoveLink() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({ key, linkId }: { key: string; linkId: string }) =>
      request<void>(`/issues/${key}/links/${linkId}`, { method: "DELETE" }),
    onSuccess: () => queryClient.invalidateQueries(),
  });
}
