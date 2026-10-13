import { useMembers } from "@/api/issues";
import { Avatar, Card, EmptyState, ErrorBanner, SectionTitle, Tag } from "@/components/ui";
import { InvitePanel } from "./InvitePanel";

/**
 * Who is in the installation and how more people get in. Letting somebody go
 * is switching their account off, on the users page with the rest of it.
 */
export function MemberList() {
  const { data, isLoading, error } = useMembers();
  const members = data?.members ?? [];

  return (
    <div className="space-y-6">
      <InvitePanel />

      <section className="space-y-2">
        <SectionTitle>Members</SectionTitle>
        {error && <ErrorBanner>{(error as Error).message}</ErrorBanner>}
        {isLoading ? (
          <p className="text-sm text-ink-muted">Loading...</p>
        ) : members.length === 0 ? (
          <EmptyState title="Nobody here yet" description="Invite people above." />
        ) : (
          <Card className="divide-y divide-border">
            {members.map((member) => (
              <div key={member.id} className="flex items-center gap-3 px-4 py-2.5 text-sm" data-member={member.name}>
                <Avatar name={member.name} src={member.avatarUrl} size="sm" />
                <span className="min-w-0 flex-1">
                  <span className="block truncate text-ink">{member.name}</span>
                  <span className="block truncate text-xs text-ink-muted">{member.email}</span>
                </span>
                <Tag className="shrink-0 capitalize">{member.role}</Tag>
              </div>
            ))}
          </Card>
        )}
      </section>
    </div>
  );
}
