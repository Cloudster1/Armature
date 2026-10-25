import { useState, type KeyboardEvent } from "react";
import { Link } from "@tanstack/react-router";
import { type Issue, isStaleEdit, useIssueHierarchy, useTransitionIssue, useTransitions, useUpdateIssue } from "@/api/issues";
import { Icon } from "@/components/icons";
import { Button, ErrorBanner, IconButton, Input, cx } from "@/components/ui";
import { StatusBadge } from "./badges";
import { Ancestry } from "./hierarchy";
import { TypeField } from "./TypePicker";
import { CloneMoveMenu } from "@/features/filters/CloneMoveMenu";
import { t } from "@/i18n";

export function IssueHeader({ issue, editable, inDrawer }: { issue: Issue; editable: boolean; inDrawer: boolean }) {
  return (
    <header
      className={cx(
        "z-10 mb-5 border-b border-border bg-canvas/95 pb-3 backdrop-blur",
        inDrawer ? "-mx-5 -mt-4 px-5 pt-4" : "sticky -top-6 -mx-8 -mt-6 px-8 pt-6",
      )}
    >
      <div className="flex flex-wrap items-center gap-1.5 text-xs">
        <Link to="/projects/$projectKey" params={{ projectKey: issue.projectKey }} className="text-ink-muted hover:text-ink">
          {issue.projectKey}
        </Link>
        <span aria-hidden="true" className="text-ink-subtle">
          /
        </span>
        <IssueAncestry issueKey={issue.key} />
      </div>
      <div className="mt-1 flex flex-wrap items-center gap-2">
        <TypeField issue={issue} editable={editable} />
        <span className="font-mono text-sm text-ink-muted">{issue.key}</span>
        <StatusBadge name={issue.status.name} category={issue.status.category} />
        <span className="flex-1" />
        <CloneMoveMenu issue={issue} editable={editable} />
        {inDrawer && (
          <Link
            to="/issues/$issueKey"
            params={{ issueKey: issue.key }}
            className="inline-flex items-center gap-1 text-sm text-accent hover:underline"
            data-open-page
          >
            {t.issueView.header.openPage} <Icon.External />
          </Link>
        )}
      </div>
      <div className="mt-2 flex flex-wrap items-start justify-between gap-3">
        <SummaryEditor issueKey={issue.key} summary={issue.summary} updatedAt={issue.updatedAt} editable={editable} />
        <TransitionBar issueKey={issue.key} />
      </div>
    </header>
  );
}

function IssueAncestry({ issueKey }: { issueKey: string }) {
  const { data } = useIssueHierarchy(issueKey);
  return <Ancestry ancestors={data?.ancestors ?? []} />;
}

// The summary is the most edited line on a tracker, so it edits in place:
// click or press the pencil, Enter saves, Escape leaves it as it was.
function SummaryEditor({ issueKey, summary, updatedAt, editable }: { issueKey: string; summary: string; updatedAt: string; editable: boolean }) {
  const update = useUpdateIssue();
  const [editing, setEditing] = useState(false);
  const [draft, setDraft] = useState(summary);
  // Like the description: the edit names the copy it began from.
  const [startedFrom, setStartedFrom] = useState(updatedAt);
  const [overtaken, setOvertaken] = useState(false);

  function start() {
    setDraft(summary);
    setStartedFrom(updatedAt);
    setOvertaken(false);
    setEditing(true);
  }
  function save() {
    const next = draft.trim();
    if (!next || next === summary) {
      setEditing(false);
      return;
    }
    update.mutate(
      { key: issueKey, summary: next, expectedUpdatedAt: overtaken ? updatedAt : startedFrom },
      {
        onSuccess: () => {
          setOvertaken(false);
          setEditing(false);
        },
        onError: (error) => {
          if (isStaleEdit(error)) setOvertaken(true);
        },
      },
    );
  }
  function onKeyDown(event: KeyboardEvent<HTMLInputElement>) {
    if (event.key === "Enter") {
      event.preventDefault();
      save();
    } else if (event.key === "Escape") {
      setEditing(false);
    }
  }

  if (editing) {
    return (
      <div className="min-w-0 flex-1">
        <Input
          aria-label={t.issueView.header.summary}
          value={draft}
          autoFocus
          onChange={(e) => setDraft(e.target.value)}
          onKeyDown={onKeyDown}
          // After a refusal, leaving the field must not quietly replace the newer summary.
          onBlur={overtaken ? undefined : save}
          controlSize="lg"
          className="border-accent text-lg font-semibold tracking-tight"
          data-summary-editor
        />
        {update.error && <p className="mt-1 text-sm text-danger">{(update.error as Error).message}</p>}
        {overtaken && (
          <p className="mt-1 text-sm text-ink-muted" data-newer-summary>
            {t.issueView.header.nowReads} <span className="font-medium text-ink">{summary}</span>
            {t.issueView.header.saveOver}
          </p>
        )}
      </div>
    );
  }
  return (
    <div className="flex min-w-0 flex-1 items-start gap-1">
      <h1 className="min-w-0 text-lg font-semibold tracking-tight text-ink">{summary}</h1>
      {editable && <IconButton icon={<Icon.Edit />} label={t.issueView.header.editSummary} size="sm" onClick={start} data-action="edit-summary" />}
    </div>
  );
}

// The transitions the workflow currently allows. The server decides what is
// offered, so a transition the user may not take never appears as a button.
function TransitionBar({ issueKey }: { issueKey: string }) {
  const { data, isLoading } = useTransitions(issueKey);
  const transition = useTransitionIssue();
  const transitions = data?.transitions ?? [];

  if (isLoading) return null;

  return (
    <div className="flex max-w-full min-w-0 flex-col items-end gap-2">
      {transitions.length === 0 ? (
        <p className="text-sm text-ink-subtle">{t.issueView.header.nowhereToGo}</p>
      ) : (
        <div className="flex flex-wrap justify-end gap-2">
          {transitions.map((t) => (
            <Button
              key={t.id}
              size="sm"
              variant="secondary"
              title={t.description || undefined}
              loading={transition.isPending && transition.variables?.transitionId === t.id}
              onClick={() => transition.mutate({ key: issueKey, transitionId: t.id })}
            >
              {t.name}
            </Button>
          ))}
        </div>
      )}
      {transition.error && <ErrorBanner>{(transition.error as Error).message}</ErrorBanner>}
    </div>
  );
}
