import type { ReactNode } from "react";
import type { Placement, Slot } from "@/api/arrange";
import { PRIORITIES, type Priority } from "@/api/issues";
import { useProjectFields } from "@/api/fields";
import { useMilestones } from "@/api/milestones";
import { useSprints } from "@/api/sprints";
import { useTeams } from "@/api/teams";
import { useComponents, useVersions } from "@/api/versions";
import { Field, Select } from "@/components/ui";
import { DESCRIPTION_ROWS } from "@/config";
import type { Answer, Draft } from "./draft";
import { FieldAnswer, LabelPicks, ManyOf, OneOf, PersonSelect } from "./controls";
import { t } from "@/i18n";

export interface AskProps {
  draft: Draft;
  set: (patch: Partial<Draft>) => void;
  projectKey: string;
  /** The fields the arrangement placed by name, which the catch-all leaves out. */
  named: string[];
}

/**
 * One control per slot, or null for a slot nothing can be said about before
 * the issue exists. Null is why the form is not simply the page's registry.
 */
export const slotAsks: Record<Slot, ((props: AskProps) => ReactNode) | null> = {
  description: ({ draft, set }) => (
    <Field
      label={t.issueList.create.description}
      rows={DESCRIPTION_ROWS}
      value={draft.description}
      onChange={(e) => set({ description: e.target.value })}
      placeholder={t.issueList.create.descriptionPlaceholder}
    />
  ),
  assignee: ({ draft, set }) => <PersonSelect label={t.issueList.create.assignee} value={draft.assigneeId} onPick={(assigneeId) => set({ assigneeId })} />,
  reporter: ({ draft, set }) => (
    <PersonSelect label={t.issueList.create.reporter} value={draft.reporterId} onPick={(reporterId) => set({ reporterId })} customers />
  ),
  parent: ({ draft, set }) => (
    <Field
      label={t.issueList.create.parent}
      value={draft.parentKey}
      onChange={(e) => set({ parentKey: e.target.value })}
      placeholder={t.issueList.create.parentPlaceholder}
    />
  ),
  schedule: ({ draft, set }) => (
    <div className="grid gap-3 sm:grid-cols-2">
      <Field label={t.issueList.create.starts} type="date" value={draft.startDate} onChange={(e) => set({ startDate: e.target.value })} />
      <Field label={t.issueList.create.due} type="date" value={draft.dueDate} onChange={(e) => set({ dueDate: e.target.value })} />
    </div>
  ),
  sprint: (props) => <SprintAsk {...props} />,
  milestone: (props) => <MilestoneAsk {...props} />,
  fixVersions: (props) => <VersionAsk {...props} role="fix" />,
  affectsVersions: (props) => <VersionAsk {...props} role="affects" />,
  components: (props) => <ComponentAsk {...props} />,
  estimate: ({ draft, set }) => (
    <Field label={t.issueList.create.estimate} type="number" step="any" value={draft.estimate} onChange={(e) => set({ estimate: e.target.value })} />
  ),
  team: (props) => <TeamAsk {...props} />,
  // The portal decides what a request was raised as, and a goal is a clock the
  // desk starts; neither is anybody's to answer here.
  request: null,
  goals: null,
  priority: ({ draft, set }) => (
    <Select label={t.issueList.create.priority} value={draft.priority} onChange={(e) => set({ priority: e.target.value as Priority })} className="capitalize">
      <option value="">{t.issueList.create.usualPriority}</option>
      {PRIORITIES.map((priority) => (
        <option key={priority} value={priority}>
          {t.issueList.priority.options[priority]}
        </option>
      ))}
    </Select>
  ),
  labels: (props) => <LabelAsk {...props} />,
  time: ({ draft, set }) => (
    <Field
      label={t.issueList.create.timeEstimate}
      type="number"
      step="any"
      hint={t.issueList.create.inMinutes}
      value={draft.timeEstimate}
      onChange={(e) => set({ timeEstimate: e.target.value })}
    />
  ),
  otherFields: (props) => <OtherFieldsAsk {...props} />,
  // Both are facts about an issue that exists.
  created: null,
  resolved: null,
};

