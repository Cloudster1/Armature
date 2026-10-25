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
            aria-label="Find schemes"
            placeholder="Find by name"
            value={q}
            onChange={(text) => onShow({ q: text })}
            className="w-56"
            data-library-search=""
          />
          {data && <span className="text-sm text-ink-muted tabular-nums">{shownOf(data.total, data.offset, schemes.length, t.kit.counted.scheme)}</span>}
        </div>
        {canConfigure && !editing.open && (
          <Button size="sm" variant="secondary" onClick={() => setEditing({ open: true })}>
            New scheme
          </Button>
        )}
      </div>
      {editing.open && <SchemeEditor scheme={editing.scheme} onDone={() => setEditing({ open: false })} />}
      {data && schemes.length === 0 && q && (
        <EmptyState icon={<Icon.Search />} title="No scheme matches that" description="Try part of its name, or clear the search." />
      )}
      {schemes.map((scheme) => (
        <SchemeCard key={scheme.id} scheme={scheme} canConfigure={canConfigure} onEdit={() => setEditing({ open: true, scheme })} />
      ))}
      {data && (
        <Pager label="Scheme pages" total={data.total} offset={data.offset} pageSize={LIBRARY_PAGE_SIZE} onChange={(next) => onShow({ q, offset: next })} />
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
    ? "Used by every project that has not named one of its own."
    : scheme.projectKeys.length > 0
      ? `Used by ${scheme.projectKeys.join(", ")}.`
      : "No project uses this yet.";
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
            <IconButton icon={<Icon.Edit />} label={`Edit ${scheme.name}`} size="sm" variant="ghost" data-action="edit" onClick={onEdit} />
            {!scheme.isDefault && (
              <IconButton
                icon={<Icon.Trash />}
                label={`Delete ${scheme.name}`}
                size="sm"
                variant="ghost"
                data-action="delete"
                aria-busy={remove.isPending}
                onClick={async () =>
                  (await confirm({ noun: t.kit.confirm.things.scheme, body: `${scheme.name} goes; projects using it fall back to the default.` })) &&
                  remove.mutate(scheme.id)
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
            {scheme.isDefault && <Tag className="text-accent">Default</Tag>}
          </p>
          <p className="mt-0.5 text-xs text-ink-muted">{usedBy}</p>
          <ul className="mt-2 space-y-0.5 text-sm text-ink-muted">
            {fallback && (
              <li>
                Everything else uses <span className="text-ink">{fallback.workflowName}</span>
              </li>
            )}
            {named.map((item) => (
              <li key={item.issueTypeId}>
                <span className="text-ink">{item.issueTypeName}</span> uses {item.workflowName}
              </li>
            ))}
          </ul>
        </div>
        <div className="flex shrink-0 items-start gap-6">
          <Stat label="Projects" value={scheme.isDefault ? "the rest" : scheme.projectKeys.length} />
          <Stat label="Types" value={named.length} />
        </div>
      </div>
      {canConfigure && !scheme.isDefault && (
        <div className="mt-3">
          <Button size="sm" variant="ghost" loading={promote.isPending} onClick={() => promote.mutate(scheme.id)}>
            Make it the default
          </Button>
        </div>
      )}
      {error && <ErrorBanner>{error.message}</ErrorBanner>}
    </Card>
  );
}
