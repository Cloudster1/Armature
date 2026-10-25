import { useState, type ReactNode } from "react";
import { useSchemes, useWorkflows } from "@/api/workflows";
import { SearchPicker, type PickerOption } from "@/components/ui";
import { LIST_SEARCH_DELAY_MS, WORKFLOW_PICKER_RESULTS } from "@/config";
import { useDebounced } from "@/lib/debounce";
import { t } from "@/i18n";

interface PickerProps {
  "aria-label": string;
  id?: string;
  /** The chosen id, or empty for none. */
  value: string;
  /** What the chosen one is called, from the row that holds it, so it stays offered. */
  chosenName?: string;
  /** Called with the chosen id and its name, which the caller keeps to show it by. */
  onChange: (id: string, name: string) => void;
  leading?: ReactNode;
  disabled?: boolean;
  className?: string;
}

/** The chosen one first when the search has moved past it, then the matches. */
function withChosen(found: PickerOption[], value: string, chosenName?: string): PickerOption[] {
  if (!value || !chosenName || found.some((option) => option.value === value)) return found;
  return [{ value, label: chosenName }, ...found];
}

/** Chooses a workflow by asking the server as somebody types, so it never needs them all. */
export function WorkflowPicker({ value, chosenName, ...rest }: PickerProps) {
  const [words, setWords] = useState("");
  const settled = useDebounced(words.trim(), LIST_SEARCH_DELAY_MS);
  const { data } = useWorkflows({ q: settled, limit: WORKFLOW_PICKER_RESULTS });
  const found = (data?.workflows ?? []).map((workflow) => ({ value: workflow.id, label: workflow.name }));
  const nothing = Boolean(settled) && data !== undefined && found.length === 0;
  return (
    <SearchPicker
      {...rest}
      value={value}
      words={words}
      onWords={setWords}
      options={withChosen(found, value, chosenName)}
      placeholder={t.workflows.picker.findWorkflow}
      nothing={nothing ? t.workflows.picker.noWorkflowCalled : undefined}
    />
  );
}

/** Chooses a scheme other than the default and those left out, by search. */
export function SchemePicker({ value, chosenName, exclude = [], ...rest }: PickerProps & { exclude?: string[] }) {
  const [words, setWords] = useState("");
  const settled = useDebounced(words.trim(), LIST_SEARCH_DELAY_MS);
  const { data } = useSchemes({ q: settled, limit: WORKFLOW_PICKER_RESULTS });
  const found = (data?.schemes ?? [])
    .filter((scheme) => !scheme.isDefault && !exclude.includes(scheme.id))
    .map((scheme) => ({ value: scheme.id, label: scheme.name }));
  const nothing = Boolean(settled) && data !== undefined && found.length === 0;
  return (
    <SearchPicker
      {...rest}
      value={value}
      words={words}
      onWords={setWords}
      options={withChosen(found, value, chosenName)}
      placeholder={t.workflows.picker.findScheme}
      nothing={nothing ? t.workflows.picker.noSchemeCalled : undefined}
    />
  );
}
