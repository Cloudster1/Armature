import { useState, type KeyboardEvent } from "react";
import type { Issue } from "@/api/issues";
import { useLabels, useSetIssueLabels } from "@/api/labels";
import { ErrorBanner, Input } from "@/components/ui";
import { LabelChip } from "./LabelChip";

/**
 * The labels on an issue, and a box to add one. Typing a word the
 * organization already has picks it from the list; typing a new one coins it.
 * Enter, comma or a blur commits the word.
 */
export function LabelPicker({ issue, editable }: { issue: Issue; editable: boolean }) {
  const { data } = useLabels();
  const set = useSetIssueLabels();
  const [draft, setDraft] = useState("");

  const current = issue.labels.map((l) => l.name);
  const suggestions = (data?.labels ?? []).filter((l) => !current.some((c) => c.toLowerCase() === l.name.toLowerCase()));

  function commit(word: string) {
    const name = word.trim().replace(/,$/, "");
    if (!name) return;
    setDraft("");
    if (current.some((c) => c.toLowerCase() === name.toLowerCase())) return;
    set.mutate({ key: issue.key, labels: [...current, name] });
  }

  function remove(name: string) {
    set.mutate({ key: issue.key, labels: current.filter((c) => c !== name) });
  }

  function onKeyDown(event: KeyboardEvent<HTMLInputElement>) {
    if (event.key === "Enter" || event.key === ",") {
      event.preventDefault();
      commit(draft);
    }
    const last = current.at(-1);
    if (event.key === "Backspace" && draft === "" && last !== undefined) {
      remove(last);
    }
  }

  return (
    <div className="flex w-full flex-col items-end gap-1" data-labels>
      <span className="flex flex-wrap justify-end gap-1">
        {issue.labels.map((label) => (
          <LabelChip key={label.id} label={label} onRemove={editable ? () => remove(label.name) : undefined} />
        ))}
        {issue.labels.length === 0 && !editable && <span className="text-ink-subtle">None</span>}
      </span>
      {editable && (
        <>
          <Input
            list="label-suggestions"
            aria-label="Add a label"
            controlSize="sm"
            value={draft}
            placeholder="Add a label"
            onChange={(e) => setDraft(e.target.value)}
            onKeyDown={onKeyDown}
            onBlur={() => commit(draft)}
            className="text-right text-xs"
          />
          <datalist id="label-suggestions">
            {suggestions.map((l) => (
              <option key={l.id} value={l.name} />
            ))}
          </datalist>
        </>
      )}
      {set.error && (
        <div className="w-full text-left">
          <ErrorBanner>{(set.error as Error).message}</ErrorBanner>
        </div>
      )}
    </div>
  );
}
