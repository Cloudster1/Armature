import { useId, useMemo, type FormEvent } from "react";
import { Link, Outlet, createRoute, useLocation, useNavigate } from "@tanstack/react-router";
import { projectRoute } from "./project";
import { useArchiveProject, useProject, useRestoreProject, useUpdateProject } from "@/api/projects";
import { canAdminister, useAccess } from "@/api/access";
import { ApiError } from "@/api/client";
import { Button, Card, EmptyState, ErrorBanner, Field, Page, PageHeader, SectionTitle, Skeleton, Switch, TabPanel, Tabs, Tag, useToast } from "@/components/ui";
import { DOCS_LABEL_MAX_LENGTH } from "@/config";
import { useDraft } from "@/lib/draft";
import { FEATURES, featureWord, hasFeature } from "@/features/projects/features";
import { projectSettingsPage, projectSetupPages, settingsPagesFor, setupAddress } from "@/features/projects/settingsTabs";
import { ShareSettings } from "@/features/projects/ShareSettings";
import { PlanningSettings } from "@/features/projects/PlanningSettings";
import { PersonPicker } from "@/features/people/PersonPicker";
import { t } from "@/i18n";

/**
 * A project's settings: its general form and, as tabs, everything it is set up
 * with. Every member opens them to see how the project is set up; each tab
 * offers changes to its administrators only, and a tab only they use is
 * neither listed nor mounted for anybody else.
 */
export const projectSettingsRoute = createRoute({
  getParentRoute: () => projectRoute,
  path: "/settings",
  component: ProjectSettingsLayout,
});

export const projectSettingsIndexRoute = createRoute({
  getParentRoute: () => projectSettingsRoute,
  path: "/",
  component: GeneralTab,
});

function ProjectSettingsLayout() {
  const { projectKey } = projectSettingsRoute.useParams();
  const { data } = useProject(projectKey);
  const { data: access } = useAccess();
  const pathname = useLocation({ select: (location) => location.pathname });
  const navigate = useNavigate();
  const panelId = useId();
  const project = data?.project;
  if (!project || !access) {
    return (
      <Page width="content">
        <Skeleton lines={6} />
      </Page>
    );
  }
  const administers = canAdminister(access, projectKey);
  const shown = settingsPagesFor(project, administers);
  const base = `/projects/${projectKey}`;
  // The longest address that opens the path wins, so /settings is General only when nothing deeper matches.
  const current = [...projectSetupPages]
    .reverse()
    .find((page) => pathname === `${base}${setupAddress(page.path)}` || pathname.startsWith(`${base}${setupAddress(page.path)}/`));
  const off = current && !hasFeature(project, current.feature) ? current : undefined;
  const refused = current?.adminOnly && !administers;
  return (
    <Page width="content">
      <PageHeader
        title={
          <>
            {t.shell.projectPages.settings}
            <Tag className="font-mono">{projectKey}</Tag>
          </>
        }
        tabs={
          <Tabs
            label={t.projectSetup.settings.tabsLabel}
            value={current?.path ?? "/settings"}
            panelId={panelId}
            onChange={(next) => navigate({ to: `${base}${setupAddress(next)}` as never })}
            tabs={shown.map((page) => ({
              value: page.path,
              label: page.tab,
              attrs: { "data-settings-tab": page === projectSettingsPage ? "general" : page.path.slice(1) },
            }))}
          />
        }
      />
      {!administers && (
        <p role="status" className="mb-4 rounded-control border border-border bg-surface-raised px-3 py-2 text-sm text-ink-muted" data-settings-read-only>
          {t.projectSetup.settings.readOnly}
        </p>
      )}
      <TabPanel id={panelId} label={current?.tab}>
        {refused ? (
          <div data-settings-refused={current.path.slice(1)}>
            <EmptyState title={t.projectSetup.settings.notAdmin} description={t.projectSetup.settings.notAdminDetail} />
          </div>
        ) : off ? (
          <div data-feature-off={off.feature}>
            <EmptyState
              title={t.projectSetup.project.featureOff(project.name, featureWord(off.feature!))}
              description={t.projectSetup.project.featureOffDetail}
              action={
                <Link to="/projects/$projectKey/settings" params={{ projectKey }} className="text-sm font-medium text-accent hover:underline">
                  {t.projectSetup.project.turnOn}
                </Link>
              }
            />
          </div>
        ) : (
          <Outlet />
        )}
      </TabPanel>
    </Page>
  );
}

