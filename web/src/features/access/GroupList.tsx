import { useState, type FormEvent } from "react";
import { useMembers } from "@/api/issues";
import {
  useAddToGroup,
  useCreateGroup,
  useDeleteGroup,
  useGroup,
  useGroups,
  useRemoveFromGroup,
  type Group,
} from "@/api/access";
import { Button, Card, EmptyState, ErrorBanner, Field, SelectInput } from "@/components/ui";
import { useConfirm } from "@/features/shell/ConfirmProvider";

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

      {isLoading && <p className="text-sm text-ink-muted">Loading...</p>}
      {!isLoading && groups.length === 0 && (
        <EmptyState
          title="No groups yet"
          description="A group is a name for a set of people, so that a role can be given once instead of to everybody in turn."
        />
      )}

      <div className="space-y-3">
        {groups.map((group) => (
          <GroupCard key={group.id} group={group} />
        ))}
      </div>
    </div>
  );
}

function GroupCard({ group }: { group: Group }) {
  const { data } = useGroup(group.id);
  const { data: memberData } = useMembers();
  const add = useAddToGroup();
  const remove = useRemoveFromGroup();
  const deleteGroup = useDeleteGroup();
  const confirm = useConfirm();
  const [picked, setPicked] = useState("");

  const members = data?.group?.members ?? [];
  const inGroup = new Set(members.map((member) => member.userId));
  const available = (memberData?.members ?? []).filter(
    (person) => !inGroup.has(person.id) && person.role !== "customer",
  );
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
                from the identity provider
              </span>
            )}
          </h3>
          <p className="mt-0.5 text-xs text-ink-muted">
            {group.memberCount} {group.memberCount === 1 ? "person" : "people"} ·{" "}
            {group.roleCount} {group.roleCount === 1 ? "role" : "roles"}
            {group.externalRef && ` · matches "${group.externalRef}"`}
          </p>
        </div>
        <Button
          size="sm"
          variant="ghost"
          className="shrink-0"
          loading={deleteGroup.isPending}
          onClick={async () => (await confirm({ noun: "group", body: `${group.name} goes, and its members lose the roles it granted.` })) && deleteGroup.mutate(group.id)}
        >
          Delete
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
                onClick={async () => (await confirm({ noun: "member", verb: "Remove", body: `${member.name} leaves ${group.name} and loses the roles it grants.` })) && remove.mutate({ id: group.id, userId: member.userId })}
              >
                Remove
              </Button>
            )}
          </li>
        ))}
        {members.length === 0 && (
          <li className="text-sm text-ink-subtle">
            {fromProvider
              ? "Nobody has signed in with this group in their token yet."
              : "Nobody in it yet."}
          </li>
        )}
      </ul>

      {!fromProvider && (
        <div className="mt-3 flex flex-wrap items-center gap-2">
          <label htmlFor={`group-add-${group.id}`} className="sr-only">
            Add somebody to {group.name}
          </label>
          <SelectInput id={`group-add-${group.id}`} value={picked} onChange={(event) => setPicked(event.target.value)}>
            <option value="">Add somebody...</option>
            {available.map((person) => (
              <option key={person.id} value={person.id}>
                {person.name}
              </option>
            ))}
          </SelectInput>
          <Button
            size="sm"
            disabled={!picked}
            loading={add.isPending}
            onClick={() => {
              add.mutate({ id: group.id, userId: picked });
              setPicked("");
            }}
          >
            Add
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
          <Field label="New group" value={name} placeholder="Release managers" onChange={(event) => setName(event.target.value)} />
          <Field label="Provider claim" value={ref} placeholder="Optional" onChange={(event) => setRef(event.target.value)} />
          <Button type="submit" loading={create.isPending} disabled={!name.trim()}>
            Create group
          </Button>
        </div>
        <p className="text-xs text-ink-subtle">The provider claim is the value your identity provider sends for this group; with it, membership follows sign-ins.</p>
        {create.error && <ErrorBanner>{(create.error as Error).message}</ErrorBanner>}
      </form>
    </Card>
  );
}
