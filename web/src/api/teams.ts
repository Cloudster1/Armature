import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { request } from "./client";
import type { Issue } from "./issues";

/**
 * A team is the people inside a project who work together. It is narrower than
 * the organization, never wider: joining one is not a way to gain access to
 * anything.
 */
export interface TeamMember {
  userId: string;
  name: string;
  email?: string;
  /** A person to ask, not a permission. Nothing is refused on it. */
  lead: boolean;
  joinedAt: string;
}

export interface Team {
  id: string;
  projectId: string;
  projectKey: string;
  name: string;
  description?: string;
  position: number;
  /** Filled in when a team is read on its own; left out of listings. */
  members?: TeamMember[];
  memberCount: number;
  issueCount: number;
  boardCount: number;
  /** Points per week the team can take on; absent when it has not said. */
  weeklyCapacity?: number;
  createdAt: string;
  updatedAt: string;
}

export const teamsQueryKey = ["teams"] as const;

export function useTeams(projectKey: string) {
  return useQuery({
    queryKey: [...teamsQueryKey, projectKey],
    queryFn: () => request<{ teams: Team[] }>(`/projects/${projectKey}/teams`),
    enabled: Boolean(projectKey),
  });
}

export function useTeam(id: string) {
  return useQuery({
    queryKey: [...teamsQueryKey, "detail", id],
    queryFn: () => request<{ team: Team }>(`/teams/${id}`),
    enabled: Boolean(id),
  });
}

/**
 * Forming a team changes which board work appears on and which backlog it sits
 * in, so nothing about the project is left assumed afterwards.
 */
function useTeamMutation<TArgs, TResult>(run: (args: TArgs) => Promise<TResult>) {
  const queryClient = useQueryClient();
  return useMutation({ mutationFn: run, onSuccess: () => queryClient.invalidateQueries() });
}

export interface TeamInput {
  name?: string;
  description?: string;
  /** Null takes the capacity back; leaving it out keeps it. */
  weeklyCapacity?: number | null;
}

export function useCreateTeam(projectKey: string) {
  return useTeamMutation((input: TeamInput) =>
    request<{ team: Team }>(`/projects/${projectKey}/teams`, { method: "POST", body: input }),
  );
}

export function useUpdateTeam() {
  return useTeamMutation(({ id, ...input }: TeamInput & { id: string }) =>
    request<{ team: Team }>(`/teams/${id}`, { method: "PATCH", body: input }),
  );
}

export function useDeleteTeam() {
  return useTeamMutation((id: string) => request<void>(`/teams/${id}`, { method: "DELETE" }));
}

export function useAddTeamMember() {
  return useTeamMutation(({ id, userId, lead }: { id: string; userId: string; lead: boolean }) =>
    request<{ team: Team }>(`/teams/${id}/members`, { method: "POST", body: { userId, lead } }),
  );
}

export function useRemoveTeamMember() {
  return useTeamMutation(({ id, userId }: { id: string; userId: string }) =>
    request<{ team: Team }>(`/teams/${id}/members/${userId}`, { method: "DELETE" }),
  );
}

/** A null team is not an omission: it is the work going back to the project. */
export function useSetIssueTeam() {
  return useTeamMutation(({ key, teamId }: { key: string; teamId: string | null }) =>
    request<{ issue: Issue }>(`/issues/${key}/team`, { method: "PUT", body: { teamId } }),
  );
}
