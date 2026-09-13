import { useState, type FormEvent } from "react";
import { useMembers } from "@/api/issues";
import { useAddWatcher, useRemoveWatcher, useWatchers } from "@/api/watchers";
import { Button, ErrorBanner, Input, SelectInput } from "@/components/ui";
import { Avatar } from "./badges";

/**
 * Who is told about the issue. Colleagues are picked from the members; anyone
 * else is named by address, and becomes a person the moment they are.
 */
export function WatchersPanel({ issueKey, editable, me }: { issueKey: string; editable: boolean; me?: string }) {
  const { data, isLoading } = useWatchers(issueKey);
  const { data: memberData } = useMembers();
  const add = useAddWatcher();
  const remove = useRemoveWatcher();
  const [adding, setAdding] = useState(false);
  const [memberId, setMemberId] = useState("");
  const [email, setEmail] = useState("");

  if (isLoading || !data) return null;
  const watchers = data.watchers;
  const watching = Boolean(me && watchers.some((w) => w.userId === me));
  const members = (memberData?.members ?? []).filter((m) => !watchers.some((w) => w.userId === m.id));

  function submit(event: FormEvent) {
    event.preventDefault();
    if (email.trim()) {
      add.mutate({ key: issueKey, email: email.trim() }, { onSuccess: () => setEmail("") });
    } else if (memberId) {
      add.mutate({ key: issueKey, userId: memberId }, { onSuccess: () => setMemberId("") });
    }
  }

  return (
    <section data-testid="watchers-panel">
      <div className="mb-3 flex items-center gap-3">
        <h2 className="text-xs font-semibold tracking-wide text-ink-muted uppercase">
          Watchers {watchers.length > 0 && `(${watchers.length})`}
        </h2>
        <span className="flex-1" />
        {me &&
          (watching ? (
            <Button variant="ghost" size="sm" loading={remove.isPending} onClick={() => remove.mutate({ key: issueKey, userId: me })} data-action="unwatch">
              Stop watching
            </Button>
          ) : (
            <Button variant="ghost" size="sm" loading={add.isPending} onClick={() => add.mutate({ key: issueKey })} data-action="watch">
              Watch
            </Button>
          ))}
        {editable && !adding && (
          <Button variant="ghost" size="sm" onClick={() => setAdding(true)}>
            Add watcher
          </Button>
        )}
      </div>

      {adding && (
        <form onSubmit={submit} className="mb-3 flex flex-wrap items-center gap-2" noValidate>
          <SelectInput aria-label="A member to add" value={memberId} onChange={(event) => setMemberId(event.target.value)} className="max-w-52">
            <option value="">Somebody here</option>
            {members.map((m) => (
              <option key={m.id} value={m.id}>
                {m.name}
              </option>
            ))}
          </SelectInput>
          <span className="text-sm text-ink-muted">or</span>
          <Input
            aria-label="An address to add"
            type="email"
            value={email}
            onChange={(event) => setEmail(event.target.value)}
            placeholder="name@example.com"
            className="w-56"
            data-watcher-address
          />
          <Button type="submit" size="sm" loading={add.isPending} disabled={!email.trim() && !memberId}>
            Add
          </Button>
          <Button type="button" variant="ghost" size="sm" onClick={() => setAdding(false)}>
            Done
          </Button>
        </form>
      )}
      {add.error && <ErrorBanner>{(add.error as Error).message}</ErrorBanner>}
      {remove.error && <ErrorBanner>{(remove.error as Error).message}</ErrorBanner>}

      {watchers.length === 0 ? (
        <p className="text-sm text-ink-subtle">Nobody is watching this yet.</p>
      ) : (
        <ul className="divide-y divide-border rounded-md border border-border">
          {watchers.map((w) => (
            <li key={w.userId} className="flex items-center gap-2 px-3 py-1.5 text-sm" data-issue-watcher={w.email}>
              <Avatar name={w.name} size="sm" />
              <span className="text-ink">{w.name}</span>
              <span className="min-w-0 flex-1 truncate text-ink-subtle">{w.email}</span>
              {editable && (
                <Button variant="link" onClick={() => remove.mutate({ key: issueKey, userId: w.userId })} aria-label={`Stop ${w.name} watching`} className="text-xs text-ink-subtle hover:text-danger">
                  Remove
                </Button>
              )}
            </li>
          ))}
        </ul>
      )}
    </section>
  );
}
