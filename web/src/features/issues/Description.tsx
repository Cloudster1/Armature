import { useState } from "react";
import { type Doc, isStaleEdit, useUpdateIssue } from "@/api/issues";
import { Button, Card, ErrorBanner } from "@/components/ui";
import { DocView } from "@/features/editor/DocView";
import { Editor } from "@/features/editor/Editor";

export function Description({ issueKey, doc, updatedAt, editable }: { issueKey: string; doc: Doc | null; updatedAt: string; editable: boolean }) {
  const update = useUpdateIssue();
  const [editing, setEditing] = useState(false);
  const [draft, setDraft] = useState<Doc | null>(doc);
  // The copy the edit started from. It is taken when editing begins, not at
  // save, so a newer copy arriving meanwhile cannot hide somebody's save.
  const [startedFrom, setStartedFrom] = useState(updatedAt);
  const [overtaken, setOvertaken] = useState(false);

  function start() {
    setDraft(doc);
    setStartedFrom(updatedAt);
    setOvertaken(false);
    setEditing(true);
  }

  function save() {
    // Once the newer text is on screen beside the draft, saving means replacing it.
    const expectedUpdatedAt = overtaken ? updatedAt : startedFrom;
    update.mutate(
      { key: issueKey, description: draft, expectedUpdatedAt },
      {
        onSuccess: () => {
          setOvertaken(false);
          setEditing(false);
        },
        onError: (error) => {
          if (isStaleEdit(error)) setOvertaken(true);
        },
      },
    );
  }

  if (!doc && !editable) return null;

  return (
    <Card className="p-4" data-description>
      <div className="mb-2 flex items-center justify-between">
        <h2 className="text-xs font-semibold tracking-wide text-ink-muted uppercase">Description</h2>
        {editable && !editing && (
          <Button size="sm" variant="ghost" onClick={start}>
            {doc ? "Edit" : "Add description"}
          </Button>
        )}
      </div>
      {editing ? (
        <div className="space-y-2">
          {overtaken && (
            <section aria-label="What it says now" className="rounded-md border border-border bg-surface-sunken p-3" data-newer-description>
              <h3 className="mb-1 text-xs font-semibold text-ink-muted">What it says now</h3>
              {doc ? <DocView doc={doc} /> : <p className="text-sm text-ink-subtle">The description has been cleared.</p>}
            </section>
          )}
          <Editor id="issue-description" value={draft} onChange={setDraft} autoFocus placeholder="Why it matters, what done looks like." aria-label="Description" onSubmit={save} />
          {update.error && <ErrorBanner>{(update.error as Error).message}</ErrorBanner>}
          <div className="flex gap-2">
            <Button size="sm" loading={update.isPending} onClick={save}>
              Save description
            </Button>
            <Button size="sm" variant="ghost" onClick={() => setEditing(false)}>
              Cancel
            </Button>
          </div>
        </div>
      ) : doc ? (
        <DocView doc={doc} />
      ) : (
        <p className="text-sm text-ink-subtle">No description yet.</p>
      )}
    </Card>
  );
}
