import { useState, type FormEvent } from "react";
import { useComponents, useCreateComponent, useDeleteComponent, useUpdateComponent, type Component } from "@/api/versions";
import { Button, Card, Drawer, EmptyState, ErrorBanner, Field, IconButton, Menu, Table, Td, Th } from "@/components/ui";
import { Icon } from "@/components/icons";
import { useConfirm } from "@/features/shell/ConfirmProvider";
import { PersonPicker, type Chosen } from "@/features/people/PersonPicker";
import { t } from "@/i18n";

// A project's parts, each with who looks after it and, when it says so, who
// new work in it goes to.
export function ComponentList({ projectKey, canEdit }: { projectKey: string; canEdit: boolean }) {
  const { data, isLoading, error } = useComponents(projectKey);
  const remove = useDeleteComponent();
  const confirm = useConfirm();
  const [editing, setEditing] = useState<Component | null>(null);
  const components = data?.components ?? [];

  if (error) return <ErrorBanner>{(error as Error).message}</ErrorBanner>;
  return (
    <div className="space-y-4">
      {canEdit && <NewComponent projectKey={projectKey} />}
      {isLoading ? null : components.length === 0 ? (
        <EmptyState title={t.schedule.components.empty} description={t.schedule.components.emptyDetail} />
      ) : (
        <Table>
          <thead>
            <tr>
              <Th>{t.schedule.components.component}</Th>
              <Th>{t.schedule.components.lead}</Th>
              <Th>{t.schedule.components.newWorkTo}</Th>
              <Th>{t.schedule.components.issues}</Th>
              {canEdit && <Th className="w-12" />}
            </tr>
          </thead>
          <tbody>
            {components.map((c) => (
              <tr key={c.id} data-component={c.name}>
                <Td>
                  <span className="block font-medium text-ink">{c.name}</span>
                  {c.description && <span className="block text-sm text-ink-muted">{c.description}</span>}
                </Td>
                <Td className="text-sm text-ink-muted">{c.lead?.name ?? t.schedule.components.nobody}</Td>
                <Td className="text-sm text-ink-muted" data-component-assignee={c.defaultAssignee?.name ?? ""}>
                  {c.defaultAssignee?.name ?? t.schedule.components.whoeverFiles}
                </Td>
                <Td className="text-sm text-ink-muted">{c.issues}</Td>
                {canEdit && (
                  <Td>
                    <Menu
                      label={t.schedule.releases.actionsFor(c.name)}
                      align="end"
                      trigger={(props) => (
                        <IconButton
                          icon={<Icon.More />}
                          label={t.schedule.releases.actionsFor(c.name)}
                          size="sm"
                          onClick={props.toggle}
                          aria-haspopup={props["aria-haspopup"]}
                          aria-expanded={props["aria-expanded"]}
                          data-component-menu={c.name}
                        />
                      )}
                      items={[
                        { label: t.schedule.components.edit, icon: <Icon.Edit />, onSelect: () => setEditing(c), attrs: { "data-action": "component-edit" } },
                        {
                          label: t.schedule.components.delete,
                          icon: <Icon.Trash />,
                          danger: true,
                          onSelect: async () =>
                            (await confirm({
                              noun: t.kit.confirm.things.component,
                              verb: t.kit.confirm.verbs.delete,
                              body: t.schedule.components.deleteBody(c.name),
                            })) && remove.mutate(c.id),
                          attrs: { "data-action": "component-delete" },
                        },
                      ]}
                    />
                  </Td>
                )}
              </tr>
            ))}
          </tbody>
        </Table>
      )}
      {editing && <EditComponent component={editing} onClose={() => setEditing(null)} />}
    </div>
  );
}

function PersonSelect({
  label,
  id,
  value,
  onChange,
  selected,
}: {
  label: string;
  id: string;
  value: string;
  onChange: (id: string) => void;
  selected?: Chosen;
}) {
  return <PersonPicker label={label} id={id} value={value} onChange={onChange} none={t.schedule.components.nobodyOption} staff selected={selected} />;
}

function NewComponent({ projectKey }: { projectKey: string }) {
  const create = useCreateComponent(projectKey);
  const [name, setName] = useState("");
  const [lead, setLead] = useState("");
  const [assignee, setAssignee] = useState("");

  function submit(event: FormEvent) {
    event.preventDefault();
    if (!name.trim()) return;
    create.mutate(
      { name: name.trim(), leadId: lead || undefined, defaultAssigneeId: assignee || undefined },
      {
        onSuccess: () => {
          setName("");
          setLead("");
          setAssignee("");
        },
      },
    );
  }

  return (
    <Card className="p-4" data-guide="new-component">
      <form onSubmit={submit} className="flex flex-wrap items-end gap-3" data-testid="new-component">
        <div className="min-w-40 flex-1">
          <Field
            label={t.schedule.components.component}
            id="field-component-name"
            value={name}
            placeholder={t.schedule.components.placeholder}
            onChange={(e) => setName(e.target.value)}
          />
        </div>
        <div className="min-w-40">
          <PersonSelect label={t.schedule.components.lead} id="field-component-lead" value={lead} onChange={setLead} />
        </div>
        <div className="min-w-40">
          <PersonSelect label={t.schedule.components.newWorkTo} id="field-component-assignee" value={assignee} onChange={setAssignee} />
        </div>
        <Button type="submit" loading={create.isPending} disabled={!name.trim()}>
          {t.schedule.components.add}
        </Button>
      </form>
      {create.error && (
        <div className="mt-3">
          <ErrorBanner>{(create.error as Error).message}</ErrorBanner>
        </div>
      )}
    </Card>
  );
}

function EditComponent({ component, onClose }: { component: Component; onClose: () => void }) {
  const update = useUpdateComponent();
  const [name, setName] = useState(component.name);
  const [description, setDescription] = useState(component.description ?? "");
  const [lead, setLead] = useState(component.lead?.id ?? "");
  const [assignee, setAssignee] = useState(component.defaultAssignee?.id ?? "");

  function submit(event: FormEvent) {
    event.preventDefault();
    update.mutate(
      {
        id: component.id,
        name: name.trim(),
        description: description.trim(),
        leadId: lead || undefined,
        clearLead: !lead,
        defaultAssigneeId: assignee || undefined,
        clearDefaultAssignee: !assignee,
      },
      { onSuccess: onClose },
    );
  }

  return (
    <Drawer open onClose={onClose} title={t.schedule.releases.editTitle(component.name)} attrs={{ "data-component-editor": component.name }}>
      <form onSubmit={submit} className="space-y-4" noValidate>
        {update.error && <ErrorBanner>{(update.error as Error).message}</ErrorBanner>}
        <Field label={t.schedule.components.name} id="field-edit-component-name" required value={name} onChange={(e) => setName(e.target.value)} />
        <Field
          label={t.schedule.components.whatItIs}
          id="field-edit-component-description"
          value={description}
          onChange={(e) => setDescription(e.target.value)}
        />
        <PersonSelect label={t.schedule.components.lead} id="field-edit-component-lead" value={lead} onChange={setLead} selected={component.lead} />
        <PersonSelect
          label={t.schedule.components.newWorkTo}
          id="field-edit-component-assignee"
          value={assignee}
          onChange={setAssignee}
          selected={component.defaultAssignee}
        />
        <Button type="submit" loading={update.isPending} disabled={!name.trim()}>
          {t.schedule.components.save}
        </Button>
      </form>
    </Drawer>
  );
}
