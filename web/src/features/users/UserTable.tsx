import { useEffect, useState, type FormEvent } from "react";
import { useMe } from "@/api/auth";
import { useManagedUsers, useSetUserPassword, useUpdateUser, type ManagedUser } from "@/api/users";
import {
  Button,
  Dialog,
  EmptyState,
  ErrorBanner,
  Field,
  IconButton,
  Menu,
  Pager,
  Select,
  Table,
  Tag,
  Td,
  Th,
  clampOffset,
  countOf,
  useToast,
} from "@/components/ui";
import { Icon } from "@/components/icons";
import { useConfirm } from "@/features/shell/ConfirmProvider";
import { WorkingWeekDialog } from "@/features/availability/WorkingWeek";
import { useRemoveMembersSecondFactor } from "@/api/secondFactor";
import { PeopleSearch } from "@/features/people/PeopleSearch";
import { PEOPLE_PAGE_SIZE } from "@/config";
import { t } from "@/i18n";

// Read while rendering, so the words follow the language.
const signsInWord = (way: ManagedUser["signsInWith"]): string =>
  way === "password" ? t.access.users.password : way === "provider" ? t.access.users.provider : t.access.users.nothingYet;
const roleWord = (role: ManagedUser["role"]): string =>
  role === "owner" ? t.access.users.owner : role === "admin" ? t.access.invite.admin : role === "member" ? t.access.invite.member : t.access.invite.customer;

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
      <PeopleSearch label={t.access.users.find} value={q} onChange={(text) => show({ q: text })} />
      {data && (
        <span className="text-sm text-ink-muted" data-users-total={data.total}>
          {countOf(data.total, t.kit.counted.account)}
        </span>
      )}
    </div>
  );

  if (error) return <ErrorBanner>{(error as Error).message}</ErrorBanner>;
  if (isLoading) return <p className="text-sm text-ink-muted">{t.access.loading}</p>;
  if (users.length === 0) {
    return (
      <div className="space-y-3">
        {header}
        {q ? (
          <EmptyState title={t.access.members.noMatch} description={t.access.members.noMatchDetail} />
        ) : (
          <EmptyState title={t.access.members.empty} description={t.access.users.emptyDetail} />
        )}
      </div>
    );
  }

  async function switchOff(user: ManagedUser) {
    if (
      await confirm({
        noun: t.kit.confirm.things.account,
        verb: t.kit.confirm.verbs.deactivate,
        body: t.access.users.deactivateBody(user.name),
      })
    ) {
      update.mutate({ id: user.id, isActive: false }, { onSuccess: () => toast.success(t.access.users.deactivated(user.name)) });
    }
  }

  // For somebody who lost both the phone and the recovery codes; they sign in with the password alone after.
  async function turnOffSecondFactor(user: ManagedUser) {
    if (
      await confirm({
        noun: t.kit.confirm.things.twoStepSignIn,
        verb: t.kit.confirm.verbs.turnOff,
        body: t.access.users.secondFactorOffBody(user.name),
      })
    ) {
      removeFactor.mutate(user.id, { onSuccess: () => toast.success(t.access.users.secondFactorOff(user.name)) });
    }
  }

  return (
    <div className="space-y-3">
      {header}
      {/* A row's refusal is the shell's toast, as on every table: a banner up here is a screen away from a row far down. */}
      <Table>
        <thead>
          <tr>
            <Th>{t.access.newUser.name}</Th>
            <Th>{t.access.newUser.email}</Th>
            <Th>{t.access.users.signsIn}</Th>
            <Th>{t.access.grants.role}</Th>
            <Th>{t.access.users.state}</Th>
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
                  {signsInWord(user.signsInWith)}
                  {user.secondFactor && (
                    <Tag className="ml-2" data-user-second-factor>
                      {t.access.users.twoStep}
                    </Tag>
                  )}
                </Td>
                <Td>
                  <Tag>{roleWord(user.role)}</Tag>
                </Td>
                <Td>
                  <Tag className={user.isActive ? "" : "text-danger"}>{user.isActive ? t.access.users.active : t.access.users.off}</Tag>
                </Td>
                <Td className="text-right">
                  <Menu
                    label={t.access.matrix.actionsFor(user.name)}
                    align="end"
                    trigger={(props) => (
                      <IconButton
                        icon={<Icon.More />}
                        label={t.access.matrix.actionsFor(user.name)}
                        size="sm"
                        onClick={props.toggle}
                        aria-haspopup={props["aria-haspopup"]}
                        aria-expanded={props["aria-expanded"]}
                        data-action="user-menu"
                      />
                    )}
                    items={[
                      {
                        label: t.access.users.rename,
                        icon: <Icon.Edit />,
                        disabled: locked,
                        onSelect: () => setRenaming(user),
                        attrs: { "data-action": "rename-user" },
                      },
                      {
                        label: t.access.users.changeRole,
                        icon: <Icon.Key />,
                        disabled: locked || user.role === "owner",
                        onSelect: () => setChangingRole(user),
                        attrs: { "data-action": "change-role" },
                      },
                      {
                        label: t.access.users.setPassword,
                        icon: <Icon.Shield />,
                        disabled: locked || user.signsInWith === "provider",
                        onSelect: () => setSettingPassword(user),
                        attrs: { "data-action": "set-password" },
                      },
                      {
                        label: t.access.users.turnOffSecondFactor,
                        icon: <Icon.Key />,
                        disabled: locked || !user.secondFactor,
                        onSelect: () => void turnOffSecondFactor(user),
                        attrs: { "data-action": "remove-second-factor" },
                      },
                      // Hours worked are not the account's, so a shared account's week is set here too; a customer has none.
                      {
                        label: t.access.users.workingWeek,
                        icon: <Icon.Calendar />,
                        disabled: user.role === "customer",
                        onSelect: () => setSettingWeek(user),
                        attrs: { "data-action": "working-week" },
                      },
                      user.isActive
                        ? {
                            label: t.access.users.deactivate,
                            icon: <Icon.EyeOff />,
                            danger: true,
                            disabled: locked,
                            onSelect: () => void switchOff(user),
                            attrs: { "data-action": "deactivate-user" },
                          }
                        : {
                            label: t.access.users.reactivate,
                            icon: <Icon.Eye />,
                            disabled: locked,
                            onSelect: () =>
                              update.mutate({ id: user.id, isActive: true }, { onSuccess: () => toast.success(t.access.users.reactivated(user.name)) }),
                            attrs: { "data-action": "reactivate-user" },
                          },
                    ]}
                  />
                </Td>
              </tr>
            );
          })}
        </tbody>
      </Table>
      {data && (
        <Pager
          label={t.access.users.pages}
          total={data.total}
          offset={data.offset}
          pageSize={PEOPLE_PAGE_SIZE}
          onChange={(next) => show({ q, offset: next })}
        />
      )}

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
    update.mutate(
      { id: user.id, name: name.trim() },
      {
        onSuccess: () => {
          toast.success(t.access.users.renamed(name.trim()));
          onClose();
        },
      },
    );
  }
  return (
    <Dialog open onClose={onClose} title={t.access.matrix.renameTitle(user.name)} attrs={{ "data-rename-user": user.email }}>
      <form onSubmit={onSubmit} className="space-y-3" noValidate>
        <Field label={t.access.users.newName} autoFocus value={name} onChange={(e) => setName(e.target.value)} />
        {update.error && <ErrorBanner>{(update.error as Error).message}</ErrorBanner>}
        <div className="flex justify-end gap-2">
          <Button type="button" variant="secondary" onClick={onClose}>
            {t.kit.cancel}
          </Button>
          <Button type="submit" loading={update.isPending} disabled={!name.trim()}>
            {t.access.users.rename}
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
    update.mutate(
      { id: user.id, role },
      {
        onSuccess: () => {
          toast.success(t.access.users.nowRole(user.name, roleWord(role)));
          onClose();
        },
      },
    );
  }
  return (
    <Dialog
      open
      onClose={onClose}
      title={t.access.users.changeRoleTitle(user.name)}
      description={t.access.users.changeRoleDetail}
      attrs={{ "data-change-role": user.email }}
    >
      <form onSubmit={onSubmit} className="space-y-3" noValidate>
        <Select label={t.access.users.newRole} value={role} onChange={(e) => setRole(e.target.value as "admin" | "member")}>
          <option value="member">{t.access.invite.member}</option>
          <option value="admin">{t.access.invite.admin}</option>
        </Select>
        {update.error && <ErrorBanner>{(update.error as Error).message}</ErrorBanner>}
        <div className="flex justify-end gap-2">
          <Button type="button" variant="secondary" onClick={onClose}>
            {t.kit.cancel}
          </Button>
          <Button type="submit" loading={update.isPending}>
            {t.access.users.changeRole}
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
    set.mutate(
      { id: user.id, password },
      {
        onSuccess: () => {
          toast.success(t.access.users.passwordSet(user.name));
          onClose();
        },
      },
    );
  }
  return (
    <Dialog
      open
      onClose={onClose}
      title={t.access.users.passwordTitle(user.name)}
      description={t.access.users.passwordDetail}
      attrs={{ "data-set-password": user.email }}
    >
      <form onSubmit={onSubmit} className="space-y-3" noValidate>
        <Field
          label={t.access.users.newPassword}
          type="password"
          autoComplete="new-password"
          autoFocus
          hint={t.access.users.newPasswordHint}
          value={password}
          onChange={(e) => setPassword(e.target.value)}
        />
        {set.error && <ErrorBanner>{(set.error as Error).message}</ErrorBanner>}
        <div className="flex justify-end gap-2">
          <Button type="button" variant="secondary" onClick={onClose}>
            {t.kit.cancel}
          </Button>
          <Button type="submit" loading={set.isPending} disabled={!password}>
            {t.access.users.setPassword}
          </Button>
        </div>
      </form>
    </Dialog>
  );
}
