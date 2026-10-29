import { Outlet, createRootRouteWithContext } from "@tanstack/react-router";
import type { QueryClient } from "@tanstack/react-query";
import { Settled } from "@/features/shell/Settled";
import { ShellNotFound } from "@/features/shell/AppShell";

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
  notFoundComponent: ShellNotFound,
});
