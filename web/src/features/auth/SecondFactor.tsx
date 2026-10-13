import { useState, type FormEvent } from "react";
import { useNavigate } from "@tanstack/react-router";
import { encode } from "uqr";
import { ApiError } from "@/api/client";
import { useLogout, useMe } from "@/api/auth";
import {
  CHALLENGE_INVALID_CODE,
  useConfirmSecondFactor,
  useLoginSecondFactor,
  useRemoveSecondFactor,
  useRenewRecoveryCodes,
  useSecondFactorSettled,
  useSecondFactorStatus,
  useStartSecondFactor,
  type SecondFactorStatus,
} from "@/api/secondFactor";
import { Button, Card, ErrorBanner, Field, SectionTitle, Wordmark } from "@/components/ui";
import { Icon } from "@/components/icons";
import { useFormat } from "@/lib/format";
import { QR_CODE_PX, QR_QUIET_MODULES, SECOND_FACTOR_CODE_DIGITS, SECRET_GROUP_LENGTH } from "@/config";

/** Draws what an authenticator app scans, dark on light whatever the theme, so every camera reads it. */
export function QrCode({ value }: { value: string }) {
  const { data, size } = encode(value, { ecc: "M", border: QR_QUIET_MODULES });
  let path = "";
  data.forEach((row, y) =>
    row.forEach((dark, x) => {
      if (dark) path += `M${x} ${y}h1v1h-1z`;
    }),
  );
  return (
    <svg
      viewBox={`0 0 ${size} ${size}`}
      width={QR_CODE_PX}
      height={QR_CODE_PX}
      role="img"
      aria-label="QR code for your authenticator app"
      shapeRendering="crispEdges"
      className="shrink-0 rounded-control"
      data-second-factor-qr
    >
      <rect width={size} height={size} fill="#ffffff" />
      <path d={path} fill="#000000" />
    </svg>
  );
}

/**
 * The second half of a password sign-in, for somebody with two-step sign-in:
 * the app's six digits, or a recovery code when the phone is not at hand.
 */
export function SecondFactorStep({ challenge, onSignedIn, onStartAgain }: { challenge: string; onSignedIn: () => void; onStartAgain: () => void }) {
  const finish = useLoginSecondFactor();
  const [code, setCode] = useState("");
  const [recovery, setRecovery] = useState(false);
  const expired = finish.error instanceof ApiError && finish.error.code === CHALLENGE_INVALID_CODE;

  function onSubmit(event: FormEvent) {
    event.preventDefault();
    if (!code.trim()) return;
    finish.mutate({ challenge, code: code.trim() }, { onSuccess: onSignedIn });
  }

  return (
    <form onSubmit={onSubmit} className="space-y-4" noValidate data-second-factor-step>
      {finish.error && <ErrorBanner>{(finish.error as Error).message}</ErrorBanner>}
      <p className="text-sm text-ink-muted">
        {recovery
          ? "Type one of the recovery codes you saved when you set up two-step sign-in. Each one works once."
          : `Open your authenticator app and type the ${SECOND_FACTOR_CODE_DIGITS} digit code it shows for Armature.`}
      </p>
      <Field
        key={recovery ? "recovery" : "app"}
        label={recovery ? "Recovery code" : "Code"}
        autoComplete="one-time-code"
        inputMode={recovery ? "text" : "numeric"}
        maxLength={recovery ? undefined : SECOND_FACTOR_CODE_DIGITS}
        autoFocus
        required
        value={code}
        onChange={(e) => setCode(e.target.value)}
      />
      {expired ? (
        <Button type="button" className="w-full" onClick={onStartAgain} data-action="sign-in-again">
          Sign in again
        </Button>
      ) : (
        <Button type="submit" loading={finish.isPending} disabled={!code.trim()} className="w-full">
          Verify
        </Button>
      )}
      <Button
        type="button"
        variant="link"
        className="w-full justify-center"
        data-action="toggle-recovery-code"
        onClick={() => {
          setRecovery(!recovery);
          setCode("");
          finish.reset();
        }}
      >
        {recovery ? "Use the code from your app" : "Use a recovery code"}
      </Button>
    </form>
  );
}

