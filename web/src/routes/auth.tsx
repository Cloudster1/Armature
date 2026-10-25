import { Link, createRoute, redirect } from "@tanstack/react-router";
import { rootRoute } from "./root";
import { LoginForm, SignupForm } from "@/features/auth/AuthForms";
import { Card } from "@/components/ui";
import { LogoHead } from "@/components/Logo";
import { meQueryKey, useSignupOpen, type SignupState } from "@/api/auth";
import { request } from "@/api/client";
import { useEffect, type ReactNode } from "react";
import type { QueryClient } from "@tanstack/react-query";
import { applyCustomTheme } from "@/lib/theme";
import { t } from "@/i18n";

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
export async function redirectIfSignedIn({ context }: { context: { queryClient: QueryClient } }) {
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
    <div className="flex min-h-full items-center justify-center bg-backdrop px-4 py-12" data-door>
      <div className="w-full max-w-sm">
        <div className="mb-6">
          <LogoHead />
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
  validateSearch: (search: Record<string, unknown>): { next?: string; reset?: "done" } => ({
    ...(typeof search.next === "string" ? { next: search.next } : {}),
    // Set by the page a password link opens, once the new password is saved.
    ...(search.reset === "done" ? { reset: "done" as const } : {}),
  }),
  beforeLoad: redirectIfSignedIn,
  component: LoginPage,
});

function LoginPage() {
  const { next, reset } = loginRoute.useSearch();
  return (
    <AuthLayout title={t.doors.login.title} footer={<SignupOffer />}>
      {reset === "done" && (
        <p role="status" className="mb-4 rounded-control border border-success/30 bg-success-subtle px-3 py-2 text-sm text-success" data-password-changed>
          {t.doors.login.passwordChanged}
        </p>
      )}
      <LoginForm next={next} />
    </AuthLayout>
  );
}

/** Offers sign-up only where it would work; elsewhere the way in is an invitation. */
export function SignupOffer() {
  const { data } = useSignupOpen();
  if (!data) return null;
  if (!data.open) return <>{t.doors.login.askToBeInvited}</>;
  return (
    <>
      {t.doors.login.noAccount}{" "}
      <Link to="/signup" className="font-medium text-accent hover:underline">
        {t.doors.login.createOne}
      </Link>
    </>
  );
}

/** What the sign-up page is headed with: setting the installation up, joining it, or neither. */
function signupHeading(state: SignupState | undefined): { title: string; subtitle?: string } {
  if (state?.open === false) return { title: t.doors.signup.closedTitle };
  if (state?.first) return { title: t.doors.signup.firstTitle, subtitle: t.doors.signup.firstSubtitle };
  return { title: t.doors.signup.title, subtitle: t.doors.signup.subtitle };
}

function SignupPage() {
  const { data } = useSignupOpen();
  const closed = data?.open === false;
  const { title, subtitle } = signupHeading(data);
  return (
    <AuthLayout
      title={title}
      subtitle={subtitle}
      footer={
        <>
          {t.doors.signup.haveAccount}{" "}
          <Link to="/login" className="font-medium text-accent hover:underline">
            {t.doors.common.signIn}
          </Link>
        </>
      }
    >
      {closed ? (
        <p className="text-sm text-ink-muted" data-signup-closed>
          {t.doors.signup.closed}
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
