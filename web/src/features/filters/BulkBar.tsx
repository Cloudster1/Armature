import { useState } from "react";
import { useBulkEdit, useBulkTransitions, type BulkChange, type BulkResult } from "@/api/filters";
import type { Issue, LabelRef } from "@/api/issues";
import { Button, countOf, Dialog, ErrorBanner, Labelled, Select, Toolbar } from "@/components/ui";
import { Icon } from "@/components/icons";
import { BULK_MAX_SELECTION } from "@/config";
import { LabelInput } from "@/features/labels/LabelInput";
import { PersonPicker } from "@/features/people/PersonPicker";
import { useConfirm } from "@/features/shell/ConfirmProvider";
import { BulkMoveDialog } from "./BulkMoveDialog";
import { BulkProjectChanges } from "./BulkProjectChanges";
import { ExportDialog, keysQuery } from "./ExportDialog";
import { t } from "@/i18n";

/** Whether a change asks for anything; the Apply button waits until it does. */
export function changeIsEmpty(change: BulkChange): boolean {
  return (
    change.priority === undefined &&
    change.assignee === undefined &&
    change.sprintId === undefined &&
    change.milestoneId === undefined &&
    change.teamId === undefined &&
    !(change.addLabels ?? []).length &&
    !(change.removeLabels ?? []).length &&
    change.fixVersions === undefined &&
    change.components === undefined &&
    Object.keys(change.fields ?? {}).length === 0 &&
    !(change.transition ?? "").trim()
  );
}

/** The project part of an issue key. */
export function projectOf(key: string): string {
  return key.slice(0, key.lastIndexOf("-")).toUpperCase();
}

/** Every label the selection carries, each once, to offer for removal. */
function labelsOf(issues: Pick<Issue, "labels">[]): LabelRef[] {
  const out: LabelRef[] = [];
  for (const issue of issues) {
    for (const label of issue.labels ?? []) {
      if (!out.some((l) => l.id === label.id)) out.push(label);
    }
  }
  return out;
}

