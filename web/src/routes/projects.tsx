import { memo, useCallback, useEffect, useState, type FormEvent } from "react";
import { Link, createRoute, useNavigate } from "@tanstack/react-router";
import { appRoute } from "./app";
import { useArchiveProject, useCreateProject, useKeyCheck, useProjects, useRestoreProject, type Project } from "@/api/projects";
import { canAdminister, useAccess } from "@/api/access";
import {
  Button,
  Card,
  EmptyState,
  ErrorBanner,
  Field,
  IconButton,
  ListSearch,
  Menu,
  Page,
  PageHeader,
  Pager,
  Skeleton,
  Switch,
  Table,
  Td,
  Th,
  clampOffset,
  countOf,
  isPageOffset,
  useToast,
} from "@/components/ui";
import { Icon } from "@/components/icons";
import { TemplateChooser } from "@/features/projects/TemplateChooser";
import { RelativeTime } from "@/features/issues/badges";
import { HealthTag } from "@/features/projects/StatusStrip";
import { PROJECT_PAGE_SIZE, PROJECT_SEARCH_DELAY_MS } from "@/config";
import { t } from "@/i18n";

interface ProjectsSearch {
  q?: string;
  offset?: number;
  archived?: boolean;
}

/** The filter, the page and whether archived ones show are in the address, so a link reopens the same list. */
export const projectsRoute = createRoute({
  getParentRoute: () => appRoute,
  path: "/projects",
  validateSearch: (search: Record<string, unknown>): ProjectsSearch => ({
    // A search of digits alone comes back from the address as a number.
    ...(typeof search.q === "number" ? { q: String(search.q) } : typeof search.q === "string" && search.q !== "" ? { q: search.q } : {}),
    ...(isPageOffset(search.offset) ? { offset: search.offset } : {}),
    ...(search.archived === true || search.archived === "true" ? { archived: true } : {}),
  }),
  component: ProjectsPage,
});

function ProjectsPage() {
  const { q = "", offset = 0, archived = false } = projectsRoute.useSearch();
  const navigate = useNavigate();
  const { data, isLoading } = useProjects({ q, offset, includeArchived: archived });
  const { data: access } = useAccess();
  const [creating, setCreating] = useState(false);
  // Archiving deletes nothing and a restore undoes it, so it asks nothing and offers
  // Undo. Held here, since an archived row leaves the list before Undo is pressed.
  const { mutate: archive } = useArchiveProject();
  const { mutate: restore } = useRestoreProject();
  const toast = useToast();
  const archiveProject = useCallback(
    (project: Project) =>
      archive(project.key, {
        onSuccess: () => toast.success(`Archived ${project.name}`, { action: { label: "Undo", onClick: () => restore(project.key) } }),
      }),
    [archive, restore, toast],
  );
  const restoreProject = useCallback(
    (project: Project) => restore(project.key, { onSuccess: () => toast.success(`Restored ${project.name}`) }),
    [restore, toast],
  );

  const show = useCallback(
    (next: ProjectsSearch, replace = false) =>
      navigate({ to: "/projects", search: { q: next.q || undefined, offset: next.offset || undefined, archived: next.archived || undefined }, replace }),
    [navigate],
  );
  // Projects leave the list while it is open; a page past the end steps back.
  useEffect(() => {
    if (data && offset > 0 && offset >= data.total) show({ q, archived, offset: clampOffset(offset, data.total, PROJECT_PAGE_SIZE) }, true);
  }, [data, offset, q, archived, show]);

  const projects = data?.projects ?? [];
  // Asked of the server rather than guessed from the org role, so that the
  // button and the endpoint cannot disagree about who may press it.
  const canCreate = access?.canCreateProject ?? false;

  return (
    <Page width="content">
      <PageHeader
        title="Projects"
        meta={data ? countOf(data.total, t.kit.counted.project) : undefined}
        actions={
          <>
            <ProjectSearch value={q} onChange={(text) => show({ q: text, archived })} />
            <Switch label="Show archived" checked={archived} onChange={(on) => show({ q, archived: on })} data-show-archived="" />
            <span className="text-sm text-ink-muted">Show archived</span>
            {canCreate && !creating && <Button onClick={() => setCreating(true)}>New project</Button>}
          </>
        }
      />

      {creating && <CreateProjectForm onDone={() => setCreating(false)} />}

      {isLoading ? (
        <Skeleton rows={4} />
      ) : projects.length === 0 && q ? (
        <EmptyState icon={<Icon.Search />} title="No project matches that" description="Try part of its key or its name, or clear the search." />
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
              <ProjectRow
                key={project.id}
                project={project}
                canAdminister={canAdminister(access, project.key)}
                onArchive={archiveProject}
                onRestore={restoreProject}
              />
            ))}
          </tbody>
        </Table>
      )}
      {data && (
        <Pager
          label="Project pages"
          total={data.total}
          offset={data.offset}
          pageSize={PROJECT_PAGE_SIZE}
          onChange={(next) => show({ q, archived, offset: next })}
        />
      )}
    </Page>
  );
}

/** The search box over the list; it writes the address once typing pauses, not per letter. */
function ProjectSearch({ value, onChange }: { value: string; onChange: (text: string) => void }) {
  return (
    <ListSearch
      aria-label="Find projects"
      placeholder="Find by key or name"
      value={value}
      onChange={onChange}
      delayMs={PROJECT_SEARCH_DELAY_MS}
      className="w-56"
      data-project-search=""
    />
  );
}

// Memoised, since the page renders again for every archive and restore.
const ProjectRow = memo(function ProjectRow({
  project,
  canAdminister: admin,
  onArchive,
  onRestore,
}: {
  project: Project;
  canAdminister: boolean;
  onArchive: (project: Project) => void;
  onRestore: (project: Project) => void;
}) {
  const navigate = useNavigate();
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
      <Td className="text-right text-sm whitespace-nowrap text-ink-subtle tabular-nums">
        <RelativeTime iso={project.updatedAt} />
      </Td>
      <Td className="text-right">
        {admin && (
          <Menu
            label={`Actions for ${project.name}`}
            align="end"
            trigger={(props) => (
              <IconButton
                icon={<Icon.More />}
                label={`Actions for ${project.name}`}
                size="sm"
                onClick={props.toggle}
                aria-haspopup={props["aria-haspopup"]}
                aria-expanded={props["aria-expanded"]}
                data-project-menu={project.key}
              />
            )}
            items={
              archived
                ? [{ label: "Restore", icon: <Icon.Archive />, onSelect: () => onRestore(project), attrs: { "data-action": "restore-project" } }]
                : [
                    {
                      label: "Settings",
                      icon: <Icon.Settings />,
                      onSelect: () => navigate({ to: "/projects/$projectKey/settings", params: { projectKey: project.key } }),
                    },
                    {
                      label: "Archive",
                      icon: <Icon.Archive />,
                      danger: true,
                      attrs: { "data-action": "archive-project" },
                      onSelect: () => onArchive(project),
                    },
                  ]
            }
          />
        )}
      </Td>
    </tr>
  );
});

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
