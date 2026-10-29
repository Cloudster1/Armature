import { useState } from "react";
import { createRoute, useNavigate } from "@tanstack/react-router";
import { projectRoute } from "./project";
import { canAdminister, useAccess } from "@/api/access";
import { pdfHref, useDashboards } from "@/api/reports";
import { useSavedFilters } from "@/api/filters";
import { Button, ButtonLink, Page, PageHeader, Segmented } from "@/components/ui";
import { DashboardGrid } from "@/features/dashboard/DashboardGrid";
import { NewDashboard } from "@/features/dashboard/NewDashboard";
import { SaveTemplateDialog } from "@/features/dashboard/SaveTemplateDialog";
import { ShareDialog } from "@/features/dashboard/ShareDialog";
import { Icon } from "@/components/icons";
import { EMPTY_FILTER, FILTER_TEXT_KEYS, composeQuery, isEmpty, parseSearch, toSearch, type FilterSearch, type FilterSpec } from "@/features/dashboard/filter";
import { t } from "@/i18n";

/** The filter, plus which dashboard is open, so a link lands on the one that was meant. */
type DashboardSearch = FilterSearch & { d?: string };

export const dashboardRoute = createRoute({
  getParentRoute: () => projectRoute,
  path: "/dashboard",
  validateSearch: (search: Record<string, unknown>): DashboardSearch => ({
    ...Object.fromEntries(FILTER_TEXT_KEYS.filter((key) => typeof search[key] === "string").map((key) => [key, search[key] as string])),
    days: typeof search.days === "number" ? search.days : undefined,
    d: typeof search.d === "string" && search.d !== "" ? search.d : undefined,
  }),
  component: DashboardPage,
});

function DashboardPage() {
  const { projectKey } = dashboardRoute.useParams();
  const { data, isLoading } = useDashboards(projectKey);
  const { data: access } = useAccess();
  const [arranging, setArranging] = useState(false);
  const [creating, setCreating] = useState(false);
  const [savingTemplate, setSavingTemplate] = useState(false);
  const [sharing, setSharing] = useState(false);

  // Arranging a dashboard is configuring the project.
  const canConfigure = canAdminister(access, projectKey);

  // Which dashboard is open, and the filter's live values, both in the address.
  const search = dashboardRoute.useSearch();
  const navigate = useNavigate();
  const dashboards = data?.dashboards ?? [];
  const dashboard = dashboards.find((each) => each.id === search.d) ?? dashboards[0];
  const choose = (id: string) => navigate({ to: "/projects/$projectKey/dashboard", params: { projectKey }, search: { d: id } });
  const filterWidget = dashboard?.widgets.find((w) => w.kind === "filter");
  const saved: FilterSpec = { ...EMPTY_FILTER, ...(filterWidget?.config?.filter ?? {}) };
  const spec = parseSearch(search, saved);
  const setSpec = (next: FilterSpec) =>
    navigate({ to: "/projects/$projectKey/dashboard", params: { projectKey }, search: { ...toSearch(next), d: search.d }, replace: true });
  // What a link and an export freeze: the filter as it stands, or nothing.
  // A saved search named by the tile is read live, so editing it changes the dashboard.
  const { data: savedFilters } = useSavedFilters();
  const savedQuery = (id: string) => savedFilters?.filters.find((f) => f.id === id)?.query;
  const query = isEmpty(spec) ? "" : composeQuery(spec, savedQuery);

  return (
    <Page width="content">
      <PageHeader
        title={dashboard?.name ?? t.dashboard.page.title}
        meta={dashboard ? t.dashboard.page.widgets(dashboard.widgets.length) : undefined}
        actions={
          canConfigure && (
            <>
              {!creating && (
                <Button variant="secondary" onClick={() => setCreating(true)} data-guide="add-dashboard">
                  {t.dashboard.page.newDashboard}
                </Button>
              )}
              {arranging && dashboard && (
                <Button variant="secondary" onClick={() => setSavingTemplate(true)} data-action="save-template">
                  {t.dashboard.page.saveTemplate}
                </Button>
              )}
              {dashboard && (
                <>
                  <ButtonLink href={pdfHref(dashboard.id, query)} download icon={<Icon.Download />} data-action="export-pdf" data-guide="export-pdf">
                    {t.dashboard.page.exportPdf}
                  </ButtonLink>
                  <Button variant="secondary" icon={<Icon.Share />} onClick={() => setSharing(true)} data-action="share" data-guide="share">
                    {t.dashboard.page.share}
                  </Button>
                </>
              )}
              <Button variant={arranging ? "primary" : "secondary"} onClick={() => setArranging(!arranging)} data-guide="arrange">
                {arranging ? t.dashboard.page.doneArranging : t.dashboard.page.arrange}
              </Button>
            </>
          )
        }
      />

      {creating && (
        <NewDashboard
          projectKey={projectKey}
          canRemoveTemplates={Boolean(canConfigure)}
          onDone={(made) => {
            setCreating(false);
            if (made) choose(made.id);
          }}
        />
      )}
      {dashboard && <SaveTemplateDialog key={dashboard.id} dashboard={dashboard} open={savingTemplate} onClose={() => setSavingTemplate(false)} />}
      {dashboard && (
        <ShareDialog
          key={`share-${dashboard.id}`}
          dashboardId={dashboard.id}
          dashboardName={dashboard.name}
          query={query}
          open={sharing}
          onClose={() => setSharing(false)}
        />
      )}

      {dashboards.length > 1 && (
        <div className="mb-4">
          <Segmented
            label={t.dashboard.page.chooser}
            value={dashboard?.id ?? ""}
            onChange={choose}
            options={dashboards.map((d) => ({ value: d.id, label: d.name, attrs: { "data-dashboard": d.name } }))}
          />
        </div>
      )}

      {isLoading || !dashboard ? null : (
        <DashboardGrid
          projectKey={projectKey}
          dashboard={dashboard}
          arranging={arranging && Boolean(canConfigure)}
          canDelete={dashboards.length > 1}
          spec={spec}
          saved={saved}
          onSpec={setSpec}
          savedQuery={savedQuery}
        />
      )}
    </Page>
  );
}
