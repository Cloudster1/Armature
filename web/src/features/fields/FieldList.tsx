import { useState, type FormEvent } from "react";
import {
  useCreateField,
  useCreateOrgField,
  useDeleteField,
  useFieldKinds,
  useOrgFields,
  useProjectFields,
  usePromoteField,
  useUpdateField,
  type Field as CustomField,
  type FieldKind,
} from "@/api/fields";
import { Button, Card, EmptyState, ErrorBanner, Field, Select, Table, Tag, Td, Th, useToast } from "@/components/ui";
import { useConfirm } from "@/features/shell/ConfirmProvider";
import { parseOptions } from "./values";

/**
 * The fields a project records beyond the standard ones, the organization's
 * shared ones first. Without a project key this is the organization's own
 * list. Defining one is administration; answering it is editing the issue.
 */
export function FieldList({ projectKey, canConfigure, canPromote = false }: { projectKey?: string; canConfigure: boolean; canPromote?: boolean }) {
  const project = useProjectFields(projectKey ?? "");
  const org = useOrgFields();
  const { data, isLoading, error } = projectKey ? project : org;
  const { data: kindData } = useFieldKinds();
  const remove = useDeleteField();
  const promote = usePromoteField();
  const confirm = useConfirm();
  const toast = useToast();
  const [adding, setAdding] = useState(false);

  const fields = data?.fields ?? [];
  const kinds = kindData?.kinds ?? [];
  const titleOf = (kind: FieldKind) => kinds.find((k) => k.kind === kind)?.title ?? kind;

  if (error) return <ErrorBanner>{(error as Error).message}</ErrorBanner>;

  return (
    <div className="space-y-4">
      {canConfigure &&
        (adding ? (
          <NewFieldForm projectKey={projectKey} onDone={() => setAdding(false)} />
        ) : (
          <div className="flex justify-end">
            <Button onClick={() => setAdding(true)}>New field</Button>
          </div>
        ))}

      {(remove.error ?? promote.error) && <ErrorBanner>{((remove.error ?? promote.error) as Error).message}</ErrorBanner>}

      {isLoading ? null : fields.length === 0 && !adding ? (
        <EmptyState
          title="No custom fields"
          description={
            !projectKey
              ? "Add one and every issue in every project gets a place for it."
              : canConfigure
                ? "Add one and every issue in this project gets a place for it: a customer, a release, a cost, a link to the spec."
                : "This project records only the standard fields."
          }
        />
      ) : fields.length > 0 ? (
        <Table>
          <thead>
            <tr>
              <Th>Field</Th>
              <Th className="w-28">Kind</Th>
              <Th>Options</Th>
              {(canConfigure || canPromote) && <Th className="w-40" />}
            </tr>
          </thead>
          <tbody>
            {fields.map((field) => (
              <FieldRow
                key={field.id}
                field={field}
                kindTitle={titleOf(field.kind)}
                // An organization field on a project's page is read there and changed in Settings.
                canConfigure={canConfigure && (field.org ? !projectKey : true)}
                onDelete={async () => (await confirm({ noun: "field", verb: "Remove", body: `${field.name} goes from every issue ${field.org ? "in every project" : "in the project"}, with the answers it held.` })) && remove.mutate(field.id)}
                onPromote={
                  canPromote && !field.org
                    ? async () =>
                        (await confirm({ noun: "field", verb: "Promote", body: `${field.name} becomes every project's; a field by the same name elsewhere is folded into it and keeps its answers.` })) &&
                        promote.mutate(field.id, { onSuccess: () => toast.info(`${field.name} is now the organization's`) })
                    : undefined
                }
              />
            ))}
          </tbody>
        </Table>
      ) : null}
    </div>
  );
}

