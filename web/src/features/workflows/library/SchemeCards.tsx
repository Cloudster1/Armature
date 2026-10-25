import { useEffect, useState } from "react";
import { useDeleteScheme, useSchemes, useSetDefaultScheme, type LibraryParams, type Scheme } from "@/api/workflows";
import { Button, Card, EmptyState, ErrorBanner, IconButton, ListSearch, Pager, Stat, Tag, clampOffset, shownOf } from "@/components/ui";
import { Icon } from "@/components/icons";
import { SchemeEditor } from "@/features/workflows/SchemeEditor";
import { useConfirm } from "@/features/shell/ConfirmProvider";
import { LIBRARY_PAGE_SIZE } from "@/config";
import { t } from "@/i18n";

interface SchemeCardsProps {
  q: string;
  offset: number;
  /** Shows another search or page; replace keeps the address's history as it is. */
  onShow: (next: LibraryParams, replace?: boolean) => void;
  canConfigure: boolean;
}

/** The schemes that hand workflows to issue types, a page of cards at a time, the default first. */
export function SchemeCards({ q, offset, onShow, canConfigure }: SchemeCardsProps) {
  const { data } = useSchemes({ q, offset });
  const [editing, setEditing] = useState<{ open: boolean; scheme?: Scheme }>({ open: false });
  const schemes = data?.schemes ?? [];

  // Schemes leave the list while it is open; a page past the end steps back.
  useEffect(() => {
    if (data && offset > 0 && offset >= data.total) onShow({ q, offset: clampOffset(offset, data.total, LIBRARY_PAGE_SIZE) }, true);
  }, [data, offset, q, onShow]);

  return (
    <section className="space-y-3">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <div className="flex items-center gap-3">
          <ListSearch
            aria-label={t.workflows.library.findSchemes}
            placeholder={t.workflows.library.findByName}
            value={q}
            onChange={(text) => onShow({ q: text })}
            className="w-56"
            data-library-search=""
          />
          {data && <span className="text-sm text-ink-muted tabular-nums">{shownOf(data.total, data.offset, schemes.length, t.kit.counted.scheme)}</span>}
        </div>
        {canConfigure && !editing.open && (
          <Button size="sm" variant="secondary" onClick={() => setEditing({ open: true })}>
            {t.workflows.library.newScheme}
          </Button>
        )}
      </div>
      {editing.open && <SchemeEditor scheme={editing.scheme} onDone={() => setEditing({ open: false })} />}
      {data && schemes.length === 0 && q && (
        <EmptyState icon={<Icon.Search />} title={t.workflows.library.noScheme} description={t.workflows.library.noMatchDetail} />
      )}
      {schemes.map((scheme) => (
        <SchemeCard key={scheme.id} scheme={scheme} canConfigure={canConfigure} onEdit={() => setEditing({ open: true, scheme })} />
      ))}
      {data && (
        <Pager
          label={t.workflows.library.schemePages}
          total={data.total}
          offset={data.offset}
          pageSize={LIBRARY_PAGE_SIZE}
          onChange={(next) => onShow({ q, offset: next })}
        />
      )}
    </section>
  );
}

function SchemeCard({ scheme, canConfigure, onEdit }: { scheme: Scheme; canConfigure: boolean; onEdit: () => void }) {
  const promote = useSetDefaultScheme();
  const remove = useDeleteScheme();
  const confirm = useConfirm();
  const error = (promote.error ?? remove.error) as Error | undefined;
  const usedBy = scheme.isDefault
    ? t.workflows.library.usedByEvery
    : scheme.projectKeys.length > 0
      ? t.workflows.library.usedByProjects(scheme.projectKeys.join(", "))
      : t.workflows.library.noProject;
  // The server refuses to delete a scheme a project uses, so it is not offered until they move.
  const inUse = scheme.projectKeys.length > 0;
  const deletable = !scheme.isDefault && !inUse;
  const fallback = scheme.items.find((item) => !item.issueTypeId);
  const named = scheme.items.filter((item) => item.issueTypeId);

  return (
    <Card
      elevated
      className="p-4"
      data-scheme-card={scheme.name}
      actions={
        canConfigure ? (
          <>
            <IconButton icon={<Icon.Edit />} label={t.workflows.library.edit(scheme.name)} size="sm" variant="ghost" data-action="edit" onClick={onEdit} />
            {deletable && (
              <IconButton
                icon={<Icon.Trash />}
                label={t.workflows.library.delete(scheme.name)}
                size="sm"
                variant="ghost"
                data-action="delete"
                aria-busy={remove.isPending}
                onClick={async () =>
                  (await confirm({ noun: t.kit.confirm.things.scheme, body: t.workflows.library.deleteSchemeBody(scheme.name) })) && remove.mutate(scheme.id)
                }
              />
            )}
          </>
        ) : undefined
      }
    >
      <div className="flex flex-wrap items-start justify-between gap-4 pr-16">
        <div className="min-w-0 flex-1">
          <p className="flex items-center gap-2 text-sm font-medium text-ink">
            {scheme.name}
            {scheme.isDefault && <Tag className="text-accent">{t.workflows.library.default}</Tag>}
          </p>
          <p className="mt-0.5 text-xs text-ink-muted">{usedBy}</p>
          {canConfigure && !scheme.isDefault && inUse && (
            <p className="mt-0.5 text-xs text-ink-muted" data-scheme-move-first="">
              {t.workflows.library.moveFirst(scheme.projectKeys.join(", "))}
            </p>
          )}
          <ul className="mt-2 space-y-0.5 text-sm text-ink-muted">
            {fallback && (
              <li>
                {t.workflows.library.everythingElseUses} <span className="text-ink">{fallback.workflowName}</span>
              </li>
            )}
            {named.map((item) => (
              <li key={item.issueTypeId}>
                <span className="text-ink">{item.issueTypeName}</span> {t.workflows.library.uses} {item.workflowName}
              </li>
            ))}
          </ul>
        </div>
        <div className="flex shrink-0 items-start gap-6">
          <Stat label={t.workflows.library.projects} value={scheme.isDefault ? t.workflows.library.theRest : scheme.projectKeys.length} />
          <Stat label={t.workflows.library.types} value={named.length} />
        </div>
      </div>
      {canConfigure && !scheme.isDefault && (
        <div className="mt-3">
          <Button size="sm" variant="ghost" loading={promote.isPending} onClick={() => promote.mutate(scheme.id)}>
            {t.workflows.library.makeDefault}
          </Button>
        </div>
      )}
      {error && <ErrorBanner>{error.message}</ErrorBanner>}
    </Card>
  );
}
