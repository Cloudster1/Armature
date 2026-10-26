import { useId, useState } from "react";
import { Link } from "@tanstack/react-router";
import type { ResourceIssue } from "@/api/resources";
import { Button, Card, SectionTitle } from "@/components/ui";
import { RESOURCE_LIST_LIMIT } from "@/config";
import { hours } from "./cells";
import { t } from "@/i18n";

/** The open work the grid cannot place, listed rather than guessed at. */
export function UnplacedWork({ unscheduled, unestimated }: { unscheduled: ResourceIssue[]; unestimated: ResourceIssue[] }) {
  return (
    <div className="mt-6 grid gap-4 md:grid-cols-2">
      <IssueList
        id="unscheduled"
        title={t.schedule.resources.unscheduled}
        empty={t.schedule.resources.unscheduledEmpty}
        hint={t.schedule.resources.unscheduledHint}
        issues={unscheduled}
      />
      <IssueList
        id="unestimated"
        title={t.schedule.resources.unestimated}
        empty={t.schedule.resources.unestimatedEmpty}
        hint={t.schedule.resources.unestimatedHint}
        issues={unestimated}
      />
    </div>
  );
}

function IssueList({ id, title, hint, empty, issues }: { id: string; title: string; hint: string; empty: string; issues: ResourceIssue[] }) {
  const [expanded, setExpanded] = useState(false);
  const listId = useId();
  const folds = issues.length > RESOURCE_LIST_LIMIT;
  const shown = expanded ? issues : issues.slice(0, RESOURCE_LIST_LIMIT);
  return (
    <section data-resource-list={id}>
      <SectionTitle className="mb-2">
        {title} <span className="font-normal text-ink-subtle tabular-nums">{issues.length}</span>
      </SectionTitle>
      <Card className="p-3">
        <p className="mb-2 text-xs text-ink-muted">{issues.length === 0 ? empty : hint}</p>
        {shown.length > 0 && (
          <ul id={listId} className="space-y-1 text-sm">
            {shown.map((issue) => (
              <li key={issue.key} className="flex items-baseline justify-between gap-3" data-resource-issue={issue.key}>
                <Link to="/issues/$issueKey" params={{ issueKey: issue.key }} className="min-w-0 truncate text-ink hover:text-accent">
                  <span className="font-mono text-xs text-ink-muted">{issue.key}</span> {issue.summary}
                </Link>
                {issue.hours !== undefined && (
                  <span className="shrink-0 text-xs text-ink-muted tabular-nums">{t.schedule.resources.hours(hours(issue.hours))}</span>
                )}
              </li>
            ))}
          </ul>
        )}
        {folds && (
          <Button
            variant="ghost"
            size="sm"
            className="mt-2"
            data-action="show-all-unplaced"
            aria-expanded={expanded}
            aria-controls={listId}
            onClick={() => setExpanded(!expanded)}
          >
            {expanded ? t.schedule.resources.showFewer : t.schedule.resources.showAll(issues.length)}
          </Button>
        )}
      </Card>
    </section>
  );
}
