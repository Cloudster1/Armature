import { useCallback, useEffect, useState, type FormEvent } from "react";
import { Link, createRoute, useNavigate } from "@tanstack/react-router";
import { appRoute } from "./app";
import { useAccess } from "@/api/access";
import { LABEL_COLORS, useDeleteLabel, useLabels, useUpdateLabel, type Label } from "@/api/labels";
import type { LabelColor } from "@/api/issues";
import {
  Button,
  Choice,
  EmptyState,
  ErrorBanner,
  Field,
  ListSearch,
  Page,
  PageHeader,
  Pager,
  Table,
  Td,
  Th,
  clampOffset,
  isPageOffset,
  shownOf,
} from "@/components/ui";
import { Icon } from "@/components/icons";
import { LABEL_PAGE_SIZE } from "@/config";
import { useConfirm } from "@/features/shell/ConfirmProvider";
import { LabelChip } from "@/features/labels/LabelChip";
import { t } from "@/i18n";

interface LabelsSearch {
  q?: string;
  offset?: number;
}

/** The search and the page are in the address, so a link reopens the same list. */
export const labelsRoute = createRoute({
  getParentRoute: () => appRoute,
  path: "/settings/labels",
  validateSearch: (search: Record<string, unknown>): LabelsSearch => ({
    // A search of digits alone comes back from the address as a number.
    ...(typeof search.q === "number" ? { q: String(search.q) } : typeof search.q === "string" && search.q !== "" ? { q: search.q } : {}),
    ...(isPageOffset(search.offset) ? { offset: search.offset } : {}),
  }),
  component: LabelsPage,
});

/**
 * The organization's words. They are coined where they are used, on an
 * issue; this page is where one is renamed or recoloured everywhere at once,
 * or retired.
 */
function LabelsPage() {
  const { q = "", offset = 0 } = labelsRoute.useSearch();
  const navigate = useNavigate();
  const { data, isLoading, error } = useLabels({ q, offset });
  const { data: access } = useAccess();
  const remove = useDeleteLabel();
  const confirm = useConfirm();
  const canAdminister = access?.canAdministerOrg ?? false;
  const labels = data?.labels ?? [];

  const show = useCallback(
    (next: LabelsSearch, replace = false) =>
      navigate({ to: "/settings/labels", search: { q: next.q || undefined, offset: next.offset || undefined }, replace }),
    [navigate],
  );
  // Labels leave the list while it is open; a page past the end steps back.
  useEffect(() => {
    if (data && offset > 0 && offset >= data.total) show({ q, offset: clampOffset(offset, data.total, LABEL_PAGE_SIZE) }, true);
  }, [data, offset, q, show]);

  return (
    <Page width="narrow">
      <PageHeader
        crumb={
          <Link to="/settings" className="hover:text-ink">
            {t.settings.crumb}
          </Link>
        }
        title={t.looks.labels.title}
        meta={t.looks.labels.meta}
      />
      {error && <ErrorBanner>{(error as Error).message}</ErrorBanner>}
      {/* A refused removal is the shell's toast, as on every table, rather than a banner above rows it may be far from. */}
      <div className="mb-3 flex items-center gap-3">
        <ListSearch
          aria-label={t.looks.labels.find}
          placeholder={t.looks.labels.findPlaceholder}
          value={q}
          onChange={(text) => show({ q: text })}
          className="w-56"
          data-label-search=""
        />
        {data && <span className="text-sm text-ink-muted tabular-nums">{shownOf(data.total, data.offset, labels.length, t.kit.counted.label)}</span>}
      </div>
      {isLoading ? null : labels.length === 0 && q ? (
        <EmptyState icon={<Icon.Search />} title={t.looks.labels.noMatch} description={t.looks.labels.noMatchDetail} />
      ) : labels.length === 0 ? (
        <EmptyState title={t.looks.labels.noneYet} description={t.looks.labels.noneYetDetail} />
      ) : (
        <Table>
          <thead>
            <tr>
              <Th>{t.looks.labels.columnLabel}</Th>
              <Th className="w-24 text-right">{t.looks.labels.columnIssues}</Th>
              {canAdminister && <Th className="w-40" />}
            </tr>
          </thead>
          <tbody>
            {labels.map((label) => (
              <LabelRow
                key={label.id}
                label={label}
                canAdminister={canAdminister}
                onDelete={async () =>
                  (await confirm({
                    noun: t.kit.confirm.things.label,
                    verb: t.kit.confirm.verbs.remove,
                    body: t.looks.labels.removeBody(label.name),
                  })) && remove.mutate(label.id)
                }
              />
            ))}
          </tbody>
        </Table>
      )}
      {data && (
        <Pager label={t.looks.labels.pages} total={data.total} offset={data.offset} pageSize={LABEL_PAGE_SIZE} onChange={(next) => show({ q, offset: next })} />
      )}
    </Page>
  );
}

function LabelRow({ label, canAdminister, onDelete }: { label: Label; canAdminister: boolean; onDelete: () => void }) {
  const update = useUpdateLabel();
  const [editing, setEditing] = useState(false);
  const [name, setName] = useState(label.name);
  const [color, setColor] = useState<LabelColor>(label.color);

  function save(event: FormEvent) {
    event.preventDefault();
    update.mutate(
      { id: label.id, name: name !== label.name ? name : undefined, color: color !== label.color ? color : undefined },
      { onSuccess: () => setEditing(false) },
    );
  }

  if (editing) {
    return (
      <tr data-label-row={label.name}>
        <Td colSpan={3}>
          <form onSubmit={save} className="flex flex-wrap items-end gap-3">
            {update.error && <ErrorBanner>{(update.error as Error).message}</ErrorBanner>}
            <Field id={`label-name-${label.id}`} label={t.looks.labels.name} value={name} onChange={(e) => setName(e.target.value)} required className="w-48" />
            <div className="space-y-1">
              <span className="block text-sm font-medium text-ink-muted">{t.looks.labels.colour}</span>
              <span className="flex gap-1" role="radiogroup" aria-label={t.looks.labels.colour}>
                {LABEL_COLORS.map((option) => (
                  <Choice
                    key={option}
                    checked={option === color}
                    aria-label={t.looks.labels.colours[option] ?? option}
                    onSelect={() => setColor(option)}
                    className={option === color ? "rounded ring-2 ring-accent" : "rounded"}
                  >
                    <LabelChip label={{ name: t.looks.labels.colours[option] ?? option, color: option }} />
                  </Choice>
                ))}
              </span>
            </div>
            <Button type="submit" size="sm" loading={update.isPending}>
              {t.looks.labels.save}
            </Button>
            <Button type="button" size="sm" variant="ghost" onClick={() => setEditing(false)}>
              {t.kit.cancel}
            </Button>
          </form>
        </Td>
      </tr>
    );
  }

  return (
    <tr data-label-row={label.name}>
      <Td>
        <LabelChip label={label} />
      </Td>
      <Td className="text-right tabular-nums">{label.issueCount}</Td>
      {canAdminister && (
        <Td className="text-right">
          <span className="flex justify-end gap-1">
            <Button size="sm" variant="ghost" onClick={() => setEditing(true)}>
              {t.looks.labels.edit}
            </Button>
            <Button size="sm" variant="ghost" aria-label={t.looks.labels.removeLabel(label.name)} onClick={onDelete}>
              {t.looks.labels.remove}
            </Button>
          </span>
        </Td>
      )}
    </tr>
  );
}
