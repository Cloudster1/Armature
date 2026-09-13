import { Link, createRoute, redirect } from "@tanstack/react-router";
import { rootRoute } from "./root";
import { LoginForm, SignupForm } from "@/features/auth/AuthForms";
import { Card } from "@/components/ui";
import { meQueryKey } from "@/api/auth";
import { request } from "@/api/client";
import type { ReactNode } from "react";

/**
 * Bounces an already signed-in visitor away from the sign-in screens. It reads
 * through the query cache so that navigating between /login and /signup does
 * not re-hit the API each time.
 *
 * The session check is resolved to a boolean before the redirect is thrown.
 * Throwing inside a try block that also catches the "not signed in" error would
 * mean catching our own redirect and swallowing it, leaving a signed-in visitor
 * stuck on the sign-in page.
 */
async function redirectIfSignedIn({ context }: { context: { queryClient: import("@tanstack/react-query").QueryClient } }) {
  const signedIn = await context.queryClient
    .ensureQueryData({
      queryKey: meQueryKey,
      queryFn: () => request("/auth/me"),
      retry: false,
    })
    .then(() => true)
    .catch(() => false);

  if (signedIn) {
    throw redirect({ to: "/" });
  }
}

export function AuthLayout({ title, subtitle, children, footer }: { title: string; subtitle?: string; children: ReactNode; footer: ReactNode }) {
  return (
    <div className="flex min-h-full items-center justify-center px-4 py-12">
      <div className="w-full max-w-sm">
        <div className="mb-6">
          <p className="font-mono text-sm font-medium tracking-wide text-ink-muted">armature</p>
          <h1 className="mt-3 text-xl font-semibold tracking-tight text-ink">{title}</h1>
          {subtitle && <p className="mt-1 text-sm text-ink-muted">{subtitle}</p>}
        </div>
        <Card className="p-5">{children}</Card>
        <p className="mt-5 text-sm text-ink-muted">{footer}</p>
      </div>
    </div>
  );
}

export const loginRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/login",
  beforeLoad: redirectIfSignedIn,
  component: () => (
    <AuthLayout
      title="Sign in"
      footer={
        <>
          No account yet?{" "}
          <Link to="/signup" className="font-medium text-accent hover:underline">
            Create one
          </Link>
        </>
      }
    >
      <LoginForm />
    </AuthLayout>
  ),
});

export const signupRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/signup",
  beforeLoad: redirectIfSignedIn,
  component: () => (
    <AuthLayout
      title="Create your organization"
      subtitle="You will be its first member and owner."
      footer={
        <>
          Already have an account?{" "}
          <Link to="/login" className="font-medium text-accent hover:underline">
            Sign in
          </Link>
        </>
      }
    >
      <SignupForm />
    </AuthLayout>
  ),
});
