import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { request } from "./client";
import type { OrgRole, Principal } from "./auth";

export type InvitableRole = Exclude<OrgRole, "owner">;

export interface Invite {
  id: string;
  email: string;
  role: InvitableRole;
  expiresAt: string;
  createdAt: string;
}

export interface CreatedInvite {
  invite: Invite;
  /** The page the invited person opens; shown once, it carries the secret. */
  link: string;
  /** Whether the link also went out by mail. */
  mailed: boolean;
}

export interface InvitePreview {
  orgName: string;
  email: string;
  role: InvitableRole;
}

const invitesKey = ["invites"] as const;
const invitePreviewKey = "invite-preview";

export function useInvites() {
  return useQuery({
    queryKey: invitesKey,
    queryFn: () => request<{ invites: Invite[] }>("/invites"),
  });
}

export function useCreateInvite() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (input: { email: string; role: InvitableRole }) =>
      request<CreatedInvite>("/invites", { method: "POST", body: input }),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: invitesKey }),
  });
}

export function useWithdrawInvite() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (id: string) =>
      request<void>(`/invites/${id}`, { method: "DELETE" }),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: invitesKey }),
  });
}

/** Where a link leads. A POST so the secret stays out of logs and history. */
export function useInvitePreview(token: string | undefined) {
  return useQuery({
    queryKey: [invitePreviewKey, token],
    queryFn: () =>
      request<{ invite: InvitePreview }>("/auth/invites/preview", {
        method: "POST",
        body: { token },
      }),
    enabled: Boolean(token),
    retry: false,
    // Asked once per visit: once accepted, the same question answers "used up",
    // which is true but not what the person who just accepted should read.
    staleTime: Infinity,
  });
}

export function useAcceptInvite() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (input: { token: string; name?: string; password?: string }) =>
      request<{ principal: Principal }>("/auth/invites/accept", {
        method: "POST",
        body: input,
      }),
    // Accepting signs the person in, or moves their session to the new
    // organization, so everything cached belongs to somebody else's view;
    // everything but the invitation itself, which is used up now.
    onSuccess: () =>
      queryClient.invalidateQueries({
        predicate: (query) => query.queryKey[0] !== invitePreviewKey,
      }),
  });
}
