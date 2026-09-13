import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { request } from "./client";
import type { Issue } from "./issues";

/**
 * A sprint only moves forwards: planned, then running, then over. A project has
 * at most one running sprint, so "the sprint" always means one thing.
 */
export type SprintState = "future" | "active" | "closed";

export interface Sprint {
  id: string;
  projectId: string;
  projectKey: string;
  /** Whose sprint this is; absent is the project's own. */
  teamId?: string;
  teamName?: string;
  name: string;
  goal?: string;
  state: SprintState;
  startsOn?: string;
  endsOn?: string;
  /** What the team can take on, in the same unit as an estimate. */
  capacity?: number;
  position: number;
  startedAt?: string;
  completedAt?: string;
  /** What the sprint turned out to be, written once when it closed. */
  committed?: number;
  completed?: number;
  createdAt: string;
  updatedAt: string;
}

/** A sprint with what has actually been committed to it. */
export interface SprintPlan {
  sprint: Sprint;
  committed: number;
  completed: number;
  issues: number;
  /** How many of those issues nobody has sized. */
  unestimated: number;
}

export interface SprintReport {
  sprint: Sprint;
  committed: number;
  completed: number;
  finished: number;
  carried: number;
  carriedTo?: string;
}

/** How far past its capacity a sprint is committed, or zero. */
export function overBy(plan: SprintPlan): number {
  if (plan.sprint.capacity === undefined || plan.sprint.capacity === null) return 0;
  return Math.max(0, plan.committed - plan.sprint.capacity);
}

export const sprintsQueryKey = ["sprints"] as const;

export function useSprints(projectKey: string, includeClosed = false) {
  return useQuery({
    queryKey: [...sprintsQueryKey, projectKey, { includeClosed }],
    queryFn: () =>
      request<{ sprints: Sprint[] }>(
        `/projects/${projectKey}/sprints${includeClosed ? "?closed=true" : ""}`,
      ),
    enabled: Boolean(projectKey),
  });
}

export interface SprintInput {
  name?: string;
  goal?: string;
  startsOn?: string | null;
  endsOn?: string | null;
  capacity?: number | null;
  /** Whose sprint this is, read only when one is created. */
  teamId?: string | null;
}

/**
 * Every write here changes what a sprint holds, and so what the plan, the
 * board and the issue list say about it. Nothing is spared.
 */
function useSprintMutation<TArgs, TResult>(run: (args: TArgs) => Promise<TResult>) {
  const queryClient = useQueryClient();
  return useMutation({ mutationFn: run, onSuccess: () => queryClient.invalidateQueries() });
}

export function useCreateSprint(projectKey: string) {
  return useSprintMutation((input: SprintInput) =>
    request<{ sprint: Sprint }>(`/projects/${projectKey}/sprints`, { method: "POST", body: input }),
  );
}

export function useUpdateSprint() {
  return useSprintMutation(({ id, ...input }: SprintInput & { id: string }) =>
    request<{ sprint: Sprint }>(`/sprints/${id}`, { method: "PATCH", body: input }),
  );
}

export function useStartSprint() {
  return useSprintMutation((id: string) =>
    request<{ sprint: Sprint }>(`/sprints/${id}/start`, { method: "POST", body: {} }),
  );
}

/** A null destination sends the unfinished work back to the backlog. */
export function useCompleteSprint() {
  return useSprintMutation(({ id, moveTo }: { id: string; moveTo: string | null }) =>
    request<{ report: SprintReport }>(`/sprints/${id}/complete`, {
      method: "POST",
      body: { moveTo },
    }),
  );
}

export function useDeleteSprint() {
  return useSprintMutation((id: string) => request<void>(`/sprints/${id}`, { method: "DELETE" }));
}

/** A null sprint is not an omission: it is the issue going back to the backlog. */
export function useSetIssueSprint() {
  return useSprintMutation(({ key, sprintId }: { key: string; sprintId: string | null }) =>
    request<{ issue: Issue }>(`/issues/${key}/sprint`, { method: "PUT", body: { sprintId } }),
  );
}

/** A null estimate clears it, which says nobody has decided rather than no work. */
export function useSetEstimate() {
  return useSprintMutation(({ key, estimate }: { key: string; estimate: number | null }) =>
    request<{ issue: Issue }>(`/issues/${key}/estimate`, { method: "PUT", body: { estimate } }),
  );
}
