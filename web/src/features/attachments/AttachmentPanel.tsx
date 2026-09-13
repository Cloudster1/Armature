import { useRef, useState, type ChangeEvent } from "react";
import {
  type AttachmentSource,
  attachmentUrl,
  canPreview,
  formatSize,
  useAttachments,
  useDeleteAttachment,
  useUploadAttachment,
} from "@/api/attachments";
import { useMe } from "@/api/auth";
import { Button, ErrorBanner } from "@/components/ui";
import { useConfirm } from "@/features/shell/ConfirmProvider";
import { relativeTime } from "@/features/issues/badges";
import { ATTACHMENT_MAX_BYTES } from "@/config";

/**
 * The files on an issue. Uploading goes through the API to the bucket; the
 * list is what the tracker knows, and the bytes are one link away.
 */
export function AttachmentPanel({
  issueKey,
  editable,
  source = "issue",
  note,
}: {
  issueKey: string;
  editable: boolean;
  /** The portal shows the same list to the customer, who may take back only their own. */
  source?: AttachmentSource;
  /** A sentence under the heading, for what the reader should know before attaching. */
  note?: string;
}) {
  const { data } = useAttachments(issueKey, source);
  const upload = useUploadAttachment(source);
  const remove = useDeleteAttachment(source);
  const { data: me } = useMe();
  const confirm = useConfirm();
  const input = useRef<HTMLInputElement>(null);
  const [tooBig, setTooBig] = useState<string | null>(null);

  const attachments = data?.attachments ?? [];
  if (attachments.length === 0 && !editable) return null;
  const mayRemove = (uploader?: string) => editable && (source === "issue" || uploader === me?.principal.user.id);

  function onPick(event: ChangeEvent<HTMLInputElement>) {
    const file = event.target.files?.[0];
    event.target.value = "";
    if (!file) return;
    if (file.size > ATTACHMENT_MAX_BYTES) {
      setTooBig(`${file.name} is ${formatSize(file.size)}; the limit is ${formatSize(ATTACHMENT_MAX_BYTES)}.`);
      return;
    }
    setTooBig(null);
    upload.mutate({ issueKey, file });
  }

  const error = tooBig ?? (upload.error as Error | null)?.message ?? (remove.error as Error | null)?.message;

  return (
    <section data-attachments>
      <div className="mb-3 flex items-center justify-between">
        <h2 className="text-xs font-semibold tracking-wide text-ink-muted uppercase">
          Attachments {attachments.length > 0 && `(${attachments.length})`}
        </h2>
        {editable && (
          <>
            <input ref={input} type="file" className="sr-only" aria-label="Attach a file" onChange={onPick} />
            <Button size="sm" variant="secondary" loading={upload.isPending} onClick={() => input.current?.click()}>
              Attach file
            </Button>
          </>
        )}
      </div>

      {note && (
        <p className="mb-3 text-sm text-ink-subtle" data-attachments-note>
          {note}
        </p>
      )}

      {error && (
        <div className="mb-3">
          <ErrorBanner>{error}</ErrorBanner>
        </div>
      )}

      {attachments.length === 0 ? (
        <p className="text-sm text-ink-subtle">Nothing attached. Screenshots, logs and documents up to {formatSize(ATTACHMENT_MAX_BYTES)}.</p>
      ) : (
        <ul className="divide-y divide-border rounded-md border border-border">
          {attachments.map((a) => (
            <li key={a.id} className="flex items-center gap-3 px-3 py-2 text-sm" data-attachment={a.fileName}>
              <a
                href={attachmentUrl(a.id, canPreview(a.contentType), source)}
                target={canPreview(a.contentType) ? "_blank" : undefined}
                rel="noreferrer"
                className="min-w-0 flex-1 truncate text-ink hover:text-accent"
                download={canPreview(a.contentType) ? undefined : a.fileName}
              >
                {a.fileName}
              </a>
              <span className="text-sm text-ink-subtle tabular-nums">{formatSize(a.size)}</span>
              <span className="hidden text-sm text-ink-subtle sm:inline">
                {a.uploader?.name ?? "Armature"} · {relativeTime(a.createdAt)}
              </span>
              {mayRemove(a.uploader?.id) && (
                <Button size="sm" variant="ghost" aria-label={`Remove ${a.fileName}`} onClick={async () => (await confirm({ noun: "attachment", verb: "Remove", body: `${a.fileName} goes from the ${source === "portal" ? "request" : "issue"} and from storage.` })) && remove.mutate(a.id)}>
                  Remove
                </Button>
              )}
            </li>
          ))}
        </ul>
      )}
    </section>
  );
}
