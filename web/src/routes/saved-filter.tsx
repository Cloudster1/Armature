import { useEffect } from "react";
import { Link, createRoute, useNavigate } from "@tanstack/react-router";
import { appRoute } from "./app";
import { useSavedFilter } from "@/api/filters";
import { EmptyState, Page, Skeleton } from "@/components/ui";
import { t } from "@/i18n";

/** Where a subscription mail sends its reader: the saved search as it reads now, not as it read when mailed. */
export const savedFilterRoute = createRoute({
  getParentRoute: () => appRoute,
  path: "/filters/$filterId",
  component: SavedFilterPage,
});

function SavedFilterPage() {
  const { filterId } = savedFilterRoute.useParams();
  const { data, error } = useSavedFilter(filterId);
  const navigate = useNavigate();
  const filter = data?.filter;
  useEffect(() => {
    if (filter) navigate({ to: "/search", search: { q: filter.query, f: filter.id }, replace: true });
  }, [filter, navigate]);

  return (
    <Page width="content">
      {error ? (
        <EmptyState
          title={t.search.filters.gone}
          description={t.search.filters.goneHint}
          action={
            <Link to="/filters" className="text-sm text-accent hover:underline">
              {t.search.filters.title}
            </Link>
          }
        />
      ) : (
        <Skeleton rows={6} />
      )}
    </Page>
  );
}
