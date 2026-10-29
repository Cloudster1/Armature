import { useState, type FormEvent } from "react";
import { groupLabel, useAddToGroup, useCreateGroup, useDeleteGroup, useGroup, useGroups, useRemoveFromGroup, type Group } from "@/api/access";
import { Button, Card, EmptyState, ErrorBanner, Field } from "@/components/ui";
import { useConfirm } from "@/features/shell/ConfirmProvider";
import { PersonPicker } from "@/features/people/PersonPicker";
import { t } from "@/i18n";

/**
 * Groups, and who is in them.
 *
 * A group whose members come from the identity provider is shown but not
 * edited: changing it here would last until the person's next sign-in, which is
 * worse than not offering it.
 */
export function GroupList() {
  const { data, isLoading, error } = useGroups();
  const groups = data?.groups ?? [];

  if (error) return <ErrorBanner>{(error as Error).message}</ErrorBanner>;

  return (
    <div className="space-y-4">
      <NewGroup />

      {isLoading && <p className="text-sm text-ink-muted">{t.access.loading}</p>}
      {!isLoading && groups.every((group) => group.builtin) && <EmptyState title={t.access.groups.empty} description={t.access.groups.emptyDetail} />}

      <div className="space-y-3">
        {groups.map((group) => (group.builtin ? <BuiltinGroupCard key={group.id} group={group} /> : <GroupCard key={group.id} group={group} />))}
      </div>
    </div>
  );
}

/**
 * Everyone, whose members are the installation's: listed by count, since it
 * holds every member, and never edited here.
 */
function BuiltinGroupCard({ group }: { group: Group }) {
  return (
    <Card className="p-4" data-group={group.name} data-group-builtin={group.builtin}>
      <h3 className="flex flex-wrap items-center gap-x-2 gap-y-1 text-sm font-semibold text-ink">
        <span className="break-words">{groupLabel(group)}</span>
        <span className="rounded-full bg-surface-raised px-2 py-0.5 text-2xs font-medium whitespace-nowrap text-ink-muted">{t.access.groups.builtin}</span>
      </h3>
      <p className="mt-0.5 text-xs text-ink-muted">
        {t.access.groups.people(group.memberCount)} · {t.access.groups.roles(group.roleCount)}
      </p>
      <p className="mt-2 text-sm text-ink-muted">{t.access.groups.everyoneDetail}</p>
    </Card>
  );
}

function GroupCard({ group }: { group: Group }) {
  const { data } = useGroup(group.id);
  const add = useAddToGroup();
  const remove = useRemoveFromGroup();
  const deleteGroup = useDeleteGroup();
  const confirm = useConfirm();
  const [picked, setPicked] = useState("");

  const members = data?.group?.members ?? [];
  const fromProvider = group.source === "oidc";
  const error = (add.error ?? remove.error ?? deleteGroup.error) as Error | undefined;

  return (
    <Card className="p-4" data-group={group.name}>
      <div className="flex items-start justify-between gap-3">
        <div className="min-w-0">
          <h3 className="flex flex-wrap items-center gap-x-2 gap-y-1 text-sm font-semibold text-ink">
            <span className="break-words">{group.name}</span>
            {fromProvider && (
              <span className="rounded-full bg-surface-raised px-2 py-0.5 text-2xs font-medium whitespace-nowrap text-ink-muted">
                {t.access.groups.fromProvider}
              </span>
            )}
          </h3>
          <p className="mt-0.5 text-xs text-ink-muted">
            {t.access.groups.people(group.memberCount)} · {t.access.groups.roles(group.roleCount)}
            {group.externalRef && t.access.groups.matches(group.externalRef)}
          </p>
        </div>
        <Button
          size="sm"
          variant="ghost"
          className="shrink-0"
          loading={deleteGroup.isPending}
          onClick={async () =>
            (await confirm({ noun: t.kit.confirm.things.group, body: t.access.groups.deleteBody(group.name) })) && deleteGroup.mutate(group.id)
          }
        >
          {t.access.delete}
        </Button>
      </div>

      <ul className="mt-2 space-y-1 text-sm">
        {members.map((member) => (
          <li key={member.userId} className="flex items-center gap-2">
            <span className="min-w-0 truncate text-ink">{member.name}</span>
            {!fromProvider && (
              <Button
                size="sm"
                variant="ghost"
                className="ml-auto h-6 px-1.5 text-2xs"
                onClick={async () =>
                  (await confirm({
                    noun: t.kit.confirm.things.member,
                    verb: t.kit.confirm.verbs.remove,
                    body: t.access.groups.removeBody(member.name, group.name),
                  })) && remove.mutate({ id: group.id, userId: member.userId })
                }
              >
                {t.access.remove}
              </Button>
            )}
          </li>
        ))}
        {members.length === 0 && <li className="text-sm text-ink-subtle">{fromProvider ? t.access.groups.nobodyFromProvider : t.access.groups.nobody}</li>}
      </ul>

      {!fromProvider && (
        <div className="mt-3 flex flex-wrap items-center gap-2">
          <PersonPicker
            id={`group-add-${group.id}`}
            aria-label={t.access.groups.addTo(group.name)}
            none={t.access.groups.addSomebody}
            staff
            exclude={members.map((member) => member.userId)}
            value={picked}
            onChange={setPicked}
          />
          <Button
            size="sm"
            disabled={!picked}
            loading={add.isPending}
            onClick={() => {
              add.mutate({ id: group.id, userId: picked });
              setPicked("");
            }}
          >
            {t.access.add}
          </Button>
        </div>
      )}

      {error && <ErrorBanner>{error.message}</ErrorBanner>}
    </Card>
  );
}

function NewGroup() {
  const create = useCreateGroup();
  const [name, setName] = useState("");
  const [ref, setRef] = useState("");

  function onSubmit(event: FormEvent) {
    event.preventDefault();
    if (!name.trim()) return;
    create.mutate(
      { name: name.trim(), externalRef: ref.trim() || undefined },
      {
        onSuccess: () => {
          setName("");
          setRef("");
        },
      },
    );
  }

  // The hint sits under the whole row rather than under its field, so the two
  // inputs and the button line up however wide the page is.
  return (
    <Card className="p-4">
      <form onSubmit={onSubmit} className="space-y-3">
        <div className="grid gap-3 sm:grid-cols-[minmax(0,1fr)_minmax(0,1fr)_auto] sm:items-end">
          <Field
            label={t.access.groups.newGroup}
            value={name}
            placeholder={t.access.groups.newGroupPlaceholder}
            onChange={(event) => setName(event.target.value)}
          />
          <Field label={t.access.groups.claim} value={ref} placeholder={t.access.optional} onChange={(event) => setRef(event.target.value)} />
          <Button type="submit" loading={create.isPending} disabled={!name.trim()}>
            {t.access.groups.create}
          </Button>
        </div>
        <p className="text-xs text-ink-subtle">{t.access.groups.claimHint}</p>
        {create.error && <ErrorBanner>{(create.error as Error).message}</ErrorBanner>}
      </form>
    </Card>
  );
}
