import { useState, type FormEvent } from "react";
import { Link, useNavigate } from "@tanstack/react-router";
import { ApiError, BASE } from "@/api/client";
import { SSO_START, useLogin, useSignup } from "@/api/auth";
import { isChallenge } from "@/api/secondFactor";
import { Button, ButtonLink, ErrorBanner, Field } from "@/components/ui";
import { SecondFactorStep } from "./SecondFactor";
import { t } from "@/i18n";

/** Pulls per-field messages out of the API's validation envelope. */
export function fieldErrors(error: unknown): Record<string, string> {
  return error instanceof ApiError ? error.fields : {};
}

export function formError(error: unknown): string | null {
  if (!error) return null;
  if (error instanceof ApiError) {
    // Field-level problems are shown against their fields instead.
    return Object.keys(error.fields).length > 0 ? null : error.message;
  }
  return t.doors.common.somethingWrong;
}

/** Where to go after signing in: a path of this application, never another site. */
export function safeNext(next: string | undefined): string | undefined {
  return next && next.startsWith("/") && !next.startsWith("//") ? next : undefined;
}

export function LoginForm({ next }: { next?: string } = {}) {
  const navigate = useNavigate();
  const login = useLogin();
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [challenge, setChallenge] = useState<string | null>(null);
  const target = safeNext(next);

  function proceed() {
    if (target) navigate({ href: target });
    else navigate({ to: "/" });
  }

  function onSubmit(event: FormEvent) {
    event.preventDefault();
    login.mutate({ email, password }, { onSuccess: (answer) => (isChallenge(answer) ? setChallenge(answer.challenge) : proceed()) });
  }

  if (challenge) {
    return (
      <SecondFactorStep
        challenge={challenge}
        onSignedIn={proceed}
        onStartAgain={() => {
          setChallenge(null);
          setPassword("");
          login.reset();
        }}
      />
    );
  }

  return (
    <form onSubmit={onSubmit} className="space-y-4" noValidate>
      {formError(login.error) && <ErrorBanner>{formError(login.error)}</ErrorBanner>}
      <Field
        label={t.doors.common.email}
        type="email"
        autoComplete="username"
        required
        value={email}
        onChange={(e) => setEmail(e.target.value)}
        error={fieldErrors(login.error).email}
      />
      <Field
        label={t.doors.common.password}
        type="password"
        autoComplete="current-password"
        required
        value={password}
        onChange={(e) => setPassword(e.target.value)}
        error={fieldErrors(login.error).password}
      />
      <Link to="/forgot-password" className="block text-sm font-medium text-accent hover:underline">
        {t.doors.login.forgot}
      </Link>
      <Button type="submit" loading={login.isPending} className="w-full">
        {t.doors.login.submit}
      </Button>

      <SingleSignOn next={target} />
    </form>
  );
}

/**
 * Signing in through the installation's identity provider. A plain link rather
 * than a fetch: the browser has to follow the provider's redirects itself.
 */
function SingleSignOn({ next }: { next?: string }) {
  const failure = ssoFailure();
  const href = `${BASE}${SSO_START}${next ? `?${new URLSearchParams({ next })}` : ""}`;
  return (
    <div className="space-y-2 border-t border-border pt-4">
      {failure && <ErrorBanner>{failure}</ErrorBanner>}
      <ButtonLink href={href} className="w-full" data-action="single-sign-on">
        {t.doors.login.sso}
      </ButtonLink>
    </div>
  );
}

/** Turns the reason carried back on the URL into something worth reading. */
function ssoFailure(): string | null {
  const reason = new URLSearchParams(window.location.search).get("sso");
  switch (reason) {
    case null:
      return null;
    case "not_a_member":
      return t.doors.login.ssoFailures.notAMember;
    case "not_configured":
      return t.doors.login.ssoFailures.notConfigured;
    case "expired":
      return t.doors.login.ssoFailures.expired;
    case "no_email":
      return t.doors.login.ssoFailures.noEmail;
    case "unverified_email":
      return t.doors.login.ssoFailures.unverifiedEmail;
    default:
      return t.doors.login.ssoFailures.other;
  }
}

/** Said when the two copies of a new password differ, in the language shown. */
export function passwordMismatch(): string {
  return t.doors.password.mismatch;
}

/**
 * A new password, typed twice. A typo in a field that shows only dots would
 * otherwise become the password nobody knows. The caller checks
 * `passwordsMatch` before it sends anything.
 */
export function NewPasswordFields({
  password,
  repeat,
  onPassword,
  onRepeat,
  checked,
  error,
}: {
  password: string;
  repeat: string;
  onPassword: (value: string) => void;
  onRepeat: (value: string) => void;
  /** Whether a submit was attempted, so a half-typed repeat is not scolded. */
  checked: boolean;
  error?: string;
}) {
  return (
    <>
      <Field
        label={t.doors.common.password}
        type="password"
        autoComplete="new-password"
        required
        minLength={12}
        value={password}
        onChange={(e) => onPassword(e.target.value)}
        hint={t.doors.password.hint}
        error={error}
      />
      <Field
        label={t.doors.password.repeat}
        type="password"
        autoComplete="new-password"
        required
        value={repeat}
        onChange={(e) => onRepeat(e.target.value)}
        error={checked && !passwordsMatch(password, repeat) ? passwordMismatch() : undefined}
      />
    </>
  );
}

export function passwordsMatch(password: string, repeat: string): boolean {
  return password === repeat;
}

export function SignupForm() {
  const navigate = useNavigate();
  const signup = useSignup();
  const [values, setValues] = useState({ name: "", email: "", password: "" });
  const [repeat, setRepeat] = useState("");
  const [checked, setChecked] = useState(false);

  function set(key: keyof typeof values) {
    return (event: { target: { value: string } }) => setValues((current) => ({ ...current, [key]: event.target.value }));
  }

  function onSubmit(event: FormEvent) {
    event.preventDefault();
    setChecked(true);
    if (!passwordsMatch(values.password, repeat)) return;
    signup.mutate(values, { onSuccess: () => navigate({ to: "/" }) });
  }

  const errors = fieldErrors(signup.error);

  return (
    <form onSubmit={onSubmit} className="space-y-4" noValidate>
      {formError(signup.error) && <ErrorBanner>{formError(signup.error)}</ErrorBanner>}
      <Field label={t.doors.common.yourName} required value={values.name} onChange={set("name")} error={errors.name} />
      <Field label={t.doors.signup.workEmail} type="email" autoComplete="username" required value={values.email} onChange={set("email")} error={errors.email} />
      <NewPasswordFields
        password={values.password}
        repeat={repeat}
        onPassword={(password) => setValues((current) => ({ ...current, password }))}
        onRepeat={setRepeat}
        checked={checked}
        error={errors.password}
      />
      <Button type="submit" loading={signup.isPending} className="w-full">
        {t.doors.signup.submit}
      </Button>
    </form>
  );
}
