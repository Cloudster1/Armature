import { useDashboardTemplates, useDeleteTemplate, type DashboardTemplate } from "@/api/reports";
import { Button, OptionCard } from "@/components/ui";
import { TEMPLATE_SHAPE_MAX_KINDS } from "@/config";
import { useConfirm } from "@/features/shell/ConfirmProvider";
import { t } from "@/i18n";

/** The key of starting from nothing, first in the chooser. */
export const BLANK_TEMPLATE = "blank";

/** How a template's contents read on its card: "6 widgets: Filter, Milestones, ...". */
export function dashboardTemplateShape(template: DashboardTemplate): string {
  const kinds = template.widgets.map((w) => w.title || w.kind.replace(/_/g, " "));
  const count = t.dashboard.page.widgets(kinds.length);
  return kinds.length > 0 ? `${count}: ${kinds.slice(0, TEMPLATE_SHAPE_MAX_KINDS).join(", ")}${kinds.length > TEMPLATE_SHAPE_MAX_KINDS ? ", ..." : ""}` : count;
}

/**
 * Picks what a new dashboard starts from: nothing, a built-in, or one of the
 * organization's own. A saved one can be removed here, since this is the
 * only place it is ever seen.
 */
export function DashboardTemplateChooser({
  projectKey,
  value,
  onChange,
  canRemove,
}: {
  projectKey: string;
  value: string;
  onChange: (key: string) => void;
  canRemove: boolean;
}) {
  const { data, isLoading } = useDashboardTemplates(projectKey);
  const remove = useDeleteTemplate();
  const confirm = useConfirm();
  const templates = data?.templates ?? [];
  if (isLoading) return <p className="text-sm text-ink-muted">{t.dashboard.templates.loading}</p>;

  return (
    <fieldset className="space-y-1.5">
      <legend className="block text-sm font-medium text-ink">{t.dashboard.templates.startFrom}</legend>
      <div role="radiogroup" aria-label={t.dashboard.templates.template} className="grid gap-2 sm:grid-cols-2 lg:grid-cols-3">
        <OptionCard
          checked={value === BLANK_TEMPLATE}
          data-dashboard-template={BLANK_TEMPLATE}
          onSelect={() => onChange(BLANK_TEMPLATE)}
          title={t.dashboard.templates.blank}
          description={t.dashboard.templates.blankDetail}
        />
        {templates.map((template) => (
          <div key={template.key} className="flex flex-col gap-1">
            <OptionCard
              checked={value === template.key}
              data-dashboard-template={template.key}
              onSelect={() => onChange(template.key)}
              title={template.name}
              description={template.description || (template.builtIn ? "" : t.dashboard.templates.savedHere)}
              extra={dashboardTemplateShape(template)}
              className="flex-1"
            />
            {!template.builtIn && canRemove && (
              <Button
                size="sm"
                variant="link"
                className="self-end"
                data-action="remove-template"
                onClick={async () => {
                  if (
                    await confirm({
                      noun: t.kit.confirm.things.template,
                      body: t.dashboard.templates.removeBody(template.name),
                    })
                  ) {
                    remove.mutate(template.id!, { onSuccess: () => value === template.key && onChange(BLANK_TEMPLATE) });
                  }
                }}
              >
                {t.dashboard.templates.remove}
              </Button>
            )}
          </div>
        ))}
      </div>
    </fieldset>
  );
}
