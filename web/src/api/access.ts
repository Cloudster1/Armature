import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { request } from "./client";

/**
 * Five roles, granted to people and to groups, over the whole organization or
 * over one project. What each grants is decided by the server and described
 * here rather than duplicated, so the two cannot drift apart.
 */
export type Role =
  | "global_administrator"
  | "project_administrator"
  | "scrum_master"
  | "user"
  | "reader";

export interface RoleDescription {
  role: Role;
  /** A role that only makes sense over the whole tenant. */
  orgWideOnly: boolean;
  permissions: string[];
}

export interface Grant {
  role: Role;
  /** Absent means the role is held over the whole organization. */
  projectKey?: string;
}

export interface Access {
  grants: Grant[];
  projects: string[];
  canAdministerOrg: boolean;
  canCreateProject: boolean;
}

export interface GroupMember {
  userId: string;
  name: string;
  email?: string;
}

export interface Group {
  id: string;
  name: string;
  description?: string;
  /** "oidc" means the identity provider owns who is in it. */
  source: "local" | "oidc";
  externalRef?: string;
  members?: GroupMember[];
  memberCount: number;
  roleCount: number;
}

export interface Assignment {
  id: string;
  role: Role;
  projectId?: string;
  projectKey?: string;
  userId?: string;
  userName?: string;
  groupId?: string;
  groupName?: string;
}

export interface Provider {
  issuer: string;
  clientId: string;
  /** The secret is never sent back; only whether one is stored. */
  hasSecret: boolean;
  groupsClaim: string;
  scopes: string;
  createGroups: boolean;
  enabled: boolean;
  updatedAt: string;
}

/** The roles that may configure a project, and the ones that may edit its issues. */
const ADMINISTERING_ROLES: Role[] = ["global_administrator", "project_administrator"];
const WRITING_ROLES: Role[] = ["global_administrator", "project_administrator", "scrum_master", "user"];

/**
 * Whether the grants held include one of the roles, over the whole
 * organization or over this project. A project scoped grant elsewhere does
 * not count, which is the point of scoping it.
 */
export function holdsRole(access: Access | undefined, roles: Role[], projectKey: string): boolean {
  if (!access) return false;
  if (access.canAdministerOrg) return true;
  return access.grants.some((g) => roles.includes(g.role) && (!g.projectKey || g.projectKey === projectKey));
}

/** Whether this person configures the project: its fields, boards, dashboards. */
export function canAdminister(access: Access | undefined, projectKey: string): boolean {
  return holdsRole(access, ADMINISTERING_ROLES, projectKey);
}

/** Whether this person may file and edit issues in the project. */
export function canWriteIssues(access: Access | undefined, projectKey: string): boolean {
  return holdsRole(access, WRITING_ROLES, projectKey);
}

export const accessQueryKey = ["access"] as const;

/** What the signed-in person may do, which decides which buttons to draw. */
export function useAccess() {
  return useQuery({
    queryKey: [...accessQueryKey, "me"],
    queryFn: () => request<Access>("/access/me"),
  });
}

export function useRoles() {
  return useQuery({
    queryKey: [...accessQueryKey, "roles"],
    queryFn: () => request<{ roles: RoleDescription[] }>("/roles"),
    staleTime: Infinity,
  });
}

export function useGroups() {
  return useQuery({
    queryKey: [...accessQueryKey, "groups"],
    queryFn: () => request<{ groups: Group[] }>("/groups"),
  });
}

export function useGroup(id: string) {
  return useQuery({
    queryKey: [...accessQueryKey, "group", id],
    queryFn: () => request<{ group: Group }>(`/groups/${id}`),
    enabled: Boolean(id),
  });
}

export function useAssignments(projectKey?: string) {
  return useQuery({
    queryKey: [...accessQueryKey, "assignments", projectKey ?? ""],
    queryFn: () =>
      request<{ assignments: Assignment[] }>(
        `/role-assignments${projectKey ? `?project=${projectKey}` : ""}`,
      ),
  });
}

export function useProvider() {
  return useQuery({
    queryKey: [...accessQueryKey, "provider"],
    queryFn: () => request<{ provider: Provider | null }>("/oidc-provider"),
  });
}

/**
 * Every write here changes what somebody may do, including possibly the person
 * making it, so nothing is left assumed afterwards.
 */
function useAccessMutation<TArgs, TResult>(run: (args: TArgs) => Promise<TResult>) {
  const queryClient = useQueryClient();
  return useMutation({ mutationFn: run, onSuccess: () => queryClient.invalidateQueries() });
}

export function useCreateGroup() {
  return useAccessMutation((input: { name: string; description?: string; externalRef?: string }) =>
    request<{ group: Group }>("/groups", { method: "POST", body: input }),
  );
}

export function useDeleteGroup() {
  return useAccessMutation((id: string) => request<void>(`/groups/${id}`, { method: "DELETE" }));
}

export function useAddToGroup() {
  return useAccessMutation(({ id, userId }: { id: string; userId: string }) =>
    request<{ group: Group }>(`/groups/${id}/members`, { method: "POST", body: { userId } }),
  );
}

export function useRemoveFromGroup() {
  return useAccessMutation(({ id, userId }: { id: string; userId: string }) =>
    request<{ group: Group }>(`/groups/${id}/members/${userId}`, { method: "DELETE" }),
  );
}

export function useGrantRole() {
  return useAccessMutation(
    (input: { role: Role; projectKey?: string; userId?: string; groupId?: string }) =>
      request<{ assignment: Assignment }>("/role-assignments", { method: "POST", body: input }),
  );
}

export function useRevokeRole() {
  return useAccessMutation((id: string) =>
    request<void>(`/role-assignments/${id}`, { method: "DELETE" }),
  );
}

export function useSaveProvider() {
  return useAccessMutation((input: Partial<Provider> & { clientSecret?: string }) =>
    request<{ provider: Provider }>("/oidc-provider", { method: "PUT", body: input }),
  );
}

/** How a role reads in a sentence, rather than as an identifier. */
export function roleName(role: Role): string {
  switch (role) {
    case "global_administrator":
      return "Global administrator";
    case "project_administrator":
      return "Project administrator";
    case "scrum_master":
      return "Scrum master";
    case "user":
      return "User";
    case "reader":
      return "Reader";
  }
}
