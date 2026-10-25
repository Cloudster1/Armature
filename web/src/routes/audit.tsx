import { useState } from "react";
import { createRoute } from "@tanstack/react-router";
import { appRoute } from "./app";
import { auditExportHref, useAuditLog, type AuditEntry, type AuditFilter } from "@/api/audit";
import { Button, ButtonLink, EmptyState, ErrorBanner, Field, Page, PageHeader, Select, Table, Tag, Td, Th } from "@/components/ui";
import { Icon } from "@/components/icons";
import { useFormat } from "@/lib/format";
import { deviceName } from "@/lib/device";
import { AUDIT_PAGE_SIZE } from "@/config";
import { t } from "@/i18n";

/** Who did what, newest first, for the administrators. */
export const auditRoute = createRoute({
  getParentRoute: () => appRoute,
  path: "/settings/audit",
  component: AuditPage,
});

// Stored rows, the export and scripts keep these keys, so only the page reads
// them under the installation's name, beside installation.renamed.
const ACTION_NAMES: Record<string, string> = {
  "org.created": "installation.created",
  "org.second_factor_required": "installation.second_factor_required",
};

const actionName = (action: string) => ACTION_NAMES[action] ?? action;

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
        title={t.access.audit.title}
        meta={t.access.audit.meta}
        actions={
          <ButtonLink href={auditExportHref(filter)} download variant="secondary" size="sm" icon={<Icon.Download />} data-action="export-audit">
            {t.access.audit.export}
          </ButtonLink>
        }
      />
      <div className="mb-4 grid gap-3 sm:grid-cols-3" data-audit-filters>
        <Select
          label={t.access.audit.action}
          id="field-audit-action"
          value={filter.action ?? ""}
          onChange={(e) => narrow({ action: e.target.value || undefined })}
        >
          <option value="">{t.access.audit.everyAction}</option>
          {actions.map((a) => (
            <option key={a} value={a}>
              {actionName(a)}
            </option>
          ))}
        </Select>
        <Field
          label={t.access.audit.from}
          id="field-audit-from"
          type="date"
          value={filter.from ?? ""}
          onChange={(e) => narrow({ from: e.target.value || undefined })}
        />
        <Field
          label={t.access.audit.to}
          id="field-audit-to"
          type="date"
          value={filter.to ?? ""}
          onChange={(e) => narrow({ to: e.target.value || undefined })}
        />
      </div>
      {error && <ErrorBanner>{(error as Error).message}</ErrorBanner>}
      {isLoading ? null : entries.length === 0 ? (
        <EmptyState title={t.access.audit.empty} description={cursors.length ? t.access.audit.lastPage : t.access.audit.noMatch} />
      ) : (
        <Table>
          <thead>
            <tr>
              <Th className="w-44">{t.access.audit.when}</Th>
              <Th>{t.access.audit.what}</Th>
              <Th className="w-40">{t.access.grants.who}</Th>
              <Th>{t.access.audit.details}</Th>
            </tr>
          </thead>
          <tbody>
            {entries.map((entry) => (
              <tr key={entry.id} data-audit-row={entry.action}>
                <Td className="whitespace-nowrap text-sm text-ink-muted" title={entry.createdAt}>
                  {format.dateTime(entry.createdAt)}
                </Td>
                <Td>
                  <Tag>{actionName(entry.action)}</Tag>
                </Td>
                <Td className="text-sm text-ink" data-audit-actor>
                  {entry.actorName || <span className="text-ink-subtle">{t.access.audit.system}</span>}
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
            {t.access.audit.newer}
          </Button>
        )}
        {entries.length === AUDIT_PAGE_SIZE && (
          <Button variant="ghost" size="sm" onClick={() => setCursors([...cursors, entries[entries.length - 1]!.createdAt])} data-action="audit-older">
            {t.access.audit.older}
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
    // A signed out session names its device the way the profile listed it.
    if (key === "userAgent") {
      parts.push(t.access.audit.device(deviceName(String(value))));
      continue;
    }
    parts.push(`${key}: ${Array.isArray(value) ? value.join(", ") : String(value)}`);
  }
  if (entry.ip) parts.push(t.access.audit.fromIp(entry.ip));
  return parts.join(" · ");
}
