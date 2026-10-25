import { useMemo, type FormEvent } from "react";
import { Link, createRoute } from "@tanstack/react-router";
import { projectRoute } from "./project";
import { useArchiveProject, useProject, useRestoreProject, useUpdateProject } from "@/api/projects";
import { canAdminister, useAccess } from "@/api/access";
import { ApiError } from "@/api/client";
import { Button, Card, EmptyState, ErrorBanner, Field, Page, PageHeader, SectionTitle, Switch, Tag, useToast } from "@/components/ui";
import { DOCS_LABEL_MAX_LENGTH } from "@/config";
import { useDraft } from "@/lib/draft";
import { FEATURES } from "@/features/projects/features";
import { ShareSettings } from "@/features/projects/ShareSettings";
import { PlanningSettings } from "@/features/projects/PlanningSettings";
import { PersonPicker } from "@/features/people/PersonPicker";
import { t } from "@/i18n";

export const projectSettingsRoute = createRoute({
  getParentRoute: () => projectRoute,
  path: "/settings",
  component: ProjectSettingsPage,
});

// The project's own facts: what it is called, who leads it, and the one way
// to put it away. Everything else a project is set up with has a page of its
// own under Setup.
function ProjectSettingsPage() {
  const { projectKey } = projectSettingsRoute.useParams();
  const { data } = useProject(projectKey);
  const { data: access } = useAccess();
  const update = useUpdateProject();
  const toggleFeatures = useUpdateProject();
  const archive = useArchiveProject();
  const restore = useRestoreProject();
  const toast = useToast();
  const project = data?.project;
  const saved = useMemo(
    () =>
      project && {
        name: project.name,
        description: project.description,
        leadId: project.leadId ?? "",
        docsUrl: project.docsUrl ?? "",
        docsLabel: project.docsLabel ?? "",
      },
    [project],
  );
  const [draft, setField] = useDraft(saved);
  const { name = "", description = "", leadId = "", docsUrl = "", docsLabel = "" } = draft ?? {};
  const fieldErrors = update.error instanceof ApiError ? update.error.fields : {};

  if (!project) return null;
  if (!canAdminister(access, projectKey)) {
    return (
      <Page width="narrow">
        <EmptyState title="Only the project's administrators change its settings" description="Ask one of them, or ask for the role." />
      </Page>
    );
  }

  const archived = Boolean(project.archivedAt);
  const restoreProject = () => restore.mutate(projectKey, { onSuccess: () => toast.success(`Restored ${project.name}`) });

  function onSubmit(event: FormEvent) {
    event.preventDefault();
    update.mutate(
      {
        key: projectKey,
        name: name.trim(),
        description: description.trim(),
        leadId: leadId || null,
        docsUrl: docsUrl.trim(),
        docsLabel: docsUrl.trim() ? docsLabel.trim() : "",
      },
      { onSuccess: () => toast.success("Project settings saved") },
    );
  }

  return (
    <Page width="narrow">
      <PageHeader
        crumb={
          <Link to="/projects/$projectKey" params={{ projectKey }} className="hover:text-ink">
            {project.name}
          </Link>
        }
        title={
          <>
            {t.shell.projectPages.settings}
            <Tag className="font-mono">{projectKey}</Tag>
          </>
        }
      />
      {archived && (
        <div
          className="mb-5 flex flex-wrap items-center justify-between gap-3 rounded-md border border-warning/40 bg-warning/10 px-3 py-2 text-sm text-ink"
          data-archived-notice=""
        >
          <span>{project.name} is archived. Restore it to change its settings.</span>
          <Button size="sm" variant="secondary" loading={restore.isPending} onClick={restoreProject} data-action="restore-project">
            Restore project
          </Button>
        </div>
      )}
      {restore.error && (
        <div className="mb-5">
          <ErrorBanner>{(restore.error as Error).message}</ErrorBanner>
        </div>
      )}

      {/* Every save on an archived project is refused, so its forms are closed until it is restored. */}
      <fieldset disabled={archived} className="min-w-0">
        <Card className="p-5">
          <form onSubmit={onSubmit} className="space-y-4" noValidate>
            {update.error && <ErrorBanner>{(update.error as Error).message}</ErrorBanner>}
            <Field label="Project name" required value={name} onChange={(e) => setField("name", e.target.value)} />
            <Field
              label="Description"
              rows={3}
              value={description}
              onChange={(e) => setField("description", e.target.value)}
              placeholder="What the project is for, in a sentence."
            />
            <PersonPicker
              label="Lead"
              value={leadId}
              onChange={(id) => setField("leadId", id)}
              none="Nobody in particular"
              staff
              selected={project.leadId ? { id: project.leadId, name: project.leadName ?? "" } : null}
              hint="A person to ask; nothing is refused on it."
            />
            <Field
              label="Documentation"
              type="url"
              value={docsUrl}
              onChange={(e) => setField("docsUrl", e.target.value)}
              placeholder="https://wiki.example.com/spaces/team"
              hint="Where the project's docs live. The sidebar links to it; leave it empty for none."
              error={fieldErrors.docsUrl}
            />
            <Field
              label="Link name"
              value={docsLabel}
              onChange={(e) => setField("docsLabel", e.target.value)}
              placeholder="Docs"
              maxLength={DOCS_LABEL_MAX_LENGTH}
              disabled={!docsUrl.trim()}
              error={fieldErrors.docsLabel}
            />
            <div className="flex items-center gap-3">
              <Button type="submit" loading={update.isPending} disabled={!name.trim()}>
                Save changes
              </Button>
              <span className="text-sm text-ink-subtle">The key {projectKey} does not change; every issue carries it.</span>
            </div>
          </form>
        </Card>

        <section className="mt-8">
          <SectionTitle className="mb-2">Features</SectionTitle>
          <Card className="p-5">
            <p className="mb-3 text-sm text-ink-muted">
              The pages this project has. Its template chose them; turning one off hides the page and refuses new entries, and deletes nothing.
            </p>
            <ul className="grid gap-2 sm:grid-cols-2">
              {FEATURES.filter((f) => !f.deskOnly || project.kind === "service").map((f) => {
                const on = project.features.includes(f.key);
                return (
                  <li key={f.key} className="flex items-center justify-between gap-3 rounded-control border border-border px-3 py-2">
                    <span className="text-sm text-ink">{f.label}</span>
                    <Switch
                      checked={on}
                      label={f.label}
                      data-feature={f.key}
                      disabled={toggleFeatures.isPending}
                      onChange={(next) => {
                        const features = next ? [...project.features, f.key] : project.features.filter((each) => each !== f.key);
                        toggleFeatures.mutate({ key: projectKey, features }, { onSuccess: () => toast.success(`${f.label} turned ${next ? "on" : "off"}`) });
                      }}
                    />
                  </li>
                );
              })}
            </ul>
            {toggleFeatures.error && (
              <div className="mt-3">
                <ErrorBanner>{(toggleFeatures.error as Error).message}</ErrorBanner>
              </div>
            )}
          </Card>
        </section>

        <PlanningSettings project={project} />

        {project.features.includes("resources") && <ShareSettings projectKey={project.key} />}
      </fieldset>

      {!archived && (
        <section className="mt-8">
          <SectionTitle className="mb-2">Archive</SectionTitle>
          <Card className="flex flex-wrap items-center justify-between gap-3 p-5">
            <p className="text-sm text-ink-muted">
              Archiving takes the project and its issues out of every list. Nothing is deleted, and it can be shown again with the archived ones and restored.
            </p>
            <Button
              variant="danger"
              loading={archive.isPending}
              data-action="archive-project"
              onClick={() =>
                archive.mutate(projectKey, {
                  onSuccess: () => toast.success(`Archived ${project.name}`, { action: { label: "Undo", onClick: restoreProject } }),
                })
              }
            >
              Archive project
            </Button>
          </Card>
          {archive.error && (
            <div className="mt-3">
              <ErrorBanner>{(archive.error as Error).message}</ErrorBanner>
            </div>
          )}
        </section>
      )}
    </Page>
  );
}
