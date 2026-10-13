import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useMutation } from "./mutation";
import { request } from "./client";
import { meQueryKey, type Principal } from "./auth";
import { usersQueryKey } from "./users";

/** The code the API holds a session back with until its person sets up two-step sign-in. */
export const SECOND_FACTOR_REQUIRED_CODE = "second_factor_required";

/** The answer to a sign-in's code step that has expired or run out of tries. */
export const CHALLENGE_INVALID_CODE = "challenge_invalid";

/** What a person sees of their own two-step sign-in; never the secret. */
export interface SecondFactorStatus {
  enrolled: boolean;
  enrolledAt?: string;
  recoveryCodesLeft: number;
  /** Whether this server can set one up at all. */
  available: boolean;
}

/** What an authenticator app is given, shown once while setting it up. */
export interface Enrolment {
  secret: string;
  /** The otpauth address the QR code carries. */
  uri: string;
}

/** Who an organization requires two-step sign-in of. */
export type SecondFactorRule = "off" | "admins" | "everyone";

/** A password sign-in for somebody with two-step sign-in answers with this instead of a session. */
export interface LoginChallenge {
  challenge: string;
  expiresInSeconds: number;
}

export type LoginAnswer = { principal: Principal } | LoginChallenge;

export function isChallenge(answer: LoginAnswer | undefined): answer is LoginChallenge {
  return answer !== undefined && "challenge" in answer;
}

export const secondFactorQueryKey = ["auth", "me", "second-factor"] as const;
const ruleQueryKey = ["organization", "second-factor"] as const;

export function useSecondFactorStatus() {
  return useQuery({
    queryKey: secondFactorQueryKey,
    queryFn: () => request<{ secondFactor: SecondFactorStatus }>("/auth/me/second-factor"),
  });
}

/** Each change is read back with the session, which may stop being held by it. */
function useSecondFactorMutation<TArgs, TResult>(run: (args: TArgs) => Promise<TResult>) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: run,
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: secondFactorQueryKey });
      void queryClient.invalidateQueries({ queryKey: meQueryKey });
    },
  });
}

export function useStartSecondFactor() {
  return useMutation({
    mutationFn: () => request<{ enrolment: Enrolment }>("/auth/me/second-factor", { method: "POST" }),
  });
}

/**
 * Turns the started secret on. Nothing is refetched here: the recovery codes it
 * answers with are shown first, and useSecondFactorSettled reads the change after.
 */
export function useConfirmSecondFactor() {
  return useMutation({
    mutationFn: (code: string) =>
      request<{ recoveryCodes: string[] }>("/auth/me/second-factor/confirm", { method: "POST", body: { code } }),
  });
}

/** Reads back the person and their factor once the recovery codes have been put away. */
export function useSecondFactorSettled() {
  const queryClient = useQueryClient();
  return () => {
    void queryClient.invalidateQueries({ queryKey: secondFactorQueryKey });
    void queryClient.invalidateQueries({ queryKey: meQueryKey });
  };
}

export function useRenewRecoveryCodes() {
  return useSecondFactorMutation((code: string) =>
    request<{ recoveryCodes: string[] }>("/auth/me/second-factor/recovery-codes", { method: "POST", body: { code } }),
  );
}

/** Turns two-step sign-in off; every other session of the person's ends. */
export function useRemoveSecondFactor() {
  return useSecondFactorMutation((code: string) => request<void>("/auth/me/second-factor/remove", { method: "POST", body: { code } }));
}

/** Finishes a password sign-in with the app's code or a recovery code. */
export function useLoginSecondFactor() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (input: { challenge: string; code: string }) =>
      request<{ principal: Principal }>("/auth/login/second-factor", { method: "POST", body: input }),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: meQueryKey }),
  });
}

export function useSecondFactorRule() {
  return useQuery({
    queryKey: ruleQueryKey,
    queryFn: () => request<{ require: SecondFactorRule }>("/organization/second-factor"),
  });
}

export function useRequireSecondFactor() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (require: SecondFactorRule) =>
      request<{ require: SecondFactorRule }>("/organization/second-factor", { method: "PUT", body: { require } }),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: ruleQueryKey });
      void queryClient.invalidateQueries({ queryKey: meQueryKey });
    },
  });
}

/** An administrator's way back in for somebody who lost the phone and the codes. */
export function useRemoveMembersSecondFactor() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (userId: string) => request<void>(`/users/${userId}/second-factor`, { method: "DELETE" }),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: usersQueryKey }),
  });
}
