import { useState, type FormEvent } from "react";
import { useAccess, useCreateRole, useDeleteRole, usePermissions, useRoles, useUpdateRole, type RoleDescription } from "@/api/access";
import { Button, Checkbox, Dialog, ErrorBanner, Field, IconButton, Menu, Skeleton, Switch, Table, Tag, Td, Th, useToast } from "@/components/ui";
import { Icon } from "@/components/icons";
import { useConfirm } from "@/features/shell/ConfirmProvider";
import { t } from "@/i18n";

/**
 * What each role grants, as a grid: permissions down, roles across, a box
 * at each crossing. A box is saved the moment it is ticked, and every grant
 * of the role follows from the next request. The five built-in roles can be
 * changed but not removed; a role the organization added can go, with its
 * grants.
 */
export function RoleMatrix() {
  const { data: roleData, isLoading, error } = useRoles();
  const { data: permissionData } = usePermissions();
  const { data: access } = useAccess();
  const update = useUpdateRole();
  const remove = useDeleteRole();
  const confirm = useConfirm();
  const toast = useToast();
  const [adding, setAdding] = useState(false);
  const [renaming, setRenaming] = useState<RoleDescription | null>(null);
  const roles = roleData?.roles ?? [];
  const permissions = permissionData?.permissions ?? [];
  const administers = access?.canAdministerOrg ?? false;
  const failure = (error ?? update.error ?? remove.error) as Error | undefined;

  function toggle(role: RoleDescription, permission: string, on: boolean) {
    const next = on ? [...role.permissions, permission] : role.permissions.filter((p) => p !== permission);
    update.mutate({ role: role.role, permissions: next });
  }

  async function drop(role: RoleDescription) {
    if (await confirm({ noun: t.kit.confirm.things.role, verb: t.kit.confirm.verbs.delete, body: t.access.matrix.deleteBody(role.name, role.inUse) })) {
      remove.mutate(role.role, { onSuccess: () => toast.success(t.access.matrix.deleted(role.name)) });
    }
  }

  if (isLoading) return <Skeleton lines={4} />;

  return (
    <div className="space-y-4" data-role-matrix>
      <div className="flex flex-wrap items-center justify-between gap-3">
        <p className="text-sm text-ink-muted">{t.access.matrix.explain}</p>
        {administers && (
          <Button icon={<Icon.Plus />} onClick={() => setAdding(true)} data-action="new-role">
            {t.access.matrix.newRole}
          </Button>
        )}
      </div>
      {failure && <ErrorBanner>{failure.message}</ErrorBanner>}
      <Table>
        <thead>
          <tr>
            <Th>{t.access.matrix.permission}</Th>
            {roles.map((role) => (
              <Th key={role.role} className="text-center align-top" data-role-column={role.role}>
                <span className="inline-flex flex-col items-center gap-1 normal-case tracking-normal">
                  <span className="whitespace-nowrap text-xs font-medium text-ink" title={role.description}>
                    {role.name}
                  </span>
                  <span className="inline-flex items-center gap-1">
                    {role.builtin && <Tag className="whitespace-nowrap">{t.access.matrix.builtIn}</Tag>}
                    {administers && (
                      <Menu
                        label={t.access.matrix.actionsFor(role.name)}
                        align="end"
                        trigger={(props) => (
                          <IconButton
                            icon={<Icon.More />}
                            label={t.access.matrix.actionsFor(role.name)}
                            size="xs"
                            onClick={props.toggle}
                            aria-haspopup={props["aria-haspopup"]}
                            aria-expanded={props["aria-expanded"]}
                            data-action="role-menu"
                          />
                        )}
                        items={[
                          { label: t.access.matrix.rename, icon: <Icon.Edit />, onSelect: () => setRenaming(role), attrs: { "data-action": "rename-role" } },
                          {
                            label: t.access.delete,
                            icon: <Icon.Trash />,
                            danger: true,
                            disabled: role.builtin,
                            onSelect: () => void drop(role),
                            attrs: { "data-action": "delete-role" },
                          },
                        ]}
                      />
                    )}
                  </span>
                </span>
              </Th>
            ))}
          </tr>
        </thead>
        <tbody>
          {permissions.map((permission) => (
            <tr key={permission.permission} data-permission-row={permission.permission}>
              <Td>
                <span className="text-ink">{permission.words}</span>
                <span className="ml-2 font-mono text-2xs text-ink-subtle">{permission.permission}</span>
              </Td>
              {roles.map((role) => {
                const held = role.permissions.includes(permission.permission);
                // The one tick that cannot come off: without it nobody could put it back.
                const fixed = role.role === "global_administrator" && permission.permission === "org.administer";
                return (
                  <Td key={role.role} className="text-center">
                    <Checkbox
                      label={
                        <span className="sr-only">{t.access.matrix.may(role.name, permission.words.charAt(0).toLowerCase() + permission.words.slice(1))}</span>
                      }
                      checked={held}
                      disabled={!administers || fixed || update.isPending}
                      onChange={(event) => toggle(role, permission.permission, event.target.checked)}
                      data-permission-cell={`${role.role}:${permission.permission}`}
                    />
                  </Td>
                );
              })}
            </tr>
          ))}
        </tbody>
      </Table>
      {adding && <NewRoleDialog onClose={() => setAdding(false)} />}
      {renaming && <RenameRoleDialog role={renaming} onClose={() => setRenaming(null)} />}
    </div>
  );
}

