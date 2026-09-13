import { useState } from "react";
import { useBulkEdit, type BulkChange, type BulkResult } from "@/api/filters";
import { useMembers } from "@/api/issues";
import { Button, Dialog, ErrorBanner, Field, Select, Toolbar } from "@/components/ui";
import { BULK_MAX_SELECTION } from "@/config";

/** Whether a change asks for anything; the Apply button waits until it does. */
export function changeIsEmpty(change: BulkChange): boolean {
  return change.priority === undefined && change.assignee === undefined && !(change.addLabels ?? []).length && !(change.transition ?? "").trim();
}

// The bar that appears over a list when rows are ticked: what to change on all
// of them, one Apply, and a dialog that lists what could not be changed and why.
export function BulkBar({ keys, onDone, onClear }: { keys: string[]; onDone?: (result: BulkResult) => void; onClear: () => void }) {
  const edit = useBulkEdit();
  const { data: members } = useMembers();
  const [change, setChange] = useState<BulkChange>({});
  const [labels, setLabels] = useState("");
  const [result, setResult] = useState<BulkResult | null>(null);
  const tooMany = keys.length > BULK_MAX_SELECTION;

  function apply() {
    const next: BulkChange = { ...change };
    const words = labels.split(/[;,]/).map((l) => l.trim()).filter(Boolean);
    if (words.length) next.addLabels = words;
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
            <span className="pb-2 text-sm font-medium text-ink">{keys.length} selected</span>
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
            <Field label="Add labels" id="field-bulk-labels" controlSize="sm" value={labels} onChange={(e) => setLabels(e.target.value)} placeholder="a; b" className="w-36" />
            <Field label="Transition" id="field-bulk-transition" controlSize="sm" value={change.transition ?? ""} onChange={(e) => setChange({ ...change, transition: e.target.value || undefined })} placeholder="Start progress" className="w-40" />
          </div>
        }
        end={
          <div className="flex items-center gap-2">
            {tooMany && <span className="text-sm text-danger">At most {BULK_MAX_SELECTION} at once.</span>}
            <Button variant="ghost" size="sm" onClick={onClear}>
              Clear
            </Button>
            <Button size="sm" onClick={apply} loading={edit.isPending} disabled={tooMany || changeIsEmpty({ ...change, addLabels: labels.trim() ? [labels] : [] })} data-action="bulk-apply">
              Apply to {keys.length}
            </Button>
          </div>
        }
      />
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
