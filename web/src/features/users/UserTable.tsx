import { useEffect, useState, type FormEvent } from "react";
import { useMe } from "@/api/auth";
import { useManagedUsers, useSetUserPassword, useUpdateUser, type ManagedUser } from "@/api/users";
import { Button, Dialog, EmptyState, ErrorBanner, Field, IconButton, Menu, Pager, Select, Table, Tag, Td, Th, clampOffset, countOf, useToast } from "@/components/ui";
import { Icon } from "@/components/icons";
import { useConfirm } from "@/features/shell/ConfirmProvider";
import { WorkingWeekDialog } from "@/features/availability/WorkingWeek";
import { useRemoveMembersSecondFactor } from "@/api/secondFactor";
import { PeopleSearch } from "@/features/people/PeopleSearch";
import { PEOPLE_PAGE_SIZE } from "@/config";

const signsInWords: Record<ManagedUser["signsInWith"], string> = { password: "Password", provider: "Identity provider", none: "Nothing yet" };
const roleWords: Record<ManagedUser["role"], string> = { owner: "Owner", admin: "Administrator", member: "Member", customer: "Customer" };

/** What the users page shows: a search and where in its matches the page starts. */
export interface UsersShown {
  q?: string;
  offset?: number;
}

/**
 * Everybody in the installation a page at a time, and the tools for their accounts:
 * letting somebody go is switching them off here, and switching on brings them back.
 */
