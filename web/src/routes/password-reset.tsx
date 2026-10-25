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

/** The one answer to asking for a link, so the page cannot tell who has an account. */
export const RESET_LINK_SENT = "If an account uses that address, a link to set a new password is on its way. It works for one hour.";

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
      Back to sign in
    </Link>
  );
}

function ForgotPasswordPage() {
  return (
    <AuthLayout title="Forgot your password?" subtitle="We mail you a link to set a new one." footer={<SignInLink />}>
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
        {RESET_LINK_SENT}
      </p>
    );
  }

  return (
    <form onSubmit={onSubmit} className="space-y-4" noValidate data-forgot-password>
      {formError(ask.error) && <ErrorBanner>{formError(ask.error)}</ErrorBanner>}
      <Field
        label="Email"
        type="email"
        autoComplete="username"
        required
        value={email}
        onChange={(event) => setEmail(event.target.value)}
        error={fieldErrors(ask.error).email}
      />
      <Button type="submit" loading={ask.isPending} className="w-full">
        Send me a link
      </Button>
    </form>
  );
}

function ResetPasswordPage() {
  const [token] = useState(readResetToken);
  const navigate = useNavigate();

  if (!token) {
    return (
      <AuthLayout title="This link is incomplete" footer={<SignInLink />}>
        <p className="text-sm text-ink-muted">Open the link exactly as it was mailed to you, including everything after the #.</p>
      </AuthLayout>
    );
  }

  function done() {
    sessionStorage.removeItem(PASSWORD_RESET_TOKEN_KEY);
    navigate({ to: "/login", search: { reset: "done" } });
  }

  return (
    <AuthLayout title="Set a new password" subtitle="Every session and access token of yours ends, and you sign in with the new one." footer={<SignInLink />}>
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
          Ask for a new link
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
        Set new password
      </Button>
    </form>
  );
}
