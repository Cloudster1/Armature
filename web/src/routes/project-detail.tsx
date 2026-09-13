import { useState } from "react";
import { Link, createRoute } from "@tanstack/react-router";
import { projectRoute } from "./project";
import { useProject } from "@/api/projects";
import { canAdminister, useAccess } from "@/api/access";
import { useIssues, type StatusCategory } from "@/api/issues";
import { useLabels } from "@/api/labels";
import { useMilestones } from "@/api/milestones";
import { IssueTable } from "@/features/issues/IssueTable";
import { QueryInput } from "@/features/search/QueryInput";
import { CreateIssueDialog } from "@/features/issues/CreateIssueDialog";
import { Button, EmptyState, ErrorBanner, Input, Page, PageHeader, Popover, Segmented, Select, Skeleton, Tag, Toolbar } from "@/components/ui";
import { Icon } from "@/components/icons";
import { StatusStrip } from "@/features/projects/StatusStrip";

export const projectDetailRoute = createRoute({
  getParentRoute: () => projectRoute,
  path: "/",
  component: ProjectDetail,
});

type Scope = "open" | "all" | "mine";

/** Words match the summary; a query is NQL, and can say anything. */
type SearchMode = "words" | "query";

const scopeFilters: Record<Scope, { category?: StatusCategory[]; assignee?: string }> = {
  open: { category: ["todo", "in_progress"] },
  all: {},
  mine: { assignee: "me" },
};

/** How many issues the list shows at once. */
const ISSUE_PAGE_SIZE = 100;

