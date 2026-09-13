import { useMemo } from "react";
import { Link } from "@tanstack/react-router";
import { useAccess } from "@/api/access";
import { useStatuses } from "@/api/issues";
import { useWorkflow } from "@/api/workflows";
import { Card, ErrorBanner, Skeleton } from "@/components/ui";
import { WorkflowCanvas } from "./designer/WorkflowCanvas";
import { fromWorkflow } from "./designer/graph";

/**
 * One workflow, drawn as the designer left it and touchable by nobody. The
 * project page shows it so a choice between workflows is a choice between
 * pictures, not between names.
 */
export function WorkflowGraph({ workflowId, caption }: { workflowId: string; caption?: string }) {
  const { data, isLoading, error } = useWorkflow(workflowId);
  const { data: statusData } = useStatuses();
  const { data: access } = useAccess();
  const statuses = useMemo(
    () => new Map((statusData?.statuses ?? []).map((status) => [status.id, status])),
    [statusData],
  );
  const design = useMemo(() => (data ? fromWorkflow(data) : null), [data]);

  if (error) return <ErrorBanner>{(error as Error).message}</ErrorBanner>;
  if (isLoading || !data || !design) {
    return (
      <Card className="p-4">
        <Skeleton lines={4} />
      </Card>
    );
  }
  const workflow = data.workflow;
  const steps = workflow.steps.length;
  const transitions = workflow.transitions.length;

  return (
    <Card className="overflow-hidden" data-workflow-graph={workflow.name}>
      <div className="flex flex-wrap items-baseline justify-between gap-x-4 gap-y-1 border-b border-border px-4 py-2.5">
        <div className="min-w-0">
          <h2 className="truncate text-sm font-medium text-ink">{workflow.name}</h2>
          {caption && <p className="text-xs text-ink-muted">{caption}</p>}
        </div>
        <p className="text-xs text-ink-subtle tabular-nums">
          {steps} {steps === 1 ? "status" : "statuses"}, {transitions} {transitions === 1 ? "transition" : "transitions"}
          {access?.canAdministerOrg && (
            <>
              {" "}
              <Link
                to="/settings/workflows"
                search={{ workflow: workflow.id }}
                className="text-ink-muted underline-offset-2 hover:text-ink hover:underline"
              >
                Open in the designer
              </Link>
            </>
          )}
        </p>
      </div>
      <div className="overflow-x-auto">
        <WorkflowCanvas design={design} statuses={statuses} readOnly label={`Workflow ${workflow.name}`} />
      </div>
    </Card>
  );
}
