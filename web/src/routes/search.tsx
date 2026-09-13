import { useEffect, useState } from "react";
import { createRoute, useNavigate } from "@tanstack/react-router";
import { appRoute } from "./app";
import { useIssues } from "@/api/issues";
import { useMe } from "@/api/auth";
import { useSavedFilters } from "@/api/filters";
import { IssueTable } from "@/features/issues/IssueTable";
import { QueryInput } from "@/features/search/QueryInput";
import { useRecentQueries } from "@/features/search/recent";
import { SavedFilterBar } from "@/features/filters/SavedFilterBar";
import { BulkBar } from "@/features/filters/BulkBar";
import { Button, EmptyState, Page, PageHeader, Skeleton } from "@/components/ui";
import { SEARCH_PAGE_SIZE } from "@/config";

/** The query is in the address, so a search somebody sends is the search they ran; f names the saved filter it came from. */
export const searchRoute = createRoute({
  getParentRoute: () => appRoute,
  path: "/search",
  validateSearch: (search: Record<string, unknown>): { q: string; f?: string } => ({
    q: typeof search.q === "string" ? search.q : "",
    ...(typeof search.f === "string" && search.f !== "" ? { f: search.f } : {}),
  }),
  component: SearchPage,
});

function SearchPage() {
  const { q, f } = searchRoute.useSearch();
  const navigate = useNavigate();
  const { data, error, isLoading } = useIssues({ q: q || undefined, limit: SEARCH_PAGE_SIZE });
  const { data: me } = useMe();
  const { data: saved } = useSavedFilters();
  const issues = data?.issues ?? [];
  const [recent, remember] = useRecentQueries();
  const [selected, setSelected] = useState<Set<string>>(new Set());
  // A query is worth remembering once it has run without the server refusing it.
  useEffect(() => {
    if (q && data) remember(q);
  }, [q, data, remember]);
  // A saved filter stays "active" while its query is the one running.
  const active = (saved?.filters ?? []).find((each) => each.id === f && each.query === q) ?? null;

  return (
    <Page width="content">
      <PageHeader title={active ? active.name : "Search"} meta={active ? `A saved search${active.ownerName ? ` by ${active.ownerName}` : ""}` : "Every issue you can see, narrowed by a query. Without one, the newest first."} />

      <div className="mb-3">
        <QueryInput value={q} autoFocus error={error} onSubmit={(next) => navigate({ to: "/search", search: { q: next } })} />
      </div>

      <SavedFilterBar query={q} me={me?.principal?.user.id} active={active} />

      {recent.length > 0 && (
        <p className="mb-4 flex flex-wrap items-baseline gap-x-3 gap-y-1 text-sm text-ink-subtle" data-recent-queries>
          <span>Recent</span>
          {recent.map((query) => (
            <Button key={query} variant="link" className="font-mono" onClick={() => navigate({ to: "/search", search: { q: query } })}>
              {query}
            </Button>
          ))}
        </p>
      )}

      {selected.size > 0 && (
        <div className="mb-3">
          <BulkBar keys={[...selected]} onClear={() => setSelected(new Set())} />
        </div>
      )}

      <p className="mb-2 text-sm text-ink-subtle tabular-nums" data-testid="search-count">
        {data ? (data.total > issues.length ? `${data.total} issues, the first ${issues.length} shown` : `${data.total} issue${data.total === 1 ? "" : "s"}`) : ""}
      </p>

      {isLoading ? (
        <Skeleton rows={6} />
      ) : issues.length === 0 ? (
        <EmptyState
          title={error ? "The query needs fixing first" : "Nothing matches that query"}
          description={error ? undefined : "Loosen a condition, or check a name against the help beside the query."}
        />
      ) : (
        <IssueTable issues={issues} showProject selected={selected} onSelect={setSelected} />
      )}
    </Page>
  );
}
