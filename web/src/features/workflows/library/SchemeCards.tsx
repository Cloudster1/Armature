import { useState } from "react";
import { useDeleteScheme, useSchemes, useSetDefaultScheme, useWorkflows, type Scheme } from "@/api/workflows";
import { Button, Card, ErrorBanner, IconButton, Stat, Tag } from "@/components/ui";
import { Icon } from "@/components/icons";
import { SchemeEditor } from "@/features/workflows/SchemeEditor";
import { useConfirm } from "@/features/shell/ConfirmProvider";

/** The schemes that hand workflows to issue types, one card each. */
export function SchemeCards({ canConfigure }: { canConfigure: boolean }) {
  const { data } = useSchemes();
  const { data: workflowData } = useWorkflows();
  const [editing, setEditing] = useState<{ open: boolean; scheme?: Scheme }>({ open: false });
  const schemes = data?.schemes ?? [];
  const workflows = workflowData?.workflows ?? [];

  return (
    <section className="space-y-3">
      {canConfigure && !editing.open && (
        <div className="flex justify-end">
          <Button size="sm" variant="secondary" onClick={() => setEditing({ open: true })}>
            New scheme
          </Button>
        </div>
      )}
      {editing.open && <SchemeEditor scheme={editing.scheme} workflows={workflows} onDone={() => setEditing({ open: false })} />}
      {schemes.map((scheme) => (
        <SchemeCard key={scheme.id} scheme={scheme} canConfigure={canConfigure} onEdit={() => setEditing({ open: true, scheme })} />
      ))}
    </section>
  );
}

function SchemeCard({ scheme, canConfigure, onEdit }: { scheme: Scheme; canConfigure: boolean; onEdit: () => void }) {
  const promote = useSetDefaultScheme();
  const remove = useDeleteScheme();
  const confirm = useConfirm();
  const error = (promote.error ?? remove.error) as Error | undefined;
  const usedBy = scheme.isDefault ? "Used by every project that has not named one of its own." : scheme.projectKeys.length > 0 ? `Used by ${scheme.projectKeys.join(", ")}.` : "No project uses this yet.";
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
                onClick={async () => (await confirm({ noun: "scheme", body: `${scheme.name} goes; projects using it fall back to the organization's.` })) && remove.mutate(scheme.id)}
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
            {scheme.isDefault && <Tag className="text-accent">The organization's</Tag>}
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
            Make it the organization's
          </Button>
        </div>
      )}
      {error && <ErrorBanner>{error.message}</ErrorBanner>}
    </Card>
  );
}
