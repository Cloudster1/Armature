import { useEffect } from "react";
import { useMembers } from "@/api/people";
import { Avatar, Card, EmptyState, ErrorBanner, Pager, SectionTitle, Tag, clampOffset, countOf } from "@/components/ui";
import { PeopleSearch } from "@/features/people/PeopleSearch";
import { PEOPLE_PAGE_SIZE } from "@/config";
import { InvitePanel } from "./InvitePanel";

/** What the members list shows: a search and where in its matches the page starts. */
export interface MembersShown {
  q?: string;
  offset?: number;
}

/**
 * Who is in the installation, a page at a time, and how more people get in. Letting
 * somebody go is switching their account off, on the users page with the rest of it.
 */
export function MemberList({ q = "", offset = 0, show }: MembersShown & { show: (next: MembersShown, replace?: boolean) => void }) {
  const { data, isLoading, error } = useMembers({ q, offset });
  const members = data?.members ?? [];

  // People leave the matches while the page is open; a page past the end steps back.
  useEffect(() => {
    if (data && offset > 0 && offset >= data.total) show({ q, offset: clampOffset(offset, data.total, PEOPLE_PAGE_SIZE) }, true);
  }, [data, offset, q, show]);

  return (
    <div className="space-y-6">
      <InvitePanel />

      <section className="space-y-2">
        <div className="flex flex-wrap items-center justify-between gap-2">
          <SectionTitle>Members</SectionTitle>
          <div className="flex items-center gap-2">
            {data && <span className="text-sm text-ink-muted" data-members-total={data.total}>{countOf(data.total, "member")}</span>}
            <PeopleSearch label="Find members" value={q} onChange={(text) => show({ q: text })} />
          </div>
        </div>
        {error && <ErrorBanner>{(error as Error).message}</ErrorBanner>}
        {isLoading ? (
          <p className="text-sm text-ink-muted">Loading...</p>
        ) : members.length === 0 && q ? (
          <EmptyState title="Nobody matches that" description="Try part of their name or their address, or clear the search." />
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
        {data && <Pager label="Member pages" total={data.total} offset={data.offset} pageSize={PEOPLE_PAGE_SIZE} onChange={(next) => show({ q, offset: next })} />}
      </section>
    </div>
  );
}
