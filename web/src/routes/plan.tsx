import { useMemo, useState } from "react";
import { Link, createRoute, useNavigate } from "@tanstack/react-router";
import { projectRoute } from "./project";
import { ErrorBanner, Page, PageHeader, useToast } from "@/components/ui";
import { canWriteIssues, useAccess } from "@/api/access";
import { useAlign, usePlan, useSchedulePlan, type Alignment } from "@/api/plan";
import { useProject } from "@/api/projects";
import { AutoPlanPreview } from "@/features/plan/AutoPlanPreview";
import { movesToSave, proposedBars } from "@/features/plan/autoplan";
import { DependencyGraph } from "@/features/plan/DependencyGraph";
import { PlanSummary, PlanToolbar } from "@/features/plan/PlanToolbar";
import { Timeline } from "@/features/plan/Timeline";
import { useClosedForDays } from "@/features/plan/settings";
import type { Zoom } from "@/features/plan/scale";
import { NO_FILTERS, meter, viewFor, type Cut, type Filters } from "@/features/plan/views";
import { useFormat } from "@/lib/format";

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
  const { data: access } = useAccess();
  const format = useFormat();
  const toast = useToast();
  const align = useAlign();
  const apply = useSchedulePlan();
  // A proposal is for the plan it was read from, so another view or query drops it.
  const scope = `${projectKey} ${view} ${q}`;
  const [held, setProposal] = useState<{ scope: string; alignment: Alignment } | null>(null);
  const proposal = held?.scope === scope ? held.alignment : null;
  const proposed = useMemo(() => proposedBars(proposal), [proposal]);

  function preview() {
    apply.reset();
    align.mutate({ projectKey, query: q }, { onSuccess: (alignment) => setProposal({ scope, alignment }) });
  }

  function applyProposal() {
    if (!proposal) return;
    const moved = proposal.moves.length;
    apply.mutate(
      { projectKey, moves: movesToSave(proposal) },
      {
        onSuccess: () => {
          setProposal(null);
          toast.success(`Moved ${moved} issue${moved === 1 ? "" : "s"} to start after the issues blocking them.`);
        },
      },
    );
  }
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
        onAutoPlan={view !== "dependencies" && canWriteIssues(access, projectKey) ? preview : undefined}
        autoPlanning={align.isPending}
      />

      {align.error && !proposal && <ErrorBanner>{(align.error as Error).message}</ErrorBanner>}
      {proposal && (
        <AutoPlanPreview
          alignment={proposal}
          locale={format.locale}
          applying={apply.isPending}
          refusal={apply.error ? (apply.error as Error).message : undefined}
          onApply={applyProposal}
          onCancel={() => setProposal(null)}
          onPreviewAgain={preview}
        />
      )}

      {plan.data && <PlanSummary numbers={numbers} />}

      {view === "dependencies" ? (
        <DependencyGraph projectKey={projectKey} cut={cut} query={q} filters={filters} />
      ) : (
        <Timeline key={view} projectKey={projectKey} view={view} cut={cut} query={q} filters={filters} zoom={zoom} proposed={proposed} />
      )}
    </Page>
  );
}
