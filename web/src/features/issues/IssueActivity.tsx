import { useRef, useState, type FormEvent } from "react";
import { ACTIVITY_FOLD } from "@/config";
import { type Comment, type Doc, useAddComment, useComments, useMembers } from "@/api/issues";
import { useAddNote, useCannedResponses, useRenderCanned } from "@/api/desk";
import { useProject } from "@/api/projects";
import { Button, Checkbox, ErrorBanner, Menu, Tag } from "@/components/ui";
import { DocView } from "@/features/editor/DocView";
import { Editor, type EditorHandle } from "@/features/editor/Editor";
import { Avatar, relativeTime } from "./badges";

// The conversation sits near the top, where it is read and added to; the
// changelog is the last section, for whoever wants to know how it got here.
export function Activity({ issueKey }: { issueKey: string }) {
  const { data } = useComments(issueKey);
  const comments = inOrder(data?.comments ?? []);
  const [expanded, setExpanded] = useState(false);
  const shown = expanded ? comments : comments.slice(-ACTIVITY_FOLD);

  return (
    <div>
      {comments.length > shown.length && (
        <Button variant="ghost" size="sm" className="mb-2" onClick={() => setExpanded(true)}>
          Show all {comments.length}
        </Button>
      )}
      {shown.length === 0 ? (
        <p className="mb-4 text-sm text-ink-subtle">Nothing said yet.</p>
      ) : (
        <ol className="mb-4 space-y-3" data-activity="comments">
          {shown.map((comment) => (
            <li key={comment.id}>
              <CommentItem comment={comment} />
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

function CommentItem({ comment }: { comment: Comment }) {
  return (
    <div className="flex gap-3" data-comment={comment.id}>
      <Avatar name={comment.author?.name} src={comment.author?.avatarUrl} />
      <div className="min-w-0 flex-1">
        <p className="flex items-center gap-1.5 text-xs text-ink-muted">
          <span className="font-medium text-ink">{comment.author?.name ?? "Armature"}</span>
          {" · "}
          {relativeTime(comment.createdAt)}
          {comment.editedAt && " · edited"}
          {comment.internal && (
            <Tag data-internal-note className="text-warning">
              internal note
            </Tag>
          )}
        </p>
        <DocView doc={comment.body} className="mt-1" />
      </div>
    </div>
  );
}

function CommentForm({ issueKey }: { issueKey: string }) {
  const add = useAddComment();
  const note = useAddNote();
  const { data: members } = useMembers();
  const projectKey = issueKey.slice(0, issueKey.lastIndexOf("-"));
  const { data: projectData } = useProject(projectKey);
  const isDesk = projectData?.project?.kind === "service";
  const { data: cannedData } = useCannedResponses(isDesk ? projectKey : "");
  const render = useRenderCanned();
  const canned = cannedData?.responses ?? [];
  // Customers are not named in comments: they are told by the desk, on their terms.
  const people = (members?.members ?? []).filter((m) => m.role !== "customer").map((m) => ({ id: m.id, name: m.name, email: m.email }));
  const [body, setBody] = useState<Doc | null>(null);
  const editor = useRef<EditorHandle | null>(null);
  // An internal note stays between agents; a comment reaches the customer.
  const [internal, setInternal] = useState(false);

  function post() {
    if (!body) return;
    const send = internal ? note : add;
    send.mutate({ key: issueKey, body }, { onSuccess: () => editor.current?.clear() });
  }

  function onSubmit(event: FormEvent) {
    event.preventDefault();
    post();
  }

  return (
    <form onSubmit={onSubmit} className="space-y-2">
      <Editor id="new-comment" value={null} onChange={setBody} people={people} rows={3} placeholder="Add a comment; @ names somebody" aria-label="Add a comment" onSubmit={post} handle={(h) => (editor.current = h)} />
      <div className="flex items-center gap-3">
        <Button type="submit" size="sm" loading={add.isPending || note.isPending} disabled={!body}>
          {internal ? "Add note" : "Comment"}
        </Button>
        <Checkbox label="Internal note, not shown to the customer" checked={internal} onChange={(e) => setInternal(e.target.checked)} data-action="internal-note" className="text-ink-muted" />
        {isDesk && canned.length > 0 && (
          <Menu
            label="Insert a canned response"
            trigger={(props) => (
              <Button variant="ghost" size="sm" onClick={props.toggle} aria-haspopup={props["aria-haspopup"]} aria-expanded={props["aria-expanded"]} data-action="canned-responses">
                Canned response
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
