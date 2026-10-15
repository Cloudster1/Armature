import { useEffect, useState } from "react";
import { Link, createRoute, useNavigate } from "@tanstack/react-router";
import { projectRoute } from "./project";
import { useProject } from "@/api/projects";
import { canAdminister, useAccess } from "@/api/access";
import { useIssues, type StatusCategory } from "@/api/issues";
import type { LabelRef } from "@/api/issues";
import { useMilestones } from "@/api/milestones";
import { IssueTable } from "@/features/issues/IssueTable";
import { QueryInput } from "@/features/search/QueryInput";
import { LabelInput } from "@/features/labels/LabelInput";
import { CreateIssueDialog } from "@/features/issues/CreateIssueDialog";
import { ExportDialog } from "@/features/filters/ExportDialog";
import { quote } from "@/features/dashboard/filter";
import { Button, EmptyState, ErrorBanner, Input, Labelled, Page, PageHeader, Pager, Popover, Segmented, Select, Skeleton, Tag, Toolbar, clampOffset, isPageOffset, shownOf } from "@/components/ui";
import { Icon } from "@/components/icons";
import { StatusStrip } from "@/features/projects/StatusStrip";
import { ISSUE_PAGE_SIZE } from "@/config";

export const projectDetailRoute = createRoute({
  getParentRoute: () => projectRoute,
  path: "/",
  // The page is in the address, so a reload or a link lands on it.
  validateSearch: (search: Record<string, unknown>): { offset?: number } => (isPageOffset(search.offset) ? { offset: search.offset } : {}),
  component: ProjectDetail,
});

export type Scope = "open" | "all" | "mine";

/** Words match the summary; a query is NQL, and can say anything. */
type SearchMode = "words" | "query";

const scopeFilters: Record<Scope, { category?: StatusCategory[]; assignee?: string }> = {
  open: { category: ["todo", "in_progress"] },
  all: {},
  mine: { assignee: "me" },
};

const scopeClauses: Record<Scope, string> = {
  open: "statusCategory in (todo, in_progress)",
  all: "",
  mine: "assignee = currentUser()",
};

/** The list's narrowing in the search language, so Export writes every page of it. Labels and milestones match by name there. */
export function listQuery({ scope, words, query, label, milestone }: { scope: Scope; words: string; query: string; label?: string; milestone?: string }): string {
  const parts = [scopeClauses[scope]];
  if (words.trim()) parts.push(`summary ~ ${quote(words.trim())}`);
  if (label) parts.push(`labels = ${quote(label)}`);
  if (milestone) parts.push(`milestone = ${quote(milestone)}`);
  const given = parts.filter(Boolean);
  // Alone, a query keeps its ORDER BY; beside other clauses it is bracketed.
  if (query.trim()) given.push(given.length ? `(${query.trim()})` : query.trim());
  return given.join(" AND ");
}

