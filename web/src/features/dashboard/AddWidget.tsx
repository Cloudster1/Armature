import { useAddWidget, useReportKinds, type ReportKind } from "@/api/reports";
import { Card, ErrorBanner, OptionCard } from "@/components/ui";

/** The reports this kind of project can show, offered as cards; a kind the dashboard can only have once is left out while it has it. */
export function AddWidget({ projectKey, dashboardId, exclude }: { projectKey: string; dashboardId: string; exclude: ReportKind[] }) {
  const { data } = useReportKinds(projectKey);
  const add = useAddWidget();
  const kinds = (data?.kinds ?? []).filter((k) => !exclude.includes(k.kind));
  return (
    <Card className="p-4">
      <h2 className="mb-2 text-2xs font-medium tracking-wide text-ink-subtle uppercase">Add a widget</h2>
      <div className="grid gap-2 sm:grid-cols-3 lg:grid-cols-4">
        {kinds.map((k) => (
          <OptionCard key={k.kind} data-add-widget={k.kind} onSelect={() => add.mutate({ dashboardId, kind: k.kind })} title={k.title} description={k.description} />
        ))}
      </div>
      {add.error && (
        <div className="mt-3">
          <ErrorBanner>{(add.error as Error).message}</ErrorBanner>
        </div>
      )}
    </Card>
  );
}
