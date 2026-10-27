import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useMutation } from "./mutation";
import { request } from "./client";

/** One ask for access, as the project's administrators read it. */
export interface AccessRequest {
  id: string;
  projectKey: string;
  projectName: string;
  issueKey: string;
  message: string;
  requester: { id: string; name: string; email: string };
  createdAt: string;
  grantedAt?: string;
  grantedBy?: { id: string; name: string; email: string };
}

const accessRequestsKey = ["access-requests"] as const;

/** Asks for access to an issue; the answer is the same whatever the key names. */
export function useAskForAccess(issueKey: string) {
  return useMutation({
    mutationFn: (message: string) =>
      request<{ received: boolean }>(`/issues/${encodeURIComponent(issueKey)}/access-requests`, { method: "POST", body: { message } }),
  });
}

export function useAccessRequest(id: string) {
  return useQuery({
    queryKey: [...accessRequestsKey, id],
    queryFn: () => request<{ accessRequest: AccessRequest }>(`/access-requests/${id}`),
    enabled: Boolean(id),
  });
}

/** Gives whoever asked the reader role on the request's project. */
export function useGrantAccessRequest(id: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: () => request<{ accessRequest: AccessRequest }>(`/access-requests/${id}/grant`, { method: "POST" }),
    onSuccess: (data) => queryClient.setQueryData([...accessRequestsKey, id], data),
  });
}