export function UserTable({ q = "", offset = 0, show }: UsersShown & { show: (next: UsersShown, replace?: boolean) => void }) {
  const { data, isLoading, error } = useManagedUsers({ q, offset });
  const { data: me } = useMe();
  const update = useUpdateUser();
  const removeFactor = useRemoveMembersSecondFactor();
  const confirm = useConfirm();
  const toast = useToast();
  const [renaming, setRenaming] = useState<ManagedUser | null>(null);
  const [changingRole, setChangingRole] = useState<ManagedUser | null>(null);
  const [settingPassword, setSettingPassword] = useState<ManagedUser | null>(null);
  const [settingWeek, setSettingWeek] = useState<ManagedUser | null>(null);
  const users = data?.users ?? [];
  const myID = me?.principal?.user.id;

  // People leave the matches while the page is open; a page past the end steps back.
  useEffect(() => {
    if (data && offset > 0 && offset >= data.total) show({ q, offset: clampOffset(offset, data.total, PEOPLE_PAGE_SIZE) }, true);
  }, [data, offset, q, show]);

  const header = (
    <div className="flex flex-wrap items-center justify-between gap-2">
      <PeopleSearch label="Find users" value={q} onChange={(text) => show({ q: text })} />
      {data && <span className="text-sm text-ink-muted" data-users-total={data.total}>{countOf(data.total, "account")}</span>}
    </div>
  );

  if (error) return <ErrorBanner>{(error as Error).message}</ErrorBanner>;
  if (isLoading) return <p className="text-sm text-ink-muted">Loading...</p>;
  if (users.length === 0) {
    return (
      <div className="space-y-3">
        {header}
        {q ? (
          <EmptyState title="Nobody matches that" description="Try part of their name or their address, or clear the search." />
        ) : (
          <EmptyState title="Nobody here yet" description="Make an account above, or invite somebody under Access." />
        )}
      </div>
    );
  }

  async function switchOff(user: ManagedUser) {
    if (await confirm({ noun: "account", verb: "Deactivate", body: `${user.name} can no longer sign in, and every session and token of theirs ends. What they wrote stays.` })) {
      update.mutate({ id: user.id, isActive: false }, { onSuccess: () => toast.success(`Deactivated ${user.name}`) });
    }
  }

  // For somebody who lost both the phone and the recovery codes; they sign in with the password alone after.
  async function turnOffSecondFactor(user: ManagedUser) {
    if (await confirm({ noun: "two-step sign-in", verb: "Turn off", body: `${user.name} signs in with the password alone until they set it up again. Do this only when you are sure it is them asking.` })) {
      removeFactor.mutate(user.id, { onSuccess: () => toast.success(`Turned off two-step sign-in for ${user.name}`) });
    }
  }

  return (
    <div className="space-y-3">
      {header}
      {update.error && <ErrorBanner>{(update.error as Error).message}</ErrorBanner>}
      {removeFactor.error && <ErrorBanner>{(removeFactor.error as Error).message}</ErrorBanner>}
      <Table>
        <thead>
          <tr>
            <Th>Name</Th>
            <Th>Email</Th>
            <Th>Signs in with</Th>
            <Th>Role</Th>
            <Th>State</Th>
            <Th className="w-10" />
          </tr>
        </thead>
        <tbody>
          {users.map((user) => {
            const mine = user.id === myID;
            const locked = !user.managed || mine;
            return (
              <tr key={user.id} data-user={user.email} data-user-active={user.isActive} data-user-managed={user.managed}>
                <Td className="text-ink">{user.name}</Td>
                <Td className="text-ink-muted">{user.email}</Td>
                <Td className="text-ink-muted">
                  {signsInWords[user.signsInWith]}
                  {user.secondFactor && (
                    <Tag className="ml-2" data-user-second-factor>
                      Two-step
                    </Tag>
                  )}
                </Td>
                <Td>
                  <Tag>{roleWords[user.role]}</Tag>
                </Td>
                <Td>
                  <Tag className={user.isActive ? "" : "text-danger"}>{user.isActive ? "Active" : "Off"}</Tag>
                </Td>
                <Td className="text-right">
                  <Menu
                    label={`Actions for ${user.name}`}
                    align="end"
                    trigger={(props) => (
                      <IconButton
                        icon={<Icon.More />}
                        label={`Actions for ${user.name}`}
                        size="sm"
                        onClick={props.toggle}
                        aria-haspopup={props["aria-haspopup"]}
                        aria-expanded={props["aria-expanded"]}
                        data-action="user-menu"
                      />
                    )}
                    items={[
                      { label: "Rename", icon: <Icon.Edit />, disabled: locked, onSelect: () => setRenaming(user), attrs: { "data-action": "rename-user" } },
                      { label: "Change role", icon: <Icon.Key />, disabled: locked || user.role === "owner", onSelect: () => setChangingRole(user), attrs: { "data-action": "change-role" } },
                      { label: "Set password", icon: <Icon.Shield />, disabled: locked || user.signsInWith === "provider", onSelect: () => setSettingPassword(user), attrs: { "data-action": "set-password" } },
                      { label: "Turn off two-step sign-in", icon: <Icon.Key />, disabled: locked || !user.secondFactor, onSelect: () => void turnOffSecondFactor(user), attrs: { "data-action": "remove-second-factor" } },
                      // Hours worked are not the account's, so a shared account's week is set here too; a customer has none.
                      { label: "Working week", icon: <Icon.Calendar />, disabled: user.role === "customer", onSelect: () => setSettingWeek(user), attrs: { "data-action": "working-week" } },
                      user.isActive
                        ? { label: "Deactivate", icon: <Icon.EyeOff />, danger: true, disabled: locked, onSelect: () => void switchOff(user), attrs: { "data-action": "deactivate-user" } }
                        : { label: "Reactivate", icon: <Icon.Eye />, disabled: locked, onSelect: () => update.mutate({ id: user.id, isActive: true }, { onSuccess: () => toast.success(`Reactivated ${user.name}`) }), attrs: { "data-action": "reactivate-user" } },
                    ]}
                  />
                </Td>
              </tr>
            );
          })}
        </tbody>
      </Table>
      {data && <Pager label="User pages" total={data.total} offset={data.offset} pageSize={PEOPLE_PAGE_SIZE} onChange={(next) => show({ q, offset: next })} />}

      {renaming && <RenameDialog user={renaming} onClose={() => setRenaming(null)} />}
      {changingRole && <RoleDialog user={changingRole} onClose={() => setChangingRole(null)} />}
      {settingPassword && <PasswordDialog user={settingPassword} onClose={() => setSettingPassword(null)} />}
      {settingWeek && <WorkingWeekDialog userId={settingWeek.id} name={settingWeek.name} onClose={() => setSettingWeek(null)} />}
    </div>
  );
}

