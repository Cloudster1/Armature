import { useProjectTemplates, type ProjectTemplate } from "@/api/projects";
import { OptionCard } from "@/components/ui";
import { leftOut } from "./features";
import { t } from "@/i18n";

/** How a template's shape reads on its card, in the words the product uses. */
export function templateShape(template: ProjectTemplate): string {
  const words = t.projectSetup.templates;
  const board = template.boardType === "scrum" ? words.scrumBoard : words.kanbanBoard;
  const workflow = template.workflowName ?? words.defaultWorkflows;
  const without = leftOut(template.features);
  return words.shape(board, workflow) + (without.length > 0 ? words.without(without.join(", ")) : "");
}

/**
 * Picks the template a new project is set up from.
 *
 * A radio group drawn as cards: each template is a decision about how work will
 * be shown and moved, and the description is what makes the decision, so it
 * sits on the card rather than behind a tooltip.
 */
export function TemplateChooser({ value, onChange }: { value: string; onChange: (key: string) => void }) {
  const { data, isLoading } = useProjectTemplates();
  const templates = data?.templates ?? [];

  if (isLoading) return <p className="text-sm text-ink-muted">{t.projectSetup.templates.loading}</p>;
  if (templates.length === 0) return null;

  // Until the user picks, the server's first template is the one selected.
  const selected = value || templates[0]!.key;

  return (
    <fieldset className="space-y-1.5">
      <legend className="block text-sm font-medium text-ink">{t.projectSetup.templates.label}</legend>
      <div role="radiogroup" aria-label={t.projectSetup.templates.label} className="grid gap-2 sm:grid-cols-3">
        {templates.map((template) => (
          <OptionCard
            key={template.key}
            checked={template.key === selected}
            data-template={template.key}
            onSelect={() => onChange(template.key)}
            title={template.name}
            description={template.description}
            extra={templateShape(template)}
          />
        ))}
      </div>
    </fieldset>
  );
}
