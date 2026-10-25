import { useState, type FormEvent } from "react";
import { Link, createRoute, useNavigate } from "@tanstack/react-router";
import { rootRoute } from "./root";
import { AuthLayout, redirectIfSignedIn } from "./auth";
import { ApiError } from "@/api/client";
import { useRequestPasswordReset, useResetPassword } from "@/api/auth";
import { NewPasswordFields, fieldErrors, formError, passwordsMatch } from "@/features/auth/AuthForms";
import { Button, ErrorBanner, Field } from "@/components/ui";
import { takeLinkSecret } from "@/lib/linkSecret";
import { PASSWORD_RESET_TOKEN_KEY } from "@/config";
import { t } from "@/i18n";

/** The one answer to asking for a link, so the page cannot tell who has an account. */
export function resetLinkSent(): string {
  return t.doors.reset.linkSent;
}

export const forgotPasswordRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/forgot-password",
  beforeLoad: redirectIfSignedIn,
  component: ForgotPasswordPage,
});

/** The page a password link opens. The secret travels after the #, like an invitation's. */
export const resetPasswordRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/reset-password",
  component: ResetPasswordPage,
});

export function readResetToken(): string | undefined {
  return takeLinkSecret(PASSWORD_RESET_TOKEN_KEY);
}

function SignInLink() {
  return (
    <Link to="/login" className="font-medium text-accent hover:underline">
      {t.doors.common.backToSignIn}
    </Link>
  );
}

function ForgotPasswordPage() {
  return (
    <AuthLayout title={t.doors.reset.forgotTitle} subtitle={t.doors.reset.forgotSubtitle} footer={<SignInLink />}>
      <ForgotPasswordForm />
    </AuthLayout>
  );
}

export function ForgotPasswordForm() {
  const ask = useRequestPasswordReset();
  const [email, setEmail] = useState("");
  const [sent, setSent] = useState(false);

  function onSubmit(event: FormEvent) {
    event.preventDefault();
    ask.mutate(email, { onSuccess: () => setSent(true) });
  }

  if (sent) {
    return (
      <p className="text-sm text-ink-muted" data-reset-sent>
        {resetLinkSent()}
      </p>
    );
  }

  return (
    <form onSubmit={onSubmit} className="space-y-4" noValidate data-forgot-password>
      {formError(ask.error) && <ErrorBanner>{formError(ask.error)}</ErrorBanner>}
      <Field
        label={t.doors.common.email}
        type="email"
        autoComplete="username"
        required
        value={email}
        onChange={(event) => setEmail(event.target.value)}
        error={fieldErrors(ask.error).email}
      />
      <Button type="submit" loading={ask.isPending} className="w-full">
        {t.doors.reset.sendLink}
      </Button>
    </form>
  );
}

function ResetPasswordPage() {
  const [token] = useState(readResetToken);
  const navigate = useNavigate();

  if (!token) {
    return (
      <AuthLayout title={t.doors.common.linkIncomplete} footer={<SignInLink />}>
        <p className="text-sm text-ink-muted">{t.doors.reset.incomplete}</p>
      </AuthLayout>
    );
  }

  function done() {
    sessionStorage.removeItem(PASSWORD_RESET_TOKEN_KEY);
    navigate({ to: "/login", search: { reset: "done" } });
  }

  return (
    <AuthLayout title={t.doors.reset.title} subtitle={t.doors.reset.subtitle} footer={<SignInLink />}>
      <ResetPasswordForm token={token} onDone={done} />
    </AuthLayout>
  );
}

export function ResetPasswordForm({ token, onDone }: { token: string; onDone: () => void }) {
  const reset = useResetPassword();
  const [password, setPassword] = useState("");
  const [repeat, setRepeat] = useState("");
  const [checked, setChecked] = useState(false);

  function onSubmit(event: FormEvent) {
    event.preventDefault();
    setChecked(true);
    if (!passwordsMatch(password, repeat)) return;
    reset.mutate({ token, password }, { onSuccess: onDone });
  }

  if (reset.error instanceof ApiError && reset.error.code === "reset_link_invalid") {
    return (
      <div className="space-y-4" data-reset-invalid>
        <p className="text-sm text-ink-muted">{reset.error.message}</p>
        <Link to="/forgot-password" className="block text-sm font-medium text-accent hover:underline">
          {t.doors.reset.askAgain}
        </Link>
      </div>
    );
  }

  return (
    <form onSubmit={onSubmit} className="space-y-4" noValidate data-reset-password>
      {formError(reset.error) && <ErrorBanner>{formError(reset.error)}</ErrorBanner>}
      <NewPasswordFields
        password={password}
        repeat={repeat}
        onPassword={setPassword}
        onRepeat={setRepeat}
        checked={checked}
        error={fieldErrors(reset.error).password}
      />
      <Button type="submit" loading={reset.isPending} className="w-full">
        {t.doors.reset.submit}
      </Button>
    </form>
  );
}
