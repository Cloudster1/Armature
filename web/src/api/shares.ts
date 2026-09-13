import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { BASE, request } from "./client";
import type { Dashboard, KindInfo } from "./reports";

/** A link to a dashboard that needs no sign-in. The address is shown once, when it is made. */
export interface Share {
  id: string;
  dashboardId: string;
  name: string;
  /** The dashboard's filter as it was when the link was made. */
  query?: string;
  createdBy?: string;
  createdAt: string;
  expiresAt?: string;
}

/** What a link opens. */
export interface SharedView {
  share: Share;
  dashboard: Dashboard;
  projectName: string;
  /** Which kinds the frozen filter reaches, so the page can say which it does not. */
  kinds: KindInfo[];
  generatedAt: string;
}

/** Where a shared dashboard's PDF is downloaded from; the token is all a visitor holds. */
export function sharedPdfHref(token: string): string {
  return `${BASE}/shared/${token}/pdf`;
}

export const sharesQueryKey = ["shares"] as const;

export function useShares(dashboardId: string) {
  return useQuery({
    queryKey: [...sharesQueryKey, dashboardId],
    queryFn: () => request<{ shares: Share[] }>(`/dashboards/${dashboardId}/shares`),
    enabled: Boolean(dashboardId),
  });
}

function useShareMutation<TArgs, TResult>(run: (args: TArgs) => Promise<TResult>) {
  const queryClient = useQueryClient();
  return useMutation({ mutationFn: run, onSuccess: () => queryClient.invalidateQueries({ queryKey: sharesQueryKey }) });
}

export function useCreateShare() {
  return useShareMutation(({ dashboardId, name, query, expiresAt }: { dashboardId: string; name: string; query: string; expiresAt?: string }) =>
    request<{ share: Share; url: string }>(`/dashboards/${dashboardId}/shares`, { method: "POST", body: { name, query, expiresAt } }),
  );
}

export function useRevokeShare() {
  return useShareMutation(({ dashboardId, shareId }: { dashboardId: string; shareId: string }) =>
    request(`/dashboards/${dashboardId}/shares/${shareId}`, { method: "DELETE" }),
  );
}

/** The shared dashboard behind a token; a gone link is an error the page names. */
export function useShared(token: string) {
  return useQuery({
    queryKey: ["shared", token],
    queryFn: () => request<SharedView>(`/shared/${token}`),
    enabled: Boolean(token),
    retry: false,
  });
}
