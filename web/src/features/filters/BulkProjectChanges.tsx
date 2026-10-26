import { useState } from "react";
import type { BulkChange, BulkSetEdit } from "@/api/filters";
import type { Field as CustomField } from "@/api/fields";
import { useProjectFields } from "@/api/fields";
import { useMilestones } from "@/api/milestones";
import { useSprints } from "@/api/sprints";
import { useTeams } from "@/api/teams";
import { useComponents, useVersions } from "@/api/versions";
import { Checkbox, Input, Labelled, Select } from "@/components/ui";
import { inputTypeFor, valueFromInput } from "@/features/fields/values";
import { NamePicker } from "@/features/versions/IssueVersionFields";
import { t } from "@/i18n";

type Named = { id: string; name: string };

// "" leaves the value alone, "-" clears it, anything else is an id.
const LEAVE = "";
const CLEAR = "-";

/** A sprint, milestone or team for every selected issue: leave it, clear it, or pick one. */
function OneOf({
  label,
  id,
  none,
  value,
  options,
  onChange,
}: {
  label: string;
  id: string;
  none: string;
  value: string | null | undefined;
  options: Named[];
  onChange: (value: string | null | undefined) => void;
}) {
  return (
    <Select
      label={label}
      id={id}
      controlSize="sm"
      value={value === undefined ? LEAVE : value === null ? CLEAR : value}
      onChange={(e) => onChange(e.target.value === LEAVE ? undefined : e.target.value === CLEAR ? null : e.target.value)}
    >
      <option value={LEAVE}>{t.search.bulk.leave}</option>
      <option value={CLEAR}>{none}</option>
      {options.map((o) => (
        <option key={o.id} value={o.id}>
          {o.name}
        </option>
      ))}
    </Select>
  );
}

/** Two pickers over one set: what to add to every issue, and what to take away. */
function AddAndRemove({
  id,
  addLabel,
  removeLabel,
  options,
  value,
  onChange,
}: {
  id: string;
  addLabel: string;
  removeLabel: string;
  options: Named[];
  value: BulkSetEdit | undefined;
  onChange: (value: BulkSetEdit | undefined) => void;
}) {
  const add = value?.add ?? [];
  const remove = value?.remove ?? [];
  // A member is added or removed, never both: picking it on one side takes it off the other.
  const set = (nextAdd: string[], nextRemove: string[]) =>
    onChange(
      nextAdd.length + nextRemove.length === 0 ? undefined : { add: nextAdd.length ? nextAdd : undefined, remove: nextRemove.length ? nextRemove : undefined },
    );
  const chosen = (ids: string[]) => options.filter((o) => ids.includes(o.id));
  return (
    <>
      <Labelled id={`${id}-add`} label={addLabel}>
        <NamePicker
          id={`${id}-add`}
          label={addLabel}
          chosen={chosen(add)}
          options={options}
          editable
          emptyWord={t.search.bulk.noneChosen}
          onChange={(ids) =>
            set(
              ids,
              remove.filter((r) => !ids.includes(r)),
            )
          }
        />
      </Labelled>
      <Labelled id={`${id}-remove`} label={removeLabel}>
        <NamePicker
          id={`${id}-remove`}
          label={removeLabel}
          chosen={chosen(remove)}
          options={options}
          editable
          emptyWord={t.search.bulk.noneChosen}
          onChange={(ids) =>
            set(
              add.filter((a) => !ids.includes(a)),
              ids,
            )
          }
        />
      </Labelled>
    </>
  );
}

