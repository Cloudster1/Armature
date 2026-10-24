import { useEffect } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { applyLanguage, languageOfLocale } from "@/i18n";
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

export interface Installation {
  id: string;
  name: string;
}

export interface Principal {
  user: User;
  installation?: Installation;
  role?: OrgRole;
  /** The one desk a session that came in without a code may see. */
  portalDesk?: string;
  /** The installation requires two-step sign-in of this person, who has not set it up yet. */
  mustEnrol?: boolean;
}

interface MeResponse {
  principal: Principal;
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

/** Follows the signed-in person's language whenever the app has asked who they are; it never asks itself. */
export function useProfileLanguage(): void {
  const { data } = useQuery({ queryKey: meQueryKey, queryFn: () => request<MeResponse>("/auth/me"), enabled: false });
  const locale = data?.principal?.user.locale;
  useEffect(() => {
    if (locale !== undefined) applyLanguage(languageOfLocale(locale));
  }, [locale]);
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
    mutationFn: (input: SignupInput) => request<{ principal: Principal }>("/auth/signup", { method: "POST", body: input }),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: meQueryKey }),
  });
}

/** Signs in with a password; somebody with two-step sign-in gets a challenge to answer with a code instead. */
export function useLogin() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (input: { email: string; password: string }) => request<LoginAnswer>("/auth/login", { method: "POST", body: input }),
    onSuccess: (answer) => {
      if (!("challenge" in answer)) void queryClient.invalidateQueries({ queryKey: meQueryKey });
    },
  });
}

/** Where single sign-on starts: the browser goes there itself, to follow the provider's redirects. */
export const SSO_START = "/auth/oidc/start";

/** Asks for a link to set a new password; the answer is the same for every address. */
export function useRequestPasswordReset() {
  return useMutation({
    mutationFn: (email: string) => request<{ expiresInSeconds: number }>("/auth/password-reset", { method: "POST", body: { email } }),
  });
}

/** Sets a new password with a mailed link, which ends every session, this tab's included. */
export function useResetPassword() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (input: { token: string; password: string }) => request<void>("/auth/password-reset/confirm", { method: "POST", body: input }),
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
  /** The connected app a grant belongs to; absent for a token made here. */
  app?: { clientId: string; name: string; clientUri?: string };
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
    // Not awaited: a new language draws the page again, and the caller's own
    // onSuccess, its confirmation, would never run once the page it came from is gone.
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: meQueryKey });
    },
  });
}

/** Changes the caller's own password; every other session of theirs ends. */
export function useChangeMyPassword() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (input: { currentPassword: string; newPassword: string }) => request<void>("/auth/me/password", { method: "PUT", body: input }),
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
