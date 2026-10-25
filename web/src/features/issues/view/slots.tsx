import type { ReactNode } from "react";
import type { Placement, Slot } from "@/api/arrange";
import { PRIORITIES, type Issue, type Priority, useUpdateIssue } from "@/api/issues";
import { useTimers } from "@/api/desk";
import { SelectInput } from "@/components/ui";
import { PriorityBadge, RelativeTime } from "../badges";
import { ParentPicker } from "../hierarchy";
import { AssigneeField, ReporterField } from "../PeopleFields";
import { ScheduleField } from "@/features/plan/ScheduleField";
import { EstimateField, SprintField } from "@/features/sprints/SprintFields";
import { MilestoneField } from "@/features/milestones/MilestoneField";
import { ComponentField, VersionField } from "@/features/versions/IssueVersionFields";
import { TeamField } from "@/features/teams/TeamFields";
import { SlaBadge } from "@/features/desk/SlaBadge";
import { companyLine } from "@/features/desk/company";
import { IssueField, IssueFields } from "@/features/fields/IssueFields";
import { LabelPicker } from "@/features/labels/LabelPicker";
import { TimeSummary } from "@/features/time/TimeTracking";
import { Detail } from "./Detail";
import { t } from "@/i18n";

export interface SlotProps {
  issue: Issue;
  editable: boolean;
  /** The project's fields the arrangement placed by name, which the catch-all leaves out. */
  named: string[];
}

// One adapter per slot, each drawing its own row: a fact with nothing to say
// draws no row at all, which it could not decide from outside.
export const slotViews: Record<Slot, (props: SlotProps) => ReactNode> = {
  // The description is a section of the left column, drawn there by name.
  description: () => null,
  assignee: ({ issue, editable }) => (
    <Detail label={t.issueView.details.assignee}>
      <AssigneeField issue={issue} editable={editable} />
    </Detail>
  ),
  reporter: ({ issue, editable }) => (
    <Detail label={t.issueView.details.reporter}>
      <ReporterField issue={issue} editable={editable} />
    </Detail>
  ),
  parent: ({ issue }) => (
    <Detail label={t.issueView.details.parent} wide>
      <ParentPicker issue={issue} />
    </Detail>
  ),
  schedule: ({ issue }) => (
    <Detail label={t.issueView.details.scheduled} wide>
      <ScheduleField issue={issue} />
    </Detail>
  ),
  sprint: ({ issue }) => (
    <Detail label={t.issueView.details.sprint}>
      <SprintField issue={issue} />
    </Detail>
  ),
  milestone: ({ issue }) => (
    <Detail label={t.issueView.details.milestone}>
      <MilestoneField issue={issue} />
    </Detail>
  ),
  fixVersions: ({ issue, editable }) => (
    <Detail label={t.issueView.details.fixVersions} wide>
      <VersionField issue={issue} editable={editable} role="fix" />
    </Detail>
  ),
  affectsVersions: ({ issue, editable }) => (
    <Detail label={t.issueView.details.affectsVersions} wide>
      <VersionField issue={issue} editable={editable} role="affects" />
    </Detail>
  ),
  components: ({ issue, editable }) => (
    <Detail label={t.issueView.details.components} wide>
      <ComponentField issue={issue} editable={editable} />
    </Detail>
  ),
  estimate: ({ issue }) => (
    <Detail label={t.issueView.details.estimate}>
      <EstimateField issue={issue} />
    </Detail>
  ),
  team: ({ issue }) => (
    <Detail label={t.issueView.details.team}>
      <TeamField issue={issue} />
    </Detail>
  ),
  request: ({ issue }) =>
    issue.requestTypeName || issue.company ? (
      <Detail label={t.issueView.details.request}>
        <span className="text-ink">{issue.requestTypeName}</span>
        {issue.company && (
          <span className="block text-ink-muted" data-request-company={issue.company.name}>
            {companyLine(issue.company.name, Boolean(issue.sharedWithCompany))}
          </span>
        )}
      </Detail>
    ) : null,
  goals: ({ issue }) => <SlaDetails issueKey={issue.key} />,
  priority: ({ issue }) => (
    <Detail label={t.issueView.details.priority}>
      <PriorityPicker issueKey={issue.key} current={issue.priority} />
    </Detail>
  ),
  labels: ({ issue, editable }) => (
    <Detail label={t.issueView.details.labels} wide>
      <LabelPicker issue={issue} editable={editable} />
    </Detail>
  ),
  time: ({ issue, editable }) => (
    <Detail label={t.issueView.details.time} wide>
      <TimeSummary issue={issue} editable={editable} />
    </Detail>
  ),
  otherFields: ({ issue, editable, named }) => <IssueFields issueKey={issue.key} editable={editable} except={named} />,
  created: ({ issue }) => (
    <Detail label={t.issueView.details.created}>
      <span className="text-ink-muted">
        <RelativeTime iso={issue.createdAt} />
      </span>
    </Detail>
  ),
  resolved: ({ issue }) =>
    issue.resolvedAt ? (
      <Detail label={t.issueView.details.resolved}>
        <span className="text-ink-muted">
          <RelativeTime iso={issue.resolvedAt} />
        </span>
      </Detail>
    ) : null,
};

/** One place drawn: a field of the project's own, or a slot this tracker knows. */
export function renderPlace(place: Placement, props: SlotProps): ReactNode {
  if (place.fieldId) return <IssueField issueKey={props.issue.key} fieldId={place.fieldId} editable={props.editable} />;
  const view = place.slot ? slotViews[place.slot] : undefined;
  return view ? view(props) : null;
}

function SlaDetails({ issueKey }: { issueKey: string }) {
  const { data } = useTimers(issueKey);
  const timers = data?.timers ?? [];
  if (timers.length === 0) return null;
  return (
    <Detail label={t.issueView.details.goals} wide>
      <span className="flex flex-col items-end gap-1" data-testid="sla">
        {timers.map((t) => (
          <SlaBadge key={t.id} timer={t} />
        ))}
      </span>
    </Detail>
  );
}

function PriorityPicker({ issueKey, current }: { issueKey: string; current: Priority }) {
  const update = useUpdateIssue();
  return (
    <span className="flex items-center justify-end gap-2">
      <PriorityBadge priority={current} />
      <SelectInput
        value={current}
        aria-label={t.issueView.details.priority}
        controlSize="sm"
        onChange={(e) => update.mutate({ key: issueKey, priority: e.target.value as Priority })}
        className="capitalize"
      >
        {PRIORITIES.map((p) => (
          <option key={p} value={p}>
            {p}
          </option>
        ))}
      </SelectInput>
    </span>
  );
}
