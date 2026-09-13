import { useState } from "react";
import { createRoute } from "@tanstack/react-router";
import { appRoute } from "./app";
import { auditExportHref, useAuditLog, type AuditEntry, type AuditFilter } from "@/api/audit";
import { Button, ButtonLink, EmptyState, ErrorBanner, Field, Page, PageHeader, Select, Table, Tag, Td, Th } from "@/components/ui";
import { Icon } from "@/components/icons";
import { useFormat } from "@/lib/format";
import { AUDIT_PAGE_SIZE } from "@/config";

/** Who did what to the organization, newest first, for its administrators. */
export const auditRoute = createRoute({
  getParentRoute: () => appRoute,
  path: "/settings/audit",
  component: AuditPage,
});

function AuditPage() {
  const [filter, setFilter] = useState<AuditFilter>({});
  // Pages are walked by the last row's time; going back drops the cursor.
  const [cursors, setCursors] = useState<string[]>([]);
  const { data, isLoading, error } = useAuditLog({ ...filter, before: cursors[cursors.length - 1] });
  const format = useFormat();
  const entries = data?.entries ?? [];
  const actions = data?.actions ?? [];

  function narrow(next: Partial<AuditFilter>) {
    setFilter({ ...filter, ...next });
    setCursors([]);
  }

  return (
    <Page width="content">
      <PageHeader
        title="Audit log"
        meta="Who did what to the organization: roles, groups, projects, workflows, tokens and sign-ins. Kept for a year."
        actions={
          <ButtonLink href={auditExportHref(filter)} download variant="secondary" size="sm" icon={<Icon.Download />} data-action="export-audit">
            Export CSV
          </ButtonLink>
        }
      />
      <div className="mb-4 grid gap-3 sm:grid-cols-3" data-audit-filters>
        <Select label="Action" id="field-audit-action" value={filter.action ?? ""} onChange={(e) => narrow({ action: e.target.value || undefined })}>
          <option value="">Every action</option>
          {actions.map((a) => (
            <option key={a} value={a}>
              {a}
            </option>
          ))}
        </Select>
        <Field label="From" id="field-audit-from" type="date" value={filter.from ?? ""} onChange={(e) => narrow({ from: e.target.value || undefined })} />
        <Field label="To" id="field-audit-to" type="date" value={filter.to ?? ""} onChange={(e) => narrow({ to: e.target.value || undefined })} />
      </div>
      {error && <ErrorBanner>{(error as Error).message}</ErrorBanner>}
      {isLoading ? null : entries.length === 0 ? (
        <EmptyState title="Nothing recorded" description={cursors.length ? "That was the last page." : "Nothing matches; widen the dates or choose every action."} />
      ) : (
        <Table>
          <thead>
            <tr>
              <Th className="w-44">When</Th>
              <Th>What</Th>
              <Th className="w-40">Who</Th>
              <Th>Details</Th>
            </tr>
          </thead>
          <tbody>
            {entries.map((entry) => (
              <tr key={entry.id} data-audit-row={entry.action}>
                <Td className="whitespace-nowrap text-sm text-ink-muted" title={entry.createdAt}>
                  {format.dateTime(entry.createdAt)}
                </Td>
                <Td>
                  <Tag>{entry.action}</Tag>
                </Td>
                <Td className="text-sm text-ink" data-audit-actor>
                  {entry.actorName || <span className="text-ink-subtle">system</span>}
                </Td>
                <Td className="text-xs text-ink-muted">{describe(entry)}</Td>
              </tr>
            ))}
          </tbody>
        </Table>
      )}
      <div className="mt-3 flex justify-end gap-2">
        {cursors.length > 0 && (
          <Button variant="ghost" size="sm" onClick={() => setCursors(cursors.slice(0, -1))}>
            Newer
          </Button>
        )}
        {entries.length === AUDIT_PAGE_SIZE && (
          <Button variant="ghost" size="sm" onClick={() => setCursors([...cursors, entries[entries.length - 1]!.createdAt])} data-action="audit-older">
            Older
          </Button>
        )}
      </div>
    </Page>
  );
}

/** The payload's words that mean something to a reader, without the ids. */
function describe(entry: AuditEntry): string {
  const parts: string[] = [];
  for (const [key, value] of Object.entries(entry.data)) {
    if (key === "actorId" || /Id$/.test(key) || value === null || value === undefined || value === "") continue;
    parts.push(`${key}: ${Array.isArray(value) ? value.join(", ") : String(value)}`);
  }
  if (entry.ip) parts.push(`from ${entry.ip}`);
  return parts.join(" · ");
}
