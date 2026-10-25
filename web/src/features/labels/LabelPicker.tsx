import type { Issue } from "@/api/issues";
import { useSetIssueLabels } from "@/api/labels";
import { ErrorBanner } from "@/components/ui";
import { LabelChip } from "./LabelChip";
import { LabelInput } from "./LabelInput";
import { t } from "@/i18n";

/**
 * The labels on an issue, and a box to add one: typing offers the labels that
 * hold the word, and a new word is coined. Enter, comma or a blur commits it.
 */
export function LabelPicker({ issue, editable }: { issue: Issue; editable: boolean }) {
  const set = useSetIssueLabels();

  return (
    <div className="flex w-full flex-col items-end gap-1" data-labels>
      {editable ? (
        <LabelInput
          aria-label={t.looks.labels.add}
          placeholder={t.looks.labels.add}
          controlSize="sm"
          align="end"
          className="w-full"
          value={issue.labels.map((l) => l.name)}
          known={issue.labels}
          onChange={(labels) => set.mutate({ key: issue.key, labels })}
        />
      ) : (
        <span className="flex flex-wrap justify-end gap-1">
          {issue.labels.map((label) => (
            <LabelChip key={label.id} label={label} />
          ))}
          {issue.labels.length === 0 && <span className="text-ink-subtle">{t.looks.labels.none}</span>}
        </span>
      )}
      {set.error && (
        <div className="w-full text-left">
          <ErrorBanner>{(set.error as Error).message}</ErrorBanner>
        </div>
      )}
    </div>
  );
}