function FieldRow({
  field,
  kindTitle,
  canConfigure,
  onDelete,
  onPromote,
}: {
  field: CustomField;
  kindTitle: string;
  canConfigure: boolean;
  onDelete: () => void;
  onPromote?: () => void;
}) {
  const update = useUpdateField();
  const [editing, setEditing] = useState(false);
  const [name, setName] = useState(field.name);
  const [options, setOptions] = useState(field.options.join("\n"));

  function save(event: FormEvent) {
    event.preventDefault();
    update.mutate(
      {
        id: field.id,
        name: name.trim() !== field.name ? name : undefined,
        options: field.kind === "select" ? parseOptions(options) : undefined,
      },
      { onSuccess: () => setEditing(false) },
    );
  }

  if (editing) {
    return (
      <tr data-field={field.name}>
        <Td colSpan={canConfigure ? 4 : 3}>
          <form onSubmit={save} className="space-y-2">
            {update.error && <ErrorBanner>{(update.error as Error).message}</ErrorBanner>}
            <div className="grid gap-3 sm:grid-cols-2">
              <Field
                id={`field-name-${field.id}`}
                label="Field name"
                value={name}
                onChange={(e) => setName(e.target.value)}
                required
              />
              {field.kind === "select" && (
                <Field label="Options, one per line" id={`field-options-${field.id}`} value={options} onChange={(e) => setOptions(e.target.value)} rows={3} />
              )}
            </div>
            <div className="flex gap-2">
              <Button type="submit" size="sm" loading={update.isPending}>
                Save field
              </Button>
              <Button type="button" size="sm" variant="ghost" onClick={() => setEditing(false)}>
                Cancel
              </Button>
            </div>
          </form>
        </Td>
      </tr>
    );
  }

  return (
    <tr data-field={field.name} data-field-scope={field.org ? "org" : "project"}>
      <Td className="font-medium text-ink">
        {field.name}
        {field.org && <Tag className="ml-2">Organization</Tag>}
      </Td>
      <Td className="text-ink-muted">{kindTitle}</Td>
      <Td>
        <span className="flex flex-wrap gap-1">
          {field.options.map((option) => (
            <Tag key={option}>{option}</Tag>
          ))}
        </span>
      </Td>
      {(canConfigure || onPromote) && (
        <Td className="text-right">
          <span className="flex justify-end gap-1">
            {onPromote && (
              <Button size="sm" variant="ghost" onClick={onPromote} data-action="promote-field">
                Promote
              </Button>
            )}
            {canConfigure && (
              <>
                <Button size="sm" variant="ghost" onClick={() => setEditing(true)}>
                  Edit
                </Button>
                <Button size="sm" variant="ghost" onClick={onDelete} aria-label={`Remove ${field.name}`}>
                  Remove
                </Button>
              </>
            )}
          </span>
        </Td>
      )}
    </tr>
  );
}

function NewFieldForm({ projectKey, onDone }: { projectKey?: string; onDone: () => void }) {
  const forProject = useCreateField();
  const forOrg = useCreateOrgField();
  const create = projectKey ? forProject : forOrg;
  const { data: kindData } = useFieldKinds();
  const kinds = kindData?.kinds ?? [];
  const [name, setName] = useState("");
  const [kind, setKind] = useState<FieldKind>("text");
  const [options, setOptions] = useState("");

  const hasOptions = kinds.find((k) => k.kind === kind)?.hasOptions ?? false;

  function onSubmit(event: FormEvent) {
    event.preventDefault();
    if (!name.trim()) return;
    const body = { name, kind, options: hasOptions ? parseOptions(options) : undefined };
    const done = {
      onSuccess: () => {
        setName("");
        setOptions("");
        onDone();
      },
    };
    if (projectKey) forProject.mutate({ projectKey, ...body }, done);
    else forOrg.mutate(body, done);
  }

  return (
    <Card className="p-4">
      <form onSubmit={onSubmit} className="space-y-3" noValidate>
        {create.error && <ErrorBanner>{(create.error as Error).message}</ErrorBanner>}
        <div className="grid gap-3 sm:grid-cols-[1fr_12rem]">
          <Field
            label="Field name"
            autoFocus
            required
            value={name}
            onChange={(e) => setName(e.target.value)}
            placeholder="Customer"
          />
          <Select id="field-kind" label="Kind" value={kind} onChange={(e) => setKind(e.target.value as FieldKind)}>
            {kinds.map((k) => (
              <option key={k.kind} value={k.kind}>
                {k.title}
              </option>
            ))}
          </Select>
        </div>
        <p className="text-sm text-ink-subtle">{kinds.find((k) => k.kind === kind)?.description}</p>
        {hasOptions && (
          <Field label="Options, one per line" id="field-options" value={options} onChange={(e) => setOptions(e.target.value)} rows={4} placeholder={"Web\nMobile\nDesktop"} />
        )}
        <div className="flex gap-2">
          <Button type="submit" loading={create.isPending} disabled={!name.trim()}>
            Create field
          </Button>
          <Button type="button" variant="ghost" onClick={onDone}>
            Cancel
          </Button>
        </div>
      </form>
    </Card>
  );
}
