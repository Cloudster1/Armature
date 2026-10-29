import { useState, type FormEvent } from "react";
import {
  useCreateProjectTemplate,
  useUpdateProjectTemplate,
  type Feature,
  type ProjectKind,
  type ProjectTemplate,
  type ProjectTemplateInput,
} from "@/api/projects";
import { LEVEL_STANDARD, useIssueTypes } from "@/api/issues";
import { useSchemes } from "@/api/workflows";
import { Button, Checkbox, Dialog, ErrorBanner, Field, Select, Switch, Tag } from "@/components/ui";
import { Icon } from "@/components/icons";
import { TEMPLATE_DESCRIPTION_MAX_LENGTH, TEMPLATE_NAME_MAX_LENGTH, TEMPLATE_SCHEME_CHOICES } from "@/config";
import { TypeBadge } from "@/features/issues/badges";
import { FEATURES } from "./features";
import { t } from "@/i18n";

const KINDS: ProjectKind[] = ["software", "service", "business"];
const BOARDS = ["kanban", "scrum"] as const;
const FORM = "template-editor-form";

/** Moves one entry a place along, or leaves the list as it was at either end. */
function shifted(ids: string[], at: number, by: -1 | 1): string[] {
  const to = at + by;
  if (to < 0 || to >= ids.length) return ids;
  const next = [...ids];
  [next[at], next[to]] = [next[to]!, next[at]!];
  return next;
}

/** The pages a kind can have: only a service project has the desk's. */
export function featuresFor(kind: ProjectKind) {
  return FEATURES.filter((feature) => !feature.deskOnly || kind === "service");
}

interface TypeRow {
  id: string;
  name: string;
  icon: string;
  level: number;
  retired: boolean;
}

/** The library as a checklist in the template's order; what is ticked is what its projects allow. */
function TypeChecklist({
  ticked,
  unticked,
  rows,
  standard,
  onChange,
}: {
  ticked: string[];
  unticked: string[];
  rows: Map<string, TypeRow>;
  standard: number;
  onChange: (ids: string[]) => void;
}) {
  const words = t.looks.projectTemplates.editor;
  return (
    <fieldset className="space-y-1.5">
      <legend className="text-sm font-medium text-ink">{words.types}</legend>
      <p className="text-xs text-ink-muted">{words.typesHint}</p>
      <ul aria-label={words.typesList} className="divide-y divide-border rounded-overlay border border-border">
        {[...ticked, ...unticked].map((id) => {
          const row = rows.get(id);
          if (!row) return null;
          const on = ticked.includes(id);
          const at = ticked.indexOf(id);
          return (
            <li key={id} className="flex flex-wrap items-center gap-3 px-3 py-2" data-template-type={row.name} data-ticked={on || undefined}>
              <Checkbox
                label={
                  <span className="flex items-center gap-2">
                    <TypeBadge icon={row.icon} name={row.name} />
                    <span className={row.retired ? "text-ink-muted" : "text-ink"}>{row.name}</span>
                    {row.retired && <Tag>{t.looks.issueTypes.retired}</Tag>}
                  </span>
                }
                aria-label={words.allow(row.name)}
                checked={on}
                onChange={(e) => onChange(e.target.checked ? [...ticked, id] : ticked.filter((each) => each !== id))}
                className="min-w-40"
              />
              {!on && <span className="text-xs text-ink-subtle">{words.notOffered}</span>}
              {on && (
                <span className="ml-auto flex gap-1">
                  <Button
                    type="button"
                    size="sm"
                    variant="ghost"
                    title={words.earlier(row.name)}
                    aria-label={words.earlier(row.name)}
                    disabled={at === 0}
                    onClick={() => onChange(shifted(ticked, at, -1))}
                  >
                    <Icon.ChevronUp />
                  </Button>
                  <Button
                    type="button"
                    size="sm"
                    variant="ghost"
                    title={words.later(row.name)}
                    aria-label={words.later(row.name)}
                    disabled={at === ticked.length - 1}
                    onClick={() => onChange(shifted(ticked, at, 1))}
                  >
                    <Icon.ChevronDown />
                  </Button>
                </span>
              )}
            </li>
          );
        })}
      </ul>
      {standard === 0 && <p className="text-xs text-ink-muted">{words.noStandard}</p>}
    </fieldset>
  );
}

/**
 * Makes a stored template, or changes one. What it sends is what is on the
 * form; the server's refusals come back as sentences and are shown under it.
 */