/** One custom field to answer on every issue, or to clear on every issue. */
function FieldChange({ fields, value, onChange }: { fields: CustomField[]; value: BulkChange["fields"]; onChange: (value: BulkChange["fields"]) => void }) {
  const [fieldID, setFieldID] = useState(value ? (Object.keys(value)[0] ?? "") : "");
  const [raw, setRaw] = useState("");
  const [clearing, setClearing] = useState(false);
  const field = fields.find((f) => f.id === fieldID);

  // An empty box asks for nothing; only "Clear it" clears, so a chosen field
  // with nothing typed never empties eighty issues by accident.
  function set(nextID: string, nextRaw: string, nextClearing: boolean) {
    setFieldID(nextID);
    setRaw(nextRaw);
    setClearing(nextClearing);
    const chosen = fields.find((f) => f.id === nextID);
    if (!chosen) return onChange(undefined);
    if (nextClearing) return onChange({ [chosen.id]: null });
    if (nextRaw.trim() === "") return onChange(undefined);
    onChange({ [chosen.id]: valueFromInput(chosen.kind, nextRaw) });
  }

  return (
    <>
      <Select label={t.search.bulk.field} id="field-bulk-field" controlSize="sm" value={fieldID} onChange={(e) => set(e.target.value, "", false)}>
        <option value="">{t.search.bulk.leave}</option>
        {fields.map((f) => (
          <option key={f.id} value={f.id}>
            {f.name}
          </option>
        ))}
      </Select>
      {field && (
        <>
          {field.kind === "select" || field.kind === "checkbox" ? (
            <Select
              label={t.search.bulk.fieldValue(field.name)}
              id="field-bulk-field-value"
              controlSize="sm"
              value={raw}
              disabled={clearing}
              onChange={(e) => set(field.id, e.target.value, false)}
            >
              <option value="">{t.search.bulk.leave}</option>
              {(field.kind === "checkbox" ? ["true", "false"] : field.options).map((option) => (
                <option key={option} value={option}>
                  {field.kind === "checkbox" ? (option === "true" ? t.search.bulk.ticked : t.search.bulk.notTicked) : option}
                </option>
              ))}
            </Select>
          ) : (
            <Labelled id="field-bulk-field-value" label={t.search.bulk.fieldValue(field.name)}>
              <Input
                id="field-bulk-field-value"
                type={inputTypeFor(field.kind)}
                controlSize="sm"
                value={raw}
                disabled={clearing}
                onChange={(e) => set(field.id, e.target.value, false)}
              />
            </Labelled>
          )}
          <Checkbox label={t.search.bulk.clearField} checked={clearing} onChange={(e) => set(field.id, "", e.target.checked)} data-action="bulk-clear-field" />
        </>
      )}
    </>
  );
}

/**
 * What belongs to one project: its sprints, milestones, teams, versions,
 * components and fields. Offered only when every selected issue is in it.
 */
export function BulkProjectChanges({ projectKey, change, onChange }: { projectKey: string; change: BulkChange; onChange: (change: BulkChange) => void }) {
  const { data: sprints } = useSprints(projectKey);
  const { data: milestones } = useMilestones(projectKey);
  const { data: teams } = useTeams(projectKey);
  const { data: versions } = useVersions(projectKey);
  const { data: components } = useComponents(projectKey);
  const { data: fields } = useProjectFields(projectKey);
  // A sprint takes only its own team's work, so a chosen team narrows the sprints.
  const sprintOptions = (sprints?.sprints ?? []).filter((s) => typeof change.teamId !== "string" || s.teamId === change.teamId);

  return (
    <>
      <OneOf
        label={t.search.bulk.team}
        id="field-bulk-team"
        none={t.search.bulk.noTeam}
        value={change.teamId}
        options={teams?.teams ?? []}
        onChange={(teamId) => onChange({ ...change, teamId })}
      />
      <OneOf
        label={t.search.bulk.sprint}
        id="field-bulk-sprint"
        none={t.search.bulk.noSprint}
        value={change.sprintId}
        options={sprintOptions}
        onChange={(sprintId) => onChange({ ...change, sprintId })}
      />
      <OneOf
        label={t.search.bulk.milestone}
        id="field-bulk-milestone"
        none={t.search.bulk.noMilestone}
        value={change.milestoneId}
        options={milestones?.milestones ?? []}
        onChange={(milestoneId) => onChange({ ...change, milestoneId })}
      />
      <AddAndRemove
        id="field-bulk-fix-versions"
        addLabel={t.search.bulk.addFixVersions}
        removeLabel={t.search.bulk.removeFixVersions}
        options={versions?.versions ?? []}
        value={change.fixVersions}
        onChange={(fixVersions) => onChange({ ...change, fixVersions })}
      />
      <AddAndRemove
        id="field-bulk-components"
        addLabel={t.search.bulk.addComponents}
        removeLabel={t.search.bulk.removeComponents}
        options={components?.components ?? []}
        value={change.components}
        onChange={(next) => onChange({ ...change, components: next })}
      />
      {(fields?.fields ?? []).length > 0 && (
        <FieldChange fields={fields?.fields ?? []} value={change.fields} onChange={(next) => onChange({ ...change, fields: next })} />
      )}
    </>
  );
}
