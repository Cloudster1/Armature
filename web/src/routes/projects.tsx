import { useState, type FormEvent } from "react";
import { Link, createRoute, useNavigate } from "@tanstack/react-router";
import { appRoute } from "./app";
import { useArchiveProject, useCreateProject, useKeyCheck, useProjects, type Project } from "@/api/projects";
import { canAdminister, useAccess } from "@/api/access";
import { Button, Card, EmptyState, ErrorBanner, Field, IconButton, Menu, Page, PageHeader, Skeleton, Switch, Table, Td, Th, useToast } from "@/components/ui";
import { Icon } from "@/components/icons";
import { TemplateChooser } from "@/features/projects/TemplateChooser";
import { relativeTime } from "@/features/issues/badges";
import { useConfirm } from "@/features/shell/ConfirmProvider";
import { HealthTag } from "@/features/projects/StatusStrip";

export const projectsRoute = createRoute({
  getParentRoute: () => appRoute,
  path: "/projects",
  component: ProjectsPage,
});

function ProjectsPage() {
  const [showArchived, setShowArchived] = useState(false);
  const { data, isLoading } = useProjects(showArchived);
  const { data: access } = useAccess();
  const [creating, setCreating] = useState(false);

  const projects = data?.projects ?? [];
  // Asked of the server rather than guessed from the org role, so that the
  // button and the endpoint cannot disagree about who may press it.
  const canCreate = access?.canCreateProject ?? false;

  return (
    <Page width="content">
      <PageHeader
        title="Projects"
        meta={data ? `${projects.length} project${projects.length === 1 ? "" : "s"}` : undefined}
        actions={
          <>
            <Switch label="Show archived" checked={showArchived} onChange={setShowArchived} data-show-archived="" />
            <span className="text-sm text-ink-muted">Show archived</span>
            {canCreate && !creating && <Button onClick={() => setCreating(true)}>New project</Button>}
          </>
        }
      />

      {creating && <CreateProjectForm onDone={() => setCreating(false)} />}

      {isLoading ? (
        <Skeleton rows={4} />
      ) : projects.length === 0 && !creating ? (
        <EmptyState
          icon={<Icon.Board />}
          title="No projects yet"
          description={canCreate ? "A project holds issues, boards and a plan. Make the first one." : "An administrator makes the first project."}
          action={canCreate ? <Button onClick={() => setCreating(true)}>New project</Button> : undefined}
        />
      ) : (
        <Table>
          <thead>
            <tr>
              <Th className="w-20">Key</Th>
              <Th>Project</Th>
              <Th className="w-28">Kind</Th>
              <Th className="w-40">Lead</Th>
              <Th className="w-28">Status</Th>
              <Th className="w-40 text-right">Issues</Th>
              <Th className="w-24 text-right">Updated</Th>
              <Th className="w-10" aria-label="Actions" />
            </tr>
          </thead>
          <tbody>
            {projects.map((project) => (
              <ProjectRow key={project.id} project={project} canAdminister={canAdminister(access, project.key)} />
            ))}
          </tbody>
        </Table>
      )}
    </Page>
  );
}