function ProjectDetail() {
  const { projectKey } = projectDetailRoute.useParams();
  const { offset = 0 } = projectDetailRoute.useSearch();
  const navigate = useNavigate();
  const { data: projectData, error } = useProject(projectKey);
  const { data: access } = useAccess();
  const [scope, setScope] = useState<Scope>("open");
  const [text, setText] = useState("");
  const [mode, setMode] = useState<SearchMode>("words");
  const [query, setQuery] = useState("");
  const [label, setLabel] = useState<LabelRef | null>(null);
  const [milestoneId, setMilestoneId] = useState("");
  const [filtersOpen, setFiltersOpen] = useState(false);
  const [creating, setCreating] = useState(false);
  const [exporting, setExporting] = useState(false);
  const { data: milestoneData } = useMilestones(projectKey, true);

  const { data: issueData, isLoading: loadingIssues, error: issueError } = useIssues({
    project: projectKey,
    ...scopeFilters[scope],
    text: mode === "words" ? text.trim() || undefined : undefined,
    q: mode === "query" ? query || undefined : undefined,
    label: label ? [label.id] : undefined,
    milestone: milestoneId || undefined,
    orderBy: "created",
    limit: ISSUE_PAGE_SIZE,
    offset: offset || undefined,
  });

  const turn = (next: number, replace = false) => navigate({ to: "/projects/$projectKey", params: { projectKey }, search: next ? { offset: next } : {}, replace });
  // A narrower list starts again at its first page.
  function narrowing<T>(set: (value: T) => void) {
    return (value: T) => {
      set(value);
      if (offset) turn(0, true);
    };
  }
  // Issues leave the list while it is open; a page past the end steps back.
  useEffect(() => {
    if (issueData && offset > 0 && offset >= issueData.total) {
      navigate({ to: "/projects/$projectKey", params: { projectKey }, search: { offset: clampOffset(offset, issueData.total, ISSUE_PAGE_SIZE) || undefined }, replace: true });
    }
  }, [issueData, offset, projectKey, navigate]);

  if (error) {
    return <ErrorBanner>{(error as Error).message}</ErrorBanner>;
  }

  const project = projectData?.project;
  const issues = issueData?.issues ?? [];
  const activeFilters = Number(Boolean(label)) + Number(Boolean(milestoneId));
  const narrowed = Boolean(mode === "words" ? text : query) || activeFilters > 0;
  // The whole page's conditions as one query, for carrying on in Search.
  const asQuery = [`project = ${projectKey}`, mode === "query" && query ? `(${query})` : ""].filter(Boolean).join(" AND ");
  const milestones = milestoneData?.milestones ?? [];
  const exported = listQuery({
    scope,
    words: mode === "words" ? text : "",
    query: mode === "query" ? query : "",
    label: label?.name,
    milestone: milestones.find((m) => m.id === milestoneId)?.name,
  });

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
              onChange={narrowing(setScope)}
              options={(["open", "all", "mine"] as Scope[]).map((option) => ({
                value: option,
                // Drawn capitalised but lower case in the DOM, which is how the
                // filter reads in a sentence and in a test.
                label: <span className="capitalize">{option}</span>,
              }))}
            />
            <div className="flex min-w-56 max-w-md flex-1 items-center gap-1">
              {mode === "words" ? (
                <Input type="search" value={text} onChange={(e) => narrowing(setText)(e.target.value)} placeholder="Search summaries" aria-label="Search issues" />
              ) : (
                <QueryInput value={query} onSubmit={narrowing(setQuery)} error={issueError} compact label="Search issues with a query" project={projectKey} />
              )}
              <Segmented<SearchMode>
                label="Search mode"
                size="sm"
                value={mode}
                onChange={narrowing(setMode)}
                options={[
                  { value: "words", label: "Words" },
                  { value: "query", label: "Query" },
                ]}
              />
            </div>
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
                <Labelled id="filter-label" label="Label">
                  <LabelInput
                    id="filter-label"
                    aria-label="Filter by label"
                    mode="one"
                    create={false}
                    placeholder="Any label"
                    value={label ? [label.name] : []}
                    known={label ? [label] : []}
                    onChange={(_, chosen) => narrowing(setLabel)(chosen[0] ?? null)}
                  />
                </Labelled>
                {milestones.length > 0 && (
                  <Select label="Milestone" aria-label="Filter by milestone" id="filter-milestone" value={milestoneId} onChange={(e) => narrowing(setMilestoneId)(e.target.value)}>
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
                    onClick={narrowing(() => {
                      setLabel(null);
                      setMilestoneId("");
                    })}
                  >
                    Clear filters
                  </Button>
                )}
              </div>
            </Popover>
          </>
        }
        end={
          <>
            <span className="text-sm text-ink-subtle tabular-nums">{issueData ? shownOf(issueData.total, issueData.offset, issues.length, "issue") : ""}</span>
            {mode === "query" && (
              <Link to="/search" search={{ q: asQuery }} className="text-sm text-accent hover:underline">
                Open in search
              </Link>
            )}
            <Button variant="secondary" size="sm" icon={<Icon.Download />} onClick={() => setExporting(true)} data-action="export-list">
              Export
            </Button>
          </>
        }
      />

      <ExportDialog query={exported} project={projectKey} open={exporting} onClose={() => setExporting(false)} description="Every issue this list shows, on all its pages, as a file with the columns you tick." />

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
      {issueData && <Pager label="Issue pages" total={issueData.total} offset={issueData.offset} pageSize={ISSUE_PAGE_SIZE} onChange={turn} />}
    </Page>
  );
}
