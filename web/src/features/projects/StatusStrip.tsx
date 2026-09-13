import { useState, type FormEvent } from "react";
import { usePostStatusUpdate, useStatusUpdates, type Health, type StatusUpdate } from "@/api/projects";
import { Button, Dialog, ErrorBanner, Field, Segmented, Tag, Textarea } from "@/components/ui";
import { cx } from "@/components/ui/cx";
import { useFormat } from "@/lib/format";

/** The three words, in the order a form offers them, with their colours. */
export const HEALTH: Array<{ value: Health; label: string; tone: string }> = [
  { value: "on_track", label: "On track", tone: "bg-success-subtle text-success" },
  { value: "at_risk", label: "At risk", tone: "bg-warning-subtle text-warning" },
  { value: "off_track", label: "Off track", tone: "bg-danger-subtle text-danger" },
];

export function healthLabel(value: Health): string {
  return HEALTH.find((h) => h.value === value)?.label ?? value;
}

/** The coloured word a status wears wherever it is shown. */
export function HealthTag({ status, className }: { status: Health; className?: string }) {
  const health = HEALTH.find((h) => h.value === status);
  return (
    <Tag className={cx(health?.tone, className)} data-project-status={status}>
      {health?.label ?? status}
    </Tag>
  );
}

/**
 * How the project says it is doing, under its title: the latest post with
 * its note, and a way to post the next one for whoever administers it.
 */
export function StatusStrip({ projectKey, latest, canPost }: { projectKey: string; latest?: StatusUpdate; canPost: boolean }) {
  const [posting, setPosting] = useState(false);
  const [history, setHistory] = useState(false);
  const format = useFormat();
  if (!latest && !canPost) return null;
  return (
    <div className="mb-4 flex flex-wrap items-center gap-3 rounded-control border border-border bg-surface px-3 py-2 text-sm" data-status-strip>
      {latest ? (
        <>
          <HealthTag status={latest.status} />
          {latest.note && <span className="text-ink" data-status-note>{latest.note}</span>}
          <span className="text-ink-subtle">
            {latest.authorName ? `${latest.authorName}, ` : ""}
            {format.relative(latest.createdAt)}
            {latest.targetOn ? `, aiming for ${format.date(latest.targetOn)}` : ""}
          </span>
          <Button size="sm" variant="link" onClick={() => setHistory(true)} data-action="status-history">
            History
          </Button>
        </>
      ) : (
        <span className="text-ink-muted">Nobody has said how this project is doing yet.</span>
      )}
      {canPost && (
        <Button size="sm" variant="secondary" className="ml-auto" onClick={() => setPosting(true)} data-action="post-status" data-guide="post-status">
          Post an update
        </Button>
      )}
      <PostDialog projectKey={projectKey} open={posting} onClose={() => setPosting(false)} initial={latest?.status ?? "on_track"} />
      <HistoryDialog projectKey={projectKey} open={history} onClose={() => setHistory(false)} />
    </div>
  );
}

function PostDialog({ projectKey, open, onClose, initial }: { projectKey: string; open: boolean; onClose: () => void; initial: Health }) {
  const post = usePostStatusUpdate();
  const [status, setStatus] = useState<Health>(initial);
  const [note, setNote] = useState("");
  const [targetOn, setTargetOn] = useState("");

  function submit(event: FormEvent) {
    event.preventDefault();
    post.mutate(
      { projectKey, status, note: note.trim() || undefined, targetOn: targetOn || undefined },
      {
        onSuccess: () => {
          setNote("");
          onClose();
        },
      },
    );
  }

  return (
    <Dialog open={open} onClose={onClose} title="How is the project doing?" attrs={{ "data-status-dialog": "" }}>
      <form onSubmit={submit} className="space-y-4" noValidate>
        {post.error && <ErrorBanner>{(post.error as Error).message}</ErrorBanner>}
        <Segmented label="Status" value={status} onChange={setStatus} options={HEALTH.map((h) => ({ value: h.value, label: h.label, attrs: { "data-status-option": h.value } }))} />
        <div>
          <label htmlFor="field-status-note" className="mb-1 block text-sm font-medium text-ink">
            What is happening
          </label>
          <Textarea id="field-status-note" rows={3} value={note} onChange={(e) => setNote(e.target.value)} placeholder="The vendor is late; we ship the first half on time." />
        </div>
        <Field label="Aiming for" id="field-target-on" type="date" value={targetOn} onChange={(e) => setTargetOn(e.target.value)} hint="The date the project is working towards, when there is one." className="max-w-xs" />
        <div className="flex justify-end gap-2">
          <Button type="button" variant="ghost" onClick={onClose}>
            Cancel
          </Button>
          <Button type="submit" loading={post.isPending} data-action="post-status-go">
            Post
          </Button>
        </div>
      </form>
    </Dialog>
  );
}

function HistoryDialog({ projectKey, open, onClose }: { projectKey: string; open: boolean; onClose: () => void }) {
  const { data } = useStatusUpdates(open ? projectKey : "");
  const format = useFormat();
  const updates = data?.updates ?? [];
  return (
    <Dialog open={open} onClose={onClose} title="Status history" attrs={{ "data-status-history": "" }}>
      <ol className="space-y-3">
        {updates.map((u) => (
          <li key={u.id} className="text-sm" data-status-update={u.status}>
            <div className="flex items-center gap-2">
              <HealthTag status={u.status} />
              <span className="text-ink-subtle">
                {u.authorName}, {format.dateTime(u.createdAt)}
                {u.targetOn ? `, aiming for ${format.date(u.targetOn)}` : ""}
              </span>
            </div>
            {u.note && <p className="mt-1 text-ink">{u.note}</p>}
          </li>
        ))}
        {updates.length === 0 && <li className="text-sm text-ink-muted">Nothing posted yet.</li>}
      </ol>
    </Dialog>
  );
}
