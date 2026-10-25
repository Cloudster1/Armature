import { useState, type MouseEvent } from "react";
import { Link } from "@tanstack/react-router";
import { canAdminister, useAccess } from "@/api/access";
import { useProjectWorkflows, useSchemes, useSetProjectAssignment, useSetProjectScheme, type Assignment } from "@/api/workflows";
import { Button, Card, ErrorBanner, Skeleton, cx, isInteractiveTarget } from "@/components/ui";
import { ScopeBadge, originExplanation } from "./ScopeBadge";
import { SchemePicker, WorkflowPicker } from "./WorkflowPicker";
import { WorkflowGraph } from "./WorkflowGraph";
import { t } from "@/i18n";

/** The columns of the assignment table: the three it always has, and the one a decider gets. */
const COLUMNS = 3;
const COLUMNS_WITH_DECIDE = COLUMNS + 1;

/**
 * The project's answer to one question: which workflow does each kind of issue
 * move through here, and did this project decide that or did the organization?
 */
export function ProjectWorkflows({ projectKey }: { projectKey: string }) {
  const { data, isLoading, error } = useProjectWorkflows(projectKey);
  const { data: access } = useAccess();
  const assign = useSetProjectAssignment();

  // Two kinds of decision: a row is the project administrator's, a whole
  // scheme the organization's.
  const canMap = canAdminister(access, projectKey);
  const canConfigure = access?.canAdministerOrg ?? false;
  const assignments = data?.assignments ?? [];

  // The drawing follows one row; the first until somebody picks another.
  const [picked, setPicked] = useState<string | null>(null);
  const shown = assignments.find((a) => a.issueTypeId === picked) ?? assignments[0];

  if (error) return <ErrorBanner>{(error as Error).message}</ErrorBanner>;

  return (
    <div className="space-y-6">
      <SchemeChoice
        projectKey={projectKey}
        schemeId={data?.schemeId ?? undefined}
        schemeName={data?.schemeName}
        defaultSchemeName={data?.defaultSchemeName}
        canConfigure={canConfigure}
      />

      {shown && <WorkflowGraph workflowId={shown.workflowId} caption={t.workflows.project.caption(shown.issueTypeName)} />}

      <Card elevated className="overflow-hidden">
        {canMap && <p className="border-b border-border px-4 py-2.5 text-sm text-ink-muted">{t.workflows.project.pickHint}</p>}
        <table data-testid="workflow-assignments" className="w-full text-sm">
          <thead className="border-b border-border bg-surface-raised text-left text-xs text-ink-muted">
            <tr>
              <th className="px-4 py-2 font-medium">{t.workflows.project.issueType}</th>
              <th className="px-4 py-2 font-medium">{t.workflows.project.workflow}</th>
              <th className="px-4 py-2 font-medium">{t.workflows.project.decidedBy}</th>
              {canMap && (
                <th className="px-4 py-2 font-medium" data-guide="workflow-decide">
                  {t.workflows.project.decide}
                </th>
              )}
            </tr>
          </thead>
          <tbody>
            {isLoading && (
              <tr>
                <td className="px-4 py-3" colSpan={canMap ? COLUMNS_WITH_DECIDE : COLUMNS}>
                  <Skeleton lines={3} />
                </td>
              </tr>
            )}
            {assignments.map((assignment) => (
              <AssignmentRow
                key={assignment.issueTypeId}
                assignment={assignment}
                shown={assignment.issueTypeId === shown?.issueTypeId}
                onShow={() => setPicked(assignment.issueTypeId)}
                canDecide={canMap}
                onDecide={(workflowId) => assign.mutate({ projectKey, issueTypeId: assignment.issueTypeId, workflowId })}
                deciding={assign.isPending}
              />
            ))}
          </tbody>
        </table>
        {assign.error && (
          <div className="border-t border-border p-3">
            <ErrorBanner>{(assign.error as Error).message}</ErrorBanner>
          </div>
        )}
      </Card>
    </div>
  );
}

