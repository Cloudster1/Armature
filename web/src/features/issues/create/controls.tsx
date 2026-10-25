import { useState } from "react";
import type { Field } from "@/api/fields";
import { Checkbox, Chip, Input, Labelled, Select, fieldId } from "@/components/ui";
import { LabelInput } from "@/features/labels/LabelInput";
import { PersonPicker } from "@/features/people/PersonPicker";
import { inputTypeFor, valueFromInput } from "@/features/fields/values";
import type { Answer } from "./draft";
import { t } from "@/i18n";

/** Anything with a name the form can offer by id. */
export interface Named {
  id: string;
  name: string;
}

/** One of the people here, found by name, or nobody; a reporter may be a customer. */
export function PersonSelect({ label, value, onPick, customers = false }: { label: string; value: string; onPick: (id: string) => void; customers?: boolean }) {
  return <PersonPicker label={label} value={value} onChange={onPick} none={t.issueList.create.nobody} staff={!customers} />;
}

/** One of a list, or none of it. */
export function OneOf({
  label,
  options,
  value,
  onPick,
  none,
}: {
  label: string;
  options: Named[];
  value: string;
  onPick: (id: string) => void;
  none?: string;
}) {
  return (
    <Select label={label} value={value} onChange={(e) => onPick(e.target.value)}>
      <option value="">{none ?? t.issueList.create.none}</option>
      {options.map((option) => (
        <option key={option.id} value={option.id}>
          {option.name}
        </option>
      ))}
    </Select>
  );
}

// Chips rather than a multiple select: a multiple select cannot be worked from
// the keyboard without telling somebody to hold a key down.
export function ManyOf({ label, options, chosen, onChange }: { label: string; options: Named[]; chosen: string[]; onChange: (ids: string[]) => void }) {
  if (options.length === 0) return null;
  const id = fieldId(label);
  return (
    <div className="space-y-1">
      <span id={`${id}-label`} className="block text-sm font-medium text-ink-muted">
        {label}
      </span>
      <div role="group" aria-labelledby={`${id}-label`} className="flex flex-wrap gap-1.5" data-pick-many={label}>
        {options.map((option) => (
          <Chip
            key={option.id}
            pressed={chosen.includes(option.id)}
            onClick={() => onChange(chosen.includes(option.id) ? chosen.filter((each) => each !== option.id) : [...chosen, option.id])}
          >
            {option.name}
          </Chip>
        ))}
      </div>
    </div>
  );
}

/** Words rather than ids: a label nobody has used yet is coined here. */
export function LabelPicks({ chosen, onChange }: { chosen: string[]; onChange: (names: string[]) => void }) {
  return (
    <Labelled id="create-labels" label={t.issueList.create.labels}>
      <LabelInput id="create-labels" placeholder={t.issueList.create.labelPlaceholder} value={chosen} onChange={onChange} />
    </Labelled>
  );
}

/** One of the project's own fields, drawn as the control its kind asks for. */
export function FieldAnswer({ field, answer, onAnswer }: { field: Field; answer: Answer | undefined; onAnswer: (answer: Answer) => void }) {
  const [text, setText] = useState("");
  const keep = (raw: string | boolean) => onAnswer({ name: field.name, value: valueFromInput(field.kind, raw) });

  if (field.kind === "select") {
    return (
      <Select label={field.name} value={typeof answer?.value === "string" ? answer.value : ""} onChange={(e) => keep(e.target.value)}>
        <option value="">{t.issueList.create.none}</option>
        {field.options.map((option) => (
          <option key={option} value={option}>
            {option}
          </option>
        ))}
      </Select>
    );
  }
  if (field.kind === "checkbox") {
    return <Checkbox label={field.name} checked={answer?.value === true} onChange={(e) => keep(e.target.checked)} />;
  }
  return (
    <Labelled id={fieldId(field.name)} label={field.name}>
      <Input
        id={fieldId(field.name)}
        type={inputTypeFor(field.kind)}
        value={text}
        step={field.kind === "number" ? "any" : undefined}
        placeholder={field.kind === "url" ? "https://" : undefined}
        onChange={(e) => {
          setText(e.target.value);
          keep(e.target.value);
        }}
      />
    </Labelled>
  );
}
