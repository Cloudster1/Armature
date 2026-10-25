import { useState, type ReactNode } from "react";
import { exportHref } from "@/api/filters";
import { Button, ButtonLink, Checkbox, Dialog } from "@/components/ui";
import { EXPORT_COLUMNS, EXPORT_DEFAULT_COLUMNS } from "@/config";
import { t } from "@/i18n";

/** The query that matches exactly these issues, for exporting them and nothing else. */
export function keysQuery(keys: string[]): string {
  return keys.length === 1 ? `key = ${keys[0]}` : `key in (${keys.join(", ")})`;
}

// Export: which columns, then the file. Search, an issue's head, the ticked
// rows and a project's list all open this one dialog with their own query.
export function ExportDialog({
  query,
  project,
  open,
  onClose,
  description = t.search.export.description,
}: {
  query: string;
  project?: string;
  open: boolean;
  onClose: () => void;
  description?: ReactNode;
}) {
  const [columns, setColumns] = useState<string[]>(EXPORT_DEFAULT_COLUMNS);
  return (
    <Dialog
      open={open}
      onClose={onClose}
      title={t.search.export.title}
      description={description}
      attrs={{ "data-export-dialog": "" }}
      footer={
        <>
          <Button variant="ghost" onClick={onClose}>
            {t.search.export.cancel}
          </Button>
          <ButtonLink href={exportHref(query, columns, project)} download variant="primary" onClick={onClose} data-action="download-csv">
            {t.search.export.download}
          </ButtonLink>
        </>
      }
    >
      <div className="grid grid-cols-2 gap-x-6 gap-y-1.5 sm:grid-cols-3">
        {EXPORT_COLUMNS.map((c) => (
          <Checkbox
            key={c}
            label={c}
            checked={columns.includes(c)}
            onChange={(e) => setColumns(e.target.checked ? [...columns, c] : columns.filter((x) => x !== c))}
            data-export-column={c}
          />
        ))}
      </div>
    </Dialog>
  );
}
