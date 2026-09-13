import { useMembers, useUpdateIssue, type Issue } from "@/api/issues";
import { SelectInput } from "@/components/ui";
import { Avatar } from "./badges";

/**
 * Who has the issue and who raised it. Both are a member of the organization
 * chosen from the list; the assignee may also be nobody.
 */
export function AssigneeField({ issue, editable }: { issue: Issue; editable: boolean }) {
  const { data } = useMembers();
  const update = useUpdateIssue();
  const people = (data?.members ?? []).filter((m) => m.role !== "customer");

  return (
    <span className="flex items-center justify-end gap-2">
      <Avatar name={issue.assignee?.name} src={issue.assignee?.avatarUrl} size="sm" />
      {editable ? (
        <SelectInput aria-label="Assignee" controlSize="sm" value={issue.assignee?.id ?? ""} onChange={(e) => update.mutate({ key: issue.key, assigneeId: e.target.value || null })} className="max-w-40 min-w-0 truncate">
          <option value="">Unassigned</option>
          {people.map((m) => (
            <option key={m.id} value={m.id}>
              {m.name}
            </option>
          ))}
        </SelectInput>
      ) : (
        <span className="text-ink">{issue.assignee?.name ?? "Unassigned"}</span>
      )}
    </span>
  );
}

export function ReporterField({ issue, editable }: { issue: Issue; editable: boolean }) {
  const { data } = useMembers();
  const update = useUpdateIssue();
  const people = data?.members ?? [];

  return (
    <span className="flex items-center justify-end gap-2">
      <Avatar name={issue.reporter?.name} src={issue.reporter?.avatarUrl} size="sm" />
      {editable ? (
        <SelectInput aria-label="Reporter" controlSize="sm" value={issue.reporter?.id ?? ""} onChange={(e) => e.target.value && update.mutate({ key: issue.key, reporterId: e.target.value })} className="max-w-40 min-w-0 truncate">
          {!issue.reporter && <option value="">Unknown</option>}
          {people.map((m) => (
            <option key={m.id} value={m.id}>
              {m.name}
            </option>
          ))}
        </SelectInput>
      ) : (
        <span className="text-ink">{issue.reporter?.name ?? "Unknown"}</span>
      )}
    </span>
  );
}
