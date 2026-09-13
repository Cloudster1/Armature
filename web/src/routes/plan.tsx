import { useMemo, useState } from "react";
import { Link, createRoute, useNavigate } from "@tanstack/react-router";
import { projectRoute } from "./project";
import { Page, PageHeader } from "@/components/ui";
import { usePlan } from "@/api/plan";
import { useProject } from "@/api/projects";
import { DependencyGraph } from "@/features/plan/DependencyGraph";
import { PlanSummary, PlanToolbar } from "@/features/plan/PlanToolbar";
import { Timeline } from "@/features/plan/Timeline";
import { useClosedForDays } from "@/features/plan/settings";
import type { Zoom } from "@/features/plan/scale";
import { NO_FILTERS, meter, viewFor, type Cut, type Filters } from "@/features/plan/views";

/** The view is in the address, so a plan somebody sends is the plan they saw. */
export const planRoute = createRoute({
  getParentRoute: () => projectRoute,
  path: "/plan",
  validateSearch: (search: Record<string, unknown>) => ({
    view: viewFor(typeof search.view === "string" ? search.view : undefined),
    q: typeof search.q === "string" && search.q !== "" ? search.q : undefined,
  }),
  component: PlanPage,
});

function PlanPage() {
  const { projectKey } = planRoute.useParams();
  const { view, q = "" } = planRoute.useSearch();
  const { data } = useProject(projectKey);
  const navigate = useNavigate();
  const [closedForDays, setClosedForDays] = useClosedForDays();
  // The filters and the zoom live here, above the views, so switching views
  // keeps them; the same request the timeline makes is read for its numbers.
  const [filters, setFilters] = useState<Filters>(NO_FILTERS);
  const [zoom, setZoom] = useState<Zoom["id"]>("fit");
  const plan = usePlan(projectKey, q);
  const cut: Cut = { closedForDays, matched: null };
  const numbers = useMemo(() => meter(plan.data?.items ?? []), [plan.data?.items]);
  const teams = useMemo(
    () => (plan.data?.load.rows ?? []).filter((row) => row.kind === "team" && row.teamId).map((row) => ({ id: row.teamId!, name: row.team })),
    [plan.data?.load.rows],
  );

  return (
    <Page width="wide">
      <PageHeader
        crumb={
          <Link to="/projects/$projectKey" params={{ projectKey }} className="hover:text-ink">
            {data?.project?.name ?? projectKey}
          </Link>
        }
        title="Plan"
      />

      <PlanToolbar
        view={view}
        onView={(next) => navigate({ to: "/projects/$projectKey/plan", params: { projectKey }, search: { view: next, q: q || undefined } })}
        query={q}
        onQuery={(next) => navigate({ to: "/projects/$projectKey/plan", params: { projectKey }, search: { view, q: next || undefined } })}
        queryError={plan.error}
        filters={filters}
        onFilters={setFilters}
        closedForDays={closedForDays}
        onClosedForDays={setClosedForDays}
        numbers={numbers}
        milestones={plan.data?.milestones ?? []}
        teams={teams}
        zoom={zoom}
        onZoom={setZoom}
        showZoom={view !== "dependencies"}
      />

      {plan.data && <PlanSummary numbers={numbers} />}

      {view === "dependencies" ? (
        <DependencyGraph projectKey={projectKey} cut={cut} query={q} filters={filters} />
      ) : (
        <Timeline key={view} projectKey={projectKey} view={view} cut={cut} query={q} filters={filters} zoom={zoom} />
      )}
    </Page>
  );
}
