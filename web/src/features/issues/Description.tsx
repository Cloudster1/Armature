import { useState } from "react";
import { type Doc, useUpdateIssue } from "@/api/issues";
import { Button, Card, ErrorBanner } from "@/components/ui";
import { DocView } from "@/features/editor/DocView";
import { Editor } from "@/features/editor/Editor";

export function Description({ issueKey, doc, editable }: { issueKey: string; doc: Doc | null; editable: boolean }) {
  const update = useUpdateIssue();
  const [editing, setEditing] = useState(false);
  const [draft, setDraft] = useState<Doc | null>(doc);

  function save() {
    update.mutate({ key: issueKey, description: draft }, { onSuccess: () => setEditing(false) });
  }

  if (!doc && !editable) return null;

  return (
    <Card className="p-4" data-description>
      <div className="mb-2 flex items-center justify-between">
        <h2 className="text-xs font-semibold tracking-wide text-ink-muted uppercase">Description</h2>
        {editable && !editing && (
          <Button
            size="sm"
            variant="ghost"
            onClick={() => {
              setDraft(doc);
              setEditing(true);
            }}
          >
            {doc ? "Edit" : "Add description"}
          </Button>
        )}
      </div>
      {editing ? (
        <div className="space-y-2">
          <Editor id="issue-description" value={doc} onChange={setDraft} autoFocus placeholder="Why it matters, what done looks like." aria-label="Description" onSubmit={save} />
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
