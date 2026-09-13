import { useState, type FormEvent } from "react";
import { useNavigate } from "@tanstack/react-router";
import { useCreateSavedFilter, useSavedFilters, useStarFilter, exportHref, type SavedFilter } from "@/api/filters";
import { Button, ButtonLink, Checkbox, Chip, Dialog, ErrorBanner, Field, IconButton } from "@/components/ui";
import { Icon } from "@/components/icons";
import { EXPORT_COLUMNS, EXPORT_DEFAULT_COLUMNS } from "@/config";

/** Starred first, then mine, then shared; by name inside each. */
export function orderFilters(filters: SavedFilter[], me: string | undefined): SavedFilter[] {
  const rank = (f: SavedFilter) => (f.starred ? 0 : f.ownerId === me ? 1 : 2);
  return [...filters].sort((a, b) => rank(a) - rank(b) || a.name.localeCompare(b.name));
}

// The row under the query on the search page: the saved searches as chips,
// with a star on each, Save for the query that is running, and Export.
export function SavedFilterBar({ query, me, active }: { query: string; me: string | undefined; active: SavedFilter | null }) {
  const { data } = useSavedFilters();
  const star = useStarFilter();
  const navigate = useNavigate();
  const [saving, setSaving] = useState(false);
  const filters = orderFilters(data?.filters ?? [], me);

  return (
    <div className="mb-4 flex flex-wrap items-center gap-2" data-saved-filters>
      {filters.map((f) => (
        <span key={f.id} className="inline-flex items-center gap-0.5">
          <Chip pressed={active?.id === f.id} onClick={() => navigate({ to: "/search", search: { q: f.query, f: f.id } })} title={f.query} data-filter={f.name} data-filter-shared={f.shared ? "true" : "false"}>
            {f.name}
          </Chip>
          <IconButton icon={f.starred ? <Icon.Check /> : <Icon.Plus />} label={f.starred ? `Unstar ${f.name}` : `Star ${f.name}`} size="sm" onClick={() => star.mutate({ id: f.id, on: !f.starred })} data-filter-star={f.name} data-starred={f.starred ? "true" : "false"} />
        </span>
      ))}
      <span className="ml-auto flex items-center gap-2">
        {query && (
          <Button variant="secondary" size="sm" onClick={() => setSaving(true)} data-action="save-search" data-guide="save-search">
            Save this search
          </Button>
        )}
        <ExportMenu query={query} />
        <ButtonLink href="/filters" variant="ghost" size="sm" data-action="all-filters">
          All saved searches
        </ButtonLink>
      </span>
      {saving && <SaveDialog query={query} onClose={() => setSaving(false)} />}
    </div>
  );
}

function SaveDialog({ query, onClose }: { query: string; onClose: () => void }) {
  const create = useCreateSavedFilter();
  const navigate = useNavigate();
  const [name, setName] = useState("");
  const [shared, setShared] = useState(false);

  function submit(event: FormEvent) {
    event.preventDefault();
    create.mutate(
      { name: name.trim(), query, shared },
      {
        onSuccess: (r) => {
          onClose();
          navigate({ to: "/search", search: { q: r.filter.query, f: r.filter.id } });
        },
      },
    );
  }

  return (
    <Dialog
      open
      onClose={onClose}
      title="Save this search"
      description={<span className="font-mono text-xs">{query}</span>}
      attrs={{ "data-save-filter": "" }}
      footer={
        <>
          <Button variant="ghost" onClick={onClose}>
            Cancel
          </Button>
          <Button type="submit" form="save-filter-form" loading={create.isPending} disabled={!name.trim()}>
            Save
          </Button>
        </>
      }
    >
      <form id="save-filter-form" onSubmit={submit} className="space-y-3" noValidate>
        {create.error && <ErrorBanner>{(create.error as Error).message}</ErrorBanner>}
        <Field label="Filter name" required value={name} onChange={(e) => setName(e.target.value)} placeholder="My open work" autoFocus />
        <Checkbox label="Share with everyone in the organization" checked={shared} onChange={(e) => setShared(e.target.checked)} id="field-share-filter" />
      </form>
    </Dialog>
  );
}

// Export: which columns, then the file.
function ExportMenu({ query }: { query: string }) {
  const [columns, setColumns] = useState<string[]>(EXPORT_DEFAULT_COLUMNS);
  const [open, setOpen] = useState(false);
  return (
    <>
      <Button variant="secondary" size="sm" icon={<Icon.Download />} onClick={() => setOpen(true)} data-action="export-csv" data-guide="export-csv">
        Export
      </Button>
      <Dialog
        open={open}
        onClose={() => setOpen(false)}
        title="Export as CSV"
        description="The issues this search matches, as a file with the columns you tick."
        attrs={{ "data-export-dialog": "" }}
        footer={
          <>
            <Button variant="ghost" onClick={() => setOpen(false)}>
              Cancel
            </Button>
            <ButtonLink href={exportHref(query, columns)} download variant="primary" onClick={() => setOpen(false)} data-action="download-csv">
              Download
            </ButtonLink>
          </>
        }
      >
        <div className="grid grid-cols-2 gap-x-6 gap-y-1.5 sm:grid-cols-3">
          {EXPORT_COLUMNS.map((c) => (
            <Checkbox key={c} label={c} checked={columns.includes(c)} onChange={(e) => setColumns(e.target.checked ? [...columns, c] : columns.filter((x) => x !== c))} data-export-column={c} />
          ))}
        </div>
      </Dialog>
    </>
  );
}
