import { useState, type FormEvent } from "react";
import { Link, createRoute } from "@tanstack/react-router";
import { appRoute } from "./app";
import { useAccess } from "@/api/access";
import { LABEL_COLORS, useDeleteLabel, useLabels, useUpdateLabel, type Label } from "@/api/labels";
import type { LabelColor } from "@/api/issues";
import { Button, Choice, EmptyState, ErrorBanner, Field, Page, PageHeader, Table, Td, Th } from "@/components/ui";
import { useConfirm } from "@/features/shell/ConfirmProvider";
import { LabelChip } from "@/features/labels/LabelChip";

export const labelsRoute = createRoute({
  getParentRoute: () => appRoute,
  path: "/settings/labels",
  component: LabelsPage,
});

/**
 * The organization's words. They are coined where they are used, on an
 * issue; this page is where one is renamed or recoloured everywhere at once,
 * or retired.
 */
function LabelsPage() {
  const { data, isLoading, error } = useLabels();
  const { data: access } = useAccess();
  const remove = useDeleteLabel();
  const confirm = useConfirm();
  const canAdminister = access?.canAdministerOrg ?? false;
  const labels = data?.labels ?? [];

  return (
    <Page width="narrow">
      <PageHeader
        crumb={
          <Link to="/settings" className="hover:text-ink">
            Settings
          </Link>
        }
        title="Labels" meta="Words shared by every project. A label is coined the first time somebody puts it on an issue." />
      {error && <ErrorBanner>{(error as Error).message}</ErrorBanner>}
      {remove.error && <ErrorBanner>{(remove.error as Error).message}</ErrorBanner>}
      {isLoading ? null : labels.length === 0 ? (
        <EmptyState title="No labels yet" description="Type a word into the Labels field on any issue and it appears here." />
      ) : (
        <Table>
          <thead>
            <tr>
              <Th>Label</Th>
              <Th className="w-24 text-right">Issues</Th>
              {canAdminister && <Th className="w-40" />}
            </tr>
          </thead>
          <tbody>
            {labels.map((label) => (
              <LabelRow key={label.id} label={label} canAdminister={canAdminister} onDelete={async () => (await confirm({ noun: "label", verb: "Remove", body: `${label.name} comes off every issue carrying it.` })) && remove.mutate(label.id)} />
            ))}
          </tbody>
        </Table>
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
            <Field id={`label-name-${label.id}`} label="Name" value={name} onChange={(e) => setName(e.target.value)} required className="w-48" />
            <div className="space-y-1">
              <span className="block text-sm font-medium text-ink-muted">Colour</span>
              <span className="flex gap-1" role="radiogroup" aria-label="Colour">
                {LABEL_COLORS.map((option) => (
                  <Choice key={option} checked={option === color} aria-label={option} onSelect={() => setColor(option)} className={option === color ? "rounded ring-2 ring-accent" : "rounded"}>
                    <LabelChip label={{ name: option, color: option }} />
                  </Choice>
                ))}
              </span>
            </div>
            <Button type="submit" size="sm" loading={update.isPending}>
              Save label
            </Button>
            <Button type="button" size="sm" variant="ghost" onClick={() => setEditing(false)}>
              Cancel
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
              Edit
            </Button>
            <Button size="sm" variant="ghost" aria-label={`Remove label ${label.name}`} onClick={onDelete}>
              Remove
            </Button>
          </span>
        </Td>
      )}
    </tr>
  );
}