/** One place asked about: a field of the project's own, or a slot this tracker knows. */
export function renderAsk(place: Placement, props: AskProps): ReactNode {
  if (place.fieldId) return <OneFieldAsk {...props} fieldId={place.fieldId} />;
  const ask = place.slot ? slotAsks[place.slot] : undefined;
  return ask ? ask(props) : null;
}

function answered(draft: Draft, fieldId: string): Answer | undefined {
  return draft.values[fieldId];
}

function keep(props: AskProps, fieldId: string, answer: Answer) {
  props.set({ values: { ...props.draft.values, [fieldId]: answer } });
}

function SprintAsk({ draft, set, projectKey }: AskProps) {
  const { data } = useSprints(projectKey);
  const open = (data?.sprints ?? []).filter((sprint) => sprint.state !== "closed");
  return (
    <OneOf label={t.issueList.create.sprint} options={open} value={draft.sprintId} onPick={(sprintId) => set({ sprintId })} none={t.issueList.create.backlog} />
  );
}

function MilestoneAsk({ draft, set, projectKey }: AskProps) {
  const { data } = useMilestones(projectKey);
  return (
    <OneOf label={t.issueList.create.milestone} options={data?.milestones ?? []} value={draft.milestoneId} onPick={(milestoneId) => set({ milestoneId })} />
  );
}

function TeamAsk({ draft, set, projectKey }: AskProps) {
  const { data } = useTeams(projectKey);
  return (
    <OneOf
      label={t.issueList.create.team}
      options={data?.teams ?? []}
      value={draft.teamId}
      onPick={(teamId) => set({ teamId })}
      none={t.issueList.create.wholeProject}
    />
  );
}

function VersionAsk({ draft, set, projectKey, role }: AskProps & { role: "fix" | "affects" }) {
  const { data } = useVersions(projectKey);
  const open = (data?.versions ?? []).filter((version) => !version.archivedAt);
  if (role === "affects") {
    return (
      <ManyOf
        label={t.issueList.create.affectsVersions}
        options={open}
        chosen={draft.affectsVersionIds}
        onChange={(affectsVersionIds) => set({ affectsVersionIds })}
      />
    );
  }
  return <ManyOf label={t.issueList.create.fixVersions} options={open} chosen={draft.fixVersionIds} onChange={(fixVersionIds) => set({ fixVersionIds })} />;
}

function ComponentAsk({ draft, set, projectKey }: AskProps) {
  const { data } = useComponents(projectKey);
  return (
    <ManyOf
      label={t.issueList.create.components}
      options={data?.components ?? []}
      chosen={draft.componentIds}
      onChange={(componentIds) => set({ componentIds })}
    />
  );
}

function LabelAsk({ draft, set }: AskProps) {
  return <LabelPicks chosen={draft.labels} onChange={(labels) => set({ labels })} />;
}

function OneFieldAsk(props: AskProps & { fieldId: string }) {
  const { data } = useProjectFields(props.projectKey);
  const field = (data?.fields ?? []).find((each) => each.id === props.fieldId);
  if (!field) return null;
  return <FieldAnswer field={field} answer={answered(props.draft, field.id)} onAnswer={(answer) => keep(props, field.id, answer)} />;
}

/** Every field of the project's the arrangement did not place by name. */
function OtherFieldsAsk(props: AskProps) {
  const { data } = useProjectFields(props.projectKey);
  const rest = (data?.fields ?? []).filter((field) => !props.named.includes(field.id));
  return (
    <>
      {rest.map((field) => (
        <FieldAnswer key={field.id} field={field} answer={answered(props.draft, field.id)} onAnswer={(answer) => keep(props, field.id, answer)} />
      ))}
    </>
  );
}