/**
 * Setting up an authenticator app: scan or type the key, confirm with a code,
 * then put the recovery codes away. The key is shown only here.
 */
export function Enrolment({ onDone }: { onDone: () => void }) {
  const start = useStartSecondFactor();
  const confirm = useConfirmSecondFactor();
  const settled = useSecondFactorSettled();
  const [code, setCode] = useState("");
  const [codes, setCodes] = useState<string[] | null>(null);

  if (codes) {
    return (
      <RecoveryCodes
        codes={codes}
        onDone={() => {
          settled();
          onDone();
        }}
      />
    );
  }

  const enrolment = start.data?.enrolment;
  if (!enrolment) {
    return (
      <div className="space-y-3">
        {start.error && <ErrorBanner>{(start.error as Error).message}</ErrorBanner>}
        <Button icon={<Icon.Shield />} loading={start.isPending} onClick={() => start.mutate()} data-action="start-second-factor">
          Set up two-step sign-in
        </Button>
      </div>
    );
  }

  function onSubmit(event: FormEvent) {
    event.preventDefault();
    if (!code.trim()) return;
    confirm.mutate(code.trim(), { onSuccess: (answer) => setCodes(answer.recoveryCodes) });
  }

  const grouped = enrolment.secret.match(new RegExp(`.{1,${SECRET_GROUP_LENGTH}}`, "g"))?.join(" ") ?? enrolment.secret;
  return (
    <form onSubmit={onSubmit} className="space-y-4" noValidate data-enrolment>
      <p className="text-sm text-ink-muted">Scan this with an authenticator app on your phone, such as the one your phone came with, 1Password or Aegis.</p>
      <div className="flex flex-wrap items-center gap-5">
        <QrCode value={enrolment.uri} />
        <div className="min-w-0 space-y-1">
          <p className="text-sm text-ink-muted">Or type this key into the app:</p>
          <code className="block break-all font-mono text-sm text-ink" data-second-factor-secret>
            {grouped}
          </code>
          <p className="text-xs text-ink-subtle">It is shown only now. Once two-step sign-in is on, nobody can read it again, you included.</p>
        </div>
      </div>
      <Field
        label="Code from the app"
        autoComplete="one-time-code"
        inputMode="numeric"
        maxLength={SECOND_FACTOR_CODE_DIGITS}
        hint="The app shows a new one every thirty seconds."
        value={code}
        onChange={(e) => setCode(e.target.value)}
      />
      {confirm.error && <ErrorBanner>{(confirm.error as Error).message}</ErrorBanner>}
      <Button type="submit" loading={confirm.isPending} disabled={!code.trim()} data-action="confirm-second-factor">
        Turn on two-step sign-in
      </Button>
    </form>
  );
}

/** The recovery codes, shown once, with a way to copy them all. */
export function RecoveryCodes({ codes, onDone }: { codes: string[]; onDone: () => void }) {
  const [copied, setCopied] = useState<boolean | null>(null);
  return (
    <div className="space-y-4" data-recovery-codes>
      <p className="text-sm text-ink-muted">
        Keep these recovery codes somewhere safe, apart from your phone. Each one signs you in once if the phone is lost. They are not shown again.
      </p>
      <ul className="grid grid-cols-2 gap-x-6 gap-y-1 rounded-control border border-border bg-surface-raised p-3 font-mono text-sm text-ink">
        {codes.map((code) => (
          <li key={code} data-recovery-code>
            {code}
          </li>
        ))}
      </ul>
      {copied === false && <ErrorBanner>The clipboard could not be reached. Select the codes and copy them by hand.</ErrorBanner>}
      <div className="flex flex-wrap gap-2">
        <Button
          type="button"
          variant="secondary"
          icon={copied ? <Icon.Check /> : <Icon.Copy />}
          data-action="copy-recovery-codes"
          onClick={() => navigator.clipboard.writeText(codes.join("\n")).then(() => setCopied(true), () => setCopied(false))}
        >
          {copied ? "Copied" : "Copy codes"}
        </Button>
        <Button type="button" onClick={onDone} data-action="saved-recovery-codes">
          I have saved them
        </Button>
      </div>
    </div>
  );
}

