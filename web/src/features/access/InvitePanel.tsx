import { useState, type FormEvent } from "react";
import { useCreateInvite, useInvites, useWithdrawInvite, type CreatedInvite, type InvitableRole } from "@/api/invites";
import { Button, Card, ErrorBanner, Field, SectionTitle, Select, useToast } from "@/components/ui";
import { useConfirm } from "@/features/shell/ConfirmProvider";
import { useFormat } from "@/lib/format";
import { t } from "@/i18n";

// Read while rendering, so the words follow the language.
const roleChoices = (): Array<{ value: InvitableRole; label: string; detail: string }> => [
  { value: "member", label: t.access.invite.member, detail: t.access.invite.memberDetail },
  { value: "admin", label: t.access.invite.admin, detail: t.access.invite.adminDetail },
  { value: "customer", label: t.access.invite.customer, detail: t.access.invite.customerDetail },
];

/**
 * Inviting somebody, and the invitations still waiting for an answer.
 *
 * The link is shown once, whether or not it was mailed: without mail it is the
 * only way the invitation reaches anybody, and with it an administrator can
 * still pass it on when a mail goes astray.
 */
export function InvitePanel() {
  const create = useCreateInvite();
  const [email, setEmail] = useState("");
  const [role, setRole] = useState<InvitableRole>("member");
  const [created, setCreated] = useState<CreatedInvite | null>(null);

  function onSubmit(event: FormEvent) {
    event.preventDefault();
    if (!email.trim()) return;
    create.mutate(
      { email: email.trim(), role },
      {
        onSuccess: (result) => {
          setCreated(result);
          setEmail("");
        },
      },
    );
  }

  return (
    <div className="space-y-3">
      <Card className="p-4">
        <form onSubmit={onSubmit} className="space-y-3" data-invite-form>
          <div className="grid gap-3 sm:grid-cols-[1fr_12rem_auto] sm:items-end">
            <Field
              label={t.access.invite.byEmail}
              type="email"
              placeholder="colleague@example.com"
              value={email}
              onChange={(event) => setEmail(event.target.value)}
            />
            <Select label={t.access.invite.as} value={role} onChange={(event) => setRole(event.target.value as InvitableRole)}>
              {roleChoices().map((choice) => (
                <option key={choice.value} value={choice.value}>
                  {choice.label}
                </option>
              ))}
            </Select>
            <Button type="submit" loading={create.isPending} disabled={!email.trim()}>
              {t.access.invite.send}
            </Button>
          </div>
          <p className="text-xs text-ink-subtle">{roleChoices().find((choice) => choice.value === role)?.detail}</p>
          {create.error && <ErrorBanner>{(create.error as Error).message}</ErrorBanner>}
        </form>
      </Card>

      {created && <CreatedLink created={created} onDone={() => setCreated(null)} />}
      <PendingInvites />
    </div>
  );
}

function CreatedLink({ created, onDone }: { created: CreatedInvite; onDone: () => void }) {
  const toast = useToast();
  return (
    <Card className="border-accent/40 bg-accent-subtle p-4" data-invite-link={created.invite.email}>
      <p className="text-sm font-medium text-ink">
        {created.mailed ? t.access.invite.mailedTo(created.invite.email) : t.access.invite.sendLinkTo(created.invite.email)}
      </p>
      <p className="mt-1 text-sm text-ink-muted">{created.mailed ? t.access.invite.mailedDetail : t.access.invite.unmailedDetail}</p>
      <code className="mt-3 block overflow-x-auto rounded border border-border bg-surface px-3 py-2 font-mono text-xs text-ink" data-invite-url>
        {created.link}
      </code>
      <div className="mt-2 flex gap-2">
        <Button
          size="sm"
          variant="secondary"
          onClick={() =>
            navigator.clipboard.writeText(created.link).then(
              () => toast.success(t.access.invite.copied),
              () => toast.error(t.access.invite.copyRefused),
            )
          }
        >
          {t.access.invite.copy}
        </Button>
        <Button size="sm" variant="ghost" onClick={onDone}>
          {t.access.done}
        </Button>
      </div>
    </Card>
  );
}

function PendingInvites() {
  const { data } = useInvites();
  const withdraw = useWithdrawInvite();
  const confirm = useConfirm();
  const format = useFormat();
  const invites = data?.invites ?? [];
  if (invites.length === 0) return null;

  return (
    <section>
      <SectionTitle className="mb-2">{t.access.invite.waiting}</SectionTitle>
      <Card className="divide-y divide-border">
        {invites.map((invite) => (
          <div key={invite.id} className="flex items-center gap-3 px-4 py-2.5 text-sm" data-pending-invite={invite.email}>
            <span className="min-w-0 flex-1">
              <span className="block truncate text-ink">{invite.email}</span>
              <span className="block text-xs text-ink-muted">
                {t.access.invite.until(roleChoices().find((choice) => choice.value === invite.role)?.label ?? invite.role, format.date(invite.expiresAt))}
              </span>
            </span>
            <Button
              size="sm"
              variant="ghost"
              className="shrink-0"
              aria-label={t.access.invite.withdrawFor(invite.email)}
              onClick={async () =>
                (await confirm({
                  noun: t.kit.confirm.things.invitation,
                  verb: t.kit.confirm.verbs.withdraw,
                  body: t.access.invite.withdrawBody(invite.email),
                })) && withdraw.mutate(invite.id)
              }
            >
              {t.access.invite.withdraw}
            </Button>
          </div>
        ))}
      </Card>
      {withdraw.error && <ErrorBanner>{(withdraw.error as Error).message}</ErrorBanner>}
    </section>
  );
}
