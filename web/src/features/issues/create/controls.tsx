import { useState } from "react";
import type { Field } from "@/api/fields";
import { useMembers } from "@/api/issues";
import { Checkbox, Chip, Input, Labelled, Select, fieldId } from "@/components/ui";
import { inputTypeFor, valueFromInput } from "@/features/fields/values";
import type { Answer } from "./draft";

/** Anything with a name the form can offer by id. */
export interface Named {
  id: string;
  name: string;
}

/** One of the organization's people, or nobody. */
export function PersonSelect({ label, value, onPick, customers = false }: { label: string; value: string; onPick: (id: string) => void; customers?: boolean }) {
  const { data } = useMembers();
  const people = (data?.members ?? []).filter((member) => customers || member.role !== "customer");
  return (
    <Select label={label} value={value} onChange={(e) => onPick(e.target.value)}>
      <option value="">Nobody</option>
      {people.map((member) => (
        <option key={member.id} value={member.id}>
          {member.name}
        </option>
      ))}
    </Select>
  );
}

/** One of a list, or none of it. */
export function OneOf({ label, options, value, onPick, none = "None" }: { label: string; options: Named[]; value: string; onPick: (id: string) => void; none?: string }) {
  return (
    <Select label={label} value={value} onChange={(e) => onPick(e.target.value)}>
      <option value="">{none}</option>
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
          <Chip key={option.id} pressed={chosen.includes(option.id)} onClick={() => onChange(chosen.includes(option.id) ? chosen.filter((each) => each !== option.id) : [...chosen, option.id])}>
            {option.name}
          </Chip>
        ))}
      </div>
    </div>
  );
}

/** Words rather than ids: a label the organization does not have yet is coined here. */
export function LabelPicks({ chosen, known, onChange }: { chosen: string[]; known: string[]; onChange: (names: string[]) => void }) {
  const [typed, setTyped] = useState("");
  function add(name: string) {
    const word = name.trim();
    if (word === "" || chosen.includes(word)) return;
    onChange([...chosen, word]);
    setTyped("");
  }
  return (
    <Labelled id="create-labels" label="Labels">
      <div className="space-y-1.5">
        <Input
          id="create-labels"
          value={typed}
          placeholder="A word, then Enter"
          onChange={(e) => setTyped(e.target.value)}
          onKeyDown={(e) => {
            if (e.key !== "Enter") return;
            e.preventDefault();
            add(typed);
          }}
        />
        <div role="group" aria-label="Labels already in use" className="flex flex-wrap gap-1.5">
          {[...new Set([...chosen, ...known])].map((name) => (
            <Chip key={name} pressed={chosen.includes(name)} onClick={() => onChange(chosen.includes(name) ? chosen.filter((each) => each !== name) : [...chosen, name])}>
              {name}
            </Chip>
          ))}
        </div>
      </div>
    </Labelled>
  );
}

/** One of the project's own fields, drawn as the control its kind asks for. */
export function FieldAnswer({ field, answer, onAnswer }: { field: Field; answer: Answer | undefined; onAnswer: (answer: Answer) => void }) {
  const [text, setText] = useState("");
  const keep = (raw: string | boolean) => onAnswer({ name: field.name, value: valueFromInput(field.kind, raw) });

  if (field.kind === "select") {
    return (
      <Select
        label={field.name}
        value={typeof answer?.value === "string" ? answer.value : ""}
        onChange={(e) => keep(e.target.value)}
      >
        <option value="">None</option>
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