// The bar that appears over a list when rows are ticked: what to change on all
// of them, one Apply, a move or a delete, a dialog that lists what was refused, and an Export.
export function BulkBar({
  keys,
  issues = [],
  onDone,
  onClear,
}: {
  keys: string[];
  /** The selected issues as the list holds them, for their projects, statuses and labels. */
  issues?: Pick<Issue, "key" | "status" | "labels">[];
  onDone?: (result: BulkResult) => void;
  onClear: () => void;
}) {
  const edit = useBulkEdit();
  const confirm = useConfirm();
  const [change, setChange] = useState<BulkChange>({});
  const [labels, setLabels] = useState<string[]>([]);
  const [typing, setTyping] = useState("");
  // A word typed but not yet added counts, as it does where a blur adds it.
  const adding = typing && !labels.some((name) => name.toLowerCase() === typing.toLowerCase()) ? [...labels, typing] : labels;
  const [removing, setRemoving] = useState<string[]>([]);
  const [result, setResult] = useState<BulkResult | null>(null);
  const [exporting, setExporting] = useState(false);
  const [moving, setMoving] = useState(false);
  const tooMany = keys.length > BULK_MAX_SELECTION;
  const projects = [...new Set(keys.map(projectOf))];
  const single = projects.length === 1 ? projects[0]! : null;
  const { data: shared } = useBulkTransitions(keys);
  const selected = issues.filter((issue) => keys.includes(issue.key));
  // On a phone the bar is the count and one button until that button opens the rest.
  const [open, setOpen] = useState(false);
  const folded = open ? "contents" : "contents max-shell:hidden";
  const full = { ...change, addLabels: adding.length ? adding : undefined, removeLabels: removing.length ? removing : undefined };

  function run(next: BulkChange) {
    edit.mutate(
      { keys, change: next },
      {
        onSuccess: (r) => {
          setMoving(false);
          setResult(r);
          onDone?.(r);
        },
      },
    );
  }

  async function remove() {
    if (await confirm({ noun: countOf(keys.length, t.kit.counted.issue), body: t.search.bulk.deleteBody(keys.length) })) run({ delete: true });
  }

  const close = () => {
    setResult(null);
    onClear();
  };

  return (
    <>
      <Toolbar
        label={t.search.bulk.label}
        start={
          <div className="flex flex-wrap items-end gap-3" data-bulk-bar={keys.length}>
            <span className="pb-2 text-sm font-medium text-ink">{t.search.bulk.selected(keys.length)}</span>
            <Button variant="secondary" size="sm" className="mb-1 shell:hidden" aria-expanded={open} onClick={() => setOpen(!open)} data-action="bulk-unfold">
              {open ? t.search.bulk.fewer : t.search.bulk.change(keys.length)}
            </Button>
            <div className={folded} data-bulk-fields>
              <Select
                label={t.search.bulk.priority}
                id="field-bulk-priority"
                controlSize="sm"
                value={change.priority ?? ""}
                onChange={(e) => setChange({ ...change, priority: (e.target.value || undefined) as BulkChange["priority"] })}
              >
                <option value="">{t.search.bulk.leave}</option>
                {["lowest", "low", "medium", "high", "highest"].map((p) => (
                  <option key={p} value={p}>
                    {t.issueList.priority.options[p] ?? p}
                  </option>
                ))}
              </Select>
              <PersonPicker
                label={t.search.bulk.assignee}
                id="field-bulk-assignee"
                controlSize="sm"
                staff
                none={t.search.bulk.leave}
                value={change.assignee === undefined ? "" : change.assignee === null ? "-" : change.assignee}
                onChange={(value) => setChange({ ...change, assignee: value === "" ? undefined : value === "-" ? null : value })}
                className="w-48"
              >
                <option value="-">{t.search.bulk.nobody}</option>
              </PersonPicker>
              <Labelled id="field-bulk-labels" label={t.search.bulk.addLabels} className="w-48">
                <LabelInput
                  id="field-bulk-labels"
                  controlSize="sm"
                  placeholder={t.search.bulk.typeLabel}
                  value={labels}
                  onChange={setLabels}
                  onTyping={setTyping}
                />
              </Labelled>
              <Labelled id="field-bulk-remove-labels" label={t.search.bulk.removeLabels} className="w-48">
                <LabelInput
                  id="field-bulk-remove-labels"
                  controlSize="sm"
                  placeholder={t.search.bulk.pickLabel}
                  value={removing}
                  onChange={setRemoving}
                  create={false}
                  known={labelsOf(selected)}
                />
              </Labelled>
              <Select
                label={t.search.bulk.transition}
                id="field-bulk-transition"
                controlSize="sm"
                value={change.transition ?? ""}
                onChange={(e) => setChange({ ...change, transition: e.target.value || undefined })}
                disabled={shared !== undefined && shared.names.length === 0}
                className="w-40"
              >
                <option value="">{shared && shared.names.length === 0 ? t.search.bulk.noSharedTransition : t.search.bulk.leave}</option>
                {(shared?.names ?? []).map((name) => (
                  <option key={name} value={name}>
                    {name}
                  </option>
                ))}
              </Select>
              {single ? (
                <BulkProjectChanges projectKey={single} change={change} onChange={setChange} />
              ) : (
                <p className="max-w-xs pb-2 text-xs text-ink-muted" data-bulk-one-project>
                  {t.search.bulk.oneProject}
                </p>
              )}
            </div>
          </div>
        }
        end={
          <div className={folded === "contents" ? "flex flex-wrap items-center gap-2" : "flex flex-wrap items-center gap-2 max-shell:hidden"}>
            {tooMany && <span className="text-sm text-danger">{t.search.bulk.atMost(BULK_MAX_SELECTION)}</span>}
            <Button variant="ghost" size="sm" onClick={onClear}>
              {t.search.bulk.clear}
            </Button>
            {/* Reading changes nothing, so neither a chosen change nor the bulk limit stands in its way. */}
            <Button variant="secondary" size="sm" icon={<Icon.Download />} onClick={() => setExporting(true)} data-action="export-selected">
              {t.search.bulk.export(keys.length)}
            </Button>
            <Button variant="secondary" size="sm" disabled={tooMany} onClick={() => setMoving(true)} data-action="bulk-move">
              {t.search.bulk.move}
            </Button>
            <Button variant="danger" size="sm" icon={<Icon.Trash />} disabled={tooMany} onClick={() => void remove()} data-action="bulk-delete">
              {t.search.bulk.delete}
            </Button>
            <Button size="sm" onClick={() => run(full)} loading={edit.isPending} disabled={tooMany || changeIsEmpty(full)} data-action="bulk-apply">
              {t.search.bulk.apply(keys.length)}
            </Button>
          </div>
        }
      />
      <ExportDialog query={keysQuery(keys)} open={exporting} onClose={() => setExporting(false)} description={t.search.bulk.exportDescription(keys.length)} />
      {moving && (
        <BulkMoveDialog
          keys={keys}
          issues={selected}
          exclude={single ? [single] : []}
          pending={edit.isPending}
          onMove={(projectKey, statuses) => run({ moveTo: { projectKey, statuses } })}
          onClose={() => setMoving(false)}
        />
      )}
      {edit.error && <ErrorBanner>{(edit.error as Error).message}</ErrorBanner>}
      <Dialog
        open={result !== null}
        onClose={close}
        title={result ? t.search.bulk.result(result.applied.length, result.refused.length) : ""}
        attrs={{ "data-bulk-result": "" }}
        footer={<Button onClick={close}>{t.search.bulk.done}</Button>}
      >
        {result && result.refused.length > 0 ? (
          <ul className="space-y-1 text-sm">
            {result.refused.map((r) => (
              <li key={r.key} data-bulk-refusal={r.key}>
                <span className="font-mono text-ink">{r.key}</span> <span className="text-ink-muted">{r.reason}</span>
              </li>
            ))}
          </ul>
        ) : (
          <p className="text-sm text-ink-muted">{t.search.bulk.allTook}</p>
        )}
        {result && result.moved.length > 0 && (
          <ul className="mt-3 space-y-1 text-sm">
            {result.moved.map((m) => (
              <li key={m.from} data-bulk-moved={m.from} className="font-mono text-ink">
                {t.search.bulk.movedTo(m.from, m.to)}
              </li>
            ))}
          </ul>
        )}
      </Dialog>
    </>
  );
}
