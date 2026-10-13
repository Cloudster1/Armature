import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useMutation } from "./mutation";
import { request, upload } from "./client";
import { cacheCustomTheme } from "@/lib/theme";
import type { LoginAnswer } from "./secondFactor";

export type OrgRole = "owner" | "admin" | "member" | "customer";

export interface User {
  id: string;
  email: string;
  name: string;
  timezone: string;
  locale: string;
  isActive: boolean;
  /** Where the picture is served from; absent means initials. */
  avatarUrl?: string;
}

export interface Org {
  id: string;
  slug: string;
  name: string;
}

export interface Principal {
  user: User;
  org?: Org;
  role?: OrgRole;
  /** The one desk a session that came in without a code may see. */
  portalDesk?: string;
  /** The organization requires two-step sign-in of this person, who has not set it up yet. */
  mustEnrol?: boolean;
}

export interface Membership {
  orgId: string;
  orgSlug: string;
  orgName: string;
  role: OrgRole;
}

interface MeResponse {
  principal: Principal;
  organizations: Membership[] | null;
  /** What the server allows, so a file can be refused before it is sent. */
  limits?: { uploadBytes: number };
}

export const meQueryKey = ["auth", "me"] as const;

/**
 * The session query. It is deliberately not retried: a 401 is a definite
 * answer, and retrying it only delays showing the sign-in screen.
 */
export function useMe() {
  return useQuery({
    queryKey: meQueryKey,
    queryFn: () => request<MeResponse>("/auth/me"),
    retry: false,
    staleTime: 30_000,
  });
}

/** Makes a demo organization with a production line in it and moves the session there. */
export function useCreateDemoOrganization() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: () => request<{ organization: Org; projectKey: string }>("/organizations/demo", { method: "POST" }),
    onSuccess: () => {
      cacheCustomTheme(null);
      queryClient.clear();
    },
  });
}

/** The person signing up; the installation is not theirs to name. */
export interface SignupInput {
  email: string;
  password: string;
  name: string;
}

/** Whether signing up would work, and whether it would set the installation up. */
export interface SignupState {
  open: boolean;
  first: boolean;
}

export function useSignupOpen() {
  return useQuery({
    queryKey: ["auth", "signup"],
    queryFn: () => request<SignupState>("/auth/signup"),
    staleTime: 60_000,
  });
}

export function useSignup() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (input: SignupInput) =>
      request<{ principal: Principal }>("/auth/signup", { method: "POST", body: input }),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: meQueryKey }),
  });
}

/** Signs in with a password; somebody with two-step sign-in gets a challenge to answer with a code instead. */
export function useLogin() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (input: { email: string; password: string }) =>
      request<LoginAnswer>("/auth/login", { method: "POST", body: input }),
    onSuccess: (answer) => {
      if (!("challenge" in answer)) void queryClient.invalidateQueries({ queryKey: meQueryKey });
    },
  });
}

/** Asks for a link to set a new password; the answer is the same for every address. */
export function useRequestPasswordReset() {
  return useMutation({
    mutationFn: (email: string) =>
      request<{ expiresInSeconds: number }>("/auth/password-reset", { method: "POST", body: { email } }),
  });
}

/** Sets a new password with a mailed link, which ends every session, this tab's included. */
export function useResetPassword() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (input: { token: string; password: string }) =>
      request<void>("/auth/password-reset/confirm", { method: "POST", body: input }),
    onSuccess: () => queryClient.removeQueries({ queryKey: meQueryKey }),
  });
}

/** The address of everything held about the caller, as one JSON file. */
export const MY_DATA_HREF = "/api/v1/auth/me/export";

/** Erases the caller's account: the identity goes, the work stays as Former user. */
export function useEraseMe() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: () => request<void>("/auth/me", { method: "DELETE" }),
    onSuccess: () => {
      cacheCustomTheme(null);
      queryClient.clear();
    },
  });
}

/** Lets a member go from the organization; their account stays theirs elsewhere. */
export function useLogout() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: () => request<void>("/auth/logout", { method: "POST" }),
    // Clear everything, not just the session: cached issues and boards belong
    // to the person who just signed out, and so does the theme.
    onSuccess: () => {
      cacheCustomTheme(null);
      queryClient.clear();
    },
  });
}

