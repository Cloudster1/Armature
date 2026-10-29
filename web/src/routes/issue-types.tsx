import { useState, type FormEvent } from "react";
import { Link, createRoute } from "@tanstack/react-router";
import { adminRoute } from "./admin";
import { useIssueTypes, type IssueType, type IssueTypeIcon } from "@/api/issues";
import { useCreateIssueType, useDeleteIssueType, useReinstateIssueType, useRetireIssueType, useUpdateIssueType } from "@/api/issueTypes";
import {
  Button,
  Card,
  Choice,
  Dialog,
  EmptyState,
  ErrorBanner,
  Field,
  IconButton,
  Menu,
  Page,
  PageHeader,
  Select,
  Table,
  Tag,
  Td,
  Th,
  useToast,
} from "@/components/ui";
import { Icon } from "@/components/icons";
import { ISSUE_TYPE_DESCRIPTION_MAX_LENGTH, ISSUE_TYPE_LEVELS, ISSUE_TYPE_NAME_MAX_LENGTH } from "@/config";
import { TYPE_ICONS, TypeBadge } from "@/features/issues/badges";
import { useConfirm } from "@/features/shell/ConfirmProvider";
import { t } from "@/i18n";

export const issueTypesRoute = createRoute({
  getParentRoute: () => adminRoute,
  path: "/issue-types",
  component: IssueTypesPage,
});

/** The installation's library of issue types, retired ones included, for its administrators to keep. */
function IssueTypesPage() {
  const { data, isLoading, error } = useIssueTypes({ retired: true });
  const types = data?.issueTypes ?? [];
  const [editing, setEditing] = useState<IssueType | null>(null);
  const retire = useRetireIssueType();
  const reinstate = useReinstateIssueType();
  const remove = useDeleteIssueType();
  const confirm = useConfirm();
  const toast = useToast();

  async function onDelete(type: IssueType) {
    if (await confirm({ noun: t.kit.confirm.things.issueType, verb: t.kit.confirm.verbs.delete, body: t.looks.issueTypes.deleteBody(type.name) })) {
      remove.mutate(type.id, { onSuccess: () => toast.success(t.looks.issueTypes.deleted(type.name)) });
    }
  }

  return (
    <Page width="narrow">
      <PageHeader
        crumb={
          <Link to="/admin" className="hover:text-ink">
            {t.shell.admin.crumb}
          </Link>
        }
        title={t.looks.issueTypes.title}
        meta={t.looks.issueTypes.meta}
      />
      {error && <ErrorBanner>{(error as Error).message}</ErrorBanner>}
      {/* A refused retirement or deletion is the shell's toast, as on every table. */}
      <NewTypeForm />
      {isLoading ? null : types.length === 0 ? (
        <EmptyState title={t.looks.issueTypes.none} description={t.looks.issueTypes.noneDetail} />
      ) : (
        <Card>
          <Table>
            <thead>
              <tr>
                <Th>{t.looks.issueTypes.columnType}</Th>
                <Th className="w-48">{t.looks.issueTypes.columnLevel}</Th>
                <Th className="w-10" />
              </tr>
            </thead>
            <tbody>
              {types.map((type) => (
                <tr key={type.id} data-issue-type-row={type.name} data-retired={type.retired || undefined}>
                  <Td>
                    <span className="flex flex-wrap items-center gap-2">
                      <TypeBadge icon={type.icon} name={type.name} />
                      <span className={type.retired ? "text-ink-muted" : "text-ink"}>{type.name}</span>
                      {type.builtin && <Tag data-builtin-type>{t.looks.issueTypes.builtIn}</Tag>}
                      {type.retired && <Tag data-retired-type>{t.looks.issueTypes.retired}</Tag>}
                    </span>
                    {type.description && <span className="mt-0.5 block text-xs text-ink-muted">{type.description}</span>}
                  </Td>
                  <Td className="text-sm text-ink-muted">{t.looks.issueTypes.levels[String(type.level)] ?? type.level}</Td>
                  <Td className="text-right">
                    <Menu
                      label={t.looks.issueTypes.actionsFor(type.name)}
                      align="end"
                      trigger={(props) => (
                        <IconButton
                          icon={<Icon.More />}
                          label={t.looks.issueTypes.actionsFor(type.name)}
                          size="sm"
                          onClick={props.toggle}
                          aria-haspopup={props["aria-haspopup"]}
                          aria-expanded={props["aria-expanded"]}
                          data-action="issue-type-menu"
                        />
                      )}
                      items={[
                        {
                          label: t.looks.issueTypes.edit,
                          icon: <Icon.Edit />,
                          onSelect: () => setEditing(type),
                          attrs: { "data-action": "edit-issue-type" },
                        },
                        type.retired
                          ? {
                              label: t.looks.issueTypes.reinstate,
                              icon: <Icon.Check />,
                              onSelect: () => reinstate.mutate(type.id, { onSuccess: () => toast.success(t.looks.issueTypes.reinstatedNow(type.name)) }),
                              attrs: { "data-action": "reinstate-issue-type" },
                            }
                          : {
                              label: t.looks.issueTypes.retire,
                              icon: <Icon.Archive />,
                              disabled: type.builtin,
                              onSelect: () => retire.mutate(type.id, { onSuccess: () => toast.success(t.looks.issueTypes.retiredNow(type.name)) }),
                              attrs: { "data-action": "retire-issue-type" },
                            },
                        {
                          label: t.looks.issueTypes.delete,
                          icon: <Icon.Trash />,
                          danger: true,
                          disabled: type.builtin,
                          onSelect: () => void onDelete(type),
                          attrs: { "data-action": "delete-issue-type" },
                        },
                      ]}
                    />
                  </Td>
                </tr>
              ))}
            </tbody>
          </Table>
        </Card>
      )}
      {editing && <EditTypeDialog type={editing} onClose={() => setEditing(null)} />}
    </Page>
  );
}

