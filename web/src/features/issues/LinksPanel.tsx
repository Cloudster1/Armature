import { useState, type FormEvent } from "react";
import { Link } from "@tanstack/react-router";
import { useAddLink, useLinkTypes, useLinks, useRemoveLink } from "@/api/plan";
import { Button, ErrorBanner, Input, SelectInput } from "@/components/ui";
import { TypeBadge } from "./badges";
import { t } from "@/i18n";

/**
 * The issue's relationships, read from its own end, with a way to add one by
 * key and to undo one. Dependencies drawn on the plan are seen and undone
 * here too, so nothing made by a drag is out of reach of the ticket.
 */
export function LinksPanel({ issueKey, editable }: { issueKey: string; editable: boolean }) {
  const { data, isLoading } = useLinks(issueKey);
  const { data: typeData } = useLinkTypes();
  const add = useAddLink();
  const remove = useRemoveLink();
  const [adding, setAdding] = useState(false);
  const [type, setType] = useState("Blocks");
  const [targetKey, setTargetKey] = useState("");

  if (isLoading || !data) return null;
  const links = data.links;
  if (links.length === 0 && !editable) return null;

  function submit(event: FormEvent) {
    event.preventDefault();
    const key = targetKey.trim().toUpperCase();
    if (!key) return;
    add.mutate({ key: issueKey, type, targetKey: key }, { onSuccess: () => setTargetKey("") });
  }

  return (
    <section data-testid="links-panel">
      <div className="mb-3 flex items-center gap-3">
        <h2 className="text-xs font-semibold tracking-wide text-ink-muted uppercase">
          {t.issuePanels.links.title} {links.length > 0 && `(${links.length})`}
        </h2>
        <span className="flex-1" />
        {editable && !adding && (
          <Button variant="ghost" size="sm" onClick={() => setAdding(true)}>
            {t.issuePanels.links.add}
          </Button>
        )}
      </div>

      {adding && (
        <form onSubmit={submit} className="mb-3 flex flex-wrap items-center gap-2" noValidate>
          <span className="text-sm text-ink-muted">{issueKey}</span>
          <SelectInput aria-label={t.issuePanels.links.kind} value={type} onChange={(event) => setType(event.target.value)} className="max-w-44">
            {(typeData?.linkTypes ?? []).map((each) => (
              <option key={each.id} value={each.name}>
                {each.outward}
              </option>
            ))}
          </SelectInput>
          <Input
            id="link-target"
            aria-label={t.issuePanels.links.target}
            value={targetKey}
            onChange={(event) => setTargetKey(event.target.value)}
            placeholder="PROJ-12"
            autoFocus
            className="w-32 font-mono uppercase"
          />
          <Button type="submit" size="sm" loading={add.isPending} disabled={!targetKey.trim()}>
            {t.issuePanels.links.link}
          </Button>
          <Button type="button" variant="ghost" size="sm" onClick={() => setAdding(false)}>
            {t.issuePanels.common.done}
          </Button>
        </form>
      )}
      {add.error && <ErrorBanner>{(add.error as Error).message}</ErrorBanner>}
      {remove.error && <ErrorBanner>{(remove.error as Error).message}</ErrorBanner>}

      {links.length === 0 ? (
        <p className="text-sm text-ink-subtle">{t.issuePanels.links.none}</p>
      ) : (
        <ul className="divide-y divide-border rounded-md border border-border">
          {links.map((link) => (
            <li key={link.id} className="flex items-center gap-2 px-3 py-1.5 text-sm" data-issue-link={link.issue.key}>
              <span className="w-28 shrink-0 text-ink-muted">{link.phrase}</span>
              <TypeBadge icon={link.issue.type.icon} name={link.issue.type.name} />
              <Link to="/issues/$issueKey" params={{ issueKey: link.issue.key }} className="shrink-0 font-mono text-xs text-accent hover:underline">
                {link.issue.key}
              </Link>
              <span className="min-w-0 flex-1 truncate text-ink">{link.issue.summary}</span>
              {editable && (
                <Button
                  variant="link"
                  onClick={() => remove.mutate({ key: issueKey, linkId: link.id })}
                  aria-label={t.issuePanels.links.removeLink(link.issue.key)}
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
