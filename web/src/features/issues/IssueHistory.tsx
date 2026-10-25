import { useState } from "react";
import { ACTIVITY_FOLD, APP_NAME } from "@/config";
import { type HistoryEntry, useHistory } from "@/api/issues";
import { Button } from "@/components/ui";
import { Avatar, RelativeTime } from "./badges";
import { inOrder } from "./IssueActivity";
import { t } from "@/i18n";

export function History({ issueKey }: { issueKey: string }) {
  const { data } = useHistory(issueKey);
  const history = inOrder(data?.history ?? []);
  const [expanded, setExpanded] = useState(false);
  const shown = expanded ? history : history.slice(-ACTIVITY_FOLD);

  return (
    <div data-issue-history>
      {history.length > shown.length && (
        <Button variant="ghost" size="sm" className="mb-2" onClick={() => setExpanded(true)}>
          {t.issuePanels.common.showAll(history.length)}
        </Button>
      )}
      {shown.length === 0 ? (
        <p className="text-sm text-ink-subtle">{t.issuePanels.history.nothingYet}</p>
      ) : (
        <ol className="space-y-3" data-activity="history">
          {shown.map((entry) => (
            <li key={entry.id}>
              <HistoryItem entry={entry} />
            </li>
          ))}
        </ol>
      )}
    </div>
  );
}

function HistoryItem({ entry }: { entry: HistoryEntry }) {
  return (
    <div className="flex gap-3 text-sm">
      <Avatar name={entry.actor?.name} src={entry.actor?.avatarUrl} size="sm" />
      <p className="min-w-0 flex-1 text-ink-muted">
        <span className="font-medium text-ink">{entry.actor?.name ?? APP_NAME}</span> {entry.changes.map((c) => describe(c.field, c.from, c.to)).join(", ")}
        <span className="text-ink-subtle">
          {" "}
          · <RelativeTime iso={entry.createdAt} />
        </span>
      </p>
    </div>
  );
}

/** Turns one recorded change into a sentence. The values compared are the server's own words. */
function describe(field: string, from?: string, to?: string): string {
  const h = t.issuePanels.history;
  const was = from ?? "";
  const now = to ?? "";
  switch (field) {
    case "created":
      return h.created(now);
    case "status":
    case "project":
      return h.moved(was, now);
    case "assignee":
      return to === "Unassigned" ? h.unassigned : h.assigned(now);
    case "resolution":
      return to === "Unresolved" ? h.reopened : h.resolved(now);
    case "description":
      return to === "cleared" ? h.clearedDescription : h.updatedDescription;
    case "type":
      return h.retyped(was, now);
    case "summary":
      return h.renamedSummary(was, now);
    case "attachment":
      return to ? h.attached(to) : h.removedFile(was);
    case "reporter":
      return h.reporter(now);
    case "labels":
      return to ? h.labelled(to) : h.labelsOff(was);
    case "timeEstimate":
      return to ? h.estimated(to) : h.estimateRemoved;
    case "timeRemaining":
      return to ? h.remaining(to) : h.remainingCleared;
    case "timeSpent":
      return h.logged(from || "0m", now);
    case "worklog":
      return to ? h.worklogCorrected(was, to) : h.worklogRemoved(was);
    case "parent":
      return to === "None" ? h.takenOutOf(was) : h.movedUnder(now);
    case "key":
      return h.rekeyed(was, now);
    case "clonedFrom":
      return h.cloned(now);
    case "fixVersion":
      return to ? h.fixVersions(to) : h.fixVersionsCleared(was);
    case "affectsVersion":
      return to ? h.affectedVersions(to) : h.affectedVersionsCleared(was);
    case "component":
      return to ? h.putIn(to) : h.takenOutOf(was);
    case "sprint":
    case "team":
    case "milestone":
    case "company":
      return to ? h.setField(h.fields[field] ?? field, to) : h.clearedField(h.fields[field] ?? field, was);
    default:
      return h.changed(field, from || h.empty, to || h.empty);
  }
}