/** Two-step sign-in on the profile: set it up, or renew the codes or turn it off with a code. */
export function SecondFactorSettings() {
  const { data } = useSecondFactorStatus();
  const status = data?.secondFactor;
  if (!status) return null;
  return (
    <section className="mt-8" data-second-factor-settings>
      <SectionTitle className="mb-2">Two-step sign-in</SectionTitle>
      <Card className="space-y-4 p-5">
        {status.enrolled ? (
          <Enrolled status={status} />
        ) : status.available ? (
          <>
            <p className="text-sm text-ink-muted">Signing in with your password then asks for a code from an app on your phone, so a password that leaked is not enough on its own.</p>
            <Enrolment onDone={() => {}} />
          </>
        ) : (
          <p className="text-sm text-ink-muted">This server cannot keep a second factor yet: whoever runs it has to set ARMATURE_SECRET_KEY first.</p>
        )}
      </Card>
    </section>
  );
}

function Enrolled({ status }: { status: SecondFactorStatus }) {
  const renew = useRenewRecoveryCodes();
  const remove = useRemoveSecondFactor();
  const format = useFormat();
  const [code, setCode] = useState("");
  const [fresh, setFresh] = useState<string[] | null>(null);

  if (fresh) return <RecoveryCodes codes={fresh} onDone={() => setFresh(null)} />;

  const error = renew.error ?? remove.error;
  return (
    <div className="space-y-4" data-second-factor-on>
      <p className="text-sm text-ink">
        On{status.enrolledAt ? ` since ${format.date(status.enrolledAt)}` : ""}.{" "}
        <span className="text-ink-muted">
          {status.recoveryCodesLeft === 1 ? "1 recovery code is left." : `${status.recoveryCodesLeft} recovery codes are left.`}
        </span>
      </p>
      <Field
        label="Code from your app or a recovery code"
        autoComplete="one-time-code"
        hint="New recovery codes replace the old ones. Turning two-step sign-in off ends every other session of yours."
        value={code}
        onChange={(e) => setCode(e.target.value)}
      />
      {error && <ErrorBanner>{(error as Error).message}</ErrorBanner>}
      <div className="flex flex-wrap gap-2">
        <Button
          variant="secondary"
          loading={renew.isPending}
          disabled={!code.trim()}
          data-action="renew-recovery-codes"
          onClick={() => {
            remove.reset();
            renew.mutate(code.trim(), { onSuccess: (answer) => { setCode(""); setFresh(answer.recoveryCodes); } });
          }}
        >
          New recovery codes
        </Button>
        <Button
          variant="danger"
          loading={remove.isPending}
          disabled={!code.trim()}
          data-action="remove-second-factor"
          onClick={() => {
            renew.reset();
            remove.mutate(code.trim(), { onSuccess: () => setCode("") });
          }}
        >
          Turn off
        </Button>
      </div>
    </div>
  );
}

/**
 * The whole screen for a session the installation holds until two-step sign-in
 * is set up: the setup itself, or the way out.
 */
export function EnrolmentRequired() {
  const { data } = useMe();
  const logout = useLogout();
  const navigate = useNavigate();

  return (
    <div className="flex min-h-full items-center justify-center px-4 py-12" data-enrolment-required>
      <div className="w-full max-w-lg">
        <Wordmark />
        <h1 className="mt-3 text-xl font-semibold tracking-tight text-ink">Set up two-step sign-in</h1>
        <p className="mt-1 text-sm text-ink-muted">
          {data?.principal?.org?.name ?? "Armature"} asks everybody in your role to sign in with a code from an app on their phone as well as a password. Set it up to carry on.
        </p>
        <Card className="mt-6 space-y-4 p-5">
          <Enrolment onDone={() => {}} />
        </Card>
        <div className="mt-4 flex flex-wrap gap-2">
          <Button variant="ghost" loading={logout.isPending} data-action="sign-out" onClick={() => logout.mutate(undefined, { onSuccess: () => navigate({ to: "/login" }) })}>
            Sign out
          </Button>
        </div>
      </div>
    </div>
  );
}
