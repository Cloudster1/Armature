import { useUpdateIssue, type Issue } from "@/api/issues";
import { PersonPicker } from "@/features/people/PersonPicker";
import { Avatar } from "./badges";
import { t } from "@/i18n";

/**
 * Who has the issue and who raised it, each found by name among everyone
 * here; the assignee may also be nobody, and is never a customer.
 */
export function AssigneeField({ issue, editable }: { issue: Issue; editable: boolean }) {
  const update = useUpdateIssue();

  return (
    <span className="flex items-center justify-end gap-2">
      <Avatar name={issue.assignee?.name} src={issue.assignee?.avatarUrl} size="sm" />
      {editable ? (
        <PersonPicker
          aria-label={t.issueView.details.assignee}
          controlSize="sm"
          staff
          none={t.issueView.details.unassigned}
          value={issue.assignee?.id ?? ""}
          selected={issue.assignee}
          onChange={(id) => update.mutate({ key: issue.key, assigneeId: id || null })}
          className="max-w-40 min-w-0"
          selectClassName="truncate"
        />
      ) : (
        <span className="text-ink">{issue.assignee?.name ?? t.issueView.details.unassigned}</span>
      )}
    </span>
  );
}

export function ReporterField({ issue, editable }: { issue: Issue; editable: boolean }) {
  const update = useUpdateIssue();

  return (
    <span className="flex items-center justify-end gap-2">
      <Avatar name={issue.reporter?.name} src={issue.reporter?.avatarUrl} size="sm" />
      {editable ? (
        <PersonPicker
          aria-label={t.issueView.details.reporter}
          controlSize="sm"
          none={issue.reporter ? undefined : t.issueView.details.unknown}
          value={issue.reporter?.id ?? ""}
          selected={issue.reporter}
          onChange={(id) => id && update.mutate({ key: issue.key, reporterId: id })}
          className="max-w-40 min-w-0"
          selectClassName="truncate"
        />
      ) : (
        <span className="text-ink">{issue.reporter?.name ?? t.issueView.details.unknown}</span>
      )}
    </span>
  );
}
