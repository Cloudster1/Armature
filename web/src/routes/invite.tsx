import { useState, type FormEvent } from "react";
import { Link, createRoute, useNavigate } from "@tanstack/react-router";
import { rootRoute } from "./root";
import { AuthLayout } from "./auth";
import { ApiError } from "@/api/client";
import { useLogout, useMe } from "@/api/auth";
import { useAcceptInvite, useInvitePreview, type InvitePreview } from "@/api/invites";
import { NewPasswordFields, passwordsMatch } from "@/features/auth/AuthForms";
import { Button, ErrorBanner, Field } from "@/components/ui";
import { takeLinkSecret } from "@/lib/linkSecret";
import { t } from "@/i18n";

/** Where the secret waits while its holder signs in first. */
export const INVITE_TOKEN_KEY = "armature.invite";

/**
 * The page an invitation link opens. The secret travels after the #, so it is
 * never sent to a server as part of an address, and it is taken off the
 * address bar at once so it does not stay in the history either.
 */
export const inviteRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/invite",
  component: InvitePage,
});

export function readInviteToken(): string | undefined {
  return takeLinkSecret(INVITE_TOKEN_KEY);
}

function InvitePage() {
  const [token] = useState(readInviteToken);
  const preview = useInvitePreview(token);
  const invite = preview.data?.invite;

  if (!token) {
    return (
      <AuthLayout title={t.doors.common.linkIncomplete} footer={<SignInLink />}>
        <p className="text-sm text-ink-muted">{t.doors.invite.incomplete}</p>
      </AuthLayout>
    );
  }
  if (preview.error) {
    const gone = preview.error instanceof ApiError && preview.error.code === "invite_invalid";
    return (
      <AuthLayout title={t.doors.invite.unusableTitle} footer={<SignInLink />}>
        <p className="text-sm text-ink-muted" data-invite-invalid>
          {gone ? t.doors.invite.gone : (preview.error as Error).message}
        </p>
      </AuthLayout>
    );
  }
  if (!invite) {
    return (
      <AuthLayout title={t.doors.invite.title} footer={<SignInLink />}>
        <p className="text-sm text-ink-muted">{t.doors.common.loading}</p>
      </AuthLayout>
    );
  }

  return (
    <AuthLayout
      title={t.doors.invite.join(invite.installation)}
      subtitle={t.doors.invite.invitedAs(t.doors.invite.roles[invite.role], invite.email)}
      footer={<SignInLink />}
    >
      <Accept token={token} invite={invite} />
    </AuthLayout>
  );
}

/**
 * Moves on once the invitation is accepted. Accepting signs the person in,
 * which swaps the form for the signed-in view before a callback on the
 * mutation could run; the promise settles either way. A failure is already
 * on the page through the mutation's error.
 */
function join(accepted: Promise<unknown>, then: () => void) {
  accepted.then(then, () => {});
}

function SignInLink() {
  return (
    <Link to="/login" className="font-medium text-accent hover:underline">
      {t.doors.common.signIn}
    </Link>
  );
}

function Accept({ token, invite }: { token: string; invite: InvitePreview }) {
  const navigate = useNavigate();
  const me = useMe();
  const accept = useAcceptInvite();
  const logout = useLogout();

  function done() {
    sessionStorage.removeItem(INVITE_TOKEN_KEY);
    navigate({ to: "/" });
  }

  if (me.isLoading) return <p className="text-sm text-ink-muted">{t.doors.common.loading}</p>;

  const signedIn = me.data?.principal?.user;
  if (signedIn) {
    if (signedIn.email.toLowerCase() !== invite.email.toLowerCase()) {
      return (
        <div className="space-y-4" data-invite-other-account>
          <p className="text-sm text-ink-muted">{t.doors.invite.otherAccount(signedIn.email, invite.email)}</p>
          <Button variant="secondary" className="w-full" loading={logout.isPending} onClick={() => logout.mutate()}>
            {t.doors.common.signOut}
          </Button>
        </div>
      );
    }
    return (
      <div className="space-y-4">
        {accept.error && <ErrorBanner>{(accept.error as Error).message}</ErrorBanner>}
        <Button className="w-full" loading={accept.isPending} data-action="accept-invite" onClick={() => join(accept.mutateAsync({ token }), done)}>
          {t.doors.invite.join(invite.installation)}
        </Button>
      </div>
    );
  }

  return <NewAccount token={token} invite={invite} onDone={done} />;
}

function NewAccount({ token, invite, onDone }: { token: string; invite: InvitePreview; onDone: () => void }) {
  const accept = useAcceptInvite();
  const [name, setName] = useState("");
  const [password, setPassword] = useState("");
  const [repeat, setRepeat] = useState("");
  const [checked, setChecked] = useState(false);

  const needsSignIn = accept.error instanceof ApiError && accept.error.code === "sign_in_to_accept";
  const fields = accept.error instanceof ApiError ? accept.error.fields : {};

  function onSubmit(event: FormEvent) {
    event.preventDefault();
    setChecked(true);
    if (!passwordsMatch(password, repeat)) return;
    join(accept.mutateAsync({ token, name, password }), onDone);
  }

  if (needsSignIn) {
    return (
      <div className="space-y-4" data-invite-sign-in>
        <p className="text-sm text-ink-muted">{t.doors.invite.alreadyHasAccount(invite.email, invite.installation)}</p>
        <Link to="/login" search={{ next: "/invite" }} className="block">
          <Button className="w-full">{t.doors.invite.signInToAccept}</Button>
        </Link>
      </div>
    );
  }

  return (
    <form onSubmit={onSubmit} className="space-y-4" noValidate data-invite-accept>
      {accept.error && Object.keys(fields).length === 0 && <ErrorBanner>{(accept.error as Error).message}</ErrorBanner>}
      <Field label={t.doors.common.email} type="email" autoComplete="username" value={invite.email} readOnly />
      <Field label={t.doors.common.yourName} required value={name} onChange={(event) => setName(event.target.value)} error={fields.name} />
      <NewPasswordFields password={password} repeat={repeat} onPassword={setPassword} onRepeat={setRepeat} checked={checked} error={fields.password} />
      <Button type="submit" loading={accept.isPending} className="w-full">
        {t.doors.invite.join(invite.installation)}
      </Button>
    </form>
  );
}