/** Deletes the current organization; its address typed back is the confirmation. */
export function useDeleteOrganization() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (confirm: string) => request<void>(`/organization?confirm=${encodeURIComponent(confirm)}`, { method: "DELETE" }),
    onSuccess: () => queryClient.invalidateQueries(),
  });
}

export function useSwitchOrg() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (slug: string) =>
      request<{ organization: Org }>("/auth/switch-org", { method: "POST", body: { slug } }),
    onSuccess: () => queryClient.invalidateQueries(),
  });
}

export interface ApiToken {
  id: string;
  name: string;
  scopes: string[];
  /** The project keys the token is confined to; empty reaches wherever its owner does. */
  projects: string[];
  lastUsedAt?: string;
  expiresAt?: string;
  createdAt: string;
  secret?: string;
}

export const tokensQueryKey = ["tokens"] as const;

export function useApiTokens() {
  return useQuery({
    queryKey: tokensQueryKey,
    queryFn: () => request<{ tokens: ApiToken[] | null }>("/tokens"),
  });
}

export function useCreateApiToken() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (input: { name: string; scopes: string[]; projects: string[]; expiresAt?: string }) =>
      request<{ token: ApiToken }>("/tokens", { method: "POST", body: input }),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: tokensQueryKey }),
  });
}

export function useRevokeApiToken() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (id: string) => request<void>(`/tokens/${id}`, { method: "DELETE" }),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: tokensQueryKey }),
  });
}

/** One place the person is signed in. */
export interface ActiveSession {
  id: string;
  userAgent: string;
  ip: string;
  /** The organization the session acts in; empty before one is chosen. */
  orgName: string;
  proof: string;
  createdAt: string;
  lastSeenAt: string;
  /** The session this browser is calling with. */
  current: boolean;
}

export const sessionsQueryKey = ["auth", "sessions"] as const;

export function useSessions() {
  return useQuery({
    queryKey: sessionsQueryKey,
    queryFn: () => request<{ sessions: ActiveSession[] }>("/auth/sessions"),
  });
}

/** Signs one of the person's other sessions out. */
export function useEndSession() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (id: string) => request<void>(`/auth/sessions/${id}`, { method: "DELETE" }),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: sessionsQueryKey }),
  });
}

/** Signs the person out everywhere but here. */
export function useEndOtherSessions() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: () => request<void>("/auth/sessions", { method: "DELETE" }),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: sessionsQueryKey }),
  });
}

export interface ProfileInput {
  name?: string;
  timezone?: string;
  locale?: string;
}

export function useUpdateProfile() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (input: ProfileInput) => request<{ principal: Principal }>("/auth/me", { method: "PATCH", body: input }),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: meQueryKey }),
  });
}

/** Changes the caller's own password; every other session of theirs ends. */
export function useChangeMyPassword() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (input: { currentPassword: string; newPassword: string }) =>
      request<void>("/auth/me/password", { method: "PUT", body: input }),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: sessionsQueryKey }),
  });
}

export function useUploadAvatar() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (file: File) => {
      const form = new FormData();
      form.append("file", file, file.name);
      return upload<{ principal: Principal }>("/auth/me/avatar", form);
    },
    // Every list that shows a face is refetched, so the new one appears everywhere.
    onSuccess: () => queryClient.invalidateQueries(),
  });
}

export function useRemoveAvatar() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: () => request<void>("/auth/me/avatar", { method: "DELETE" }),
    onSuccess: () => queryClient.invalidateQueries(),
  });
}

/** The languages the API offers, as a select shows them. */
export const LOCALES: Array<{ code: string; label: string }> = [
  { code: "en-GB", label: "English (United Kingdom)" },
  { code: "en-US", label: "English (United States)" },
  { code: "de-DE", label: "Deutsch (Deutschland)" },
  { code: "fr-FR", label: "Francais (France)" },
  { code: "es-ES", label: "Espanol (Espana)" },
  { code: "nl-NL", label: "Nederlands (Nederland)" },
  { code: "it-IT", label: "Italiano (Italia)" },
  { code: "pt-BR", label: "Portugues (Brasil)" },
];
