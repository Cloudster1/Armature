import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { request } from "./client";

/**
 * Roles are the organization's own: the five it starts with and any it adds,
 * each a key and a list of permissions the server decides. A role is a
 * string here, because the list is data.
 */
export type Role = string;

/** One thing somebody may do; the server names them and says what each means. */
export type Permission = string;

export interface RoleDescription {
  role: Role;
  name: string;
  description: string;
  /** A role that only makes sense over the whole tenant. */
  orgWideOnly: boolean;
  /** One of the five every organization starts with: editable, never deleted. */
  builtin: boolean;
  permissions: Permission[];
  /** How many grants name it. */
  inUse: number;
}

export interface PermissionDescription {
  permission: Permission;
  words: string;
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
  /** What is held: over the whole organization, and in each project named explicitly. */
  permissions: { org: Permission[]; projects: Record<string, Permission[]> };
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

/**
 * Whether a permission is held in this project, from a grant over the whole
 * organization or over this project alone. A project scoped grant elsewhere
 * does not count, which is the point of scoping it. Decided by permission,
 * never by role name: roles are the organization's to redefine.
 */
export function holds(access: Access | undefined, permission: Permission, projectKey: string): boolean {
  if (!access) return false;
  if (access.permissions.org.includes(permission)) return true;
  return (access.permissions.projects[projectKey] ?? []).includes(permission);
}

/** Whether this person configures the project: its fields, boards, dashboards. */
export function canAdminister(access: Access | undefined, projectKey: string): boolean {
  return holds(access, "project.administer", projectKey);
}

/** Whether this person may file and edit issues in the project. */
export function canWriteIssues(access: Access | undefined, projectKey: string): boolean {
  return holds(access, "issue.write", projectKey);
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
  });
}

export function usePermissions() {
  return useQuery({
    queryKey: [...accessQueryKey, "permissions"],
    queryFn: () => request<{ permissions: PermissionDescription[] }>("/permissions"),
    staleTime: Infinity,
  });
}

/** A role's name in words, from the organization's list; the key made readable while it loads. */
export function useRoleName(): (role: Role) => string {
  const { data } = useRoles();
  const names = new Map((data?.roles ?? []).map((each) => [each.role, each.name]));
  return (role) => names.get(role) ?? roleName(role);
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

export interface RoleInput {
  key?: string;
  name: string;
  description?: string;
  orgWideOnly?: boolean;
  permissions: Permission[];
}

export function useCreateRole() {
  return useAccessMutation((input: RoleInput) => request<{ role: RoleDescription }>("/roles", { method: "POST", body: input }));
}

export function useUpdateRole() {
  return useAccessMutation(({ role, ...input }: { role: Role; name?: string; description?: string; orgWideOnly?: boolean; permissions?: Permission[] }) =>
    request<{ role: RoleDescription }>(`/roles/${role}`, { method: "PATCH", body: input }),
  );
}

export function useDeleteRole() {
  return useAccessMutation((role: Role) => request<void>(`/roles/${role}`, { method: "DELETE" }));
}

export function useSaveProvider() {
  return useAccessMutation((input: Partial<Provider> & { clientSecret?: string }) =>
    request<{ provider: Provider }>("/oidc-provider", { method: "PUT", body: input }),
  );
}

/** How a role reads in a sentence, rather than as an identifier. */
/**
 * What a permission lets somebody do, in words. The server names permissions
 * for code; somebody deciding whom to give a role reads this instead. An
 * unknown one is shown as it is rather than hidden.
 */
export function permissionWords(permission: string): string {
  const words: Record<string, string> = {
    read: "see projects",
    "issue.write": "create and edit issues",
    "issue.transition": "move issues through the workflow",
    "comment.write": "comment",
    "sprint.manage": "plan sprints",
    "team.manage": "manage teams",
    "board.configure": "set up boards",
    "project.administer": "administer projects",
    "project.create": "create projects",
    "org.administer": "administer the organization",
  };
  return words[permission] ?? permission;
}

/** A role's key made readable, for before the organization's list has loaded. */
export function roleName(role: Role): string {
  const words = role.replace(/_/g, " ").trim();
  return words ? words[0]!.toUpperCase() + words.slice(1) : role;
}
