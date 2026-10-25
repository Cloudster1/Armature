import { useState, type FormEvent } from "react";
import { useNavigate } from "@tanstack/react-router";
import type { Milestone } from "@/api/milestones";
import { useCreateDashboard, useDashboardTemplates } from "@/api/reports";
import { Button, Dialog, ErrorBanner, Field, Select } from "@/components/ui";
import { t as words } from "@/i18n";

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
    <Dialog
      open={open}
      onClose={onClose}
      title={words.schedule.milestones.dashboardFor(milestone.name)}
      description={words.schedule.milestones.dashboardDetail}
      attrs={{ "data-milestone-dashboard": milestone.name }}
    >
      <form onSubmit={onSubmit} className="space-y-3" noValidate>
        <Field label={words.schedule.milestones.dashboardName} autoFocus value={name} onChange={(e) => setName(e.target.value)} />
        <Select label={words.schedule.milestones.template} value={template} onChange={(e) => setTemplate(e.target.value)}>
          {templates.map((t) => (
            <option key={t.key} value={t.key}>
              {t.name}
              {t.builtIn ? "" : words.schedule.milestones.saved}
            </option>
          ))}
        </Select>
        {create.error && <ErrorBanner>{(create.error as Error).message}</ErrorBanner>}
        <div className="flex justify-end gap-2">
          <Button type="button" variant="secondary" onClick={onClose}>
            {words.schedule.milestones.cancel}
          </Button>
          <Button type="submit" loading={create.isPending} disabled={!name.trim()}>
            {words.schedule.milestones.createDashboard}
          </Button>
        </div>
      </form>
    </Dialog>
  );
}
