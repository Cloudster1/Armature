import type { Notification } from "@/api/notifications";

/** The request a notice about an ask for access opens, read off its link. */
export function accessRequestOf(n: Pick<Notification, "kind" | "link">): string | undefined {
  if (n.kind !== "access_requested" || !n.link) return undefined;
  const id = n.link.split("/access-requests/")[1];
  return id ? id.split(/[?#]/)[0] : undefined;
}
