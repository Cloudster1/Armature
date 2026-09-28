import { useState, type FormEvent } from "react";
import { useAccess, useCreateRole, useDeleteRole, usePermissions, useRoles, useUpdateRole, type RoleDescription } from "@/api/access";
import { Button, Checkbox, Dialog, ErrorBanner, Field, IconButton, Menu, Switch, Table, Tag, Td, Th, useToast } from "@/components/ui";
import { Icon } from "@/components/icons";
import { useConfirm } from "@/features/shell/ConfirmProvider";

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
    const grants = role.inUse === 0 ? "Nobody holds it." : role.inUse === 1 ? "The one grant of it goes with it." : `Its ${role.inUse} grants go with it.`;
    if (await confirm({ noun: "role", verb: "Delete", body: `${role.name} goes for good. ${grants}` })) {
      remove.mutate(role.role, { onSuccess: () => toast.success(`Deleted ${role.name}`) });
    }
  }

  if (isLoading) return <p className="text-sm text-ink-muted">Loading...</p>;

  return (
    <div className="space-y-4" data-role-matrix>
      <div className="flex flex-wrap items-center justify-between gap-3">
        <p className="text-sm text-ink-muted">A tick is saved at once, and everybody holding the role has it from their next request. The built-in roles stay; the global administrator always administers the organization.</p>
        {administers && (
          <Button icon={<Icon.Plus />} onClick={() => setAdding(true)} data-action="new-role">
            New role
          </Button>
        )}
      </div>
      {failure && <ErrorBanner>{failure.message}</ErrorBanner>}
      <Table>
        <thead>
          <tr>
            <Th>Permission</Th>
            {roles.map((role) => (
              <Th key={role.role} className="text-center align-top" data-role-column={role.role}>
                <span className="inline-flex flex-col items-center gap-1 normal-case tracking-normal">
                  <span className="whitespace-nowrap text-xs font-medium text-ink" title={role.description}>{role.name}</span>
                  <span className="inline-flex items-center gap-1">
                  {role.builtin && <Tag className="whitespace-nowrap">Built in</Tag>}
                  {administers && (
                    <Menu
                      label={`Actions for ${role.name}`}
                      align="end"
                      trigger={(props) => <IconButton icon={<Icon.More />} label={`Actions for ${role.name}`} size="xs" onClick={props.toggle} aria-haspopup={props["aria-haspopup"]} aria-expanded={props["aria-expanded"]} data-action="role-menu" />}
                      items={[
                        { label: "Rename or describe", icon: <Icon.Edit />, onSelect: () => setRenaming(role), attrs: { "data-action": "rename-role" } },
                        { label: "Delete", icon: <Icon.Trash />, danger: true, disabled: role.builtin, onSelect: () => void drop(role), attrs: { "data-action": "delete-role" } },
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
                      label={<span className="sr-only">{`${role.name} may ${permission.words.toLowerCase()}`}</span>}
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
      { onSuccess: (made) => { toast.success(`Added ${made.role.name}; tick what it grants`); onClose(); } },
    );
  }
  return (
    <Dialog open onClose={onClose} title="New role" attrs={{ "data-new-role": "" }}>
      <form onSubmit={submit} className="space-y-3" noValidate>
        <Field label="Role name" id="field-role-name" autoFocus value={name} onChange={(e) => setName(e.target.value)} />
        <Field label="What it is for" id="field-role-description" value={description} onChange={(e) => setDescription(e.target.value)} />
        <label className="flex items-center gap-2 text-sm text-ink-muted">
          <Switch checked={orgWideOnly} onChange={setOrgWideOnly} label="Only over the whole organization" data-role-org-wide={orgWideOnly ? "true" : "false"} />
          Only over the whole organization, never one project
        </label>
        <p className="text-xs text-ink-subtle">It starts with seeing projects; tick the rest in the matrix.</p>
        {create.error && <ErrorBanner>{(create.error as Error).message}</ErrorBanner>}
        <div className="flex justify-end gap-2">
          <Button type="button" variant="secondary" onClick={onClose}>
            Cancel
          </Button>
          <Button type="submit" loading={create.isPending} disabled={!name.trim()}>
            Add role
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
    update.mutate({ role: role.role, name: name.trim(), description: description.trim() }, { onSuccess: () => { toast.success(`Saved ${name.trim()}`); onClose(); } });
  }
  return (
    <Dialog open onClose={onClose} title={`Rename ${role.name}`} attrs={{ "data-rename-role": role.role }}>
      <form onSubmit={submit} className="space-y-3" noValidate>
        <Field label="Role name" id="field-role-name" autoFocus value={name} onChange={(e) => setName(e.target.value)} />
        <Field label="What it is for" id="field-role-description" value={description} onChange={(e) => setDescription(e.target.value)} />
        {update.error && <ErrorBanner>{(update.error as Error).message}</ErrorBanner>}
        <div className="flex justify-end gap-2">
          <Button type="button" variant="secondary" onClick={onClose}>
            Cancel
          </Button>
          <Button type="submit" loading={update.isPending} disabled={!name.trim()}>
            Save
          </Button>
        </div>
      </form>
    </Dialog>
  );
}
