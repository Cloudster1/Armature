import { MutationCache, QueryCache, QueryClient } from "@tanstack/react-query";
import { ApiError } from "@/api/client";
import { meQueryKey } from "@/api/auth";
import { SECOND_FACTOR_REQUIRED_CODE } from "@/api/secondFactor";
import { QUERY_STALE_MS } from "@/config";

/** Said when a change failed without the server giving a sentence of its own. */
export const UNSAVED_MESSAGE = "That change could not be saved. Check your connection and try again.";

type Listener = (message: string) => void;

const listeners = new WeakMap<QueryClient, Set<Listener>>();

/** The one query client: a 401 means the session ended and goes to onSessionEnded;
 * another failed change its caller does not show goes to the listeners. */
export function createQueryClient(onSessionEnded: () => void = () => {}): QueryClient {
  // Only a session the page believed in can end; a refused sign-in has none,
  // and once it is dropped the other requests still in flight change nothing.
  function refused(error: unknown): boolean {
    if (!(error instanceof ApiError) || !error.isSessionMissing) return false;
    if (queryClient.getQueryData(meQueryKey) !== undefined) {
      queryClient.removeQueries({ queryKey: meQueryKey, exact: true });
      onSessionEnded();
    }
    return true;
  }

  // A session the organization has just started holding is told so by every
  // page's request; reading the session again shows the setup instead.
  function held(error: unknown) {
    if (error instanceof ApiError && error.code === SECOND_FACTOR_REQUIRED_CODE) void queryClient.invalidateQueries({ queryKey: meQueryKey, exact: true });
  }

  const queryClient: QueryClient = new QueryClient({
    queryCache: new QueryCache({
      onError: (error) => {
        if (!refused(error)) held(error);
      },
    }),
    mutationCache: new MutationCache({
      onError: (error, _variables, _context, mutation) => {
        // Sign in says what happened to the session; a toast would only repeat it.
        if (refused(error) || mutation.meta?.handlesError) return;
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
