import { useEffect } from "react";
import { useMembers } from "@/api/people";
import { Avatar, Card, clampOffset, countOf, EmptyState, ErrorBanner, Pager, SectionTitle, Skeleton, Tag } from "@/components/ui";
import { PeopleSearch } from "@/features/people/PeopleSearch";
import { PEOPLE_PAGE_SIZE } from "@/config";
import { InvitePanel } from "./InvitePanel";
import { t } from "@/i18n";

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
          <SectionTitle>{t.access.members.title}</SectionTitle>
          <div className="flex items-center gap-2">
            {data && (
              <span className="text-sm text-ink-muted" data-members-total={data.total}>
                {countOf(data.total, t.kit.counted.member)}
              </span>
            )}
            <PeopleSearch label={t.access.members.find} value={q} onChange={(text) => show({ q: text })} />
          </div>
        </div>
        {error && <ErrorBanner>{(error as Error).message}</ErrorBanner>}
        {isLoading ? (
          <Skeleton lines={4} />
        ) : members.length === 0 && q ? (
          <EmptyState title={t.access.members.noMatch} description={t.access.members.noMatchDetail} />
        ) : members.length === 0 ? (
          <EmptyState title={t.access.members.empty} description={t.access.members.emptyDetail} />
        ) : (
          <Card className="divide-y divide-border">
            {members.map((member) => (
              <div key={member.id} className="flex items-center gap-3 px-4 py-2.5 text-sm" data-member={member.name}>
                <Avatar name={member.name} src={member.avatarUrl} size="sm" />
                <span className="min-w-0 flex-1">
                  <span className="block truncate text-ink">{member.name}</span>
                  <span className="block truncate text-xs text-ink-muted">{member.email}</span>
                </span>
                <Tag className="shrink-0 capitalize">{t.shell.account.roles[member.role] ?? member.role}</Tag>
              </div>
            ))}
          </Card>
        )}
        {data && (
          <Pager
            label={t.access.members.pages}
            total={data.total}
            offset={data.offset}
            pageSize={PEOPLE_PAGE_SIZE}
            onChange={(next) => show({ q, offset: next })}
          />
        )}
      </section>
    </div>
  );
}
