import type { MouseEvent } from "react";
import { Link } from "@tanstack/react-router";
import type { Issue } from "@/api/issues";
import { Checkbox, IconButton, Table, Td, Th, cx, isInteractiveTarget } from "@/components/ui";
import { Icon } from "@/components/icons";
import { useIssueDrawer, useIssueList } from "./IssueDrawer";
import { Avatar, PriorityBadge, StatusBadge, TypeBadge, RelativeTime } from "./badges";
import { LabelChips } from "@/features/labels/LabelChip";
import { t } from "@/i18n";

/**
 * Issues as rows. The columns are the things somebody scans a list for, in the
 * order they scan them: what it is, which one, what it says, then its state.
 */
export function IssueTable({
  issues,
  showProject = false,
  selected,
  onSelect,
}: {
  issues: Issue[];
  showProject?: boolean;
  /** When given, each row has a box and the header box ticks them all. */
  selected?: Set<string>;
  onSelect?: (keys: Set<string>) => void;
}) {
  const drawer = useIssueDrawer();
  const selecting = selected !== undefined && onSelect !== undefined;
  const allTicked = selecting && issues.length > 0 && issues.every((i) => selected.has(i.key));
  function toggle(key: string, on: boolean) {
    if (!selecting) return;
    const next = new Set(selected);
    if (on) next.add(key);
    else next.delete(key);
    onSelect(next);
  }
  useIssueList(issues.map((issue) => issue.key));
  // A click on the row opens the issue beside the list; a link or a button in
  // the row still does what it says.
  function pick(event: MouseEvent<HTMLTableRowElement>, key: string) {
    if (isInteractiveTarget(event.target)) return;
    drawer.open(key);
  }
  return (
    <Table data-issue-list="">
      <thead>
        <tr>
          {selecting && (
            <Th className="w-8">
              <Checkbox
                label={<span className="sr-only">{t.issueList.table.selectAll}</span>}
                checked={allTicked}
                onChange={(e) => onSelect(e.target.checked ? new Set(issues.map((i) => i.key)) : new Set())}
                data-select-all
              />
            </Th>
          )}
          <Th className="w-8" aria-label={t.issueList.table.type} />
          <Th className="w-24">{t.issueList.table.key}</Th>
          <Th>{t.issueList.table.summary}</Th>
          {showProject && <Th className="w-28">{t.issueList.table.project}</Th>}
          <Th className="w-16" aria-label={t.issueList.table.priority} />
          <Th className="w-36">{t.issueList.table.status}</Th>
          <Th className="w-10" aria-label={t.issueList.table.assignee} />
          <Th className="w-24 text-right">{t.issueList.table.updated}</Th>
          <Th className="w-10" aria-label={t.issueList.table.openBeside} />
        </tr>
      </thead>
      <tbody>
        {issues.map((issue) => (
          <tr
            key={issue.id}
            className={cx("group cursor-pointer", drawer.current === issue.key ? "bg-accent-subtle/60" : "hover:bg-surface-raised/60")}
            data-issue-row={issue.key}
            data-selected={drawer.current === issue.key ? "" : undefined}
            aria-selected={drawer.current === issue.key}
            onClick={(event) => pick(event, issue.key)}
          >
            {selecting && (
              <Td>
                <Checkbox
                  label={<span className="sr-only">{t.issueList.table.select(issue.key)}</span>}
                  checked={selected.has(issue.key)}
                  onChange={(e) => toggle(issue.key, e.target.checked)}
                  data-select-issue={issue.key}
                />
              </Td>
            )}
            <Td>
              <TypeBadge icon={issue.type.icon} name={issue.type.name} />
            </Td>
            <Td className="font-mono text-sm text-ink-muted">{issue.key}</Td>
            <Td className="max-w-0">
              <Link to="/issues/$issueKey" params={{ issueKey: issue.key }} className="block min-w-48 truncate text-ink group-hover:text-accent">
                {issue.summary}
              </Link>
              <LabelChips labels={issue.labels ?? []} className="mt-0.5" />
            </Td>
            {showProject && (
              <Td>
                <Link to="/projects/$projectKey" params={{ projectKey: issue.projectKey }} className="font-mono text-sm text-ink-muted hover:text-ink">
                  {issue.projectKey}
                </Link>
              </Td>
            )}
            <Td>
              <PriorityBadge priority={issue.priority} />
            </Td>
            <Td>
              <StatusBadge name={issue.status.name} category={issue.status.category} />
            </Td>
            <Td>
              <Avatar name={issue.assignee?.name} src={issue.assignee?.avatarUrl} size="sm" />
            </Td>
            <Td className="text-right text-sm whitespace-nowrap text-ink-subtle tabular-nums">
              <RelativeTime iso={issue.updatedAt} />
            </Td>
            <Td className="text-right">
              <IconButton
                icon={<Icon.ChevronRight />}
                label={t.issueList.table.openKeyBeside(issue.key)}
                size="sm"
                onClick={() => drawer.open(issue.key)}
                data-open-drawer={issue.key}
              />
            </Td>
          </tr>
        ))}
      </tbody>
    </Table>
  );
}
