import { useState } from "react";
import { createRoute } from "@tanstack/react-router";
import { projectSettingsRoute } from "./project-settings";
import { canAdminister, useAccess } from "@/api/access";
import { LEVEL_STANDARD, useIssueTypes } from "@/api/issues";
import { useProjectIssueTypes, useSetProjectIssueTypes, type ProjectIssueType } from "@/api/issueTypes";
import { Button, Card, Checkbox, ErrorBanner, Tag, useToast } from "@/components/ui";
import { Icon } from "@/components/icons";
import { TypeBadge } from "@/features/issues/badges";
import { t } from "@/i18n";

export const projectIssueTypesRoute = createRoute({
  getParentRoute: () => projectSettingsRoute,
  path: "/issue-types",
  component: ProjectIssueTypesTab,
});

type Row = Pick<ProjectIssueType, "id" | "name" | "icon" | "level" | "retired"> & { issues: number };

/** Moves one entry a place along, or leaves the list as it was at either end. */
function shifted(ids: string[], at: number, by: -1 | 1): string[] {
  const to = at + by;
  if (to < 0 || to >= ids.length) return ids;
  const next = [...ids];
  [next[at], next[to]] = [next[to]!, next[at]!];
  return next;
}

/** The library as a checklist in the project's order; what is ticked is what its pickers offer. */
function ProjectIssueTypesTab() {
  const { projectKey } = projectIssueTypesRoute.useParams();
  const { data: kept } = useProjectIssueTypes(projectKey, { retired: true });
  const { data: library } = useIssueTypes();
  const save = useSetProjectIssueTypes(projectKey);
  const toast = useToast();
  const { data: access } = useAccess();
  const readOnly = !canAdminister(access, projectKey);
  const [edited, setEdited] = useState<string[] | null>(null);
  if (!kept || !library || !access) return null;

  const rows = new Map<string, Row>();
  for (const type of library.issueTypes) rows.set(type.id, { ...type, issues: 0 });
  for (const type of kept.issueTypes) rows.set(type.id, type);
  const saved = kept.issueTypes.map((type) => type.id);
  const ticked = edited ?? saved;
  // Unticked rows follow the ticked ones in the library's order; a retired type nobody ticked is not offered for adding.
  const unticked = library.issueTypes.filter((type) => !ticked.includes(type.id)).map((type) => type.id);
  const standard = ticked.filter((id) => rows.get(id)?.level === LEVEL_STANDARD);
  const changed = ticked.length !== saved.length || ticked.some((id, i) => id !== saved[i]);

  function tick(id: string, on: boolean) {
    setEdited(on ? [...ticked, id] : ticked.filter((each) => each !== id));
  }

  function onSave() {
    save.mutate(ticked, {
      onSuccess: () => {
        setEdited(null);
        toast.success(t.looks.issueTypes.project.saved);
      },
    });
  }

  return (
    <fieldset disabled={readOnly} className="max-w-3xl min-w-0" data-project-issue-types>
      <p className="mb-4 text-sm text-ink-muted">{t.looks.issueTypes.project.meta}</p>
      <Card>
        <ul aria-label={t.looks.issueTypes.project.checklist} className="divide-y divide-border">
          {[...ticked, ...unticked].map((id) => {
            const row = rows.get(id)!;
            const on = ticked.includes(id);
            const at = ticked.indexOf(id);
            const inUse = row.issues > 0;
            const lastStandard = on && row.level === LEVEL_STANDARD && standard.length === 1;
            const reason = inUse ? t.looks.issueTypes.project.inUse(row.issues) : lastStandard ? t.looks.issueTypes.project.onlyStandard : undefined;
            return (
              <li key={id} className="flex flex-wrap items-center gap-3 px-4 py-2.5" data-project-type={row.name} data-ticked={on || undefined}>
                <Checkbox
                  label={
                    <span className="flex items-center gap-2">
                      <TypeBadge icon={row.icon} name={row.name} />
                      <span className={row.retired ? "text-ink-muted" : "text-ink"}>{row.name}</span>
                      {row.retired && <Tag>{t.looks.issueTypes.retired}</Tag>}
                    </span>
                  }
                  aria-label={t.looks.issueTypes.project.allow(row.name)}
                  checked={on}
                  disabled={on && Boolean(reason)}
                  onChange={(e) => tick(id, e.target.checked)}
                  className="min-w-40"
                />
                {on && reason && (
                  <span className="text-xs text-ink-muted" data-type-reason>
                    {reason}
                  </span>
                )}
                {!on && <span className="text-xs text-ink-subtle">{t.looks.issueTypes.project.notOffered}</span>}
                {on && (
                  <span className="ml-auto flex gap-1">
                    <Button
                      size="sm"
                      variant="ghost"
                      title={t.looks.issueTypes.project.earlier(row.name)}
                      aria-label={t.looks.issueTypes.project.earlier(row.name)}
                      disabled={at === 0}
                      onClick={() => setEdited(shifted(ticked, at, -1))}
                      data-action="type-earlier"
                    >
                      <Icon.ChevronUp />
                    </Button>
                    <Button
                      size="sm"
                      variant="ghost"
                      title={t.looks.issueTypes.project.later(row.name)}
                      aria-label={t.looks.issueTypes.project.later(row.name)}
                      disabled={at === ticked.length - 1}
                      onClick={() => setEdited(shifted(ticked, at, 1))}
                      data-action="type-later"
                    >
                      <Icon.ChevronDown />
                    </Button>
                  </span>
                )}
              </li>
            );
          })}
        </ul>
      </Card>
      {save.error && (
        <div className="mt-3">
          <ErrorBanner>{(save.error as Error).message}</ErrorBanner>
        </div>
      )}
      {!readOnly && (
        <div className="mt-4 flex items-center gap-3">
          <Button loading={save.isPending} disabled={!changed || standard.length === 0} onClick={onSave} data-action="save-project-types">
            {t.looks.issueTypes.project.save}
          </Button>
          {standard.length === 0 && <span className="text-sm text-ink-muted">{t.looks.issueTypes.project.none}</span>}
        </div>
      )}
    </fieldset>
  );
}