function AssignmentRow({
  assignment,
  shown,
  onShow,
  canDecide,
  onDecide,
  deciding,
}: {
  assignment: Assignment;
  shown: boolean;
  onShow: () => void;
  /** Whether the reader may choose the row's workflow rather than only look. */
  canDecide: boolean;
  onDecide: (workflowId: string | null) => void;
  deciding: boolean;
}) {
  // A row the project's own fallback decides shows that workflow too; it cannot
  // be handed back one type at a time, so the option to is not offered there.
  const byProject = assignment.origin.scope === "project";
  const decided = byProject ? assignment.workflowId : "";
  const canFollow = !byProject || assignment.origin.named;
  // A click on the row shows its workflow; a click on a link inside it still goes where the link goes.
  function pick(event: MouseEvent<HTMLTableRowElement>) {
    if (isInteractiveTarget(event.target)) return;
    onShow();
  }
  return (
    <tr
      data-assignment={assignment.issueTypeName}
      data-assignment-shown={shown ? "" : undefined}
      aria-selected={shown}
      onClick={pick}
      className={cx("cursor-pointer border-b border-border last:border-0", shown ? "bg-accent-subtle/60" : "hover:bg-surface-raised/60")}
    >
      <td className="px-4 py-2.5 font-medium text-ink">{assignment.issueTypeName}</td>
      <td className="px-4 py-2.5">
        <Link to="/settings/workflows" search={{ workflow: assignment.workflowId, q: assignment.workflowName }} className="text-ink hover:text-accent">
          {assignment.workflowName}
        </Link>
      </td>
      <td className="px-4 py-2.5">
        <ScopeBadge origin={assignment.origin} />
        <span className="ml-2 text-xs text-ink-subtle">{originExplanation(assignment.origin)}</span>
      </td>
      {canDecide && (
        <td className="px-4 py-1.5" data-assignment-control>
          <WorkflowPicker
            aria-label={t.workflows.project.workflowFor(assignment.issueTypeName)}
            value={decided}
            chosenName={decided ? assignment.workflowName : undefined}
            leading={canFollow && <option value="">{t.workflows.project.followDefault}</option>}
            disabled={deciding}
            onChange={(workflowId) => onDecide(workflowId || null)}
          />
        </td>
      )}
    </tr>
  );
}

function SchemeChoice({
  projectKey,
  schemeId,
  schemeName,
  defaultSchemeName,
  canConfigure,
}: {
  projectKey: string;
  schemeId?: string;
  schemeName?: string;
  defaultSchemeName?: string;
  canConfigure: boolean;
}) {
  const setScheme = useSetProjectScheme();
  const [picked, setPicked] = useState({ id: "", name: "" });
  // One scheme is enough to learn how many there are: the default and this project's own are not on offer.
  const { data: schemeCount } = useSchemes({ limit: 1 });

  const own = schemeId ? { id: schemeId, name: schemeName ?? "" } : undefined;
  const others = schemeCount ? schemeCount.total - 1 - (own ? 1 : 0) : 0;

  return (
    <Card elevated className="p-4">
      {own ? (
        <>
          <p className="text-sm font-medium text-ink">{t.workflows.project.ownScheme(own.name)}</p>
          <p className="mt-1 text-sm text-ink-muted">{t.workflows.project.stillFollow(defaultSchemeName || t.workflows.project.theDefaultScheme)}</p>
        </>
      ) : (
        <>
          <p className="text-sm font-medium text-ink">{t.workflows.project.followsDefault}</p>
          <p className="mt-1 text-sm text-ink-muted">{t.workflows.project.everyTypeUses(defaultSchemeName || t.workflows.project.theDefaultScheme)}</p>
        </>
      )}

      {canConfigure && schemeCount && others <= 0 && !own && (
        <p className="mt-2 text-sm text-ink-subtle">
          {t.workflows.project.noOtherBefore}{" "}
          <Link to="/settings/workflows" search={{ workflow: undefined }} className="underline underline-offset-2 hover:text-ink">
            {t.workflows.project.noOtherLink}
          </Link>
          {t.workflows.project.noOtherAfter}
        </p>
      )}

      {canConfigure && (others > 0 || own) && (
        <div className="mt-3 flex flex-wrap items-center gap-2">
          <span className="text-sm text-ink-muted">{t.workflows.project.orExisting}</span>
          <label htmlFor="project-scheme" className="sr-only">
            {t.workflows.project.scheme}
          </label>
          <SchemePicker
            id="project-scheme"
            aria-label={t.workflows.project.scheme}
            value={picked.id}
            chosenName={picked.name}
            exclude={own ? [own.id] : []}
            leading={<option value="">{t.workflows.project.chooseScheme}</option>}
            onChange={(id, name) => setPicked({ id, name })}
          />
          <Button size="sm" disabled={!picked.id} loading={setScheme.isPending} onClick={() => setScheme.mutate({ projectKey, schemeId: picked.id })}>
            {t.workflows.project.useScheme}
          </Button>
          {own && (
            <Button size="sm" variant="ghost" onClick={() => setScheme.mutate({ projectKey, schemeId: null })}>
              {t.workflows.project.followDefault}
            </Button>
          )}
          <Link to="/settings/workflows" search={{ workflow: undefined }} className="text-xs text-ink-muted underline-offset-2 hover:text-ink hover:underline">
            {t.workflows.project.manage}
          </Link>
        </div>
      )}

      {setScheme.error && <ErrorBanner>{(setScheme.error as Error).message}</ErrorBanner>}
    </Card>
  );
}
