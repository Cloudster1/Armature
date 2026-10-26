import { useState } from "react";
import type { Issue, Status } from "@/api/issues";
import { useProjectStatuses } from "@/api/issues";
import { Button, Dialog, Select } from "@/components/ui";
import { ProjectPicker } from "@/features/projects/ProjectPicker";
import { t } from "@/i18n";

/** The statuses the selection is in, each once, in the order first met. */
export function statusesOf(issues: Pick<Issue, "status">[]): Status[] {
  const out: Status[] = [];
  for (const issue of issues) {
    if (!out.some((s) => s.id === issue.status.id)) out.push(issue.status);
  }
  return out;
}

/**
 * Where each status the selection is in lands in the target: the status itself
 * when the target has it, else one of the same name, else nothing chosen yet.
 */
export function defaultMapping(from: Status[], there: Status[]): Record<string, string> {
  const out: Record<string, string> = {};
  for (const status of from) {
    const same = there.find((s) => s.id === status.id) ?? there.find((s) => s.name.toLowerCase() === status.name.toLowerCase());
    if (same) out[status.id] = same.id;
  }
  return out;
}

/** Picks the project the selected issues move to and the status each of theirs takes there. */
export function BulkMoveDialog({
  keys,
  issues,
  exclude,
  pending,
  onMove,
  onClose,
}: {
  keys: string[];
  issues: Pick<Issue, "status">[];
  exclude: string[];
  pending: boolean;
  onMove: (projectKey: string, statuses: Record<string, string>) => void;
  onClose: () => void;
}) {
  const [projectKey, setProjectKey] = useState("");
  const [chosen, setChosen] = useState<Record<string, string>>({});
  const { data } = useProjectStatuses(projectKey);
  const there = data?.statuses ?? [];
  const from = statusesOf(issues);
  // What the person picked wins; the rest follows the target's same status.
  const mapping = { ...defaultMapping(from, there), ...chosen };
  const unmapped = from.some((s) => !mapping[s.id]);

  return (
    <Dialog
      open
      onClose={onClose}
      title={t.search.bulk.moveTitle(keys.length)}
      description={t.search.bulk.moveDescription}
      attrs={{ "data-bulk-move": "" }}
      footer={
        <>
          <Button variant="ghost" onClick={onClose}>
            {t.search.bulk.cancel}
          </Button>
          <Button loading={pending} disabled={!projectKey || unmapped} onClick={() => onMove(projectKey, mapping)} data-action="bulk-move-go">
            {t.search.bulk.moveGo}
          </Button>
        </>
      }
    >
      <div className="space-y-4">
        <ProjectPicker
          label={t.search.bulk.moveProject}
          id="field-bulk-move-project"
          value={projectKey}
          exclude={exclude}
          onChange={(key) => {
            setProjectKey(key);
            setChosen({});
          }}
        />
        {projectKey && from.length > 0 && (
          <fieldset className="space-y-2">
            <legend className="mb-1 text-sm font-medium text-ink">{t.search.bulk.statusesTitle}</legend>
            {from.map((status) => (
              <Select
                key={status.id}
                label={t.search.bulk.statusFrom(status.name)}
                id={`field-bulk-move-status-${status.id}`}
                controlSize="sm"
                value={mapping[status.id] ?? ""}
                onChange={(e) => setChosen({ ...chosen, [status.id]: e.target.value })}
                data-bulk-move-status={status.name}
              >
                <option value="" disabled>
                  {t.search.bulk.pickStatus}
                </option>
                {there.map((s) => (
                  <option key={s.id} value={s.id}>
                    {s.name}
                  </option>
                ))}
              </Select>
            ))}
          </fieldset>
        )}
      </div>
    </Dialog>
  );
}