// The project's own facts: what it is called, who leads it, and the one way
// to put it away. The rest of its setup has a tab of its own beside this one.
function GeneralTab() {
  const { projectKey } = projectSettingsRoute.useParams();
  const { data } = useProject(projectKey);
  const update = useUpdateProject();
  const toggleFeatures = useUpdateProject();
  const reveal = useUpdateProject();
  const archive = useArchiveProject();
  const restore = useRestoreProject();
  const toast = useToast();
  const { data: access } = useAccess();
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

  if (!project) return <Skeleton lines={6} />;
  const archived = Boolean(project.archivedAt);
  const readOnly = !canAdminister(access, projectKey);
  const restoreProject = () => restore.mutate(projectKey, { onSuccess: () => toast.success(t.projectSetup.settings.restored(project.name)) });

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
      { onSuccess: () => toast.success(t.projectSetup.settings.saved) },
    );
  }

  return (
    <div className="max-w-4xl" data-settings-general>
      {archived && !readOnly && (
        <div
          className="mb-5 flex flex-wrap items-center justify-between gap-3 rounded-md border border-warning/40 bg-warning/10 px-3 py-2 text-sm text-ink"
          data-archived-notice=""
        >
          <span>{t.projectSetup.settings.archived(project.name)}</span>
          <Button size="sm" variant="secondary" loading={restore.isPending} onClick={restoreProject} data-action="restore-project">
            {t.projectSetup.settings.restore}
          </Button>
        </div>
      )}
      {restore.error && (
        <div className="mb-5">
          <ErrorBanner>{(restore.error as Error).message}</ErrorBanner>
        </div>
      )}

      {/* Every save on an archived project is refused, so its forms are closed until it is restored. */}
      <fieldset disabled={archived || readOnly} className="min-w-0">
        <Card className="p-5">
          <form onSubmit={onSubmit} className="space-y-4" noValidate>
            <Field label={t.projectSetup.settings.name} required value={name} onChange={(e) => setField("name", e.target.value)} />
            <Field
              label={t.projectSetup.settings.description}
              rows={3}
              value={description}
              onChange={(e) => setField("description", e.target.value)}
              placeholder={t.projectSetup.settings.descriptionPlaceholder}
            />
            <PersonPicker
              label={t.projectSetup.settings.lead}
              value={leadId}
              onChange={(id) => setField("leadId", id)}
              none={t.projectSetup.settings.nobody}
              staff
              selected={project.leadId ? { id: project.leadId, name: project.leadName ?? "" } : null}
              hint={t.projectSetup.settings.leadHint}
            />
            <Field
              label={t.projectSetup.settings.docs}
              type="url"
              value={docsUrl}
              onChange={(e) => setField("docsUrl", e.target.value)}
              placeholder="https://wiki.example.com/spaces/team"
              hint={t.projectSetup.settings.docsHint}
              error={fieldErrors.docsUrl}
            />
            <Field
              label={t.projectSetup.settings.linkName}
              value={docsLabel}
              onChange={(e) => setField("docsLabel", e.target.value)}
              placeholder={t.projectSetup.settings.linkNamePlaceholder}
              maxLength={DOCS_LABEL_MAX_LENGTH}
              disabled={!docsUrl.trim()}
              error={fieldErrors.docsLabel}
            />
            {/* Beside Save, which a phone shows a screen below the top of the form. */}
            {update.error && <ErrorBanner>{(update.error as Error).message}</ErrorBanner>}
            <div className="flex items-center gap-3">
              <Button type="submit" loading={update.isPending} disabled={!name.trim()}>
                {t.projectSetup.settings.save}
              </Button>
              <span className="text-sm text-ink-subtle">{t.projectSetup.settings.keyStays(projectKey)}</span>
            </div>
          </form>
        </Card>

        <section className="mt-8">
          <SectionTitle className="mb-2">{t.projectSetup.settings.features}</SectionTitle>
          <Card className="p-5">
            <p className="mb-3 text-sm text-ink-muted">{t.projectSetup.settings.featuresDetail}</p>
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
                        toggleFeatures.mutate(
                          { key: projectKey, features },
                          { onSuccess: () => toast.success(next ? t.projectSetup.settings.turnedOn(f.label) : t.projectSetup.settings.turnedOff(f.label)) },
                        );
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

        <section className="mt-8">
          <SectionTitle className="mb-2">{t.projectSetup.settings.visibility}</SectionTitle>
          <Card className="p-5">
            <div className="flex items-start justify-between gap-4">
              <div>
                <p className="text-sm font-medium text-ink">{t.projectSetup.settings.revealsIssues}</p>
                <p className="mt-1 text-sm text-ink-muted">{t.projectSetup.settings.revealsIssuesDetail}</p>
              </div>
              <Switch
                checked={project.revealsIssues}
                label={t.projectSetup.settings.revealsIssues}
                data-setting="reveals-issues"
                disabled={reveal.isPending}
                onChange={(next) =>
                  reveal.mutate(
                    { key: projectKey, revealsIssues: next },
                    { onSuccess: () => toast.success(next ? t.projectSetup.settings.revealsOn : t.projectSetup.settings.revealsOff) },
                  )
                }
              />
            </div>
            {reveal.error && (
              <div className="mt-3">
                <ErrorBanner>{(reveal.error as Error).message}</ErrorBanner>
              </div>
            )}
          </Card>
        </section>

        <PlanningSettings project={project} />

        {project.features.includes("resources") && <ShareSettings projectKey={project.key} />}
      </fieldset>

      {!archived && !readOnly && (
        <section className="mt-8">
          <SectionTitle className="mb-2">{t.projectSetup.settings.archive}</SectionTitle>
          <Card className="flex flex-wrap items-center justify-between gap-3 p-5">
            <p className="text-sm text-ink-muted">{t.projectSetup.settings.archiveDetail}</p>
            <Button
              variant="danger"
              loading={archive.isPending}
              data-action="archive-project"
              onClick={() =>
                archive.mutate(projectKey, {
                  onSuccess: () =>
                    toast.success(t.projectSetup.settings.archivedToast(project.name), {
                      action: { label: t.projectSetup.settings.undo, onClick: restoreProject },
                    }),
                })
              }
            >
              {t.projectSetup.settings.archiveProject}
            </Button>
          </Card>
          {archive.error && (
            <div className="mt-3">
              <ErrorBanner>{(archive.error as Error).message}</ErrorBanner>
            </div>
          )}
        </section>
      )}
    </div>
  );
}
