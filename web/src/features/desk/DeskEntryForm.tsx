import { useState, type FormEvent } from "react";
import { ApiError } from "@/api/client";
import { useEnterOpenDoor, useEnterWithCode, useRequestCode, type OpenDoor } from "@/api/desk";
import { Button, ErrorBanner, Field } from "@/components/ui";

// A desk whose door is open takes a name and an address on trust; whoever
// gives them is a customer of that desk alone and sent on to where they were going.
export function OpenDoorForm({ slug, door, onEntered }: { slug: string; door: OpenDoor; onEntered: () => void }) {
  const enter = useEnterOpenDoor();
  const [name, setName] = useState("");
  const [email, setEmail] = useState("");

  function walkIn(event: FormEvent) {
    event.preventDefault();
    if (!email.trim()) return;
    enter.mutate({ slug, desk: door.key, email: email.trim(), name: name.trim() }, { onSuccess: onEntered });
  }

  const error = enter.error as Error | null;
  const fieldError = error instanceof ApiError ? error.fields.email : undefined;

  return (
    <form onSubmit={walkIn} className="space-y-4" noValidate data-desk-entry={slug} data-open-door={door.key}>
      <p className="text-sm text-ink-muted">{door.name} answers requests here. Say who you are and where to reach you; no code and no account are needed.</p>
      {error && !fieldError && <ErrorBanner>{error.message}</ErrorBanner>}
      <Field label="Your name" autoComplete="name" autoFocus value={name} onChange={(e) => setName(e.target.value)} />
      <Field label="Email" type="email" autoComplete="email" required value={email} onChange={(e) => setEmail(e.target.value)} error={fieldError} />
      <Button type="submit" loading={enter.isPending} className="w-full" disabled={!email.trim()} data-action="enter-open">
        Continue
      </Button>
    </form>
  );
}

/**
 * The portal's door for somebody without an account: an address, then the
 * code that was mailed to it. Whoever types the code is let in as a customer
 * of the desk and sent on to where they were going.
 */
export function DeskEntryForm({ slug, deskName, onEntered }: { slug: string; deskName: string; onEntered: () => void }) {
  const requestCode = useRequestCode();
  const enter = useEnterWithCode();
  const [email, setEmail] = useState("");
  const [code, setCode] = useState("");
  const [sentTo, setSentTo] = useState("");

  function sendCode(event: FormEvent) {
    event.preventDefault();
    if (!email.trim()) return;
    requestCode.mutate({ slug, email: email.trim() }, { onSuccess: () => setSentTo(email.trim()) });
  }

  function enterWithCode(event: FormEvent) {
    event.preventDefault();
    if (!code.trim()) return;
    enter.mutate({ slug, email: sentTo, code: code.trim() }, { onSuccess: onEntered });
  }

  const error = (enter.error ?? requestCode.error) as Error | null;
  const fieldError = error instanceof ApiError ? error.fields.email : undefined;

  if (!sentTo) {
    return (
      <form onSubmit={sendCode} className="space-y-4" noValidate data-desk-entry={slug}>
        <p className="text-sm text-ink-muted">
          {deskName} answers requests here. Say where to reach you and a code arrives by mail; no account is needed.
        </p>
        {error && !fieldError && <ErrorBanner>{error.message}</ErrorBanner>}
        <Field
          label="Email"
          type="email"
          autoComplete="email"
          autoFocus
          required
          value={email}
          onChange={(e) => setEmail(e.target.value)}
          error={fieldError}
        />
        <Button type="submit" loading={requestCode.isPending} className="w-full" disabled={!email.trim()}>
          Send me a code
        </Button>
      </form>
    );
  }

  return (
    <form onSubmit={enterWithCode} className="space-y-4" noValidate data-desk-entry={slug}>
      <p className="text-sm text-ink-muted">
        A code is on its way to <span className="font-medium text-ink">{sentTo}</span>. It works for ten minutes.
      </p>
      {error && <ErrorBanner>{error.message}</ErrorBanner>}
      <Field
        label="Code"
        inputMode="numeric"
        autoComplete="one-time-code"
        autoFocus
        required
        value={code}
        onChange={(e) => setCode(e.target.value)}
        data-desk-code
      />
      <Button type="submit" loading={enter.isPending} className="w-full" disabled={!code.trim()}>
        Enter
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
        Use another address
      </Button>
    </form>
  );
}