function ProjectDetail() {
  const { projectKey } = projectDetailRoute.useParams();
  const { data: projectData, error } = useProject(projectKey);
  const { data: access } = useAccess();
  const [scope, setScope] = useState<Scope>("open");
  const [text, setText] = useState("");
  const [mode, setMode] = useState<SearchMode>("words");
  const [query, setQuery] = useState("");
  const [labelId, setLabelId] = useState("");
  const [milestoneId, setMilestoneId] = useState("");
  const [filtersOpen, setFiltersOpen] = useState(false);
  const [creating, setCreating] = useState(false);
  const { data: labelData } = useLabels();
  const { data: milestoneData } = useMilestones(projectKey, true);

  const { data: issueData, isLoading: loadingIssues, error: issueError } = useIssues({
    project: projectKey,
    ...scopeFilters[scope],
    text: mode === "words" ? text.trim() || undefined : undefined,
    q: mode === "query" ? query || undefined : undefined,
    label: labelId ? [labelId] : undefined,
    milestone: milestoneId || undefined,
    orderBy: "created",
    limit: ISSUE_PAGE_SIZE,
  });

  if (error) {
    return <ErrorBanner>{(error as Error).message}</ErrorBanner>;
  }

  const project = projectData?.project;
  const issues = issueData?.issues ?? [];
  const activeFilters = Number(Boolean(labelId)) + Number(Boolean(milestoneId));
  const narrowed = Boolean(mode === "words" ? text : query) || activeFilters > 0;
  // The whole page's conditions as one query, for carrying on in Search.
  const asQuery = [`project = ${projectKey}`, mode === "query" && query ? `(${query})` : ""].filter(Boolean).join(" AND ");
  const labels = labelData?.labels ?? [];
  const milestones = milestoneData?.milestones ?? [];

  return (
    <Page width="content">
      <PageHeader
        crumb={
          <Link to="/projects" className="hover:text-ink">
            Projects
          </Link>
        }
        title={
          <>
            {project?.name ?? projectKey}
            <Tag className="font-mono">{projectKey}</Tag>
          </>
        }
        meta={project ? `${project.openIssueCount} open of ${project.issueCount}${project.leadName ? ` · led by ${project.leadName}` : ""}` : undefined}
        actions={<Button onClick={() => setCreating(true)}>New issue</Button>}
      />

      <CreateIssueDialog open={creating} onClose={() => setCreating(false)} projectKey={projectKey} />
      <StatusStrip projectKey={projectKey} latest={project?.status} canPost={canAdminister(access, projectKey)} />

      <Toolbar
        label="Narrow the issues"
        start={
          <>
            <Segmented
              label="Filter issues"
              value={scope}
              onChange={setScope}
              options={(["open", "all", "mine"] as Scope[]).map((option) => ({
                value: option,
                // Drawn capitalised but lower case in the DOM, which is how the
                // filter reads in a sentence and in a test.
                label: <span className="capitalize">{option}</span>,
              }))}
            />
            <div className="flex min-w-56 max-w-md flex-1 items-center gap-1">
              {mode === "words" ? (
                <Input type="search" value={text} onChange={(e) => setText(e.target.value)} placeholder="Search summaries" aria-label="Search issues" />
              ) : (
                <QueryInput value={query} onSubmit={setQuery} error={issueError} compact label="Search issues with a query" project={projectKey} />
              )}
              <Segmented<SearchMode>
                label="Search mode"
                size="sm"
                value={mode}
                onChange={setMode}
                options={[
                  { value: "words", label: "Words" },
                  { value: "query", label: "Query" },
                ]}
              />
            </div>
            {(labels.length > 0 || milestones.length > 0) && (
              <Popover
                open={filtersOpen}
                onClose={() => setFiltersOpen(false)}
                label="Filters"
                trigger={
                  <Button variant="secondary" icon={<Icon.Filter />} onClick={() => setFiltersOpen((o) => !o)} aria-expanded={filtersOpen} data-action="filters">
                    Filters{activeFilters > 0 && <Tag className="ml-1 bg-accent-subtle text-accent">{activeFilters}</Tag>}
                  </Button>
                }
              >
                <div className="w-64 space-y-3">
                  {labels.length > 0 && (
                    <Select label="Label" aria-label="Filter by label" id="filter-label" value={labelId} onChange={(e) => setLabelId(e.target.value)}>
                      <option value="">Any label</option>
                      {labels.map((label) => (
                        <option key={label.id} value={label.id}>
                          {label.name} ({label.issueCount})
                        </option>
                      ))}
                    </Select>
                  )}
                  {milestones.length > 0 && (
                    <Select label="Milestone" aria-label="Filter by milestone" id="filter-milestone" value={milestoneId} onChange={(e) => setMilestoneId(e.target.value)}>
                      <option value="">Any milestone</option>
                      {milestones.map((milestone) => (
                        <option key={milestone.id} value={milestone.id}>
                          {milestone.name} ({milestone.progress.issues})
                        </option>
                      ))}
                    </Select>
                  )}
                  {activeFilters > 0 && (
                    <Button
                      size="sm"
                      variant="ghost"
                      onClick={() => {
                        setLabelId("");
                        setMilestoneId("");
                      }}
                    >
                      Clear filters
                    </Button>
                  )}
                </div>
              </Popover>
            )}
          </>
        }
        end={
          <>
            <span className="text-sm text-ink-subtle tabular-nums">{issueData ? `${issueData.total} issue${issueData.total === 1 ? "" : "s"}` : ""}</span>
            {mode === "query" && (
              <Link to="/search" search={{ q: asQuery }} className="text-sm text-accent hover:underline">
                Open in search
              </Link>
            )}
          </>
        }
      />

      {loadingIssues ? (
        <Skeleton rows={6} />
      ) : issues.length === 0 ? (
        <EmptyState
          icon={<Icon.Issue />}
          title={narrowed ? "Nothing matches that search" : scope === "mine" ? "Nothing assigned to you" : "No issues yet"}
          description={narrowed || scope !== "open" ? undefined : "File the first one and it lands on the board."}
          action={!narrowed && scope === "open" ? <Button onClick={() => setCreating(true)}>New issue</Button> : undefined}
        />
      ) : (
        <IssueTable issues={issues} />
      )}
    </Page>
  );
}
