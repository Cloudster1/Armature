import { useQuery } from "@tanstack/react-query";
import { request } from "./client";
import { AUDIT_PAGE_SIZE } from "@/config";

/**
 * The organization's record of who did what: the administrative events copied
 * off the stream, plus the acts the stream never sees, such as a sign-in.
 */
export interface AuditEntry {
  id: string;
  action: string;
  targetType: string;
  targetId?: string;
  actorId?: string;
  actorName: string;
  data: Record<string, unknown>;
  ip?: string;
  createdAt: string;
}

export interface AuditFilter {
  action?: string;
  actor?: string;
  from?: string;
  to?: string;
  before?: string;
}

function params(filter: AuditFilter, extra: Record<string, string> = {}): string {
  const p = new URLSearchParams();
  for (const [key, value] of Object.entries({ ...filter, ...extra })) {
    if (value) p.set(key, value);
  }
  return p.toString();
}

export function useAuditLog(filter: AuditFilter) {
  return useQuery({
    queryKey: ["audit", filter],
    queryFn: () => request<{ entries: AuditEntry[]; actions: string[] }>(`/audit?${params(filter, { limit: String(AUDIT_PAGE_SIZE) })}`),
  });
}

/** The address of the same log as a CSV file. */
export function auditExportHref(filter: AuditFilter): string {
  const { before: _before, ...rest } = filter;
  return `/api/v1/audit/export?${params(rest)}`;
}
