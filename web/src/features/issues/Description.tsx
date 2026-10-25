import { useState } from "react";
import { type Doc, isStaleEdit, useUpdateIssue } from "@/api/issues";
import { Button, Card, ErrorBanner } from "@/components/ui";
import { DocView } from "@/features/editor/DocView";
import { Editor } from "@/features/editor/Editor";
import { forgetDraft, keepDraft, readDraft } from "./drafts";
import { t } from "@/i18n";

export function Description({ issueKey, doc, updatedAt, editable }: { issueKey: string; doc: Doc | null; updatedAt: string; editable: boolean }) {
  const update = useUpdateIssue();
  // A draft left on this issue earlier reopens the editor where it was, still
  // measured against the copy it started from.
  const [opened, setOpened] = useState(() => readDraft("description", issueKey));
  const editing = opened !== undefined;
  const [draft, setDraft] = useState<Doc | null>(opened ? opened.doc : doc);
  const [overtaken, setOvertaken] = useState(false);
  // Taken when editing begins, not at save, so a newer copy arriving meanwhile
  // cannot hide somebody's save.
  const startedFrom = opened?.startedFrom ?? updatedAt;

  function edit() {
    keepDraft("description", issueKey, doc, updatedAt);
    setDraft(doc);
    setOvertaken(false);
    setOpened({ doc, startedFrom: updatedAt });
  }

  function change(next: Doc | null) {
    keepDraft("description", issueKey, next, startedFrom);
    setDraft(next);
  }

  function close() {
    forgetDraft("description", issueKey);
    setOvertaken(false);
    setOpened(undefined);
  }

  // The promise outlives the page, so a save that lands after a step away
  // still forgets the draft it sent; a refusal is shown by update.error.
  function save() {
    // Once the newer text is on screen beside the draft, saving means replacing it.
    const expectedUpdatedAt = overtaken ? updatedAt : startedFrom;
    update.mutateAsync({ key: issueKey, description: draft, expectedUpdatedAt }).then(close, (error) => {
      if (isStaleEdit(error)) setOvertaken(true);
    });
  }

  if (!doc && !editable) return null;

  return (
    <Card className="p-4" data-description>
      <div className="mb-2 flex items-center justify-between">
        <h2 className="text-xs font-semibold tracking-wide text-ink-muted uppercase">{t.issueView.description.title}</h2>
        {editable && !editing && (
          <Button size="sm" variant="ghost" onClick={edit}>
            {doc ? t.issueView.description.edit : t.issueView.description.add}
          </Button>
        )}
      </div>
      {editing ? (
        <div className="space-y-2">
          {overtaken && (
            <section aria-label={t.issueView.description.nowSays} className="rounded-md border border-border bg-surface-sunken p-3" data-newer-description>
              <h3 className="mb-1 text-xs font-semibold text-ink-muted">{t.issueView.description.nowSays}</h3>
              {doc ? <DocView doc={doc} /> : <p className="text-sm text-ink-subtle">{t.issueView.description.cleared}</p>}
            </section>
          )}
          <Editor
            id="issue-description"
            value={opened.doc}
            onChange={change}
            autoFocus
            placeholder={t.issueView.description.placeholder}
            aria-label={t.issueView.description.title}
            onSubmit={save}
          />
          {update.error && <ErrorBanner>{(update.error as Error).message}</ErrorBanner>}
          <div className="flex gap-2">
            <Button size="sm" loading={update.isPending} onClick={save}>
              {t.issueView.description.save}
            </Button>
            <Button size="sm" variant="ghost" onClick={close}>
              {t.issueView.description.cancel}
            </Button>
          </div>
        </div>
      ) : doc ? (
        <DocView doc={doc} />
      ) : (
        <p className="text-sm text-ink-subtle">{t.issueView.description.none}</p>
      )}
    </Card>
  );
}
