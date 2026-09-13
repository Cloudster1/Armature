import { useState } from "react";
import { ACTIVITY_FOLD } from "@/config";
import { type HistoryEntry, useHistory } from "@/api/issues";
import { Button } from "@/components/ui";
import { Avatar, relativeTime } from "./badges";
import { inOrder } from "./IssueActivity";

export function History({ issueKey }: { issueKey: string }) {
  const { data } = useHistory(issueKey);
  const history = inOrder(data?.history ?? []);
  const [expanded, setExpanded] = useState(false);
  const shown = expanded ? history : history.slice(-ACTIVITY_FOLD);

  return (
    <div data-issue-history>
      {history.length > shown.length && (
        <Button variant="ghost" size="sm" className="mb-2" onClick={() => setExpanded(true)}>
          Show all {history.length}
        </Button>
      )}
      {shown.length === 0 ? (
        <p className="text-sm text-ink-subtle">Nothing yet.</p>
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
        <span className="font-medium text-ink">{entry.actor?.name ?? "Armature"}</span> {entry.changes.map((c) => describe(c.field, c.from, c.to)).join(", ")}
        <span className="text-ink-subtle"> · {relativeTime(entry.createdAt)}</span>
      </p>
    </div>
  );
}

/** Turns one recorded change into a sentence. */
function describe(field: string, from?: string, to?: string): string {
  switch (field) {
    case "created":
      return `created ${to}`;
    case "status":
      return `moved this from ${from} to ${to}`;
    case "assignee":
      return to === "Unassigned" ? "unassigned this" : `assigned this to ${to}`;
    case "resolution":
      return to === "Unresolved" ? "reopened this" : `resolved this as ${to}`;
    case "description":
      return to === "cleared" ? "cleared the description" : "updated the description";
    case "summary":
      return `renamed this from "${from}" to "${to}"`;
    case "attachment":
      return to ? `attached ${to}` : `removed ${from}`;
    case "reporter":
      return `made ${to} the reporter`;
    case "labels":
      return to ? `labelled this ${to}` : `took the labels off (${from})`;
    case "timeEstimate":
      return to ? `estimated this at ${to}` : "removed the time estimate";
    case "timeRemaining":
      return to ? `has ${to} remaining` : "cleared the remaining time";
    case "timeSpent":
      return `logged work, ${from || "0m"} to ${to}`;
    case "worklog":
      return to ? `corrected a work log from ${from} to ${to}` : `removed a work log of ${from}`;
    case "parent":
      return to === "None" ? `took this out of ${from}` : `moved this under ${to}`;
    case "project":
      return `moved this from ${from} to ${to}`;
    case "key":
      return `renamed it from ${from} to ${to}`;
    case "clonedFrom":
      return `cloned this from ${to}`;
    case "fixVersion":
      return to ? `set the fix versions to ${to}` : `cleared the fix versions (${from})`;
    case "affectsVersion":
      return to ? `set the affected versions to ${to}` : `cleared the affected versions (${from})`;
    case "component":
      return to ? `put this in ${to}` : `took this out of ${from}`;
    case "sprint":
    case "team":
    case "milestone":
      return to ? `set the ${field} to ${to}` : `cleared the ${field} (${from})`;
    default:
      return `changed ${field} from ${from || "empty"} to ${to || "empty"}`;
  }
}
