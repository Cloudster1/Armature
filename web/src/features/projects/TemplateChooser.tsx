import { useProjectTemplates, type ProjectTemplate } from "@/api/projects";
import { OptionCard } from "@/components/ui";
import { leftOut } from "./features";
import { t } from "@/i18n";

/** How a template's shape reads on its card, in the words the product uses. */
export function templateShape(template: ProjectTemplate): string {
  const words = t.projectSetup.templates;
  const board = template.boardType === "scrum" ? words.scrumBoard : words.kanbanBoard;
  // A stored template names its scheme and its types; a built-in brings its workflow and every type.
  const stored = template.builtin === false;
  const workflow = (stored ? template.scheme?.name : undefined) ?? template.workflowName ?? words.defaultWorkflows;
  const types = stored && template.issueTypes ? template.issueTypes.map((type) => type.name).join(", ") : "";
  const without = leftOut(template.features);
  return words.shape(board, workflow) + (types ? words.types(types) : "") + (without.length > 0 ? words.without(without.join(", ")) : "");
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
  const templates = (data?.templates ?? []).filter((template) => !template.archived);
  // The server lists the built-ins first and the stored ones after; the chooser keeps them apart.
  const made = templates.filter((template) => template.builtin === false);
  const builtIn = templates.filter((template) => template.builtin !== false);

  if (isLoading) return <p className="text-sm text-ink-muted">{t.projectSetup.templates.loading}</p>;
  if (templates.length === 0) return null;

  // Until the user picks, the server's first template is the one selected.
  const selected = value || templates[0]!.key;
  const cards = (list: ProjectTemplate[], label: string) => (
    <div role="radiogroup" aria-label={label} className="grid gap-2 sm:grid-cols-3">
      {list.map((template) => (
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
  );

  return (
    <fieldset className="space-y-1.5">
      <legend className="block text-sm font-medium text-ink">{t.projectSetup.templates.label}</legend>
      {cards(builtIn, t.projectSetup.templates.label)}
      {made.length > 0 && (
        <div className="space-y-1.5 pt-1" data-template-group="made">
          <p className="text-xs font-medium text-ink-muted">{t.projectSetup.templates.madeHere}</p>
          {cards(made, t.projectSetup.templates.madeHere)}
        </div>
      )}
    </fieldset>
  );
}
