import { useState, type FormEvent } from "react";
import { ApiError } from "@/api/client";
import { useEnterOpenDoor, useEnterWithCode, useRequestCode, type DoorDesk } from "@/api/desk";
import { Button, ErrorBanner, Field } from "@/components/ui";
import { t } from "@/i18n";

// A desk whose door is open takes a name and an address on trust; whoever
// gives them is a customer of that desk alone and sent on to where they were going.
export function OpenDoorForm({ door, onEntered }: { door: DoorDesk; onEntered: () => void }) {
  const enter = useEnterOpenDoor();
  const [name, setName] = useState("");
  const [email, setEmail] = useState("");

  function walkIn(event: FormEvent) {
    event.preventDefault();
    if (!email.trim()) return;
    enter.mutate({ desk: door.key, email: email.trim(), name: name.trim() }, { onSuccess: onEntered });
  }

  const error = enter.error as Error | null;
  const fieldError = error instanceof ApiError ? error.fields.email : undefined;

  return (
    <form onSubmit={walkIn} className="space-y-4" noValidate data-desk-entry={door.key} data-open-door={door.key}>
      <p className="text-sm text-ink-muted">{t.desk.entry.openIntro(door.name)}</p>
      {error && !fieldError && <ErrorBanner>{error.message}</ErrorBanner>}
      <Field label={t.desk.entry.yourName} autoComplete="name" autoFocus value={name} onChange={(e) => setName(e.target.value)} />
      <Field
        label={t.desk.entry.email}
        type="email"
        autoComplete="email"
        required
        value={email}
        onChange={(e) => setEmail(e.target.value)}
        error={fieldError}
      />
      <Button type="submit" loading={enter.isPending} className="w-full" disabled={!email.trim()} data-action="enter-open">
        {t.desk.entry.continue}
      </Button>
    </form>
  );
}

/**
 * The portal's door for somebody without an account: an address, then the
 * code that was mailed to it. Whoever types the code is let in as a customer
 * of the desk and sent on to where they were going.
 */
export function DeskEntryForm({ desk, deskName, onEntered }: { desk?: string; deskName: string; onEntered: () => void }) {
  const requestCode = useRequestCode();
  const enter = useEnterWithCode();
  const [email, setEmail] = useState("");
  const [code, setCode] = useState("");
  const [sentTo, setSentTo] = useState("");

  function sendCode(event: FormEvent) {
    event.preventDefault();
    if (!email.trim()) return;
    requestCode.mutate({ ...(desk ? { desk } : {}), email: email.trim() }, { onSuccess: () => setSentTo(email.trim()) });
  }

  function enterWithCode(event: FormEvent) {
    event.preventDefault();
    if (!code.trim()) return;
    enter.mutate({ ...(desk ? { desk } : {}), email: sentTo, code: code.trim() }, { onSuccess: onEntered });
  }

  const error = (enter.error ?? requestCode.error) as Error | null;
  const fieldError = error instanceof ApiError ? error.fields.email : undefined;

  if (!sentTo) {
    return (
      <form onSubmit={sendCode} className="space-y-4" noValidate data-desk-entry={desk ?? ""}>
        <p className="text-sm text-ink-muted">{t.desk.entry.codeIntro(deskName)}</p>
        {error && !fieldError && <ErrorBanner>{error.message}</ErrorBanner>}
        <Field
          label={t.desk.entry.email}
          type="email"
          autoComplete="email"
          autoFocus
          required
          value={email}
          onChange={(e) => setEmail(e.target.value)}
          error={fieldError}
        />
        <Button type="submit" loading={requestCode.isPending} className="w-full" disabled={!email.trim()}>
          {t.desk.entry.sendCode}
        </Button>
      </form>
    );
  }

  return (
    <form onSubmit={enterWithCode} className="space-y-4" noValidate data-desk-entry={desk ?? ""}>
      <p className="text-sm text-ink-muted">
        {t.desk.entry.codeOnItsWay} <span className="font-medium text-ink">{sentTo}</span>
        {t.desk.entry.codeWorks}
      </p>
      {error && <ErrorBanner>{error.message}</ErrorBanner>}
      <Field
        label={t.desk.entry.code}
        inputMode="numeric"
        autoComplete="one-time-code"
        autoFocus
        required
        value={code}
        onChange={(e) => setCode(e.target.value)}
        data-desk-code
      />
      <Button type="submit" loading={enter.isPending} className="w-full" disabled={!code.trim()}>
        {t.desk.entry.enter}
      </Button>
      <Button
        variant="link"
        onClick={() => {
          setSentTo("");
          setCode("");
          enter.reset();
          requestCode.reset();
        }}
      >
        {t.desk.entry.anotherAddress}
      </Button>
    </form>
  );
}
