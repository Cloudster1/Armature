import { useState, type FormEvent } from "react";
import { useNavigate } from "@tanstack/react-router";
import { useCreateSavedFilter, useSavedFilters, useStarFilter, type SavedFilter } from "@/api/filters";
import { Button, ButtonLink, Checkbox, Chip, Dialog, ErrorBanner, Field, IconButton } from "@/components/ui";
import { Icon } from "@/components/icons";
import { ExportDialog } from "./ExportDialog";
import { t } from "@/i18n";

/** Starred first, then mine, then shared; by name inside each. */
export function orderFilters(filters: SavedFilter[], me: string | undefined): SavedFilter[] {
  const rank = (f: SavedFilter) => (f.starred ? 0 : f.ownerId === me ? 1 : 2);
  return [...filters].sort((a, b) => rank(a) - rank(b) || a.name.localeCompare(b.name));
}

/** What the row offers: what I starred or made, and the search that is running;
 *  everyone's shared searches would grow it without end. */
export function chipFilters(filters: SavedFilter[], me: string | undefined, activeId: string | null): SavedFilter[] {
  return filters.filter((f) => f.starred || f.ownerId === me || f.id === activeId);
}

// The row under the query on the search page: my saved searches as chips,
// with a star on each, Save for the query that is running, and Export.
export function SavedFilterBar({ query, me, active }: { query: string; me: string | undefined; active: SavedFilter | null }) {
  const { data } = useSavedFilters();
  const star = useStarFilter();
  const navigate = useNavigate();
  const [saving, setSaving] = useState(false);
  const filters = orderFilters(chipFilters(data?.filters ?? [], me, active?.id ?? null), me);

  return (
    <div className="mb-4 flex flex-wrap items-center gap-2" data-saved-filters>
      {filters.map((f) => (
        <span key={f.id} className="inline-flex items-center gap-0.5">
          <Chip
            pressed={active?.id === f.id}
            onClick={() => navigate({ to: "/search", search: { q: f.query, f: f.id } })}
            title={f.query}
            data-filter={f.name}
            data-filter-shared={f.shared ? "true" : "false"}
          >
            {f.name}
          </Chip>
          <IconButton
            icon={f.starred ? <Icon.Check /> : <Icon.Plus />}
            label={f.starred ? t.search.saved.unstar(f.name) : t.search.saved.star(f.name)}
            size="sm"
            onClick={() => star.mutate({ id: f.id, on: !f.starred })}
            data-filter-star={f.name}
            data-starred={f.starred ? "true" : "false"}
          />
        </span>
      ))}
      <span className="ml-auto flex items-center gap-2">
        {query && (
          <Button variant="secondary" size="sm" onClick={() => setSaving(true)} data-action="save-search" data-guide="save-search">
            {t.search.saved.save}
          </Button>
        )}
        <ExportMenu query={query} />
        <ButtonLink href="/filters" variant="ghost" size="sm" data-action="all-filters">
          {t.search.saved.allSaved}
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
      title={t.search.saved.save}
      description={<span className="font-mono text-xs">{query}</span>}
      attrs={{ "data-save-filter": "" }}
      footer={
        <>
          <Button variant="ghost" onClick={onClose}>
            {t.search.saved.cancel}
          </Button>
          <Button type="submit" form="save-filter-form" loading={create.isPending} disabled={!name.trim()}>
            {t.search.saved.saveButton}
          </Button>
        </>
      }
    >
      <form id="save-filter-form" onSubmit={submit} className="space-y-3" noValidate>
        {create.error && <ErrorBanner>{(create.error as Error).message}</ErrorBanner>}
        <Field
          label={t.search.saved.name}
          required
          value={name}
          onChange={(e) => setName(e.target.value)}
          placeholder={t.search.saved.namePlaceholder}
          autoFocus
        />
        <Checkbox label={t.search.saved.share} checked={shared} onChange={(e) => setShared(e.target.checked)} id="field-share-filter" />
      </form>
    </Dialog>
  );
}

function ExportMenu({ query }: { query: string }) {
  const [open, setOpen] = useState(false);
  return (
    <>
      <Button variant="secondary" size="sm" icon={<Icon.Download />} onClick={() => setOpen(true)} data-action="export-csv" data-guide="export-csv">
        {t.search.saved.export}
      </Button>
      <ExportDialog query={query} open={open} onClose={() => setOpen(false)} />
    </>
  );
}
