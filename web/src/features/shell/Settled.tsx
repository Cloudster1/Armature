import { useEffect } from "react";
import { useIsFetching, useIsMutating, useQueryClient } from "@tanstack/react-query";
import { useRouter, useRouterState } from "@tanstack/react-router";

// The browser suite waits for this instead of network silence: silence is only
// known half a second after the fact, while the page knows at once.
export function Settled() {
  const queryClient = useQueryClient();
  const router = useRouter();
  // Subscribed only so a change re-renders this and runs the effect below.
  useIsFetching();
  useIsMutating();
  useRouterState({ select: (s) => s.status });

  // A fetch or mutation marks the page busy the moment it starts, so no
  // render sits between a query resolving and the fetch it leads to.
  useEffect(() => {
    const busy = () => {
      document.body.dataset.settled = "false";
    };
    const unsubscribeQueries = queryClient.getQueryCache().subscribe((event) => {
      if (event.type === "updated" && event.action.type === "fetch") busy();
    });
    const unsubscribeMutations = queryClient.getMutationCache().subscribe((event) => {
      if (event.type === "updated" && event.action.type === "pending") busy();
    });
    return () => {
      unsubscribeQueries();
      unsubscribeMutations();
    };
  }, [queryClient]);

  // Read live rather than from the render: children's effects, which start
  // their fetches, have run by the time this one does. A mutation counts
  // because what it does on success, often a navigation, has not begun yet.
  useEffect(() => {
    const busy = queryClient.isFetching() > 0 || queryClient.isMutating() > 0 || router.state.status === "pending";
    document.body.dataset.settled = busy ? "false" : "true";
  });

  return null;
}
