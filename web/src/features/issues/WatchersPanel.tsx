import { useState, type FormEvent } from "react";
import { useAddWatcher, useRemoveWatcher, useWatchers } from "@/api/watchers";
import { Button, ErrorBanner, Input } from "@/components/ui";
import { PersonPicker } from "@/features/people/PersonPicker";
import { Avatar } from "./badges";
import { t } from "@/i18n";

/**
 * Who is told about the issue. Colleagues are picked from the members; anyone
 * else is named by address, and becomes a person the moment they are.
 */
export function WatchersPanel({ issueKey, editable, me }: { issueKey: string; editable: boolean; me?: string }) {
  const { data, isLoading } = useWatchers(issueKey);
  const add = useAddWatcher();
  const remove = useRemoveWatcher();
  const [adding, setAdding] = useState(false);
  const [memberId, setMemberId] = useState("");
  const [email, setEmail] = useState("");

  if (isLoading || !data) return null;
  const watchers = data.watchers;
  const watching = Boolean(me && watchers.some((w) => w.userId === me));

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
          {t.issuePanels.watchers.title} {watchers.length > 0 && `(${watchers.length})`}
        </h2>
        <span className="flex-1" />
        {me &&
          (watching ? (
            <Button variant="ghost" size="sm" loading={remove.isPending} onClick={() => remove.mutate({ key: issueKey, userId: me })} data-action="unwatch">
              {t.issuePanels.watchers.stop}
            </Button>
          ) : (
            <Button variant="ghost" size="sm" loading={add.isPending} onClick={() => add.mutate({ key: issueKey })} data-action="watch">
              {t.issuePanels.watchers.watch}
            </Button>
          ))}
        {editable && !adding && (
          <Button variant="ghost" size="sm" onClick={() => setAdding(true)}>
            {t.issuePanels.watchers.add}
          </Button>
        )}
      </div>

      {adding && (
        <form onSubmit={submit} className="mb-3 flex flex-wrap items-center gap-2" noValidate>
          <PersonPicker
            aria-label={t.issuePanels.watchers.member}
            none={t.issuePanels.watchers.somebodyHere}
            value={memberId}
            onChange={setMemberId}
            exclude={watchers.map((w) => w.userId)}
            className="max-w-52"
          />
          <span className="text-sm text-ink-muted">{t.issuePanels.watchers.or}</span>
          <Input
            aria-label={t.issuePanels.watchers.address}
            type="email"
            value={email}
            onChange={(event) => setEmail(event.target.value)}
            placeholder="name@example.com"
            className="w-56"
            data-watcher-address
          />
          <Button type="submit" size="sm" loading={add.isPending} disabled={!email.trim() && !memberId}>
            {t.issuePanels.common.add}
          </Button>
          <Button type="button" variant="ghost" size="sm" onClick={() => setAdding(false)}>
            {t.issuePanels.common.done}
          </Button>
        </form>
      )}
      {add.error && <ErrorBanner>{(add.error as Error).message}</ErrorBanner>}
      {remove.error && <ErrorBanner>{(remove.error as Error).message}</ErrorBanner>}

      {watchers.length === 0 ? (
        <p className="text-sm text-ink-subtle">{t.issuePanels.watchers.none}</p>
      ) : (
        <ul className="divide-y divide-border rounded-md border border-border">
          {watchers.map((w) => (
            <li key={w.userId} className="flex items-center gap-2 px-3 py-1.5 text-sm" data-issue-watcher={w.email}>
              <Avatar name={w.name} size="sm" />
              <span className="text-ink">{w.name}</span>
              <span className="min-w-0 flex-1 truncate text-ink-subtle">{w.email}</span>
              {editable && (
                <Button
                  variant="link"
                  onClick={() => remove.mutate({ key: issueKey, userId: w.userId })}
                  aria-label={t.issuePanels.watchers.stopWatcher(w.name)}
                  className="text-xs text-ink-subtle hover:text-danger"
                >
                  {t.issuePanels.common.remove}
                </Button>
              )}
            </li>
          ))}
        </ul>
      )}
    </section>
  );
}
