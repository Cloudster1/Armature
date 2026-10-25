import { markdownToDoc } from "@/features/editor/markdown";
import { useState, type FormEvent } from "react";
import { Link } from "@tanstack/react-router";
import { useCreateIssue, useIssueHierarchy, useIssueTypes, useSetParent, type Issue, type Progress, type TreeNode } from "@/api/issues";
import { useParentCandidates } from "@/api/issueReads";
import { Button, ErrorBanner, Input, ProgressBar as Bar, SelectInput, Textarea, cx } from "@/components/ui";
import { Avatar, StatusBadge, TypeBadge } from "./badges";
import { t } from "@/i18n";

/**
 * A roll-up of the work directly underneath an issue. Three segments rather
 * than one, because "half done" and "half in flight" are different situations.
 */
export function ProgressBar({ progress, className }: { progress: Progress; className?: string }) {
  if (progress.total === 0) return null;
  return (
    <div className={cx("flex items-center gap-2", className)}>
      <Bar
        value={progress.done}
        max={progress.total}
        label={t.issuePanels.hierarchy.progress(progress.done, progress.total)}
        size="xs"
        className="min-w-24 flex-1"
        segments={[
          { value: progress.done, tone: "done" },
          { value: progress.inProgress, tone: "accent" },
        ]}
      />
      <span className="shrink-0 text-2xs whitespace-nowrap text-ink-muted">{t.issuePanels.hierarchy.progressShort(progress.done, progress.total)}</span>
    </div>
  );
}

/** The chain above an issue, top first, the way a breadcrumb reads. */
export function Ancestry({ ancestors }: { ancestors: Issue[] }) {
  if (ancestors.length === 0) return null;

  return (
    <nav aria-label={t.issuePanels.hierarchy.parentIssues} className="flex flex-wrap items-center gap-1.5 text-xs">
      {ancestors.map((ancestor) => (
        <span key={ancestor.id} className="flex items-center gap-1.5">
          <TypeBadge icon={ancestor.type.icon} name={ancestor.type.name} />
          <Link to="/issues/$issueKey" params={{ issueKey: ancestor.key }} className="text-ink-muted hover:text-accent hover:underline">
            {ancestor.summary}
          </Link>
          <span aria-hidden="true" className="text-ink-subtle">
            /
          </span>
        </span>
      ))}
    </nav>
  );
}

/**
 * Everything under an issue, with its roll-up and a way to add more. The types
 * offered are the ones the server would accept, so no choice here can be wrong.
 */
export function ChildrenPanel({ issueKey }: { issueKey: string }) {
  const { data, isLoading } = useIssueHierarchy(issueKey);
  const [adding, setAdding] = useState(false);

  if (isLoading || !data) return null;

  const canHaveChildren = data.childTypes.length > 0;
  if (!canHaveChildren && data.children.length === 0) return null;

  return (
    <section>
      <div className="mb-3 flex items-center gap-3">
        <h2 className="text-xs font-semibold tracking-wide text-ink-muted uppercase">
          {t.issuePanels.hierarchy.childIssues} {data.children.length > 0 && `(${data.children.length})`}
        </h2>
        <ProgressBar progress={data.progress} className="max-w-56 flex-1" />
        <span className="flex-1" />
        {canHaveChildren && !adding && (
          <Button variant="ghost" size="sm" onClick={() => setAdding(true)}>
            {t.issuePanels.hierarchy.addChild}
          </Button>
        )}
      </div>

      {data.children.length === 0 ? (
        <p className="text-sm text-ink-subtle">{t.issuePanels.hierarchy.nothingUnder}</p>
      ) : (
        <ul className="divide-y divide-border rounded-md border border-border">
          {data.children.map((child) => (
            <ChildRow key={child.issue.id} node={child} />
          ))}
        </ul>
      )}

      {adding && <AddChildForm parentKey={issueKey} projectKey={data.issue.projectKey} types={data.childTypes} onDone={() => setAdding(false)} />}
    </section>
  );
}

function ChildRow({ node }: { node: TreeNode }) {
  const { issue, progress } = node;
  return (
    <li className="flex items-center gap-3 px-3 py-2">
      <TypeBadge icon={issue.type.icon} name={issue.type.name} />
      <Link to="/issues/$issueKey" params={{ issueKey: issue.key }} className="shrink-0 font-mono text-2xs text-ink-muted hover:text-accent">
        {issue.key}
      </Link>
      <Link to="/issues/$issueKey" params={{ issueKey: issue.key }} className="min-w-0 flex-1 truncate text-sm text-ink hover:text-accent">
        {issue.summary}
      </Link>
      {progress.total > 0 && <ProgressBar progress={progress} className="w-32 shrink-0" />}
      <StatusBadge name={issue.status.name} category={issue.status.category} />
      <Avatar name={issue.assignee?.name} src={issue.assignee?.avatarUrl} size="sm" />
    </li>
  );
}