function NewRoleDialog({ onClose }: { onClose: () => void }) {
  const create = useCreateRole();
  const toast = useToast();
  const [name, setName] = useState("");
  const [description, setDescription] = useState("");
  const [orgWideOnly, setOrgWideOnly] = useState(false);
  function submit(event: FormEvent) {
    event.preventDefault();
    if (!name.trim()) return;
    create.mutate(
      { name: name.trim(), description: description.trim(), orgWideOnly, permissions: ["read"] },
      {
        onSuccess: (made) => {
          toast.success(t.access.matrix.added(made.role.name));
          onClose();
        },
      },
    );
  }
  return (
    <Dialog open onClose={onClose} title={t.access.matrix.newRole} attrs={{ "data-new-role": "" }}>
      <form onSubmit={submit} className="space-y-3" noValidate>
        <Field label={t.access.matrix.name} id="field-role-name" autoFocus value={name} onChange={(e) => setName(e.target.value)} />
        <Field label={t.access.matrix.purpose} id="field-role-description" value={description} onChange={(e) => setDescription(e.target.value)} />
        <label className="flex items-center gap-2 text-sm text-ink-muted">
          <Switch checked={orgWideOnly} onChange={setOrgWideOnly} label={t.access.matrix.allAtOnce} data-role-org-wide={orgWideOnly ? "true" : "false"} />
          {t.access.matrix.allAtOnceDetail}
        </label>
        <p className="text-xs text-ink-subtle">{t.access.matrix.startsWith}</p>
        {create.error && <ErrorBanner>{(create.error as Error).message}</ErrorBanner>}
        <div className="flex justify-end gap-2">
          <Button type="button" variant="secondary" onClick={onClose}>
            {t.kit.cancel}
          </Button>
          <Button type="submit" loading={create.isPending} disabled={!name.trim()}>
            {t.access.matrix.addRole}
          </Button>
        </div>
      </form>
    </Dialog>
  );
}

function RenameRoleDialog({ role, onClose }: { role: RoleDescription; onClose: () => void }) {
  const update = useUpdateRole();
  const toast = useToast();
  const [name, setName] = useState(role.name);
  const [description, setDescription] = useState(role.description);
  function submit(event: FormEvent) {
    event.preventDefault();
    if (!name.trim()) return;
    update.mutate(
      { role: role.role, name: name.trim(), description: description.trim() },
      {
        onSuccess: () => {
          toast.success(t.access.matrix.saved(name.trim()));
          onClose();
        },
      },
    );
  }
  return (
    <Dialog open onClose={onClose} title={t.access.matrix.renameTitle(role.name)} attrs={{ "data-rename-role": role.role }}>
      <form onSubmit={submit} className="space-y-3" noValidate>
        <Field label={t.access.matrix.name} id="field-role-name" autoFocus value={name} onChange={(e) => setName(e.target.value)} />
        <Field label={t.access.matrix.purpose} id="field-role-description" value={description} onChange={(e) => setDescription(e.target.value)} />
        {update.error && <ErrorBanner>{(update.error as Error).message}</ErrorBanner>}
        <div className="flex justify-end gap-2">
          <Button type="button" variant="secondary" onClick={onClose}>
            {t.kit.cancel}
          </Button>
          <Button type="submit" loading={update.isPending} disabled={!name.trim()}>
            {t.access.save}
          </Button>
        </div>
      </form>
    </Dialog>
  );
}
