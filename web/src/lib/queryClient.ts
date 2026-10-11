import { MutationCache, QueryClient } from "@tanstack/react-query";
import { ApiError } from "@/api/client";
import { QUERY_STALE_MS } from "@/config";

/** Said when a change failed without the server giving a sentence of its own. */
export const UNSAVED_MESSAGE = "That change could not be saved. Check your connection and try again.";

type Listener = (message: string) => void;

const listeners = new WeakMap<QueryClient, Set<Listener>>();

/**
 * The application's one query client. A change that fails, and whose caller
 * does not show the failure itself, is passed to whoever listens for it.
 */
export function createQueryClient(): QueryClient {
  const queryClient: QueryClient = new QueryClient({
    mutationCache: new MutationCache({
      onError: (error, _variables, _context, mutation) => {
        if (mutation.meta?.handlesError) return;
        const message = error instanceof ApiError ? error.message : UNSAVED_MESSAGE;
        listeners.get(queryClient)?.forEach((listener) => listener(message));
      },
    }),
    defaultOptions: {
      queries: {
        // A failed request is usually a real answer (401, 404, validation), not
        // a blip worth retrying three times while the user waits.
        retry: false,
        refetchOnWindowFocus: false,
        staleTime: QUERY_STALE_MS,
      },
    },
  });
  return queryClient;
}

/** Hears every failed change nobody else reported; returns how to stop. */
export function onUnreportedMutationError(queryClient: QueryClient, listener: Listener): () => void {
  const set = listeners.get(queryClient) ?? new Set<Listener>();
  listeners.set(queryClient, set);
  set.add(listener);
  return () => {
    set.delete(listener);
  };
}