function AddChildForm({
  parentKey,
  projectKey,
  types,
  onDone,
}: {
  parentKey: string;
  projectKey: string;
  types: Array<{ id: string; name: string }>;
  onDone: () => void;
}) {
  const create = useCreateIssue();
  const [summary, setSummary] = useState("");
  const [description, setDescription] = useState("");
  const [typeId, setTypeId] = useState(types[0]?.id ?? "");

  function onSubmit(event: FormEvent) {
    event.preventDefault();
    if (!summary.trim()) return;
    create.mutate(
      { projectKey, summary, description: markdownToDoc(description) ?? undefined, typeId, parentKey },
      {
        onSuccess: () => {
          setSummary("");
          setDescription("");
          onDone();
        },
      },
    );
  }

  return (
    <form onSubmit={onSubmit} className="mt-3 space-y-2">
      {create.error && <ErrorBanner>{(create.error as Error).message}</ErrorBanner>}
      <div className="flex gap-2">
        <label htmlFor="child-type" className="sr-only">
          {t.issuePanels.hierarchy.childType}
        </label>
        <SelectInput id="child-type" value={typeId} onChange={(e) => setTypeId(e.target.value)} controlSize="sm">
          {types.map((type) => (
            <option key={type.id} value={type.id}>
              {type.name}
            </option>
          ))}
        </SelectInput>
        <label htmlFor="child-summary" className="sr-only">
          {t.issuePanels.hierarchy.whatNeedsDoing}
        </label>
        <Input
          id="child-summary"
          value={summary}
          autoFocus
          onChange={(e) => setSummary(e.target.value)}
          placeholder={t.issuePanels.hierarchy.whatNeedsDoingPlaceholder}
          controlSize="sm"
          className="min-w-0 flex-1"
        />
        <Button type="submit" size="sm" loading={create.isPending} disabled={!summary.trim()}>
          {t.issuePanels.common.add}
        </Button>
        <Button type="button" size="sm" variant="ghost" onClick={onDone}>
          {t.issuePanels.common.cancel}
        </Button>
      </div>
      <label htmlFor="child-description" className="sr-only">
        {t.issuePanels.hierarchy.description}
      </label>
      <Textarea
        id="child-description"
        value={description}
        onChange={(e) => setDescription(e.target.value)}
        rows={2}
        placeholder={t.issuePanels.hierarchy.descriptionPlaceholder}
      />
    </form>
  );
}

/**
 * Moving an issue under a different parent. The candidates are the issues one
 * level above it in the same project, which is exactly what the server accepts.
 */
export function ParentPicker({ issue }: { issue: Issue }) {
  const setParent = useSetParent();
  const [words, setWords] = useState("");
  const { data: typeData } = useIssueTypes();
  // The server is asked for the level above by type, so a parent older than
  // any one page is still found by typing its name.
  const levelAbove = (typeData?.issueTypes ?? []).filter((type) => type.level === issue.type.level + 1).map((type) => type.id);
  const { data } = useParentCandidates(issue.projectKey, levelAbove, words.trim());

  const found = data?.issues ?? [];
  // The current parent stays selectable even when the search has moved past it.
  const current = issue.parent && !found.some((candidate) => candidate.key === issue.parent?.key) ? [issue.parent] : [];
  const candidates = [...current, ...found];
  const detachable = issue.type.level >= 0;

  if (!words && candidates.length === 0) {
    return <span className="text-xs text-ink-subtle">{t.issuePanels.hierarchy.nothingToSitUnder}</span>;
  }

  return (
    <span className="flex flex-col items-end gap-1">
      <label htmlFor="issue-parent-search" className="sr-only">
        {t.issuePanels.hierarchy.findParent}
      </label>
      <Input
        id="issue-parent-search"
        type="search"
        controlSize="sm"
        value={words}
        onChange={(e) => setWords(e.target.value)}
        placeholder={t.issuePanels.hierarchy.findParent}
        className="max-w-44 text-xs"
      />
      <label htmlFor="issue-parent" className="sr-only">
        {t.issuePanels.hierarchy.parent}
      </label>
      <SelectInput
        id="issue-parent"
        value={issue.parent?.key ?? ""}
        onChange={(e) => setParent.mutate({ key: issue.key, parentKey: e.target.value || null })}
        controlSize="sm"
        className="max-w-44 truncate text-xs"
      >
        {(detachable || !issue.parent) && <option value="">{t.issuePanels.hierarchy.none}</option>}
        {candidates.map((candidate) => (
          <option key={candidate.id} value={candidate.key}>
            {candidate.key} · {candidate.summary}
          </option>
        ))}
      </SelectInput>
      {setParent.error && <span className="text-right text-2xs text-danger">{(setParent.error as Error).message}</span>}
    </span>
  );
}
