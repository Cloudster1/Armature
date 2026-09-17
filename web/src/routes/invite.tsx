import { useState, type FormEvent } from "react";
import { Link, createRoute, useNavigate } from "@tanstack/react-router";
import { rootRoute } from "./root";
import { AuthLayout } from "./auth";
import { ApiError } from "@/api/client";
import { useLogout, useMe } from "@/api/auth";
import { useAcceptInvite, useInvitePreview, type InvitePreview } from "@/api/invites";
import { NewPasswordFields, passwordsMatch } from "@/features/auth/AuthForms";
import { Button, ErrorBanner, Field } from "@/components/ui";

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
  const fromLink = window.location.hash.replace(/^#/, "");
  if (fromLink) {
    sessionStorage.setItem(INVITE_TOKEN_KEY, fromLink);
    window.history.replaceState(null, "", window.location.pathname);
    return fromLink;
  }
  return sessionStorage.getItem(INVITE_TOKEN_KEY) ?? undefined;
}

const roleWords: Record<InvitePreview["role"], string> = {
  member: "a member",
  admin: "an administrator",
  customer: "a customer",
};

function InvitePage() {
  const [token] = useState(readInviteToken);
  const preview = useInvitePreview(token);
  const invite = preview.data?.invite;

  if (!token) {
    return (
      <AuthLayout title="This link is incomplete" footer={<SignInLink />}>
        <p className="text-sm text-ink-muted">Open the invitation link exactly as it was sent to you, including everything after the #.</p>
      </AuthLayout>
    );
  }
  if (preview.error) {
    const gone = preview.error instanceof ApiError && preview.error.code === "invite_invalid";
    return (
      <AuthLayout title="This invitation cannot be used" footer={<SignInLink />}>
        <p className="text-sm text-ink-muted" data-invite-invalid>
          {gone ? "It was already accepted, withdrawn, or it has expired. Ask whoever invited you for a new one." : (preview.error as Error).message}
        </p>
      </AuthLayout>
    );
  }
  if (!invite) {
    return (
      <AuthLayout title="Invitation" footer={<SignInLink />}>
        <p className="text-sm text-ink-muted">Loading...</p>
      </AuthLayout>
    );
  }

  return (
    <AuthLayout title={`Join ${invite.orgName}`} subtitle={`You are invited as ${roleWords[invite.role]}, as ${invite.email}.`} footer={<SignInLink />}>
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
      Sign in
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

  if (me.isLoading) return <p className="text-sm text-ink-muted">Loading...</p>;

  const signedIn = me.data?.principal?.user;
  if (signedIn) {
    if (signedIn.email.toLowerCase() !== invite.email.toLowerCase()) {
      return (
        <div className="space-y-4" data-invite-other-account>
          <p className="text-sm text-ink-muted">
            You are signed in as {signedIn.email}, and this invitation is for {invite.email}. Sign out, then accept it with that address.
          </p>
          <Button variant="secondary" className="w-full" loading={logout.isPending} onClick={() => logout.mutate()}>
            Sign out
          </Button>
        </div>
      );
    }
    return (
      <div className="space-y-4">
        {accept.error && <ErrorBanner>{(accept.error as Error).message}</ErrorBanner>}
        <Button className="w-full" loading={accept.isPending} data-action="accept-invite" onClick={() => join(accept.mutateAsync({ token }), done)}>
          Join {invite.orgName}
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
        <p className="text-sm text-ink-muted">
          {invite.email} already has an account. Sign in with it, and you come back here to join {invite.orgName}.
        </p>
        <Link to="/login" search={{ next: "/invite" }} className="block">
          <Button className="w-full">Sign in to accept</Button>
        </Link>
      </div>
    );
  }

  return (
    <form onSubmit={onSubmit} className="space-y-4" noValidate data-invite-accept>
      {accept.error && Object.keys(fields).length === 0 && <ErrorBanner>{(accept.error as Error).message}</ErrorBanner>}
      <Field label="Email" type="email" autoComplete="username" value={invite.email} readOnly />
      <Field label="Your name" required value={name} onChange={(event) => setName(event.target.value)} error={fields.name} />
      <NewPasswordFields password={password} repeat={repeat} onPassword={setPassword} onRepeat={setRepeat} checked={checked} error={fields.password} />
      <Button type="submit" loading={accept.isPending} className="w-full">
        Join {invite.orgName}
      </Button>
    </form>
  );
}
