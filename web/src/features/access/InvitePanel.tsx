import { useState, type FormEvent } from "react";
import {
  useCreateInvite,
  useInvites,
  useWithdrawInvite,
  type CreatedInvite,
  type InvitableRole,
} from "@/api/invites";
import {
  Button,
  Card,
  ErrorBanner,
  Field,
  SectionTitle,
  Select,
  useToast,
} from "@/components/ui";
import { useConfirm } from "@/features/shell/ConfirmProvider";

const roleChoices: Array<{
  value: InvitableRole;
  label: string;
  detail: string;
}> = [
  {
    value: "member",
    label: "Member",
    detail: "Works in the projects their roles open to them.",
  },
  {
    value: "admin",
    label: "Administrator",
    detail: "Also manages the organization: people, roles and settings.",
  },
  {
    value: "customer",
    label: "Customer",
    detail: "Sees only the service desk portal.",
  },
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
              label="Invite by email"
              type="email"
              placeholder="colleague@example.com"
              value={email}
              onChange={(event) => setEmail(event.target.value)}
            />
            <Select
              label="As"
              value={role}
              onChange={(event) => setRole(event.target.value as InvitableRole)}
            >
              {roleChoices.map((choice) => (
                <option key={choice.value} value={choice.value}>
                  {choice.label}
                </option>
              ))}
            </Select>
            <Button
              type="submit"
              loading={create.isPending}
              disabled={!email.trim()}
            >
              Send invitation
            </Button>
          </div>
          <p className="text-xs text-ink-subtle">
            {roleChoices.find((choice) => choice.value === role)?.detail}
          </p>
          {create.error && (
            <ErrorBanner>{(create.error as Error).message}</ErrorBanner>
          )}
        </form>
      </Card>

      {created && (
        <CreatedLink created={created} onDone={() => setCreated(null)} />
      )}
      <PendingInvites />
    </div>
  );
}

function CreatedLink({
  created,
  onDone,
}: {
  created: CreatedInvite;
  onDone: () => void;
}) {
  const toast = useToast();
  return (
    <Card
      className="border-accent/40 bg-accent-subtle p-4"
      data-invite-link={created.invite.email}
    >
      <p className="text-sm font-medium text-ink">
        {created.mailed
          ? `Invitation mailed to ${created.invite.email}`
          : `Send this link to ${created.invite.email}`}
      </p>
      <p className="mt-1 text-sm text-ink-muted">
        {created.mailed
          ? "If it does not arrive, send them this link. It is shown only now, works once and expires in a week."
          : "Mail is not set up here, so the invitation goes nowhere by itself. The link is shown only now, works once and expires in a week."}
      </p>
      <code
        className="mt-3 block overflow-x-auto rounded border border-border bg-surface px-3 py-2 font-mono text-xs text-ink"
        data-invite-url
      >
        {created.link}
      </code>
      <div className="mt-2 flex gap-2">
        <Button
          size="sm"
          variant="secondary"
          onClick={() =>
            navigator.clipboard.writeText(created.link).then(
              () => toast.success("Link copied"),
              () =>
                toast.error(
                  "The browser did not allow copying. Select the link and copy it by hand.",
                ),
            )
          }
        >
          Copy link
        </Button>
        <Button size="sm" variant="ghost" onClick={onDone}>
          Done
        </Button>
      </div>
    </Card>
  );
}

function PendingInvites() {
  const { data } = useInvites();
  const withdraw = useWithdrawInvite();
  const confirm = useConfirm();
  const invites = data?.invites ?? [];
  if (invites.length === 0) return null;

  return (
    <section>
      <SectionTitle className="mb-2">Waiting for an answer</SectionTitle>
      <Card className="divide-y divide-border">
        {invites.map((invite) => (
          <div
            key={invite.id}
            className="flex items-center gap-3 px-4 py-2.5 text-sm"
            data-pending-invite={invite.email}
          >
            <span className="min-w-0 flex-1">
              <span className="block truncate text-ink">{invite.email}</span>
              <span className="block text-xs text-ink-muted">
                {roleChoices.find((choice) => choice.value === invite.role)?.label ?? invite.role}, until{" "}
                {new Date(invite.expiresAt).toLocaleDateString()}
              </span>
            </span>
            <Button
              size="sm"
              variant="ghost"
              className="shrink-0"
              aria-label={`Withdraw the invitation for ${invite.email}`}
              onClick={async () =>
                (await confirm({
                  noun: "invitation",
                  verb: "Withdraw",
                  body: `The link sent to ${invite.email} stops working.`,
                })) && withdraw.mutate(invite.id)
              }
            >
              Withdraw
            </Button>
          </div>
        ))}
      </Card>
      {withdraw.error && (
        <ErrorBanner>{(withdraw.error as Error).message}</ErrorBanner>
      )}
    </section>
  );
}
