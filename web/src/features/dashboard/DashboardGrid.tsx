import {
  narrowingKinds,
  useAddWidget,
  useDeleteDashboard,
  useRemoveWidget,
  useReorderWidgets,
  useReport,
  useReportKinds,
  useUpdateWidget,
  type Breakdown,
  type Dashboard,
  type ReportKind,
  type Widget,
} from "@/api/reports";
import { ApiError } from "@/api/client";
import { Button, EmptyState, ErrorBanner, useToast } from "@/components/ui";
import { useConfirm } from "@/features/shell/ConfirmProvider";
import { FilterTile, type QueryFailure } from "./FilterTile";
import { WidgetBody } from "./WidgetBody";
import { WidgetCard } from "./WidgetCard";
import { WidgetSettings } from "./WidgetSettings";
import { AddWidget } from "./AddWidget";
import { composeQuery, isEmpty, type FilterSpec, type SavedQueryLookup } from "./filter";

/** The widgets of one dashboard, and what arranging them offers. */
export function DashboardGrid({
  projectKey,
  dashboard,
  arranging,
  canDelete,
  spec,
  saved,
  onSpec,
  savedQuery = () => undefined,
}: {
  projectKey: string;
  dashboard: Dashboard;
  arranging: boolean;
  canDelete: boolean;
  spec: FilterSpec;
  saved: FilterSpec;
  onSpec: (next: FilterSpec) => void;
  savedQuery?: SavedQueryLookup;
}) {
  const remove = useRemoveWidget();
  const add = useAddWidget();
  const toast = useToast();
  const reorder = useReorderWidgets();
  const update = useUpdateWidget();
  const deleteDashboard = useDeleteDashboard();
  const confirm = useConfirm();
  const { data: kindData } = useReportKinds(projectKey);
  const narrows = narrowingKinds(kindData?.kinds);

  // One query for every widget the filter reaches; none at all without a filter tile.
  const filterWidget = dashboard.widgets.find((w) => w.kind === "filter");
  const narrow = filterWidget && !isEmpty(spec) ? composeQuery(spec, savedQuery) : undefined;
  // The first narrowed widget's request doubles as the check on the query: the
  // same key, so no second request, and its refusal is shown on the tile.
  const first = dashboard.widgets.find((w) => narrows.has(w.kind));
  const probe = useReport<Breakdown>(projectKey, (first?.kind ?? "status_breakdown") as ReportKind, first?.config ?? {}, first ? narrow : undefined);
  const failure: QueryFailure | undefined =
    narrow && probe.error instanceof ApiError && probe.error.code === "bad_query" ? { message: probe.error.message, position: probe.error.position } : undefined;

  // Removing a widget is undone by adding it back, so it asks nothing and
  // offers Undo instead.
  function removeWidget(widget: Widget) {
    remove.mutate(widget.id, {
      onSuccess: () =>
        toast.info(`Removed ${widget.title}`, {
          action: { label: "Undo", onClick: () => add.mutate({ dashboardId: dashboard.id, kind: widget.kind, title: widget.title, width: widget.width, config: widget.config }) },
        }),
    });
  }
  const widgets = dashboard.widgets;

  function move(index: number, direction: -1 | 1) {
    const next = [...widgets];
    const swap = index + direction;
    if (swap < 0 || swap >= next.length) return;
    [next[index], next[swap]] = [next[swap]!, next[index]!];
    reorder.mutate({ dashboardId: dashboard.id, order: next.map((w) => w.id) });
  }

  return (
    <div className="space-y-4">
      {arranging && <AddWidget projectKey={projectKey} dashboardId={dashboard.id} exclude={filterWidget ? ["filter"] : []} />}

      {widgets.length === 0 ? (
        <EmptyState title="Nothing on this dashboard" description="Arrange it and add a widget." />
      ) : (
        <div className="grid gap-4 md:grid-cols-2" data-testid="dashboard">
          {widgets.map((widget, index) => (
            <WidgetCard
              key={widget.id}
              widget={widget}
              notNarrowed={widget.kind !== "filter" && Boolean(narrow) && !narrows.has(widget.kind)}
              controls={
                arranging ? (
                  <div className="flex shrink-0 items-center gap-1">
                    <WidgetSettings projectKey={projectKey} widget={widget} onChange={(config) => update.mutate({ id: widget.id, config })} />
                    <Button size="sm" variant="ghost" title="Narrower or wider" onClick={() => update.mutate({ id: widget.id, width: widget.width === 2 ? 1 : 2 })}>
                      {widget.width === 2 ? "Half" : "Full"}
                    </Button>
                    <Button size="sm" variant="ghost" title="Move earlier" onClick={() => move(index, -1)} disabled={index === 0}>
                      ←
                    </Button>
                    <Button size="sm" variant="ghost" title="Move later" onClick={() => move(index, 1)} disabled={index === widgets.length - 1}>
                      →
                    </Button>
                    <Button size="sm" variant="ghost" onClick={() => removeWidget(widget)}>
                      Remove
                    </Button>
                  </div>
                ) : null
              }
            >
              {widget.kind === "filter" ? (
                <FilterTile
                  projectKey={projectKey}
                  spec={spec}
                  onChange={onSpec}
                  failure={failure}
                  arranging={arranging}
                  saved={saved}
                  saving={update.isPending}
                  onSaveDefault={(next) => update.mutate({ id: widget.id, config: { ...widget.config, filter: next } })}
                  savedQuery={savedQuery}
                />
              ) : (
                <WidgetBody projectKey={projectKey} widget={widget} narrow={narrows.has(widget.kind) ? narrow : undefined} />
              )}
            </WidgetCard>
          ))}
        </div>
      )}

      {arranging && canDelete && (
        <div className="flex justify-end">
          <Button size="sm" variant="ghost" loading={deleteDashboard.isPending} onClick={async () => (await confirm({ noun: "dashboard", body: `${dashboard.name} and its arrangement go; the numbers are computed from the issues and lose nothing.` })) && deleteDashboard.mutate(dashboard.id)}>
            Delete this dashboard
          </Button>
        </div>
      )}
      {(remove.error ?? reorder.error ?? update.error ?? deleteDashboard.error) && (
        <ErrorBanner>{((remove.error ?? reorder.error ?? update.error ?? deleteDashboard.error) as Error).message}</ErrorBanner>
      )}
    </div>
  );
}
