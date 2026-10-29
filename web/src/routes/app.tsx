import { createRoute, redirect } from "@tanstack/react-router";
import { rootRoute } from "./root";
import { meQueryKey } from "@/api/auth";
import { request, ApiError } from "@/api/client";
import { trySilentSignIn } from "@/lib/silentSignIn";
import { AppShell, RouteError } from "@/features/shell/AppShell";

/**
 * The authenticated shell. Everything under it can assume a signed-in user in
 * an organization, because this guard has already run.
 */
export const appRoute = createRoute({
  getParentRoute: () => rootRoute,
  id: "app",
  beforeLoad: async ({ context, location }) => {
    try {
      await context.queryClient.ensureQueryData({
        queryKey: meQueryKey,
        queryFn: () => request("/auth/me"),
        retry: false,
      });
    } catch (error) {
      if (error instanceof ApiError && error.isUnauthenticated) {
        // Remember where they were headed, query and all, so the sign-in can
        // return them there; the provider may let them in without a click.
        const next = location.href;
        if (await trySilentSignIn(next)) {
          // The browser is on its way out; drawing the sign-in page first would only flash.
          await new Promise(() => {});
        }
        throw redirect({ to: "/login", search: { next } });
      }
      throw error;
    }
  },
  component: AppShell,
  errorComponent: RouteError,
});
