import { useState, type MouseEvent } from "react";
import { Link } from "@tanstack/react-router";
import { canAdminister, useAccess } from "@/api/access";
import {
  useProjectWorkflows,
  useSchemes,
  useSetProjectAssignment,
  useSetProjectScheme,
  useWorkflows,
  type Assignment,
  type WorkflowSummary,
} from "@/api/workflows";
import { Button, Card, ErrorBanner, SelectInput, Skeleton, cx, isInteractiveTarget } from "@/components/ui";
import { ScopeBadge, originExplanation } from "./ScopeBadge";
import { WorkflowGraph } from "./WorkflowGraph";

/** The columns of the assignment table: the three it always has, and the one a decider gets. */
const COLUMNS = 3;
const COLUMNS_WITH_DECIDE = COLUMNS + 1;

/**
 * The project's answer to one question: which workflow does each kind of issue
 * move through here, and did this project decide that or did the organization?
 */
export function ProjectWorkflows({ projectKey }: { projectKey: string }) {
  const { data, isLoading, error } = useProjectWorkflows(projectKey);
  const { data: schemeData } = useSchemes();
  const { data: workflowData } = useWorkflows();
  const { data: access } = useAccess();
  const assign = useSetProjectAssignment();

  // Two kinds of decision: a row is the project administrator's, a whole
  // scheme the organization's.
  const canMap = canAdminister(access, projectKey);
  const canConfigure = access?.canAdministerOrg ?? false;
  const schemes = schemeData?.schemes ?? [];
  const workflows = workflowData?.workflows ?? [];
  const assignments = data?.assignments ?? [];

  // The drawing follows one row; the first until somebody picks another.
  const [picked, setPicked] = useState<string | null>(null);
  const shown = assignments.find((a) => a.issueTypeId === picked) ?? assignments[0];

  if (error) return <ErrorBanner>{(error as Error).message}</ErrorBanner>;

  return (
    <div className="space-y-6">
      <SchemeChoice
        projectKey={projectKey}
        schemeId={data?.schemeId}
        schemes={schemes}
        canConfigure={canConfigure}
      />

      {shown && (
        <WorkflowGraph
          workflowId={shown.workflowId}
          caption={`${shown.issueTypeName} issues follow this workflow here.`}
        />
      )}

      <Card elevated className="overflow-hidden">
        {canMap && (
          <p className="border-b border-border px-4 py-2.5 text-sm text-ink-muted">
            Pick a workflow per issue type. Anything left on Follow the organization uses the organization's scheme.
          </p>
        )}
        <table data-testid="workflow-assignments" className="w-full text-sm">
          <thead className="border-b border-border bg-surface-raised text-left text-xs text-ink-muted">
            <tr>
              <th className="px-4 py-2 font-medium">Issue type</th>
              <th className="px-4 py-2 font-medium">Workflow</th>
              <th className="px-4 py-2 font-medium">Decided by</th>
              {canMap && <th className="px-4 py-2 font-medium" data-guide="workflow-decide">Decide</th>}
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
                choices={canMap ? workflows : undefined}
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
  choices,
  onDecide,
  deciding,
}: {
  assignment: Assignment;
  shown: boolean;
  onShow: () => void;
  /** The organization's workflows to choose from; absent when the reader may only look. */
  choices?: WorkflowSummary[];
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
        <Link
          to="/settings/workflows"
          search={{ workflow: assignment.workflowId }}
          className="text-ink hover:text-accent"
        >
          {assignment.workflowName}
        </Link>
      </td>
      <td className="px-4 py-2.5">
        <ScopeBadge origin={assignment.origin} />
        <span className="ml-2 text-xs text-ink-subtle">{originExplanation(assignment.origin)}</span>
      </td>
      {choices && (
        <td className="px-4 py-1.5" data-assignment-control>
          <SelectInput
            aria-label={`Workflow for ${assignment.issueTypeName}`}
            controlSize="sm"
            value={decided}
            disabled={deciding}
            onChange={(event) => onDecide(event.target.value || null)}
          >
            {canFollow && <option value="">Follow the organization</option>}
            {choices.map((workflow) => (
              <option key={workflow.id} value={workflow.id}>
                {workflow.name}
              </option>
            ))}
          </SelectInput>
        </td>
      )}
    </tr>
  );
}

function SchemeChoice({
  projectKey,
  schemeId,
  schemes,
  canConfigure,
}: {
  projectKey: string;
  schemeId?: string;
  schemes: Array<{ id: string; name: string; isDefault: boolean }>;
  canConfigure: boolean;
}) {
  const setScheme = useSetProjectScheme();
  const [picked, setPicked] = useState("");

  const organisation = schemes.find((scheme) => scheme.isDefault);
  const own = schemes.find((scheme) => scheme.id === schemeId);
  const available = schemes.filter((scheme) => !scheme.isDefault && scheme.id !== schemeId);

  return (
    <Card elevated className="p-4">
      {own ? (
        <>
          <p className="text-sm font-medium text-ink">
            This project uses its own scheme, {own.name}.
          </p>
          <p className="mt-1 text-sm text-ink-muted">
            Issue types it does not name still follow {organisation?.name ?? "the organization"}.
          </p>
        </>
      ) : (
        <>
          <p className="text-sm font-medium text-ink">
            This project follows the organization.
          </p>
          <p className="mt-1 text-sm text-ink-muted">
            Every issue type uses {organisation?.name ?? "the organization's scheme"}. Give the
            project a scheme of its own to disagree about some of them.
          </p>
        </>
      )}

      {canConfigure && available.length === 0 && !own && (
        <p className="mt-2 text-sm text-ink-subtle">
          There is no other scheme to give it yet. Build one under{" "}
          <Link
            to="/settings/workflows"
            search={{ workflow: undefined }}
            className="underline underline-offset-2 hover:text-ink"
          >
            Workflows
          </Link>
          , naming only the issue types this project should handle differently.
        </p>
      )}

      {canConfigure && (available.length > 0 || own) && (
        <div className="mt-3 flex flex-wrap items-center gap-2">
          <span className="text-sm text-ink-muted">Or use an existing scheme:</span>
          <label htmlFor="project-scheme" className="sr-only">
            Workflow scheme
          </label>
          <SelectInput id="project-scheme" value={picked} onChange={(event) => setPicked(event.target.value)}>
            <option value="">Choose a scheme...</option>
            {available.map((scheme) => (
              <option key={scheme.id} value={scheme.id}>
                {scheme.name}
              </option>
            ))}
          </SelectInput>
          <Button
            size="sm"
            disabled={!picked}
            loading={setScheme.isPending}
            onClick={() => setScheme.mutate({ projectKey, schemeId: picked })}
          >
            Use this scheme
          </Button>
          {own && (
            <Button
              size="sm"
              variant="ghost"
              onClick={() => setScheme.mutate({ projectKey, schemeId: null })}
            >
              Follow the organization
            </Button>
          )}
          <Link
            to="/settings/workflows"
            search={{ workflow: undefined }}
            className="text-xs text-ink-muted underline-offset-2 hover:text-ink hover:underline"
          >
            Manage schemes
          </Link>
        </div>
      )}

      {setScheme.error && <ErrorBanner>{(setScheme.error as Error).message}</ErrorBanner>}
    </Card>
  );
}
