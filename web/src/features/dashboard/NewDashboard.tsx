import { useState, type FormEvent } from "react";
import { useCreateDashboard, type Dashboard } from "@/api/reports";
import { Button, Card, ErrorBanner, Field } from "@/components/ui";
import { BLANK_TEMPLATE, DashboardTemplateChooser } from "./DashboardTemplateChooser";

/** A dashboard is named and started from a template, or from nothing. */
export function NewDashboard({ projectKey, onDone, canRemoveTemplates }: { projectKey: string; onDone: (made?: Dashboard) => void; canRemoveTemplates: boolean }) {
  const create = useCreateDashboard(projectKey);
  const [name, setName] = useState("");
  const [template, setTemplate] = useState(BLANK_TEMPLATE);
  function onSubmit(event: FormEvent) {
    event.preventDefault();
    if (!name.trim()) return;
    create.mutate({ name: name.trim(), template: template === BLANK_TEMPLATE ? undefined : template }, { onSuccess: (made) => onDone(made.dashboard) });
  }
  return (
    <Card className="mb-4 p-4">
      <form onSubmit={onSubmit} className="space-y-4" noValidate>
        <Field label="Dashboard name" autoFocus value={name} onChange={(e) => setName(e.target.value)} placeholder="Release readiness" className="w-64" />
        <DashboardTemplateChooser projectKey={projectKey} value={template} onChange={setTemplate} canRemove={canRemoveTemplates} />
        <div className="flex flex-wrap items-center gap-2">
          <Button type="submit" loading={create.isPending} disabled={!name.trim()}>
            Create dashboard
          </Button>
          <Button type="button" variant="ghost" onClick={() => onDone()}>
            Cancel
          </Button>
        </div>
        {create.error && <ErrorBanner>{(create.error as Error).message}</ErrorBanner>}
      </form>
    </Card>
  );
}
