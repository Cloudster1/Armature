import { useState } from "react";
import { Link, createRoute } from "@tanstack/react-router";
import { useQueryClient } from "@tanstack/react-query";
import { projectRoute } from "./project";
import { useProject, useUpdateProject, type ResourceGrouping } from "@/api/projects";
import { resourcesQueryKey, useResources } from "@/api/resources";
import { canAdminister, useAccess } from "@/api/access";
import { EmptyState, ErrorBanner, Page, PageHeader, Skeleton } from "@/components/ui";
import { RESOURCE_STEP_WEEKS, RESOURCE_WINDOW_WEEKS } from "@/config";
import { ResourceGrid } from "@/features/resources/ResourceGrid";
import { ResourceToolbar } from "@/features/resources/ResourceToolbar";
import { UnplacedWork } from "@/features/resources/UnplacedWork";
import { hasPlannedRows, mondayOf, shiftWeeks, windowFrom } from "@/features/resources/cells";
import { t } from "@/i18n";

/** The work per team or person per week, in hours, against the hours they have. */
export const resourcesRoute = createRoute({
  getParentRoute: () => projectRoute,
  path: "/resources",
  component: ResourcesPage,
});

function ResourcesPage() {
  const { projectKey } = resourcesRoute.useParams();
  const { data } = useProject(projectKey);
  const { data: access } = useAccess();
  const queryClient = useQueryClient();
  const update = useUpdateProject();
  const [monday, setMonday] = useState(() => mondayOf(new Date()));
  const { from, to } = windowFrom(monday, RESOURCE_WINDOW_WEEKS);
  const resources = useResources(projectKey, from, to);
  const project = data?.project;
  const plan = resources.data;

  function group(grouping: ResourceGrouping) {
    update.mutate({ key: projectKey, resourceGrouping: grouping }, { onSuccess: () => queryClient.invalidateQueries({ queryKey: resourcesQueryKey }) });
  }

  return (
    <Page width="wide">
      <PageHeader
        crumb={
          <Link to="/projects/$projectKey" params={{ projectKey }} className="hover:text-ink">
            {project?.name ?? projectKey}
          </Link>
        }
        title={t.schedule.resources.title}
        meta={t.schedule.resources.meta}
      />
      {project && (
        <ResourceToolbar
          method={project.planningMethod}
          grouping={plan?.grouping ?? project.resourceGrouping}
          canChange={canAdminister(access, projectKey)}
          saving={update.isPending}
          onGrouping={group}
          onPrevious={() => setMonday(shiftWeeks(monday, -RESOURCE_STEP_WEEKS))}
          onToday={() => setMonday(mondayOf(new Date()))}
          onNext={() => setMonday(shiftWeeks(monday, RESOURCE_STEP_WEEKS))}
        />
      )}
      {update.error && <ErrorBanner>{(update.error as Error).message}</ErrorBanner>}
      {resources.error && <ErrorBanner>{(resources.error as Error).message}</ErrorBanner>}
      {!plan && !resources.error && <Skeleton rows={4} />}
      {plan && plan.warnings.length > 0 && (
        <ul className="mb-3 space-y-0.5 text-sm text-danger" data-resource-warnings={plan.warnings.length}>
          {plan.warnings.map((w) => (
            <li key={w.message}>{w.message}.</li>
          ))}
        </ul>
      )}
      {plan &&
        (hasPlannedRows(plan.rows) ? (
          <ResourceGrid plan={plan} />
        ) : plan.grouping === "person" ? (
          <EmptyState title={t.schedule.resources.nobody} description={t.schedule.resources.nobodyDetail} />
        ) : (
          <EmptyState
            title={t.schedule.resources.noTeams}
            description={t.schedule.resources.noTeamsDetail}
            action={
              <Link to="/projects/$projectKey/teams" params={{ projectKey }} className="text-sm font-medium text-accent hover:underline">
                {t.schedule.resources.goToTeams}
              </Link>
            }
          />
        ))}
      {plan && <UnplacedWork unscheduled={plan.unscheduled} unestimated={plan.unestimated} />}
    </Page>
  );
}
