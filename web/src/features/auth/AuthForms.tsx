import { useState, type FormEvent } from "react";
import { useNavigate } from "@tanstack/react-router";
import { ApiError, BASE } from "@/api/client";
import { useLogin, useSignup } from "@/api/auth";
import { Button, ErrorBanner, Field } from "@/components/ui";

/** Pulls per-field messages out of the API's validation envelope. */
function fieldErrors(error: unknown): Record<string, string> {
  return error instanceof ApiError ? error.fields : {};
}

function formError(error: unknown): string | null {
  if (!error) return null;
  if (error instanceof ApiError) {
    // Field-level problems are shown against their fields instead.
    return Object.keys(error.fields).length > 0 ? null : error.message;
  }
  return "Something went wrong. Please try again.";
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
  const [org, setOrg] = useState("");
  const [sso, setSSO] = useState(false);

  function onSubmit(event: FormEvent) {
    event.preventDefault();
    const target = safeNext(next);
    login.mutate({ email, password }, { onSuccess: () => (target ? navigate({ href: target }) : navigate({ to: "/" })) });
  }

  return (
    <form onSubmit={onSubmit} className="space-y-4" noValidate>
      {formError(login.error) && <ErrorBanner>{formError(login.error)}</ErrorBanner>}
      <Field
        label="Email"
        type="email"
        autoComplete="username"
        required
        value={email}
        onChange={(e) => setEmail(e.target.value)}
        error={fieldErrors(login.error).email}
      />
      <Field
        label="Password"
        type="password"
        autoComplete="current-password"
        required
        value={password}
        onChange={(e) => setPassword(e.target.value)}
        error={fieldErrors(login.error).password}
      />
      <Button type="submit" loading={login.isPending} className="w-full">
        Sign in
      </Button>

      <SingleSignOn open={sso} org={org} onOrg={setOrg} onOpen={() => setSSO(true)} />
    </form>
  );
}

/**
 * Signing in through an identity provider.
 *
 * The organization has to be named first, because a sign-in has to know which
 * tenant it is for before there is anybody signed in to ask. The link is a
 * plain navigation rather than a fetch: the browser has to follow the
 * provider's redirects itself.
 */
function SingleSignOn({
  open,
  org,
  onOrg,
  onOpen,
}: {
  open: boolean;
  org: string;
  onOrg: (value: string) => void;
  onOpen: () => void;
}) {
  const failure = ssoFailure();

  if (!open && !failure) {
    return (
      <Button variant="link" onClick={onOpen} className="w-full justify-center">
        Sign in with single sign-on
      </Button>
    );
  }

  return (
    <div className="space-y-2 border-t border-border pt-4">
      {failure && <ErrorBanner>{failure}</ErrorBanner>}
      <Field
        label="Organization"
        placeholder="your-company"
        value={org}
        onChange={(event) => onOrg(event.target.value)}
      />
      <Button
        type="button"
        variant="secondary"
        className="w-full"
        disabled={!org.trim()}
        onClick={() => {
          window.location.href = `${BASE}/auth/oidc/${encodeURIComponent(org.trim())}/start`;
        }}
      >
        Continue to your provider
      </Button>
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
      return "Your provider knows you, but you have not been invited to that organization.";
    case "not_configured":
      return "That organization does not use single sign-on.";
    case "expired":
      return "That sign-in took too long. Start it again.";
    case "no_email":
      return "Your provider did not send an email address, so there is no account to match.";
    case "unverified_email":
      return "Your provider has not verified your email address. Verify it there, then sign in again.";
    default:
      return "Signing in through your provider did not work.";
  }
}

/** Said when the two copies of a new password differ. */
export const PASSWORD_MISMATCH = "The two passwords are not the same. Type the same password in both fields.";

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
        label="Password"
        type="password"
        autoComplete="new-password"
        required
        minLength={12}
        value={password}
        onChange={(e) => onPassword(e.target.value)}
        hint="At least 12 characters."
        error={error}
      />
      <Field
        label="Repeat password"
        type="password"
        autoComplete="new-password"
        required
        value={repeat}
        onChange={(e) => onRepeat(e.target.value)}
        error={checked && !passwordsMatch(password, repeat) ? PASSWORD_MISMATCH : undefined}
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
  const [values, setValues] = useState({ name: "", email: "", password: "", orgName: "" });
  const [repeat, setRepeat] = useState("");
  const [checked, setChecked] = useState(false);

  function set(key: keyof typeof values) {
    return (event: { target: { value: string } }) =>
      setValues((current) => ({ ...current, [key]: event.target.value }));
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
      <Field label="Your name" required value={values.name} onChange={set("name")} error={errors.name} />
      <Field
        label="Work email"
        type="email"
        autoComplete="username"
        required
        value={values.email}
        onChange={set("email")}
        error={errors.email}
      />
      <NewPasswordFields
        password={values.password}
        repeat={repeat}
        onPassword={(password) => setValues((current) => ({ ...current, password }))}
        onRepeat={setRepeat}
        checked={checked}
        error={errors.password}
      />
      <Field
        label="Organization name"
        required
        value={values.orgName}
        onChange={set("orgName")}
        hint="You can change this later."
        error={errors.orgName}
      />
      <Button type="submit" loading={signup.isPending} className="w-full">
        Create your organization
      </Button>
    </form>
  );
}
