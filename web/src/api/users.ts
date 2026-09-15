import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { request } from "./client";
import type { OrgRole } from "./auth";

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
}

export const usersQueryKey = ["users"] as const;

export function useManagedUsers() {
  return useQuery({
    queryKey: usersQueryKey,
    queryFn: () => request<{ users: ManagedUser[] }>("/users"),
  });
}

/** Every change to an account shows in the pickers too, so both lists are refetched. */
function useUserMutation<TArgs, TResult>(run: (args: TArgs) => Promise<TResult>) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: run,
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: usersQueryKey });
      void queryClient.invalidateQueries({ queryKey: ["members"] });
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
