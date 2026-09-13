import { useState } from "react";
import { Link, createRoute } from "@tanstack/react-router";
import { projectRoute } from "./project";
import { useProject } from "@/api/projects";
import { useProjectHierarchy, type TreeNode } from "@/api/issues";
import { ProgressBar } from "@/features/issues/hierarchy";
import { Avatar, PriorityBadge, StatusBadge, TypeBadge } from "@/features/issues/badges";
import { Card, ErrorBanner, IconButton, Page, PageHeader, cx } from "@/components/ui";
import { Icon } from "@/components/icons";
import { MAX_TREE_DEPTH } from "@/config";

export const hierarchyRoute = createRoute({
  getParentRoute: () => projectRoute,
  path: "/hierarchy",
  component: HierarchyPage,
});

function HierarchyPage() {
  const { projectKey } = hierarchyRoute.useParams();
  const { data: projectData } = useProject(projectKey);
  const { data, isLoading, error } = useProjectHierarchy(projectKey);

  const tree = data?.tree ?? [];

  return (
    <Page width="content">
      <PageHeader
        crumb={
          <Link to="/projects/$projectKey" params={{ projectKey }} className="hover:text-ink">
            {projectData?.project?.name ?? projectKey}
          </Link>
        }
        title="Hierarchy"
      />

      {error && <ErrorBanner>{(error as Error).message}</ErrorBanner>}

      {isLoading ? (
        <p className="text-sm text-ink-muted">Loading the tree...</p>
      ) : tree.length === 0 ? (
        <Card className="p-6 text-center text-sm text-ink-muted">
          This project has no issues yet.
        </Card>
      ) : (
        <Card className="divide-y divide-border">
          <ol>
            {tree.map((node) => (
              <TreeRow key={node.issue.id} node={node} depth={0} />
            ))}
          </ol>
        </Card>
      )}
    </Page>
  );
}

/**
 * One row and everything under it. Rows start expanded: a tree that hides its
 * contents makes you click to find out whether clicking was worth it.
 */
function TreeRow({ node, depth }: { node: TreeNode; depth: number }) {
  const [open, setOpen] = useState(true);
  const { issue, progress, children } = node;
  const hasChildren = children.length > 0;

  return (
    <li>
      <div
        className="flex items-center gap-2.5 border-b border-border px-3 py-2 last:border-b-0 hover:bg-surface-raised"
        style={{ paddingLeft: `${Math.min(depth, MAX_TREE_DEPTH) * 1.5 + 0.75}rem` }}
      >
        {hasChildren ? (
          <IconButton
            size="xs"
            icon={<Icon.ChevronRight className={cx("transition-transform", open && "rotate-90")} />}
            label={`${open ? "Collapse" : "Expand"} ${issue.key}`}
            aria-expanded={open}
            onClick={() => setOpen(!open)}
          />
        ) : (
          <span className="size-5 shrink-0" />
        )}

        <TypeBadge icon={issue.type.icon} name={issue.type.name} />
        <Link
          to="/issues/$issueKey"
          params={{ issueKey: issue.key }}
          className="shrink-0 font-mono text-2xs text-ink-muted hover:text-accent"
        >
          {issue.key}
        </Link>
        <Link
          to="/issues/$issueKey"
          params={{ issueKey: issue.key }}
          className={cx(
            "min-w-0 flex-1 truncate text-sm hover:text-accent",
            issue.status.category === "done" ? "text-ink-muted line-through" : "text-ink",
          )}
        >
          {issue.summary}
        </Link>

        {progress.total > 0 && <ProgressBar progress={progress} className="w-36 shrink-0" />}
        <PriorityBadge priority={issue.priority} />
        <StatusBadge name={issue.status.name} category={issue.status.category} />
        <Avatar name={issue.assignee?.name} src={issue.assignee?.avatarUrl} size="sm" />
      </div>

      {open && hasChildren && (
        <ol>
          {children.map((child) => (
            <TreeRow key={child.issue.id} node={child} depth={depth + 1} />
          ))}
        </ol>
      )}
    </li>
  );
}
