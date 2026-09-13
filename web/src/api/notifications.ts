import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { request } from "./client";
import { INBOX_PAGE_SIZE, UNREAD_POLL_MS } from "@/config";

export interface Notification {
  id: string;
  issueKey?: string;
  kind: string;
  title: string;
  body?: string;
  link?: string;
  createdAt: string;
  readAt?: string;
}

/** A kind absent from a map is on; only false turns it off. */
export interface NotificationPreferences {
  mail: Record<string, boolean>;
  inapp: Record<string, boolean>;
  digest: "off" | "hourly" | "daily";
  watchOwn: boolean;
}

/** The reasons a person is told, in the order the preferences page lists them. */
export const NOTIFICATION_KINDS: Array<{ kind: string; label: string }> = [
  { kind: "assigned", label: "An issue is assigned to me" },
  { kind: "mentioned", label: "Somebody mentions me" },
  { kind: "commented", label: "Somebody comments on my issues" },
  { kind: "transitioned", label: "My issues move" },
  { kind: "watching", label: "Issues I watch change" },
  { kind: "sla_breached", label: "A request misses its goal" },
  { kind: "rule", label: "An automation rule writes to me" },
  { kind: "filter", label: "A saved search reports" },
];

const inboxKey = ["notifications"];

export function useInbox(unreadOnly: boolean) {
  return useQuery({
    queryKey: [...inboxKey, "list", unreadOnly],
    queryFn: () => request<{ notifications: Notification[] }>(`/notifications?limit=${INBOX_PAGE_SIZE}${unreadOnly ? "&unread=true" : ""}`),
  });
}

/** The badge polls: a bell that only moves on reload is a bell nobody trusts. */
export function useUnreadCount() {
  return useQuery({
    queryKey: [...inboxKey, "unread"],
    queryFn: () => request<{ unread: number }>("/notifications/unread-count"),
    refetchInterval: UNREAD_POLL_MS,
  });
}

export function useMarkRead() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (input: { ids?: string[]; all?: boolean }) => request<void>("/notifications/read", { method: "POST", body: input }),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: inboxKey }),
  });
}

export function useNotificationPreferences() {
  return useQuery({
    queryKey: ["notification-preferences"],
    queryFn: () => request<{ preferences: NotificationPreferences }>("/notification-preferences"),
  });
}

export function useSaveNotificationPreferences() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (input: NotificationPreferences) => request<{ preferences: NotificationPreferences }>("/notification-preferences", { method: "PUT", body: input }),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ["notification-preferences"] }),
  });
}
