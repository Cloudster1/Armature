import { useState } from "react";
import { createRoute } from "@tanstack/react-router";
import { adminRoute } from "./admin";
import { useArchiveProjectTemplate, useCreateProjectTemplate, useProjectTemplates, useRestoreProjectTemplate, type ProjectTemplate } from "@/api/projects";
import { Button, Card, EmptyState, ErrorBanner, Page, PageHeader, Segmented, Tag, useToast } from "@/components/ui";
import { TemplateEditor } from "@/features/projects/TemplateEditor";
import { templateShape } from "@/features/projects/TemplateChooser";
import { useConfirm } from "@/features/shell/ConfirmProvider";
import { t } from "@/i18n";

export const projectTemplatesRoute = createRoute({
  getParentRoute: () => adminRoute,
  path: "/project-templates",
  component: ProjectTemplatesPage,
});

type View = "available" | "archived";

/** The templates a new project starts from: duplicate a built-in one, keep your own, archive what is no longer wanted. */
function ProjectTemplatesPage() {
  const words = t.looks.projectTemplates;
  const [view, setView] = useState<View>("available");
  const { data, isLoading, error } = useProjectTemplates({ archived: view === "archived" });
  const [editing, setEditing] = useState<ProjectTemplate | "new" | null>(null);
  const duplicate = useCreateProjectTemplate();
  const archive = useArchiveProjectTemplate();
  const restore = useRestoreProjectTemplate();
  const confirm = useConfirm();
  const toast = useToast();

  const all = data?.templates ?? [];
  const builtIn = all.filter((template) => template.builtin);
  const made = all.filter((template) => !template.builtin && !template.archived);
  const archived = all.filter((template) => !template.builtin && template.archived);

  function onDuplicate(template: ProjectTemplate) {
    duplicate.mutate(
      { from: template.key },
      {
        onSuccess: ({ template: copy }) => {
          toast.success(words.duplicated(copy.name));
          setEditing(copy);
        },
      },
    );
  }

  async function onArchive(template: ProjectTemplate) {
    if (await confirm({ noun: t.kit.confirm.things.projectTemplate, verb: t.kit.confirm.verbs.archive, body: words.archiveBody(template.name) })) {
      archive.mutate(template.key, { onSuccess: () => toast.success(words.archivedNow(template.name)) });
    }
  }

  function card(template: ProjectTemplate) {
    return (
      <Card key={template.key} className="p-4" data-template-card={template.name} data-archived={template.archived || undefined}>
        <div className="flex flex-wrap items-start gap-2">
          <h3 className="text-sm font-medium text-ink">{template.name}</h3>
          {template.builtin && <Tag data-builtin-template>{words.builtIn}</Tag>}
          {template.archived && <Tag data-archived-template>{words.archivedTag}</Tag>}
        </div>
        {template.description && <p className="mt-1 text-xs text-ink-muted">{template.description}</p>}
        <p className="mt-2 text-2xs text-ink-subtle">{templateShape(template)}</p>
        <div className="mt-3 flex flex-wrap gap-2">
          {template.builtin && (
            <Button size="sm" variant="secondary" loading={duplicate.isPending} onClick={() => onDuplicate(template)} data-action="duplicate-template">
              {words.duplicate}
            </Button>
          )}
          {!template.builtin && !template.archived && (
            <>
              <Button size="sm" variant="secondary" onClick={() => setEditing(template)} data-action="edit-template">
                {words.edit}
              </Button>
              <Button size="sm" variant="ghost" onClick={() => onArchive(template)} data-action="archive-template">
                {words.archive}
              </Button>
            </>
          )}
          {template.archived && (
            <Button
              size="sm"
              variant="secondary"
              onClick={() => restore.mutate(template.key, { onSuccess: () => toast.success(words.restoredNow(template.name)) })}
              data-action="restore-template"
            >
              {words.restore}
            </Button>
          )}
        </div>
      </Card>
    );
  }

  return (
    <Page width="content">
      <PageHeader
        title={words.title}
        meta={words.meta}
        actions={
          <Button onClick={() => setEditing("new")} data-action="new-template">
            {words.newTemplate}
          </Button>
        }
      />
      <div className="mb-4">
        <Segmented
          label={words.viewLabel}
          value={view}
          onChange={setView}
          options={[
            { value: "available", label: words.available, attrs: { "data-view": "available" } },
            { value: "archived", label: words.archivedView, attrs: { "data-view": "archived" } },
          ]}
        />
      </div>
      {error && <ErrorBanner>{(error as Error).message}</ErrorBanner>}
      {/* A refused duplicate, archive or restore is the shell's toast, as on every table. */}
      {isLoading ? null : view === "available" ? (
        <div className="space-y-6">
          <section aria-label={words.builtInHeading} className="space-y-2">
            <h2 className="text-sm font-semibold text-ink">{words.builtInHeading}</h2>
            <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-3">{builtIn.map(card)}</div>
          </section>
          <section aria-label={words.madeHereHeading} className="space-y-2">
            <h2 className="text-sm font-semibold text-ink">{words.madeHereHeading}</h2>
            {made.length === 0 ? (
              <EmptyState title={words.noneMade} description={words.noneMadeDetail} />
            ) : (
              <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-3">{made.map(card)}</div>
            )}
          </section>
        </div>
      ) : archived.length === 0 ? (
        <EmptyState title={words.noneArchived} description={words.noneArchivedDetail} />
      ) : (
        <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-3">{archived.map(card)}</div>
      )}
      {editing && <TemplateEditor template={editing === "new" ? undefined : editing} onClose={() => setEditing(null)} />}
    </Page>
  );
}