function RenameDialog({ user, onClose }: { user: ManagedUser; onClose: () => void }) {
  const update = useUpdateUser();
  const toast = useToast();
  const [name, setName] = useState(user.name);
  function onSubmit(event: FormEvent) {
    event.preventDefault();
    if (!name.trim()) return;
    update.mutate({ id: user.id, name: name.trim() }, { onSuccess: () => { toast.success(`Renamed to ${name.trim()}`); onClose(); } });
  }
  return (
    <Dialog open onClose={onClose} title={`Rename ${user.name}`} attrs={{ "data-rename-user": user.email }}>
      <form onSubmit={onSubmit} className="space-y-3" noValidate>
        <Field label="New name" autoFocus value={name} onChange={(e) => setName(e.target.value)} />
        {update.error && <ErrorBanner>{(update.error as Error).message}</ErrorBanner>}
        <div className="flex justify-end gap-2">
          <Button type="button" variant="secondary" onClick={onClose}>
            Cancel
          </Button>
          <Button type="submit" loading={update.isPending} disabled={!name.trim()}>
            Rename
          </Button>
        </div>
      </form>
    </Dialog>
  );
}

function RoleDialog({ user, onClose }: { user: ManagedUser; onClose: () => void }) {
  const update = useUpdateUser();
  const toast = useToast();
  const [role, setRole] = useState<"admin" | "member">(user.role === "admin" ? "admin" : "member");
  function onSubmit(event: FormEvent) {
    event.preventDefault();
    update.mutate({ id: user.id, role }, { onSuccess: () => { toast.success(`${user.name} is now ${roleWords[role].toLowerCase()}`); onClose(); } });
  }
  return (
    <Dialog open onClose={onClose} title={`Change ${user.name}'s role`} description="An administrator holds every key to Armature; a member starts as a user in every project." attrs={{ "data-change-role": user.email }}>
      <form onSubmit={onSubmit} className="space-y-3" noValidate>
        <Select label="New role" value={role} onChange={(e) => setRole(e.target.value as "admin" | "member")}>
          <option value="member">Member</option>
          <option value="admin">Administrator</option>
        </Select>
        {update.error && <ErrorBanner>{(update.error as Error).message}</ErrorBanner>}
        <div className="flex justify-end gap-2">
          <Button type="button" variant="secondary" onClick={onClose}>
            Cancel
          </Button>
          <Button type="submit" loading={update.isPending}>
            Change role
          </Button>
        </div>
      </form>
    </Dialog>
  );
}

function PasswordDialog({ user, onClose }: { user: ManagedUser; onClose: () => void }) {
  const set = useSetUserPassword();
  const toast = useToast();
  const [password, setPassword] = useState("");
  function onSubmit(event: FormEvent) {
    event.preventDefault();
    if (!password) return;
    set.mutate({ id: user.id, password }, { onSuccess: () => { toast.success(`Set a new password for ${user.name}`); onClose(); } });
  }
  return (
    <Dialog open onClose={onClose} title={`Set a password for ${user.name}`} description="Every session they have ends. Tell them the password another way, and ask them to change it from their profile." attrs={{ "data-set-password": user.email }}>
      <form onSubmit={onSubmit} className="space-y-3" noValidate>
        <Field label="New password" type="password" autoComplete="new-password" autoFocus hint="At least 12 characters." value={password} onChange={(e) => setPassword(e.target.value)} />
        {set.error && <ErrorBanner>{(set.error as Error).message}</ErrorBanner>}
        <div className="flex justify-end gap-2">
          <Button type="button" variant="secondary" onClick={onClose}>
            Cancel
          </Button>
          <Button type="submit" loading={set.isPending} disabled={!password}>
            Set password
          </Button>
        </div>
      </form>
    </Dialog>
  );
}
