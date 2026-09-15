import { Link, createRoute, redirect } from "@tanstack/react-router";
import { rootRoute } from "./root";
import { LoginForm, SignupForm } from "@/features/auth/AuthForms";
import { Card } from "@/components/ui";
import { meQueryKey, useSignupOpen } from "@/api/auth";
import { request } from "@/api/client";
import { useEffect, type ReactNode } from "react";
import { applyCustomTheme } from "@/lib/theme";

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
  // A theme is somebody's; the door has nobody yet, so it wears the built-in one.
  useEffect(() => applyCustomTheme(null), []);
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
  validateSearch: (search: Record<string, unknown>): { next?: string } =>
    typeof search.next === "string" ? { next: search.next } : {},
  beforeLoad: redirectIfSignedIn,
  component: LoginPage,
});

function LoginPage() {
  const { next } = loginRoute.useSearch();
  return (
    <AuthLayout title="Sign in" footer={<SignupOffer />}>
      <LoginForm next={next} />
    </AuthLayout>
  );
}

/** Offers sign-up only where it would work; elsewhere the way in is an invitation. */
export function SignupOffer() {
  const { data } = useSignupOpen();
  if (!data) return null;
  if (!data.open) return <>No account yet? Ask an administrator of your organization to invite you.</>;
  return (
    <>
      No account yet?{" "}
      <Link to="/signup" className="font-medium text-accent hover:underline">
        Create one
      </Link>
    </>
  );
}

function SignupPage() {
  const { data } = useSignupOpen();
  const closed = data?.open === false;
  return (
    <AuthLayout
      title={closed ? "Sign-up is closed" : "Create your organization"}
      subtitle={closed ? undefined : "You will be its first member and owner."}
      footer={
        <>
          Already have an account?{" "}
          <Link to="/login" className="font-medium text-accent hover:underline">
            Sign in
          </Link>
        </>
      }
    >
      {closed ? (
        <p className="text-sm text-ink-muted" data-signup-closed>
          New organizations cannot be created here. To join one, ask one of its administrators to invite you.
        </p>
      ) : (
        <SignupForm />
      )}
    </AuthLayout>
  );
}

export const signupRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/signup",
  beforeLoad: redirectIfSignedIn,
  component: SignupPage,
});