function ProjectRow({ project, canAdminister: admin }: { project: Project; canAdminister: boolean }) {
  const navigate = useNavigate();
  const archive = useArchiveProject();
  const confirm = useConfirm();
  const toast = useToast();
  const archived = Boolean(project.archivedAt);
  return (
    <tr className="group hover:bg-surface-raised/60" data-project={project.key} data-archived={archived || undefined}>
      <Td className="font-mono text-sm text-ink-muted">{project.key}</Td>
      <Td>
        <Link to="/projects/$projectKey" params={{ projectKey: project.key }} className="font-medium text-ink group-hover:text-accent">
          {project.name}
        </Link>
        {archived && <span className="ml-2 text-xs text-ink-subtle">archived</span>}
      </Td>
      <Td className="text-sm text-ink-muted">{project.template ? templateLabel(project.template) : templateLabel(project.kind)}</Td>
      <Td className="text-sm text-ink-muted">{project.leadName || ""}</Td>
      <Td className="text-sm" title={project.status?.note}>
        {project.status ? <HealthTag status={project.status.status} /> : <span className="text-ink-subtle">Not said</span>}
      </Td>
      <Td className="text-right text-sm whitespace-nowrap tabular-nums">
        <span className="text-ink">{project.openIssueCount} open</span>
        <span className="text-ink-subtle"> · {project.issueCount} total</span>
      </Td>
      <Td className="text-right text-sm whitespace-nowrap text-ink-subtle tabular-nums">{relativeTime(project.updatedAt)}</Td>
      <Td className="text-right">
        {admin && !archived && (
          <Menu
            label={`Actions for ${project.name}`}
            align="end"
            trigger={(props) => (
              <IconButton icon={<Icon.More />} label={`Actions for ${project.name}`} size="sm" onClick={props.toggle} aria-haspopup={props["aria-haspopup"]} aria-expanded={props["aria-expanded"]} />
            )}
            items={[
              { label: "Settings", icon: <Icon.Settings />, onSelect: () => navigate({ to: "/projects/$projectKey/settings", params: { projectKey: project.key } }) },
              {
                label: "Archive",
                icon: <Icon.Archive />,
                danger: true,
                attrs: { "data-action": "archive-project" },
                onSelect: async () => {
                  if (await confirm({ noun: "project", verb: "Archive", body: `${project.name} and its issues leave the lists; nothing is deleted, and it can be shown again with the archived ones.` })) {
                    archive.mutate(project.key, { onSuccess: () => toast.success(`Archived ${project.name}`) });
                  }
                },
              },
            ]}
          />
        )}
      </Td>
    </tr>
  );
}

/** A template key as it reads in a sentence. */
function templateLabel(key: string): string {
  return key.replace(/-/g, " ").replace(/^./, (c) => c.toUpperCase());
}

// Creating a project takes you to it: the next thing anybody does with a new
// project is open it.
function CreateProjectForm({ onDone }: { onDone: () => void }) {
  const create = useCreateProject();
  const navigate = useNavigate();
  const toast = useToast();
  const [name, setName] = useState("");
  const [key, setKey] = useState("");
  // Empty leaves the choice to the server, whose first template is the default.
  const [template, setTemplate] = useState("");
  // While the user has not typed a key, the server suggests one from the name.
  const check = useKeyCheck(key ? { key } : { name });
  const suggestion = check.data?.suggestion ?? "";
  const effectiveKey = key || suggestion;

  const keyTaken = Boolean(key) && check.data?.available === false;
  const keyInvalid = Boolean(key) && check.data?.valid === false;

  function onSubmit(event: FormEvent) {
    event.preventDefault();
    create.mutate(
      { name, key: effectiveKey || undefined, template: template || undefined },
      {
        onSuccess: ({ project }) => {
          onDone();
          toast.success(`Created ${project.name}`);
          navigate({ to: "/projects/$projectKey", params: { projectKey: project.key } });
        },
      },
    );
  }

  return (
    <Card className="mb-5 p-5">
      <form onSubmit={onSubmit} className="space-y-4" noValidate>
        {create.error && <ErrorBanner>{(create.error as Error).message}</ErrorBanner>}

        <div className="grid gap-4 sm:grid-cols-[1fr_10rem]">
          <Field label="Project name" required autoFocus value={name} onChange={(e) => setName(e.target.value)} placeholder="Customer Portal" />
          <Field
            label="Key"
            value={effectiveKey}
            onChange={(e) => setKey(e.target.value.toUpperCase())}
            className="font-mono uppercase"
            hint="Prefixes every issue, as in ABC-1."
            error={keyTaken ? "That key is already in use." : keyInvalid ? "Use 2 to 10 letters or digits, starting with a letter." : undefined}
          />
        </div>

        <TemplateChooser value={template} onChange={setTemplate} />

        <div className="flex gap-2">
          <Button type="submit" loading={create.isPending} disabled={!name.trim() || keyTaken || keyInvalid}>
            Create project
          </Button>
          <Button type="button" variant="ghost" onClick={onDone}>
            Cancel
          </Button>
        </div>
      </form>
    </Card>
  );
}
