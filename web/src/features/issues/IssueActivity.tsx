import { useCallback, useRef, useState, type FormEvent } from "react";
import { ACTIVITY_FOLD, APP_NAME } from "@/config";
import { type Comment, type Doc, useAddComment, useComments, useDeleteComment, useUpdateComment } from "@/api/issues";
import { useFindMentionable } from "@/api/people";
import { canAdminister, useAccess } from "@/api/access";
import { useAddNote, useCannedResponses, useRenderCanned } from "@/api/desk";
import { useProject } from "@/api/projects";
import { Icon } from "@/components/icons";
import { Button, Checkbox, ErrorBanner, IconButton, Menu, Tag } from "@/components/ui";
import { DocView } from "@/features/editor/DocView";
import { Editor, type EditorHandle } from "@/features/editor/Editor";
import { useConfirm } from "@/features/shell/ConfirmProvider";
import { Avatar, RelativeTime } from "./badges";
import { forgetDraft, keepDraft, readDraft } from "./drafts";
import { t } from "@/i18n";

// The conversation sits near the top, where it is read and added to; the
// changelog is the last section, for whoever wants to know how it got here.
export function Activity({ issueKey, me }: { issueKey: string; me?: string }) {
  const { data } = useComments(issueKey);
  const { data: access } = useAccess();
  const administers = canAdminister(access, projectOf(issueKey));
  const comments = inOrder(data?.comments ?? []);
  const [expanded, setExpanded] = useState(false);
  const shown = expanded ? comments : comments.slice(-ACTIVITY_FOLD);

  return (
    <div>
      {comments.length > shown.length && (
        <Button variant="ghost" size="sm" className="mb-2" onClick={() => setExpanded(true)}>
          {t.issuePanels.common.showAll(comments.length)}
        </Button>
      )}
      {shown.length === 0 ? (
        <p className="mb-4 text-sm text-ink-subtle">{t.issuePanels.activity.nothingSaid}</p>
      ) : (
        <ol className="mb-4 space-y-3" data-activity="comments">
          {shown.map((comment) => (
            <li key={comment.id}>
              <CommentItem comment={comment} issueKey={issueKey} mine={me !== undefined && comment.author?.id === me} administers={administers} />
            </li>
          ))}
        </ol>
      )}
      <CommentForm issueKey={issueKey} />
    </div>
  );
}

/** Oldest first, the way a story is read; the newest is last and nearest the form. */
export function inOrder<T extends { createdAt: string }>(entries: T[]): T[] {
  return [...entries].sort((a, b) => a.createdAt.localeCompare(b.createdAt));
}

function projectOf(issueKey: string): string {
  return issueKey.slice(0, issueKey.lastIndexOf("-"));
}

// Only the author rewrites a comment; whoever administers the project may also
// take one down, which the server allows for the same reason.
function CommentItem({ comment, issueKey, mine, administers }: { comment: Comment; issueKey: string; mine: boolean; administers: boolean }) {
  const remove = useDeleteComment();
  const confirm = useConfirm();
  const [editing, setEditing] = useState(false);

  async function deleteComment() {
    const body = comment.internal ? t.issuePanels.activity.noteGone : t.issuePanels.activity.commentGone;
    if (await confirm({ noun: "comment", verb: "Delete", body })) remove.mutate({ key: issueKey, id: comment.id });
  }

  return (
    <div className="flex gap-3" data-comment={comment.id}>
      <Avatar name={comment.author?.name} src={comment.author?.avatarUrl} />
      <div className="min-w-0 flex-1">
        <div className="flex items-center gap-1.5 text-xs text-ink-muted">
          <span className="font-medium text-ink">{comment.author?.name ?? APP_NAME}</span>
          {" · "}
          <RelativeTime iso={comment.createdAt} />
          {comment.editedAt && <span data-comment-edited>{t.issuePanels.activity.edited}</span>}
          {comment.internal && (
            <Tag data-internal-note className="text-warning">
              {t.issuePanels.activity.internalNote}
            </Tag>
          )}
          {!editing && (mine || administers) && (
            <span className="ml-auto flex gap-1">
              {mine && (
                <IconButton
                  icon={<Icon.Edit />}
                  label={t.issuePanels.activity.editComment}
                  size="xs"
                  onClick={() => setEditing(true)}
                  data-action="edit-comment"
                />
              )}
              <IconButton icon={<Icon.Trash />} label={t.issuePanels.activity.deleteComment} size="xs" onClick={deleteComment} data-action="delete-comment" />
            </span>
          )}
        </div>
        {editing ? <CommentEditor comment={comment} issueKey={issueKey} close={() => setEditing(false)} /> : <DocView doc={comment.body} className="mt-1" />}
        {remove.error && <ErrorBanner>{(remove.error as Error).message}</ErrorBanner>}
      </div>
    </div>
  );
}

