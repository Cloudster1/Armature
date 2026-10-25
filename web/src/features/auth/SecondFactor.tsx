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
import { Button, Card, ErrorBanner, Field, SectionTitle } from "@/components/ui";
import { LogoHead } from "@/components/Logo";
import { Icon } from "@/components/icons";
import { useFormat } from "@/lib/format";
import { APP_NAME, QR_CODE_PX, QR_DARK, QR_LIGHT, QR_QUIET_MODULES, SECOND_FACTOR_CODE_DIGITS, SECRET_GROUP_LENGTH } from "@/config";
import { t } from "@/i18n";

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
      aria-label={t.doors.secondFactor.qrLabel}
      shapeRendering="crispEdges"
      className="shrink-0 rounded-control"
      data-second-factor-qr
    >
      <rect width={size} height={size} fill={QR_LIGHT} />
      <path d={path} fill={QR_DARK} />
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
      <p className="text-sm text-ink-muted">{recovery ? t.doors.secondFactor.recoveryPrompt : t.doors.secondFactor.appPrompt(SECOND_FACTOR_CODE_DIGITS)}</p>
      <Field
        key={recovery ? "recovery" : "app"}
        label={recovery ? t.doors.secondFactor.recoveryCode : t.doors.secondFactor.code}
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
          {t.doors.secondFactor.signInAgain}
        </Button>
      ) : (
        <Button type="submit" loading={finish.isPending} disabled={!code.trim()} className="w-full">
          {t.doors.secondFactor.verify}
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
        {recovery ? t.doors.secondFactor.useApp : t.doors.secondFactor.useRecovery}
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
          {t.doors.secondFactor.setUp}
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
      <p className="text-sm text-ink-muted">{t.doors.secondFactor.scan}</p>
      <div className="flex flex-wrap items-center gap-5">
        <QrCode value={enrolment.uri} />
        <div className="min-w-0 space-y-1">
          <p className="text-sm text-ink-muted">{t.doors.secondFactor.typeKey}</p>
          <code className="block break-all font-mono text-sm text-ink" data-second-factor-secret>
            {grouped}
          </code>
          <p className="text-xs text-ink-subtle">{t.doors.secondFactor.shownOnce}</p>
        </div>
      </div>
      <Field
        label={t.doors.secondFactor.codeFromApp}
        autoComplete="one-time-code"
        inputMode="numeric"
        maxLength={SECOND_FACTOR_CODE_DIGITS}
        hint={t.doors.secondFactor.newEvery}
        value={code}
        onChange={(e) => setCode(e.target.value)}
      />
      {confirm.error && <ErrorBanner>{(confirm.error as Error).message}</ErrorBanner>}
      <Button type="submit" loading={confirm.isPending} disabled={!code.trim()} data-action="confirm-second-factor">
        {t.doors.secondFactor.turnOn}
      </Button>
    </form>
  );
}

/** The recovery codes, shown once, with a way to copy them all. */
export function RecoveryCodes({ codes, onDone }: { codes: string[]; onDone: () => void }) {
  const [copied, setCopied] = useState<boolean | null>(null);
  return (
    <div className="space-y-4" data-recovery-codes>
      <p className="text-sm text-ink-muted">{t.doors.secondFactor.keepCodes}</p>
      <ul className="grid grid-cols-2 gap-x-6 gap-y-1 rounded-control border border-border bg-surface-raised p-3 font-mono text-sm text-ink">
        {codes.map((code) => (
          <li key={code} data-recovery-code>
            {code}
          </li>
        ))}
      </ul>
      {copied === false && <ErrorBanner>{t.doors.secondFactor.clipboardFailed}</ErrorBanner>}
      <div className="flex flex-wrap gap-2">
        <Button
          type="button"
          variant="secondary"
          icon={copied ? <Icon.Check /> : <Icon.Copy />}
          data-action="copy-recovery-codes"
          onClick={() =>
            navigator.clipboard.writeText(codes.join("\n")).then(
              () => setCopied(true),
              () => setCopied(false),
            )
          }
        >
          {copied ? t.doors.secondFactor.copied : t.doors.secondFactor.copy}
        </Button>
        <Button type="button" onClick={onDone} data-action="saved-recovery-codes">
          {t.doors.secondFactor.saved}
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
      <SectionTitle className="mb-2">{t.doors.secondFactor.title}</SectionTitle>
      <Card className="space-y-4 p-5">
        {status.enrolled ? (
          <Enrolled status={status} />
        ) : status.available ? (
          <>
            <p className="text-sm text-ink-muted">{t.doors.secondFactor.explain}</p>
            <Enrolment onDone={() => {}} />
          </>
        ) : (
          <p className="text-sm text-ink-muted">{t.doors.secondFactor.unavailable}</p>
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
        {t.doors.secondFactor.on}
        {status.enrolledAt ? t.doors.secondFactor.since(format.date(status.enrolledAt)) : ""}.{" "}
        <span className="text-ink-muted">{t.doors.secondFactor.codesLeft(status.recoveryCodesLeft)}</span>
      </p>
      <Field
        label={t.doors.secondFactor.codeOrRecovery}
        autoComplete="one-time-code"
        hint={t.doors.secondFactor.renewHint}
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
            renew.mutate(code.trim(), {
              onSuccess: (answer) => {
                setCode("");
                setFresh(answer.recoveryCodes);
              },
            });
          }}
        >
          {t.doors.secondFactor.newCodes}
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
          {t.doors.secondFactor.turnOff}
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
    <div className="flex min-h-full items-center justify-center bg-backdrop px-4 py-12" data-enrolment-required>
      <div className="w-full max-w-lg">
        <LogoHead />
        <h1 className="mt-3 text-xl font-semibold tracking-tight text-ink">{t.doors.secondFactor.setUp}</h1>
        <p className="mt-1 text-sm text-ink-muted">{t.doors.secondFactor.requiredBody(data?.principal?.installation?.name ?? APP_NAME)}</p>
        <Card className="mt-6 space-y-4 p-5">
          <Enrolment onDone={() => {}} />
        </Card>
        <div className="mt-4 flex flex-wrap gap-2">
          <Button
            variant="ghost"
            loading={logout.isPending}
            data-action="sign-out"
            onClick={() => logout.mutate(undefined, { onSuccess: () => navigate({ to: "/login" }) })}
          >
            {t.doors.common.signOut}
          </Button>
        </div>
      </div>
    </div>
  );
}
