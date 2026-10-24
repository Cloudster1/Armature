import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useMutation } from "./mutation";
import { request } from "./client";
import { tokensQueryKey } from "./auth";

/** A scope on the consent page, in the words the person reads. */
export interface ScopeConsent {
  scope: string;
  words: string;
}

/** What the consent page shows for an /oauth/authorize query. */
export interface Consent {
  client: { id: string; name: string; clientUri?: string };
  redirectHost: string;
  /** Nobody has allowed the app yet; only an administrator may. */
  pending: boolean;
  person: string;
  scopes: ScopeConsent[];
  mayAllow: boolean;
  mayCancel: boolean;
  /** Why the person may not allow it, when they may not. */
  refusal?: string;
}

/** Either the page to show, or where the browser goes at once. */
export interface ConsentAnswer {
  consent?: Consent;
  redirectTo?: string;
}

/** The consent the page was opened with: its own query, passed on whole. */
export function useConsent(query: string) {
  return useQuery({
    queryKey: ["oauth", "consent", query],
    queryFn: () => request<ConsentAnswer>(`/oauth/consent?${query}`),
    retry: false,
    // A code is made when the answer is to go at once; asking again would make another.
    staleTime: Infinity,
    refetchOnWindowFocus: false,
  });
}

export function useDecideConsent() {
  return useMutation({
    mutationFn: (input: { query: string; decision: "allow" | "deny" }) => request<{ redirectTo: string }>("/oauth/consent", { method: "POST", body: input }),
  });
}

/** A partner tool registered to ask people for a token. */
export interface ConnectedApp {
  id: string;
  name: string;
  clientUri: string;
  redirectUris: string[];
  scopes: string[];
  confidential: boolean;
  /** active, or pending until an administrator allows it. */
  status: string;
  keycloakClientId: string;
  /** The webhook the app set up for itself, if it has. */
  webhook?: { id: string; url: string; topics: string[]; enabled: boolean } | null;
  grants: number;
  createdAt: string;
  updatedAt: string;
  /** Only on the answer that made the app or rotated its secret. */
  secret?: string;
}

export interface ConnectedAppInput {
  name: string;
  clientUri?: string;
  redirectUris: string[];
  scopes: string[];
  confidential?: boolean;
  keycloakClientId?: string;
}

export type ConnectedAppChanges = Partial<Omit<ConnectedAppInput, "confidential">>;

const appsQueryKey = ["oauth", "clients"] as const;

export function useConnectedApps() {
  return useQuery({ queryKey: appsQueryKey, queryFn: () => request<{ clients: ConnectedApp[] }>("/oauth/clients") });
}

/** Every change to an app can change the grants in somebody's token list too. */
function useAppMutation<TVariables>(fn: (input: TVariables) => Promise<{ client: ConnectedApp } | void>) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: fn,
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: appsQueryKey });
      void queryClient.invalidateQueries({ queryKey: tokensQueryKey });
    },
  });
}

export function useCreateConnectedApp() {
  return useAppMutation((input: ConnectedAppInput) => request<{ client: ConnectedApp }>("/oauth/clients", { method: "POST", body: input }));
}

export function useUpdateConnectedApp() {
  return useAppMutation(({ id, ...changes }: ConnectedAppChanges & { id: string }) =>
    request<{ client: ConnectedApp }>(`/oauth/clients/${id}`, { method: "PATCH", body: changes }),
  );
}

export function useApproveConnectedApp() {
  return useAppMutation((id: string) => request<{ client: ConnectedApp }>(`/oauth/clients/${id}/approve`, { method: "POST" }));
}

export function useRotateConnectedAppSecret() {
  return useAppMutation((id: string) => request<{ client: ConnectedApp }>(`/oauth/clients/${id}/rotate-secret`, { method: "POST" }));
}

export function useDeleteConnectedApp() {
  return useAppMutation((id: string) => request<void>(`/oauth/clients/${id}`, { method: "DELETE" }));
}
