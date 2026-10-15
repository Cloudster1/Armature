import { useState } from "react";
import { useBulkEdit, type BulkChange, type BulkResult } from "@/api/filters";
import { useMembers } from "@/api/issues";
import { Button, Dialog, ErrorBanner, Field, Labelled, Select, Toolbar } from "@/components/ui";
import { Icon } from "@/components/icons";
import { BULK_MAX_SELECTION } from "@/config";
import { LabelInput } from "@/features/labels/LabelInput";
import { ExportDialog, keysQuery } from "./ExportDialog";

/** Whether a change asks for anything; the Apply button waits until it does. */
export function changeIsEmpty(change: BulkChange): boolean {
  return change.priority === undefined && change.assignee === undefined && !(change.addLabels ?? []).length && !(change.transition ?? "").trim();
}

// The bar that appears over a list when rows are ticked: what to change on all
// of them, one Apply, a dialog that lists what was refused, and an Export.
export function BulkBar({ keys, onDone, onClear }: { keys: string[]; onDone?: (result: BulkResult) => void; onClear: () => void }) {
  const edit = useBulkEdit();
  const { data: members } = useMembers();
  const [change, setChange] = useState<BulkChange>({});
  const [labels, setLabels] = useState<string[]>([]);
  const [result, setResult] = useState<BulkResult | null>(null);
  const [exporting, setExporting] = useState(false);
  const tooMany = keys.length > BULK_MAX_SELECTION;

  function apply() {
    const next: BulkChange = { ...change };
    if (labels.length) next.addLabels = labels;
    edit.mutate(
      { keys, change: next },
      {
        onSuccess: (r) => {
          setResult(r);
          onDone?.(r);
        },
      },
    );
  }

  return (
    <>
      <Toolbar
        label="Change the selected issues"
        start={
          <div className="flex flex-wrap items-end gap-3" data-bulk-bar={keys.length}>
            <span className="pb-2 text-sm font-medium text-ink">{keys.length} selected on this page</span>
            <Select label="Priority" id="field-bulk-priority" controlSize="sm" value={change.priority ?? ""} onChange={(e) => setChange({ ...change, priority: e.target.value || undefined })}>
              <option value="">Leave</option>
              {["lowest", "low", "medium", "high", "highest"].map((p) => (
                <option key={p} value={p}>
                  {p}
                </option>
              ))}
            </Select>
            <Select label="Assignee" id="field-bulk-assignee" controlSize="sm" value={change.assignee === undefined ? "" : change.assignee === null ? "-" : change.assignee} onChange={(e) => setChange({ ...change, assignee: e.target.value === "" ? undefined : e.target.value === "-" ? null : e.target.value })}>
              <option value="">Leave</option>
              <option value="-">Nobody</option>
              {(members?.members ?? []).filter((m) => m.role !== "customer").map((m) => (
                <option key={m.id} value={m.id}>
                  {m.name}
                </option>
              ))}
            </Select>
            <Labelled id="field-bulk-labels" label="Add labels" className="w-48">
              <LabelInput id="field-bulk-labels" controlSize="sm" placeholder="Type a label" value={labels} onChange={setLabels} />
            </Labelled>
            <Field label="Transition" id="field-bulk-transition" controlSize="sm" value={change.transition ?? ""} onChange={(e) => setChange({ ...change, transition: e.target.value || undefined })} placeholder="Start progress" className="w-40" />
          </div>
        }
        end={
          <div className="flex items-center gap-2">
            {tooMany && <span className="text-sm text-danger">At most {BULK_MAX_SELECTION} at once.</span>}
            <Button variant="ghost" size="sm" onClick={onClear}>
              Clear
            </Button>
            {/* Reading changes nothing, so neither a chosen change nor the bulk limit stands in its way. */}
            <Button variant="secondary" size="sm" icon={<Icon.Download />} onClick={() => setExporting(true)} data-action="export-selected">
              Export {keys.length} selected
            </Button>
            <Button size="sm" onClick={apply} loading={edit.isPending} disabled={tooMany || changeIsEmpty({ ...change, addLabels: labels })} data-action="bulk-apply">
              Apply to {keys.length}
            </Button>
          </div>
        }
      />
      <ExportDialog query={keysQuery(keys)} open={exporting} onClose={() => setExporting(false)} description={`The ${keys.length} selected issues, as a file with the columns you tick.`} />
      {edit.error && <ErrorBanner>{(edit.error as Error).message}</ErrorBanner>}
      <Dialog
        open={result !== null}
        onClose={() => {
          setResult(null);
          onClear();
        }}
        title={result ? `${result.applied.length} changed${result.refused.length ? `, ${result.refused.length} not` : ""}` : ""}
        attrs={{ "data-bulk-result": "" }}
        footer={
          <Button
            onClick={() => {
              setResult(null);
              onClear();
            }}
          >
            Done
          </Button>
        }
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
          <p className="text-sm text-ink-muted">Every selected issue took the change.</p>
        )}
      </Dialog>
    </>
  );
}
