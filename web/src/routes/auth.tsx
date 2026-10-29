import { Link, createRoute, redirect } from "@tanstack/react-router";
import { rootRoute } from "./root";
import { LoginForm, SignupForm, SingleSignOn, safeNext } from "@/features/auth/AuthForms";
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
  validateSearch: (search: Record<string, unknown>): { next?: string; reset?: "done"; sso?: string } => ({
    ...(typeof search.next === "string" ? { next: search.next } : {}),
    // Set by the page a password link opens, once the new password is saved.
    ...(search.reset === "done" ? { reset: "done" as const } : {}),
    // Put on the address by the API when a sign-in through the provider did not end in a session.
    ...(typeof search.sso === "string" ? { sso: search.sso } : {}),
  }),
  beforeLoad: redirectIfSignedIn,
  component: LoginPage,
});

function LoginPage() {
  const { next, reset, sso } = loginRoute.useSearch();
  return <SignInPage next={next} reset={reset === "done"} sso={sso} />;
}

/** The way in: the identity provider first, then a password for somebody given one here. */
export function SignInPage({ next, reset, sso }: { next?: string; reset?: boolean; sso?: string }) {
  useEffect(() => applyCustomTheme(null), []);
  const target = safeNext(next);
  return (
    <main className="flex min-h-full items-center justify-center bg-backdrop px-4 py-12" data-door data-login>
      <div className="w-full max-w-sm">
        <div className="mb-6">
          <LogoHead />
          <h1 className="mt-3 text-xl font-semibold tracking-tight text-ink">{t.doors.login.title}</h1>
          <p className="mt-1 text-sm text-ink-muted">{t.doors.login.subtitle}</p>
        </div>
        {reset && (
          <p role="status" className="mb-5 rounded-control border border-success/30 bg-success-subtle px-3 py-2 text-sm text-success" data-password-changed>
            {t.doors.login.passwordChanged}
          </p>
        )}
        <Card className="p-5">
          <SingleSignOn next={target} reason={sso} />
        </Card>
        <Card className="mt-5 space-y-4 p-5">
          <div>
            <h2 id="password-sign-in" className="text-sm font-semibold text-ink">
              {t.doors.login.passwordTitle}
            </h2>
            <p className="mt-1 text-sm text-ink-muted">{t.doors.login.passwordHint}</p>
          </div>
          <LoginForm next={target} />
        </Card>
        <p className="mt-5 text-sm text-ink-muted">
          <SignupOffer />
        </p>
      </div>
    </main>
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
