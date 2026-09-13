import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { request } from "./client";
import type { Issue } from "./issues";

/**
 * How far a milestone has got, counted by the status category of the issues
 * assigned to it. Read off the issues every time, never stored.
 */
export interface Progress {
  issues: number;
  done: number;
  inProgress: number;
  todo: number;
  /** Done over everything, rounded down; zero with nothing assigned. */
  percent: number;
}

export interface Milestone {
  id: string;
  projectId: string;
  projectKey: string;
  name: string;
  description?: string;
  /** When it should be reached; absent while it is an intention, not a date. */
  dueOn?: string;
  /** Set once it has been declared reached or dropped. */
  closedAt?: string;
  position: number;
  progress: Progress;
  createdAt: string;
  updatedAt: string;
}

/** A milestone as it hangs off an issue. */
export interface MilestoneRef {
  id: string;
  name: string;
}

/**
 * The day a milestone is due, as YYYY-MM-DD. The server writes the date as a
 * timestamp at midnight; a date input and a label both want the day alone.
 */
export function dayOf(iso: string): string {
  return iso.slice(0, 10);
}

/** Whether the date has passed with work still to do. */
export function isOverdue(milestone: Pick<Milestone, "dueOn" | "closedAt" | "progress">, today: Date): boolean {
  if (!milestone.dueOn || milestone.closedAt) return false;
  if (milestone.progress.issues > 0 && milestone.progress.percent === 100) return false;
  const due = Date.parse(`${dayOf(milestone.dueOn)}T00:00:00Z`);
  return due < Date.UTC(today.getUTCFullYear(), today.getUTCMonth(), today.getUTCDate());
}

/** A day the way the plan writes one: 24 Sep 2026. */
export function formatDay(iso: string): string {
  return new Date(`${dayOf(iso)}T00:00:00Z`).toLocaleDateString("en-GB", {
    day: "numeric",
    month: "short",
    year: "numeric",
    timeZone: "UTC",
  });
}

/** The one line a person reads off a milestone: how much of it is done. */
export function describeProgress(progress: Progress): string {
  if (progress.issues === 0) return "Nothing assigned yet";
  return `${progress.done} of ${progress.issues} done, ${progress.percent}%`;
}

export const milestonesQueryKey = ["milestones"] as const;

export function useMilestones(projectKey: string, includeClosed = false) {
  return useQuery({
    queryKey: [...milestonesQueryKey, projectKey, { includeClosed }],
    queryFn: () =>
      request<{ milestones: Milestone[] }>(
        `/projects/${projectKey}/milestones${includeClosed ? "?closed=true" : ""}`,
      ),
    enabled: Boolean(projectKey),
  });
}

export interface MilestoneInput {
  name?: string;
  description?: string;
  /** Null clears the date. */
  dueOn?: string | null;
}

/**
 * Every write here changes what a milestone holds or when it is due, and so
 * what the plan and the issue say about it. Nothing is spared.
 */
function useMilestoneMutation<TArgs, TResult>(run: (args: TArgs) => Promise<TResult>) {
  const queryClient = useQueryClient();
  return useMutation({ mutationFn: run, onSuccess: () => queryClient.invalidateQueries() });
}

export function useCreateMilestone(projectKey: string) {
  return useMilestoneMutation((input: MilestoneInput) =>
    request<{ milestone: Milestone }>(`/projects/${projectKey}/milestones`, { method: "POST", body: input }),
  );
}

export function useUpdateMilestone() {
  return useMilestoneMutation(({ id, ...input }: MilestoneInput & { id: string }) =>
    request<{ milestone: Milestone }>(`/milestones/${id}`, { method: "PATCH", body: input }),
  );
}

export function useCloseMilestone() {
  return useMilestoneMutation((id: string) =>
    request<{ milestone: Milestone }>(`/milestones/${id}/close`, { method: "POST", body: {} }),
  );
}

export function useReopenMilestone() {
  return useMilestoneMutation((id: string) =>
    request<{ milestone: Milestone }>(`/milestones/${id}/reopen`, { method: "POST", body: {} }),
  );
}

export function useDeleteMilestone() {
  return useMilestoneMutation((id: string) => request<void>(`/milestones/${id}`, { method: "DELETE" }));
}

/** A null milestone is the issue counting towards none. */
export function useSetIssueMilestone() {
  return useMilestoneMutation(({ key, milestoneId }: { key: string; milestoneId: string | null }) =>
    request<{ issue: Issue }>(`/issues/${key}/milestone`, { method: "PUT", body: { milestoneId } }),
  );
}