export function TemplateEditor({ template, onClose }: { template?: ProjectTemplate; onClose: () => void }) {
  const create = useCreateProjectTemplate();
  const update = useUpdateProjectTemplate();
  const { data: library } = useIssueTypes();
  const { data: schemes } = useSchemes({ limit: TEMPLATE_SCHEME_CHOICES });
  const words = t.looks.projectTemplates.editor;

  const [name, setName] = useState(template?.name ?? "");
  const [description, setDescription] = useState(template?.description ?? "");
  const [kind, setKind] = useState<ProjectKind>(template?.kind ?? "software");
  const [board, setBoard] = useState<"scrum" | "kanban">(template?.boardType ?? "kanban");
  const [planning, setPlanning] = useState<"scrum" | "kanban">(template?.planningMethod ?? "kanban");
  const [features, setFeatures] = useState<Feature[]>(template?.features ?? featuresFor("software").map((feature) => feature.key));
  const [schemeId, setSchemeId] = useState(template?.scheme?.id ?? "");
  const [desk, setDesk] = useState(template?.desk ?? false);
  // A new template starts with the built-in types, as the server's own start does.
  const [typeIds, setTypeIds] = useState<string[] | null>(template ? template.issueTypes!.map((type) => type.id) : null);

  const offered = library?.issueTypes ?? [];
  const ticked = typeIds ?? offered.filter((type) => type.builtin).map((type) => type.id);
  const rows = new Map<string, TypeRow>();
  for (const type of offered) rows.set(type.id, { ...type, retired: false });
  // A retired type the template already holds stays on it until it is unticked.
  for (const type of template?.issueTypes ?? []) rows.set(type.id, type);
  const unticked = offered.filter((type) => !ticked.includes(type.id)).map((type) => type.id);
  const standard = ticked.filter((id) => rows.get(id)?.level === LEVEL_STANDARD && !rows.get(id)?.retired);

  const saving = template ? update : create;
  const schemeList = schemes?.schemes ?? [];
  // A scheme beyond the first page still shows as the one chosen.
  const chosenMissing = template?.scheme?.id && !schemeList.some((scheme) => scheme.id === template.scheme?.id);

  function pickKind(next: ProjectKind) {
    setKind(next);
    if (next !== "service") {
      setFeatures(features.filter((key) => !FEATURES.find((feature) => feature.key === key)?.deskOnly));
      setDesk(false);
    }
  }

  function tickFeature(key: Feature, on: boolean) {
    setFeatures(on ? [...features, key] : features.filter((each) => each !== key));
  }

  function onSubmit(event: FormEvent) {
    event.preventDefault();
    if (!name.trim() || standard.length === 0) return;
    const body: ProjectTemplateInput = {
      name: name.trim(),
      description: description.trim(),
      kind,
      boardType: board,
      planningMethod: planning,
      // Sent in the order pages are listed, whatever order they were ticked in.
      features: FEATURES.filter((feature) => features.includes(feature.key)).map((feature) => feature.key),
      desk: kind === "service" && desk,
      issueTypeIds: ticked,
      ...(schemeId ? { workflowSchemeId: schemeId } : { defaultWorkflowScheme: true }),
    };
    if (template) update.mutate({ id: template.key, ...body }, { onSuccess: onClose });
    else create.mutate(body, { onSuccess: onClose });
  }

  return (
    <Dialog
      open
      onClose={onClose}
      size="lg"
      title={template ? words.editTitle(template.name) : words.newTitle}
      footer={
        <div className="flex justify-end gap-2">
          <Button type="button" variant="secondary" onClick={onClose}>
            {words.cancel}
          </Button>
          <Button type="submit" form={FORM} loading={saving.isPending} disabled={!name.trim() || standard.length === 0} data-action="save-template">
            {template ? words.save : words.create}
          </Button>
        </div>
      }
    >
      <form id={FORM} onSubmit={onSubmit} className="space-y-4" noValidate data-template-editor>
        <Field id="template-name" label={words.name} autoFocus value={name} maxLength={TEMPLATE_NAME_MAX_LENGTH} onChange={(e) => setName(e.target.value)} />
        <Field
          id="template-description"
          label={words.description}
          rows={2}
          value={description}
          maxLength={TEMPLATE_DESCRIPTION_MAX_LENGTH}
          onChange={(e) => setDescription(e.target.value)}
        />
        <div className="grid gap-3 sm:grid-cols-3">
          <Select id="template-kind" label={words.kind} value={kind} onChange={(e) => pickKind(e.target.value as ProjectKind)}>
            {KINDS.map((each) => (
              <option key={each} value={each}>
                {words.kinds[each]}
              </option>
            ))}
          </Select>
          <Select id="template-board" label={words.board} value={board} onChange={(e) => setBoard(e.target.value as "scrum" | "kanban")}>
            {BOARDS.map((each) => (
              <option key={each} value={each}>
                {words.boards[each]}
              </option>
            ))}
          </Select>
          <Select id="template-planning" label={words.planning} value={planning} onChange={(e) => setPlanning(e.target.value as "scrum" | "kanban")}>
            {BOARDS.map((each) => (
              <option key={each} value={each}>
                {words.plannings[each]}
              </option>
            ))}
          </Select>
        </div>

        <fieldset className="space-y-1.5">
          <legend className="text-sm font-medium text-ink">{words.features}</legend>
          <p className="text-xs text-ink-muted">{words.featuresHint}</p>
          <div className="grid gap-x-4 gap-y-1.5 sm:grid-cols-3">
            {featuresFor(kind).map((feature) => (
              <Checkbox
                key={feature.key}
                label={feature.label}
                checked={features.includes(feature.key)}
                onChange={(e) => tickFeature(feature.key, e.target.checked)}
                data-feature={feature.key}
              />
            ))}
          </div>
        </fieldset>

        <Select id="template-scheme" label={words.scheme} hint={words.schemeHint} value={schemeId} onChange={(e) => setSchemeId(e.target.value)}>
          <option value="">{words.schemeDefault}</option>
          {chosenMissing && <option value={template!.scheme!.id}>{template!.scheme!.name}</option>}
          {schemeList.map((scheme) => (
            <option key={scheme.id} value={scheme.id}>
              {scheme.name}
            </option>
          ))}
        </Select>

        {kind === "service" && (
          <div className="flex items-start gap-3">
            <Switch checked={desk} onChange={setDesk} label={words.desk} data-template-desk="" />
            <span>
              <span className="block text-sm font-medium text-ink">{words.desk}</span>
              <span className="block text-xs text-ink-muted">{words.deskHint}</span>
            </span>
          </div>
        )}

        <TypeChecklist ticked={ticked} unticked={unticked} rows={rows} standard={standard.length} onChange={setTypeIds} />

        {saving.error && <ErrorBanner>{(saving.error as Error).message}</ErrorBanner>}
      </form>
    </Dialog>
  );
}
