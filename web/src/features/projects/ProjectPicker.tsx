import { useState } from "react";
import { useProject, useProjectSearch } from "@/api/projects";
import { Input, Labelled, SelectInput, fieldId } from "@/components/ui";
import { PROJECT_SEARCH_DELAY_MS } from "@/config";
import { useDebounced } from "@/lib/debounce";

interface ProjectPickerProps {
  label: string;
  /** The chosen project's key, or empty for none. */
  value: string;
  onChange: (key: string) => void;
  /** What choosing no project means, such as "Every project"; without it the empty choice asks for one. */
  none?: string;
  /** Keys not to offer, such as the project an issue is already in. */
  exclude?: string[];
  disabled?: boolean;
  id?: string;
  hint?: string;
  "aria-label"?: string;
  className?: string;
}

/** Chooses one project by asking the server as somebody types, so it never needs the whole list. */
export function ProjectPicker({ label, value, onChange, none, exclude = [], disabled, id, hint, "aria-label": ariaLabel, className }: ProjectPickerProps) {
  const [words, setWords] = useState("");
  const settled = useDebounced(words.trim(), PROJECT_SEARCH_DELAY_MS);
  const { data } = useProjectSearch(settled);
  const { data: chosen } = useProject(value);
  const selectId = id ?? fieldId(label);

  const found = (data?.projects ?? []).filter((project) => !exclude.includes(project.key));
  // The chosen project stays selectable even when the search has moved past it.
  const kept = value && chosen?.project && !found.some((project) => project.key === value) ? [chosen.project] : [];
  const options = [...kept, ...found];
  const nothing = Boolean(settled) && data !== undefined && found.length === 0;

  return (
    <Labelled id={selectId} label={label} hint={nothing ? "No project matches that. Try part of its key or its name." : hint} className={className}>
      <div className="space-y-1.5" data-project-picker={selectId}>
        <Input
          type="search"
          controlSize="sm"
          aria-label={`Find a project for ${ariaLabel ?? label}`}
          placeholder="Find by key or name"
          value={words}
          disabled={disabled}
          onChange={(event) => setWords(event.target.value)}
          // Enter in the search narrows the list; it must not submit the form around it.
          onKeyDown={(event) => event.key === "Enter" && event.preventDefault()}
          data-project-search=""
        />
        <SelectInput
          id={selectId}
          aria-label={ariaLabel}
          aria-describedby={nothing || hint ? `${selectId}-hint` : undefined}
          value={value}
          disabled={disabled}
          onChange={(event) => onChange(event.target.value)}
          className="w-full"
        >
          <option value="">{none ?? "Choose a project"}</option>
          {options.map((project) => (
            <option key={project.key} value={project.key}>
              {project.name} ({project.key})
            </option>
          ))}
        </SelectInput>
      </div>
    </Labelled>
  );
}
