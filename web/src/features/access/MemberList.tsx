import { useMembers } from "@/api/issues";
import { useMe, useRemoveMember } from "@/api/auth";
import { Avatar, Button, Card, EmptyState, ErrorBanner, SectionTitle, Tag, useToast } from "@/components/ui";
import { useConfirm } from "@/features/shell/ConfirmProvider";
import { InvitePanel } from "./InvitePanel";

/**
 * Who is in the organization, how more people get in, and the one way to let
 * somebody go. Removing takes the membership and what hung off it; the
 * person's account stays theirs for their other organizations, and what they
 * wrote stays here.
 */
export function MemberList() {
  const { data, isLoading, error } = useMembers();
  const { data: me } = useMe();
  const remove = useRemoveMember();
  const confirm = useConfirm();
  const toast = useToast();
  const members = data?.members ?? [];

  return (
    <div className="space-y-6">
      <InvitePanel />

      <section className="space-y-2">
        <SectionTitle>Members</SectionTitle>
        {error && <ErrorBanner>{(error as Error).message}</ErrorBanner>}
        {remove.error && <ErrorBanner>{(remove.error as Error).message}</ErrorBanner>}
        {isLoading ? (
          <p className="text-sm text-ink-muted">Loading...</p>
        ) : members.length === 0 ? (
          <EmptyState title="Nobody here yet" description="Invite people above." />
        ) : (
          <Card className="divide-y divide-border">
            {members.map((member) => (
              <div key={member.id} className="flex items-center gap-3 px-4 py-2.5 text-sm" data-member={member.name}>
                <Avatar name={member.name} src={member.avatarUrl} size="sm" />
                <span className="min-w-0 flex-1 truncate text-ink">{member.name}</span>
                <span className="hidden truncate text-ink-muted sm:block">{member.email}</span>
                <Tag className="capitalize">{member.role}</Tag>
                {/* Spelled out: an X beside the role read as taking the role away,
                    when it takes the person out of the organization. */}
                <Button
                  aria-label={`Remove ${member.name} from the organization`}
                  size="sm"
                  variant="ghost"
                  disabled={member.id === me?.principal?.user.id || remove.isPending}
                  data-action="remove-member"
                  onClick={async () => {
                    if (await confirm({ noun: "member", verb: "Remove", body: `${member.name} leaves this organization: their roles, groups, teams and tokens here go. What they wrote stays.` })) {
                      remove.mutate(member.id, { onSuccess: () => toast.success(`Removed ${member.name}`) });
                    }
                  }}
                >
                  Remove from organization
                </Button>
              </div>
            ))}
          </Card>
        )}
      </section>
    </div>
  );
}
