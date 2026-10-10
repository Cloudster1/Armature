import { MutationCache, QueryCache, QueryClient } from "@tanstack/react-query";
import { ApiError } from "./client";
import { meQueryKey } from "./auth";
import { QUERY_STALE_MS } from "@/config";

/** A 401 from any request means the session ended behind the page's back
 * (signed out elsewhere, or expired), so it is answered here once, for all. */
export function createQueryClient(onSessionEnded: () => void): QueryClient {
  // Only a session the page believed in can end; a refused sign-in has none,
  // and once it is dropped the other requests still in flight change nothing.
  function refused(error: unknown) {
    if (!(error instanceof ApiError) || !error.isSessionMissing) return;
    if (client.getQueryData(meQueryKey) === undefined) return;
    client.removeQueries({ queryKey: meQueryKey, exact: true });
    onSessionEnded();
  }

  const client = new QueryClient({
    queryCache: new QueryCache({ onError: refused }),
    mutationCache: new MutationCache({ onError: refused }),
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
  return client;
}
