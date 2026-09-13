import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { request } from "./client";
import { issuesQueryKey } from "./issues";

/**
 * Who is told about an issue. A watcher is a person, by account or by mail
 * address; an address nobody has yet becomes a person with no password.
 */
export interface Watcher {
  userId: string;
  name: string;
  email: string;
  addedBy?: string;
  addedAt: string;
}

export const watchersQueryKey = ["watchers"] as const;

export function useWatchers(issueKey: string) {
  return useQuery({
    queryKey: [...watchersQueryKey, issueKey],
    queryFn: () => request<{ watchers: Watcher[] }>(`/issues/${issueKey}/watchers`),
    enabled: Boolean(issueKey),
  });
}

function useWatcherMutation<TArgs, TResult>(run: (args: TArgs) => Promise<TResult>) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: run,
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: watchersQueryKey });
      queryClient.invalidateQueries({ queryKey: issuesQueryKey });
    },
  });
}

/** Adds a watcher by account, by address, or, with neither, the caller. */
export function useAddWatcher() {
  return useWatcherMutation(({ key, ...who }: { key: string; userId?: string; email?: string }) =>
    request<{ watcher: Watcher }>(`/issues/${key}/watchers`, { method: "POST", body: who }),
  );
}

export function useRemoveWatcher() {
  return useWatcherMutation(({ key, userId }: { key: string; userId: string }) =>
    request<void>(`/issues/${key}/watchers/${userId}`, { method: "DELETE" }),
  );
}

/** Ends a watching with the token a mail carried; no session is involved. */
export function useUnwatch() {
  return useMutation({
    mutationFn: (token: string) => request<void>("/unwatch", { method: "POST", body: { token } }),
  });
}
