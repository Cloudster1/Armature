import type { ReactNode } from "react";
import type { Placement, Slot } from "@/api/arrange";
import { PRIORITIES, type Priority } from "@/api/issues";
import { useProjectFields } from "@/api/fields";
import { useLabels } from "@/api/labels";
import { useMilestones } from "@/api/milestones";
import { useSprints } from "@/api/sprints";
import { useTeams } from "@/api/teams";
import { useComponents, useVersions } from "@/api/versions";
import { Field, Select } from "@/components/ui";
import { DESCRIPTION_ROWS } from "@/config";
import type { Answer, Draft } from "./draft";
import { FieldAnswer, LabelPicks, ManyOf, OneOf, PersonSelect } from "./controls";

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
  description: ({ draft, set }) => <Field label="Description" rows={DESCRIPTION_ROWS} value={draft.description} onChange={(e) => set({ description: e.target.value })} placeholder="Why it matters and what done looks like" />,
  assignee: ({ draft, set }) => <PersonSelect label="Assignee" value={draft.assigneeId} onPick={(assigneeId) => set({ assigneeId })} />,
  reporter: ({ draft, set }) => <PersonSelect label="Reporter" value={draft.reporterId} onPick={(reporterId) => set({ reporterId })} customers />,
  parent: ({ draft, set }) => <Field label="Parent" value={draft.parentKey} onChange={(e) => set({ parentKey: e.target.value })} placeholder="The key of the issue above it" />,
  schedule: ({ draft, set }) => (
    <div className="grid gap-3 sm:grid-cols-2">
      <Field label="Starts" type="date" value={draft.startDate} onChange={(e) => set({ startDate: e.target.value })} />
      <Field label="Due" type="date" value={draft.dueDate} onChange={(e) => set({ dueDate: e.target.value })} />
    </div>
  ),
  sprint: (props) => <SprintAsk {...props} />,
  milestone: (props) => <MilestoneAsk {...props} />,
  fixVersions: (props) => <VersionAsk {...props} role="fix" />,
  affectsVersions: (props) => <VersionAsk {...props} role="affects" />,
  components: (props) => <ComponentAsk {...props} />,
  estimate: ({ draft, set }) => <Field label="Estimate" type="number" step="any" value={draft.estimate} onChange={(e) => set({ estimate: e.target.value })} />,
  team: (props) => <TeamAsk {...props} />,
  // The portal decides what a request was raised as, and a goal is a clock the
  // desk starts; neither is anybody's to answer here.
  request: null,
  goals: null,
  priority: ({ draft, set }) => (
    <Select label="Priority" value={draft.priority} onChange={(e) => set({ priority: e.target.value as Priority })} className="capitalize">
      <option value="">The project's usual</option>
      {PRIORITIES.map((priority) => (
        <option key={priority} value={priority}>
          {priority}
        </option>
      ))}
    </Select>
  ),
  labels: (props) => <LabelAsk {...props} />,
  time: ({ draft, set }) => <Field label="Time estimate" type="number" step="any" hint="In minutes." value={draft.timeEstimate} onChange={(e) => set({ timeEstimate: e.target.value })} />,
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
  return <OneOf label="Sprint" options={open} value={draft.sprintId} onPick={(sprintId) => set({ sprintId })} none="The backlog" />;
}

function MilestoneAsk({ draft, set, projectKey }: AskProps) {
  const { data } = useMilestones(projectKey);
  return <OneOf label="Milestone" options={data?.milestones ?? []} value={draft.milestoneId} onPick={(milestoneId) => set({ milestoneId })} />;
}

function TeamAsk({ draft, set, projectKey }: AskProps) {
  const { data } = useTeams(projectKey);
  return <OneOf label="Team" options={data?.teams ?? []} value={draft.teamId} onPick={(teamId) => set({ teamId })} none="The project at large" />;
}

function VersionAsk({ draft, set, projectKey, role }: AskProps & { role: "fix" | "affects" }) {
  const { data } = useVersions(projectKey);
  const open = (data?.versions ?? []).filter((version) => !version.archivedAt);
  if (role === "affects") {
    return <ManyOf label="Affects versions" options={open} chosen={draft.affectsVersionIds} onChange={(affectsVersionIds) => set({ affectsVersionIds })} />;
  }
  return <ManyOf label="Fix versions" options={open} chosen={draft.fixVersionIds} onChange={(fixVersionIds) => set({ fixVersionIds })} />;
}

function ComponentAsk({ draft, set, projectKey }: AskProps) {
  const { data } = useComponents(projectKey);
  return <ManyOf label="Components" options={data?.components ?? []} chosen={draft.componentIds} onChange={(componentIds) => set({ componentIds })} />;
}

function LabelAsk({ draft, set }: AskProps) {
  const { data } = useLabels();
  return <LabelPicks chosen={draft.labels} known={(data?.labels ?? []).map((label) => label.name)} onChange={(labels) => set({ labels })} />;
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
