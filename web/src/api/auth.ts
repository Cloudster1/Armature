import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { request, upload } from "./client";

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

export interface SignupInput {
  email: string;
  password: string;
  name: string;
  orgName: string;
  orgSlug?: string;
}

/** Whether this installation lets a new organization be created by signing up. */
export function useSignupOpen() {
  return useQuery({
    queryKey: ["auth", "signup"],
    queryFn: () => request<{ open: boolean }>("/auth/signup"),
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

export function useLogin() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (input: { email: string; password: string }) =>
      request<{ principal: Principal }>("/auth/login", { method: "POST", body: input }),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: meQueryKey }),
  });
}

/** The address of everything held about the caller, as one JSON file. */
export const MY_DATA_HREF = "/api/v1/auth/me/export";

/** Erases the caller's account: the identity goes, the work stays as Former user. */
export function useEraseMe() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: () => request<void>("/auth/me", { method: "DELETE" }),
    onSuccess: () => queryClient.clear(),
  });
}

/** Lets a member go from the organization; their account stays theirs elsewhere. */
export function useRemoveMember() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (userID: string) => request<void>(`/members/${userID}`, { method: "DELETE" }),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ["members"] }),
  });
}

export function useLogout() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: () => request<void>("/auth/logout", { method: "POST" }),
    // Clear everything, not just the session: cached issues and boards belong
    // to the person who just signed out.
    onSuccess: () => queryClient.clear(),
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