function NewTypeForm() {
  const create = useCreateIssueType();
  const toast = useToast();
  const [name, setName] = useState("");
  const [icon, setIcon] = useState<IssueTypeIcon>("task");
  const [level, setLevel] = useState(0);

  function onSubmit(event: FormEvent) {
    event.preventDefault();
    if (!name.trim()) return;
    create.mutate(
      { name: name.trim(), icon, level },
      {
        onSuccess: ({ issueType }) => {
          setName("");
          toast.success(t.looks.issueTypes.made(issueType.name));
        },
      },
    );
  }

  return (
    <form onSubmit={onSubmit} className="mb-4 space-y-3" noValidate data-new-issue-type>
      <div className="flex flex-wrap items-end gap-3">
        <Field
          label={t.looks.issueTypes.newType}
          placeholder={t.looks.issueTypes.newPlaceholder}
          value={name}
          maxLength={ISSUE_TYPE_NAME_MAX_LENGTH}
          onChange={(e) => setName(e.target.value)}
          className="w-56"
        />
        <LevelSelect id="new-issue-type-level" value={level} onChange={setLevel} />
        <Button type="submit" loading={create.isPending} disabled={!name.trim()}>
          {t.looks.issueTypes.add}
        </Button>
      </div>
      <IconPicker value={icon} onChange={setIcon} />
      {create.error && <ErrorBanner>{(create.error as Error).message}</ErrorBanner>}
    </form>
  );
}

function EditTypeDialog({ type, onClose }: { type: IssueType; onClose: () => void }) {
  const update = useUpdateIssueType();
  const [name, setName] = useState(type.name);
  const [description, setDescription] = useState(type.description ?? "");
  const [icon, setIcon] = useState<IssueTypeIcon>(type.icon);
  const [level, setLevel] = useState(type.level);

  function onSubmit(event: FormEvent) {
    event.preventDefault();
    if (!name.trim()) return;
    // Only what changed is sent, so a built-in type is never asked to keep a name it already has.
    update.mutate(
      {
        id: type.id,
        name: name.trim() !== type.name ? name.trim() : undefined,
        description: description.trim() !== (type.description ?? "") ? description.trim() : undefined,
        icon: icon !== type.icon ? icon : undefined,
        level: level !== type.level ? level : undefined,
      },
      { onSuccess: onClose },
    );
  }

  return (
    <Dialog open onClose={onClose} title={t.looks.issueTypes.editTitle(type.name)}>
      <form onSubmit={onSubmit} className="space-y-3" noValidate data-edit-issue-type>
        {type.builtin && <p className="text-sm text-ink-muted">{t.looks.issueTypes.builtinKeeps}</p>}
        <Field
          id="issue-type-name"
          label={t.looks.issueTypes.name}
          autoFocus
          value={name}
          disabled={type.builtin}
          maxLength={ISSUE_TYPE_NAME_MAX_LENGTH}
          onChange={(e) => setName(e.target.value)}
        />
        <Field
          id="issue-type-description"
          label={t.looks.issueTypes.description}
          rows={2}
          value={description}
          maxLength={ISSUE_TYPE_DESCRIPTION_MAX_LENGTH}
          onChange={(e) => setDescription(e.target.value)}
        />
        <LevelSelect id="issue-type-level" value={level} onChange={setLevel} disabled={type.builtin} />
        <IconPicker value={icon} onChange={setIcon} />
        {update.error && <ErrorBanner>{(update.error as Error).message}</ErrorBanner>}
        <div className="flex justify-end gap-2">
          <Button type="button" variant="secondary" onClick={onClose}>
            {t.looks.issueTypes.cancel}
          </Button>
          <Button type="submit" loading={update.isPending} disabled={!name.trim()}>
            {t.looks.issueTypes.save}
          </Button>
        </div>
      </form>
    </Dialog>
  );
}

function LevelSelect({ id, value, onChange, disabled }: { id: string; value: number; onChange: (level: number) => void; disabled?: boolean }) {
  return (
    <Select
      id={id}
      label={t.looks.issueTypes.level}
      value={String(value)}
      disabled={disabled}
      onChange={(e) => onChange(Number(e.target.value))}
      className="w-64"
    >
      {ISSUE_TYPE_LEVELS.map((level) => (
        <option key={level} value={level}>
          {t.looks.issueTypes.levels[String(level)]}
        </option>
      ))}
    </Select>
  );
}

function IconPicker({ value, onChange }: { value: IssueTypeIcon; onChange: (icon: IssueTypeIcon) => void }) {
  return (
    <div className="space-y-1">
      <span className="block text-sm font-medium text-ink-muted">{t.looks.issueTypes.icon}</span>
      <span className="flex flex-wrap gap-1" role="radiogroup" aria-label={t.looks.issueTypes.icon}>
        {TYPE_ICONS.map((option) => (
          <Choice
            key={option}
            checked={option === value}
            aria-label={t.looks.issueTypes.icons[option] ?? option}
            title={t.looks.issueTypes.icons[option] ?? option}
            onSelect={() => onChange(option)}
            className={option === value ? "rounded p-1 ring-2 ring-accent" : "rounded p-1"}
            data-icon={option}
          >
            <TypeBadge icon={option} name={t.looks.issueTypes.icons[option] ?? option} />
          </Choice>
        ))}
      </span>
    </div>
  );
}