/** The comment's own words in the editor, saved in place over them. */
function CommentEditor({ comment, issueKey, close }: { comment: Comment; issueKey: string; close: () => void }) {
  const update = useUpdateComment();
  const findPeople = useFindMentionable();
  const [draft, setDraft] = useState<Doc | null>(comment.body);

  // A refusal stays open with the draft, shown by update.error.
  function save() {
    if (!draft) return;
    update.mutateAsync({ key: issueKey, id: comment.id, body: draft }).then(close, () => {});
  }

  return (
    <div className="mt-1 space-y-2">
      <Editor
        id={`comment-${comment.id}`}
        value={comment.body}
        onChange={setDraft}
        findPeople={findPeople}
        autoFocus
        aria-label={t.issuePanels.activity.editComment}
        onSubmit={save}
      />
      {update.error && <ErrorBanner>{(update.error as Error).message}</ErrorBanner>}
      <div className="flex gap-2">
        <Button size="sm" loading={update.isPending} disabled={!draft} onClick={save} data-action="save-comment">
          {t.issuePanels.common.save}
        </Button>
        <Button size="sm" variant="ghost" onClick={close}>
          {t.issuePanels.common.cancel}
        </Button>
      </div>
    </div>
  );
}

function CommentForm({ issueKey }: { issueKey: string }) {
  const add = useAddComment();
  const note = useAddNote();
  const projectKey = projectOf(issueKey);
  const { data: projectData } = useProject(projectKey);
  const isDesk = projectData?.project?.kind === "service";
  const { data: cannedData } = useCannedResponses(isDesk ? projectKey : "");
  const render = useRenderCanned();
  const canned = cannedData?.responses ?? [];
  // Customers are not named in comments: they are told by the desk, on their terms.
  const findPeople = useFindMentionable();
  // A comment half typed on this issue earlier is still here to finish.
  const [opened] = useState(() => readDraft("comment", issueKey)?.doc ?? null);
  const [body, setBody] = useState<Doc | null>(opened);
  const editor = useRef<EditorHandle | null>(null);
  // An internal note stays between agents; a comment reaches the customer.
  const [internal, setInternal] = useState(false);

  const change = useCallback(
    (next: Doc | null) => {
      if (next) keepDraft("comment", issueKey, next);
      else forgetDraft("comment", issueKey);
      setBody(next);
    },
    [issueKey],
  );

  // A ref, not isPending: a second Ctrl+Enter can land before the render that
  // would show the first one pending.
  const posting = useRef(false);

  // The promise outlives the page, so a comment that lands after a step away
  // still forgets its draft; a refusal is shown by the mutation's error.
  function post() {
    if (!body || posting.current) return;
    posting.current = true;
    const send = internal ? note : add;
    send
      .mutateAsync({ key: issueKey, body })
      .then(
        () => {
          forgetDraft("comment", issueKey);
          editor.current?.clear();
        },
        () => {},
      )
      .finally(() => {
        posting.current = false;
      });
  }

  function onSubmit(event: FormEvent) {
    event.preventDefault();
    post();
  }

  return (
    <form onSubmit={onSubmit} className="space-y-2">
      <Editor
        id="new-comment"
        value={opened}
        onChange={change}
        findPeople={findPeople}
        rows={3}
        placeholder={t.issuePanels.activity.addPlaceholder}
        aria-label={t.issuePanels.activity.addLabel}
        onSubmit={post}
        handle={(h) => (editor.current = h)}
      />
      <div className="flex items-center gap-3">
        <Button type="submit" size="sm" loading={add.isPending || note.isPending} disabled={!body}>
          {internal ? t.issuePanels.activity.addNote : t.issuePanels.activity.comment}
        </Button>
        <Checkbox
          label={t.issuePanels.activity.internalNoteLabel}
          checked={internal}
          onChange={(e) => setInternal(e.target.checked)}
          data-action="internal-note"
          className="text-ink-muted"
        />
        {isDesk && canned.length > 0 && (
          <Menu
            label={t.issuePanels.activity.insertCanned}
            trigger={(props) => (
              <Button
                variant="ghost"
                size="sm"
                onClick={props.toggle}
                aria-haspopup={props["aria-haspopup"]}
                aria-expanded={props["aria-expanded"]}
                data-action="canned-responses"
              >
                {t.issuePanels.activity.canned}
              </Button>
            )}
            items={canned.map((c) => ({
              label: c.name,
              onSelect: () => render.mutate({ id: c.id, issueKey }, { onSuccess: (r) => editor.current?.insertMarkdown(r.text) }),
              attrs: { "data-canned-option": c.name },
            }))}
          />
        )}
      </div>
      {(add.error ?? note.error) && <ErrorBanner>{((add.error ?? note.error) as Error).message}</ErrorBanner>}
    </form>
  );
}
