import { useState } from "react";
import { useRemoteLinks, useRemoveRemoteLink, type RemoteLink } from "@/api/remoteLinks";
import { Button, ErrorBanner } from "@/components/ui";

type Relation = RemoteLink["relation"];

// Each relation under a heading of its own, pages first, since most links are.
const groups: { relation: Relation; heading: string; noun: string }[] = [
  { relation: "page", heading: "Pages", noun: "page" },
  { relation: "requirement", heading: "Requirements", noun: "requirement" },
];

/**
 * Pages and requirements in other applications about the issue, put here by the
 * application that holds them, so the section only appears once there is one.
 */
export function PagesPanel({ issueKey, editable }: { issueKey: string; editable: boolean }) {
  const { data, isLoading } = useRemoteLinks(issueKey);
  const remove = useRemoveRemoteLink();

  if (isLoading || !data || data.remoteLinks.length === 0) return null;
  const links = data.remoteLinks;

  return (
    <section data-testid="pages-panel" className="space-y-4">
      {remove.error && <ErrorBanner>{(remove.error as Error).message}</ErrorBanner>}
      {groups.map(({ relation, heading, noun }) => {
        const held = links.filter((link) => link.relation === relation);
        if (held.length === 0) return null;
        return (
          <div key={relation} data-remote-link-group={relation}>
            <h2 className="mb-3 text-xs font-semibold tracking-wide text-ink-muted uppercase">
              {heading} ({held.length})
            </h2>
            <ul aria-label={heading} className="divide-y divide-border rounded-md border border-border">
              {held.map((link) => (
                <li key={link.id} className="flex items-center gap-2 px-3 py-1.5 text-sm" data-remote-link={link.url}>
                  <SourceIcon page={link} />
                  <a href={link.url} target="_blank" rel="noopener noreferrer" className="min-w-0 flex-1 truncate text-accent hover:underline">
                    {link.title}
                  </a>
                  <span className="shrink-0 text-xs text-ink-subtle">{link.source}</span>
                  {editable && (
                    <Button variant="link" onClick={() => remove.mutate({ key: issueKey, id: link.id })} aria-label={`Remove the ${noun} ${link.title}`} className="text-xs text-ink-subtle hover:text-danger">
                      Remove
                    </Button>
                  )}
                </li>
              ))}
            </ul>
          </div>
        );
      })}
    </section>
  );
}

// The icon lives on the other application's server, which may refuse it or
// be out of reach; the source's initial stands in so the row keeps its shape.
function SourceIcon({ page }: { page: RemoteLink }) {
  const [broken, setBroken] = useState(false);
  if (page.iconUrl && !broken) {
    return <img src={page.iconUrl} alt="" referrerPolicy="no-referrer" className="size-4 shrink-0 rounded-sm" onError={() => setBroken(true)} data-remote-link-icon />;
  }
  return (
    <span aria-hidden className="flex size-4 shrink-0 items-center justify-center rounded-sm bg-surface-sunken text-2xs font-semibold text-ink-muted uppercase">
      {page.source.slice(0, 1)}
    </span>
  );
}
