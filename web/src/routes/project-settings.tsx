import { useEffect, useState, type FormEvent } from "react";
import { Link, createRoute, useNavigate } from "@tanstack/react-router";
import { projectRoute } from "./project";
import { useArchiveProject, useProject, useUpdateProject } from "@/api/projects";
import { useMembers } from "@/api/issues";
import { canAdminister, useAccess } from "@/api/access";
import { ApiError } from "@/api/client";
import { Button, Card, EmptyState, ErrorBanner, Field, Page, PageHeader, SectionTitle, Select, Switch, Tag, useToast } from "@/components/ui";
import { DOCS_LABEL_MAX_LENGTH } from "@/config";
import { useConfirm } from "@/features/shell/ConfirmProvider";
import { FEATURES } from "@/features/projects/features";
import { ShareSettings } from "@/features/projects/ShareSettings";
import { PlanningSettings } from "@/features/projects/PlanningSettings";

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
  const { data: memberData } = useMembers();
  const update = useUpdateProject();
  const toggleFeatures = useUpdateProject();
  const archive = useArchiveProject();
  const confirm = useConfirm();
  const toast = useToast();
  const navigate = useNavigate();
  const project = data?.project;
  const [name, setName] = useState("");
  const [description, setDescription] = useState("");
  const [leadId, setLeadId] = useState("");
  const [docsUrl, setDocsUrl] = useState("");
  const [docsLabel, setDocsLabel] = useState("");
  const fieldErrors = update.error instanceof ApiError ? update.error.fields : {};

  useEffect(() => {
    if (project) {
      setName(project.name);
      setDescription(project.description);
      setLeadId(project.leadId ?? "");
      setDocsUrl(project.docsUrl ?? "");
      setDocsLabel(project.docsLabel ?? "");
    }
  }, [project]);

  if (!project) return null;
  if (!canAdminister(access, projectKey)) {
    return (
      <Page width="narrow">
        <EmptyState title="Only the project's administrators change its settings" description="Ask one of them, or ask for the role." />
      </Page>
    );
  }

  function onSubmit(event: FormEvent) {
    event.preventDefault();
    update.mutate(
      { key: projectKey, name: name.trim(), description: description.trim(), leadId: leadId || null, docsUrl: docsUrl.trim(), docsLabel: docsUrl.trim() ? docsLabel.trim() : "" },
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
            Settings
            <Tag className="font-mono">{projectKey}</Tag>
          </>
        }
      />
      <Card className="p-5">
        <form onSubmit={onSubmit} className="space-y-4" noValidate>
          {update.error && <ErrorBanner>{(update.error as Error).message}</ErrorBanner>}
          <Field label="Project name" required value={name} onChange={(e) => setName(e.target.value)} />
          <Field label="Description" rows={3} value={description} onChange={(e) => setDescription(e.target.value)} placeholder="What the project is for, in a sentence." />
          <Select label="Lead" value={leadId} onChange={(e) => setLeadId(e.target.value)} hint="A person to ask; nothing is refused on it.">
            <option value="">Nobody in particular</option>
            {(memberData?.members ?? []).map((m) => (
              <option key={m.id} value={m.id}>
                {m.name}
              </option>
            ))}
          </Select>
          <Field
            label="Documentation"
            type="url"
            value={docsUrl}
            onChange={(e) => setDocsUrl(e.target.value)}
            placeholder="https://wiki.example.com/spaces/team"
            hint="Where the project's docs live. The sidebar links to it; leave it empty for none."
            error={fieldErrors.docsUrl}
          />
          <Field
            label="Link name"
            value={docsLabel}
            onChange={(e) => setDocsLabel(e.target.value)}
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
          <p className="mb-3 text-sm text-ink-muted">The pages this project has. Its template chose them; turning one off hides the page and refuses new entries, and deletes nothing.</p>
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
                        { onSuccess: () => toast.success(`${f.label} turned ${next ? "on" : "off"}`) },
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

      <PlanningSettings project={project} />

      {project.features.includes("resources") && <ShareSettings projectKey={project.key} />}

      <section className="mt-8">
        <SectionTitle className="mb-2">Archive</SectionTitle>
        <Card className="flex flex-wrap items-center justify-between gap-3 p-5">
          <p className="text-sm text-ink-muted">Archiving takes the project and its issues out of every list. Nothing is deleted, and the archived projects can be shown again.</p>
          <Button
            variant="danger"
            loading={archive.isPending}
            data-action="archive-project"
            onClick={async () => {
              if (await confirm({ noun: "project", verb: "Archive", body: `${project.name} and its issues leave the lists; nothing is deleted.` })) {
                archive.mutate(projectKey, {
                  onSuccess: () => {
                    toast.success(`Archived ${project.name}`);
                    navigate({ to: "/projects" });
                  },
                });
              }
            }}
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
    </Page>
  );
}
