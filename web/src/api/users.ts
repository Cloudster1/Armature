import { keepPreviousData, useQuery, useQueryClient } from "@tanstack/react-query";
import { useMutation } from "./mutation";
import { request } from "./client";
import type { OrgRole } from "./auth";
import { membersQueryKey } from "./people";
import { PEOPLE_PAGE_SIZE } from "@/config";

export type SignInMethod = "password" | "provider" | "none";

/** A member as the users page sees them, with what an administrator may do. */
export interface ManagedUser {
  id: string;
  email: string;
  name: string;
  role: OrgRole;
  isActive: boolean;
  signsInWith: SignInMethod;
  /** This is the one place the person belongs, so their account is this organization's to change. */
  managed: boolean;
  createdAt: string;
  /** Two-step sign-in is on for them. */
  secondFactor: boolean;
}

export const usersQueryKey = ["users"] as const;

/** One page of the accounts that matched, and how many did in all. */
export interface UserPage {
  users: ManagedUser[];
  total: number;
  limit: number;
  offset: number;
}

/** A page of the accounts by name, found by part of a name or an address. */
export function useManagedUsers({ q = "", offset = 0, limit = PEOPLE_PAGE_SIZE }: { q?: string; offset?: number; limit?: number } = {}) {
  const text = q.trim();
  return useQuery({
    queryKey: [...usersQueryKey, { q: text, offset, limit }],
    queryFn: () => {
      const params = new URLSearchParams({ limit: String(limit) });
      if (text) params.set("q", text);
      if (offset) params.set("offset", String(offset));
      return request<UserPage>(`/users?${params}`);
    },
    placeholderData: keepPreviousData,
  });
}

/** Every change to an account shows in the pickers too, so both lists are refetched. */
function useUserMutation<TArgs, TResult>(run: (args: TArgs) => Promise<TResult>) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: run,
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: usersQueryKey });
      void queryClient.invalidateQueries({ queryKey: membersQueryKey });
    },
  });
}

export interface CreateUserInput {
  email: string;
  name: string;
  role: "admin" | "member";
  password: string;
}

export function useCreateUser() {
  return useUserMutation((input: CreateUserInput) => request<{ user: ManagedUser }>("/users", { method: "POST", body: input }));
}

export interface UpdateUserInput {
  id: string;
  name?: string;
  role?: "admin" | "member";
  isActive?: boolean;
}

export function useUpdateUser() {
  return useUserMutation(({ id, ...body }: UpdateUserInput) => request<{ user: ManagedUser }>(`/users/${id}`, { method: "PATCH", body }));
}

export function useSetUserPassword() {
  return useUserMutation(({ id, password }: { id: string; password: string }) =>
    request<void>(`/users/${id}/password`, { method: "PUT", body: { password } }),
  );
}

/** Somebody the identity provider vouched for who is waiting to be let in. */
export interface JoinRequest {
  userId: string;
  email: string;
  name: string;
  requestedAt: string;
}

export const joinRequestsQueryKey = ["users", "requests"] as const;

export function useJoinRequests() {
  return useQuery({
    queryKey: joinRequestsQueryKey,
    queryFn: () => request<{ requests: JoinRequest[] }>("/users/requests"),
  });
}

export function useAdmitJoinRequest() {
  return useUserMutation(({ userId, role }: { userId: string; role: "admin" | "member" }) =>
    request<{ user: ManagedUser }>(`/users/requests/${userId}/admit`, { method: "POST", body: { role } }),
  );
}

export function useDeclineJoinRequest() {
  return useUserMutation(({ userId }: { userId: string }) => request<void>(`/users/requests/${userId}`, { method: "DELETE" }));
}
