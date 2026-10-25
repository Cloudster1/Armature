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
import { t } from "@/i18n";

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
            <Button onClick={() => setAdding(true)}>{t.looks.fields.newField}</Button>
          </div>
        ))}

      {(remove.error ?? promote.error) && <ErrorBanner>{((remove.error ?? promote.error) as Error).message}</ErrorBanner>}

      {isLoading ? null : fields.length === 0 && !adding ? (
        <EmptyState
          title={t.looks.fields.noneTitle}
          description={!projectKey ? t.looks.fields.noneEveryone : canConfigure ? t.looks.fields.noneProject : t.looks.fields.noneReadOnly}
        />
      ) : fields.length > 0 ? (
        <Table>
          <thead>
            <tr>
              <Th>{t.looks.fields.columnField}</Th>
              <Th className="w-28">{t.looks.fields.columnKind}</Th>
              <Th>{t.looks.fields.columnOptions}</Th>
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
                onDelete={async () =>
                  (await confirm({
                    noun: t.kit.confirm.things.field,
                    verb: t.kit.confirm.verbs.remove,
                    body: t.looks.fields.removeBody(field.name, field.org),
                  })) && remove.mutate(field.id)
                }
                onPromote={
                  canPromote && !field.org
                    ? async () =>
                        (await confirm({
                          noun: t.kit.confirm.things.field,
                          verb: t.kit.confirm.verbs.promote,
                          body: t.looks.fields.promoteBody(field.name),
                        })) && promote.mutate(field.id, { onSuccess: () => toast.info(t.looks.fields.promoted(field.name)) })
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
              <Field id={`field-name-${field.id}`} label={t.looks.fields.name} value={name} onChange={(e) => setName(e.target.value)} required />
              {field.kind === "select" && (
                <Field label={t.looks.fields.options} id={`field-options-${field.id}`} value={options} onChange={(e) => setOptions(e.target.value)} rows={3} />
              )}
            </div>
            <div className="flex gap-2">
              <Button type="submit" size="sm" loading={update.isPending}>
                {t.looks.fields.save}
              </Button>
              <Button type="button" size="sm" variant="ghost" onClick={() => setEditing(false)}>
                {t.kit.cancel}
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
        {field.org && <Tag className="ml-2">{t.looks.fields.allProjects}</Tag>}
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
                {t.looks.fields.promote}
              </Button>
            )}
            {canConfigure && (
              <>
                <Button size="sm" variant="ghost" onClick={() => setEditing(true)}>
                  {t.looks.fields.edit}
                </Button>
                <Button size="sm" variant="ghost" onClick={onDelete} aria-label={t.looks.fields.removeNamed(field.name)}>
                  {t.looks.fields.remove}
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
            label={t.looks.fields.name}
            autoFocus
            required
            value={name}
            onChange={(e) => setName(e.target.value)}
            placeholder={t.looks.fields.namePlaceholder}
          />
          <Select id="field-kind" label={t.looks.fields.kind} value={kind} onChange={(e) => setKind(e.target.value as FieldKind)}>
            {kinds.map((k) => (
              <option key={k.kind} value={k.kind}>
                {k.title}
              </option>
            ))}
          </Select>
        </div>
        <p className="text-sm text-ink-subtle">{kinds.find((k) => k.kind === kind)?.description}</p>
        {hasOptions && (
          <Field
            label={t.looks.fields.options}
            id="field-options"
            value={options}
            onChange={(e) => setOptions(e.target.value)}
            rows={4}
            placeholder={t.looks.fields.optionsPlaceholder}
          />
        )}
        <div className="flex gap-2">
          <Button type="submit" loading={create.isPending} disabled={!name.trim()}>
            {t.looks.fields.create}
          </Button>
          <Button type="button" variant="ghost" onClick={onDone}>
            {t.kit.cancel}
          </Button>
        </div>
      </form>
    </Card>
  );
}
