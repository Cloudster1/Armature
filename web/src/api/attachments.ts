import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { BASE, request, upload } from "./client";
import { issuesQueryKey, type UserRef } from "./issues";

/** What the tracker knows about a file on an issue; the bytes are behind attachmentUrl. */
export interface Attachment {
  id: string;
  issueId: string;
  issueKey: string;
  uploader?: UserRef;
  fileName: string;
  contentType: string;
  size: number;
  createdAt: string;
}

export const attachmentsQueryKey = ["attachments"] as const;

/** Which side of the desk asks: an agent through the issue, a customer through the portal. */
export type AttachmentSource = "issue" | "portal";

function listPath(source: AttachmentSource, issueKey: string): string {
  return source === "portal" ? `/portal/requests/${issueKey}/attachments` : `/issues/${issueKey}/attachments`;
}

function filePath(source: AttachmentSource, id: string): string {
  return source === "portal" ? `/portal/attachments/${id}` : `/attachments/${id}`;
}

export function useAttachments(issueKey: string, source: AttachmentSource = "issue") {
  return useQuery({
    queryKey: [...attachmentsQueryKey, source, issueKey],
    queryFn: () => request<{ attachments: Attachment[] }>(listPath(source, issueKey)),
    enabled: Boolean(issueKey),
  });
}

export function useUploadAttachment(source: AttachmentSource = "issue") {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({ issueKey, file }: { issueKey: string; file: File }) => {
      const form = new FormData();
      form.append("file", file, file.name);
      return upload<{ attachment: Attachment }>(listPath(source, issueKey), form);
    },
    // The upload is in the changelog as well as the list.
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: attachmentsQueryKey });
      void queryClient.invalidateQueries({ queryKey: issuesQueryKey });
    },
  });
}

export function useDeleteAttachment(source: AttachmentSource = "issue") {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (id: string) => request<void>(filePath(source, id), { method: "DELETE" }),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: attachmentsQueryKey });
      void queryClient.invalidateQueries({ queryKey: issuesQueryKey });
    },
  });
}

/** Where the bytes are. The session cookie goes with the request, so a plain link works. */
export function attachmentUrl(id: string, inline = false, source: AttachmentSource = "issue"): string {
  return `${BASE}${filePath(source, id)}${inline ? "?inline=1" : ""}`;
}

/** Whether the browser can show this in a tab rather than download it. */
export function canPreview(contentType: string): boolean {
  return contentType.startsWith("image/") || contentType === "application/pdf" || contentType === "text/plain";
}

const SIZE_UNITS = ["B", "KB", "MB", "GB"];

/** A file size the way a person reads one: 12 KB, 3.4 MB. */
export function formatSize(bytes: number): string {
  let value = bytes;
  let unit = 0;
  while (value >= 1024 && unit < SIZE_UNITS.length - 1) {
    value /= 1024;
    unit += 1;
  }
  const shown = unit === 0 ? String(value) : value < 10 ? value.toFixed(1) : String(Math.round(value));
  return `${shown} ${SIZE_UNITS[unit]}`;
}
