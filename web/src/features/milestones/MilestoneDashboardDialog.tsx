import { useState, type FormEvent } from "react";
import { useNavigate } from "@tanstack/react-router";
import type { Milestone } from "@/api/milestones";
import { useCreateDashboard, useDashboardTemplates } from "@/api/reports";
import { Button, Dialog, ErrorBanner, Field, Select } from "@/components/ui";

/** The built-in template a milestone's dashboard starts from unless another is chosen. */
const MILESTONE_TEMPLATE = "milestone";

/**
 * Makes a dashboard about one milestone: the template's filter and milestones
 * widget are pinned to it, and the page goes there once it exists.
 */
export function MilestoneDashboardDialog({ milestone, open, onClose }: { milestone: Milestone; open: boolean; onClose: () => void }) {
  const { data } = useDashboardTemplates(milestone.projectKey);
  const create = useCreateDashboard(milestone.projectKey);
  const navigate = useNavigate();
  const [name, setName] = useState(milestone.name);
  const [template, setTemplate] = useState(MILESTONE_TEMPLATE);
  const templates = data?.templates ?? [];

  function onSubmit(event: FormEvent) {
    event.preventDefault();
    if (!name.trim()) return;
    create.mutate(
      { name: name.trim(), template, milestoneId: milestone.id },
      {
        onSuccess: (made) => {
          onClose();
          navigate({ to: "/projects/$projectKey/dashboard", params: { projectKey: milestone.projectKey }, search: { d: made.dashboard.id } });
        },
      },
    );
  }

  return (
    <Dialog open={open} onClose={onClose} title={`A dashboard for ${milestone.name}`} description="Every widget on it counts only the issues heading for this milestone." attrs={{ "data-milestone-dashboard": milestone.name }}>
      <form onSubmit={onSubmit} className="space-y-3" noValidate>
        <Field label="Dashboard name" autoFocus value={name} onChange={(e) => setName(e.target.value)} />
        <Select label="Template" value={template} onChange={(e) => setTemplate(e.target.value)}>
          {templates.map((t) => (
            <option key={t.key} value={t.key}>
              {t.name}
              {t.builtIn ? "" : " (saved)"}
            </option>
          ))}
        </Select>
        {create.error && <ErrorBanner>{(create.error as Error).message}</ErrorBanner>}
        <div className="flex justify-end gap-2">
          <Button type="button" variant="secondary" onClick={onClose}>
            Cancel
          </Button>
          <Button type="submit" loading={create.isPending} disabled={!name.trim()}>
            Create dashboard
          </Button>
        </div>
      </form>
    </Dialog>
  );
}
