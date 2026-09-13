import { Outlet, createRootRouteWithContext } from "@tanstack/react-router";
import type { QueryClient } from "@tanstack/react-query";
import { Settled } from "@/features/shell/Settled";

export interface RouterContext {
  queryClient: QueryClient;
}

export const rootRoute = createRootRouteWithContext<RouterContext>()({
  component: () => (
    <>
      <Outlet />
      <Settled />
    </>
  ),
  notFoundComponent: () => (
    <div className="flex h-full items-center justify-center p-8 text-center">
      <div>
        <p className="text-lg font-semibold text-ink">Page not found</p>
        <p className="mt-1 text-sm text-ink-muted">
          The page you asked for does not exist.
        </p>
      </div>
    </div>
  ),
});
