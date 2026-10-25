import { useState } from "react";
import { useStatuses } from "@/api/issues";
import { Button, Card } from "@/components/ui";
import { StatusBadge } from "@/features/issues/badges";
import { NewStatusForm } from "@/features/workflows/designer/NewStatusForm";
import { t } from "@/i18n";

/** The statuses workflows are built from, one card each; a new one is coined here as in the designer. */
export function StatusCards({ canConfigure }: { canConfigure: boolean }) {
  const { data } = useStatuses();
  const [coining, setCoining] = useState(false);
  const statuses = data?.statuses ?? [];
  return (
    <section className="space-y-3">
      {canConfigure && !coining && (
        <div className="flex justify-end">
          <Button size="sm" variant="secondary" onClick={() => setCoining(true)}>
            {t.workflows.library.newStatus}
          </Button>
        </div>
      )}
      {coining && (
        <Card className="p-4">
          <NewStatusForm heading={t.workflows.library.newStatusHeading} submitLabel={t.workflows.library.createStatus} onCreated={() => setCoining(false)} />
        </Card>
      )}
      {statuses.map((status) => (
        <Card key={status.id} elevated className="flex flex-wrap items-center gap-3 p-4" data-status-card={status.name}>
          <StatusBadge name={status.name} category={status.category} />
          <span className="min-w-0 flex-1 text-sm text-ink-muted">{status.description || t.workflows.library.noDescription}</span>
        </Card>
      ))}
    </section>
  );
}
