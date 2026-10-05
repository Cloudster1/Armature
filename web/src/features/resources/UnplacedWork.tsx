import { Link } from "@tanstack/react-router";
import type { ResourceIssue } from "@/api/resources";
import { Card, SectionTitle } from "@/components/ui";
import { RESOURCE_LIST_LIMIT } from "@/config";
import { hours } from "./cells";

/** The open work the grid cannot place, listed rather than guessed at. */
export function UnplacedWork({ unscheduled, unestimated }: { unscheduled: ResourceIssue[]; unestimated: ResourceIssue[] }) {
  return (
    <div className="mt-6 grid gap-4 md:grid-cols-2">
      <IssueList
        id="unscheduled"
        title="Unscheduled"
        empty="Every piece of work with hours has a start and a due day."
        hint="Has hours, but not both a start and a due day."
        issues={unscheduled}
      />
      <IssueList id="unestimated" title="Unestimated" empty="Every open issue has hours on it." hint="Has no remaining time and no estimate." issues={unestimated} />
    </div>
  );
}

function IssueList({ id, title, hint, empty, issues }: { id: string; title: string; hint: string; empty: string; issues: ResourceIssue[] }) {
  const shown = issues.slice(0, RESOURCE_LIST_LIMIT);
  return (
    <section data-resource-list={id}>
      <SectionTitle className="mb-2">
        {title} <span className="font-normal text-ink-subtle tabular-nums">{issues.length}</span>
      </SectionTitle>
      <Card className="p-3">
        <p className="mb-2 text-xs text-ink-muted">{issues.length === 0 ? empty : hint}</p>
        {shown.length > 0 && (
          <ul className="space-y-1 text-sm">
            {shown.map((issue) => (
              <li key={issue.key} className="flex items-baseline justify-between gap-3" data-resource-issue={issue.key}>
                <Link to="/issues/$issueKey" params={{ issueKey: issue.key }} className="min-w-0 truncate text-ink hover:text-accent">
                  <span className="font-mono text-xs text-ink-muted">{issue.key}</span> {issue.summary}
                </Link>
                {issue.hours !== undefined && <span className="shrink-0 text-xs text-ink-muted tabular-nums">{hours(issue.hours)} h</span>}
              </li>
            ))}
          </ul>
        )}
        {issues.length > shown.length && <p className="mt-2 text-xs text-ink-subtle">and {issues.length - shown.length} more</p>}
      </Card>
    </section>
  );
}
