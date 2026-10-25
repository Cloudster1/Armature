import { Outlet, createRootRouteWithContext } from "@tanstack/react-router";
import type { QueryClient } from "@tanstack/react-query";
import { Settled } from "@/features/shell/Settled";
import { t } from "@/i18n";

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
        <p className="text-lg font-semibold text-ink">{t.doors.notFound.title}</p>
        <p className="mt-1 text-sm text-ink-muted">{t.doors.notFound.body}</p>
      </div>
    </div>
  ),
});
