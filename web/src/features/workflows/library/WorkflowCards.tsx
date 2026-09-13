import { useNavigate } from "@tanstack/react-router";
import { useStatuses } from "@/api/issues";
import { useCopyWorkflow, useDeleteWorkflow, useSchemes, useWorkflows, type WorkflowSummary } from "@/api/workflows";
import { Button, Card, ErrorBanner, IconButton, Stat, Tag } from "@/components/ui";
import { Icon } from "@/components/icons";
import { useConfirm } from "@/features/shell/ConfirmProvider";

/** The organization's workflows, one card each: what it is, who uses it, and what can be done to it. */
export function WorkflowCards({ focused, canConfigure }: { focused?: string; canConfigure: boolean }) {
  const { data, isLoading } = useWorkflows();
  const { data: schemeData } = useSchemes();
  const { data: statusData } = useStatuses();
  const navigate = useNavigate();
  const workflows = data?.workflows ?? [];
  const schemeCount = schemeData?.schemes.length ?? 0;
  const statusCount = statusData?.statuses.length ?? 0;

  return (
    <section className="space-y-3">
      {canConfigure && (
        <div className="flex justify-end">
          <Button size="sm" variant="secondary" onClick={() => navigate({ to: "/settings/workflows/new" })}>
            New workflow
          </Button>
        </div>
      )}
      {isLoading && <p className="text-sm text-ink-muted">Loading...</p>}
      {workflows.map((workflow) => (
        <WorkflowCard key={workflow.id} workflow={workflow} highlighted={workflow.id === focused} canConfigure={canConfigure} schemeCount={schemeCount} statusCount={statusCount} />
      ))}
    </section>
  );
}

function WorkflowCard({ workflow, highlighted, canConfigure, schemeCount, statusCount }: { workflow: WorkflowSummary; highlighted: boolean; canConfigure: boolean; schemeCount: number; statusCount: number }) {
  const copy = useCopyWorkflow();
  const remove = useDeleteWorkflow();
  const confirm = useConfirm();
  const navigate = useNavigate();
  const error = (copy.error ?? remove.error) as Error | undefined;
  const usedBy = workflow.inDefault
    ? "Used by every project that has not named one of its own."
    : workflow.schemeCount === 0
      ? "Used by nothing yet."
      : `Used by ${workflow.schemeCount} ${workflow.schemeCount === 1 ? "scheme" : "schemes"}.`;

  return (
    <Card
      elevated
      className={highlighted ? "border-accent p-4" : "p-4"}
      data-workflow-card={workflow.name}
      actions={
        canConfigure ? (
          <>
            <IconButton icon={<Icon.Edit />} label={`Edit ${workflow.name}`} size="sm" variant="ghost" data-action="edit" onClick={() => navigate({ to: "/settings/workflows/$workflowId/design", params: { workflowId: workflow.id } })} />
            <IconButton icon={<Icon.Copy />} label={`Duplicate ${workflow.name}`} size="sm" variant="ghost" data-action="duplicate" aria-busy={copy.isPending} onClick={() => copy.mutate({ id: workflow.id, name: `${workflow.name} copy` })} />
            {workflow.schemeCount === 0 && (
              <IconButton
                icon={<Icon.Trash />}
                label={`Delete ${workflow.name}`}
                size="sm"
                variant="ghost"
                data-action="delete"
                aria-busy={remove.isPending}
                onClick={async () => (await confirm({ noun: "workflow", body: `${workflow.name} goes, if nothing still uses it.` })) && remove.mutate(workflow.id)}
              />
            )}
          </>
        ) : undefined
      }
    >
      <div className="flex flex-wrap items-start justify-between gap-4 pr-24">
        <div className="min-w-0 flex-1">
          <p className="flex items-center gap-2 text-sm font-medium text-ink">
            {workflow.name}
            {workflow.inDefault && <Tag className="text-accent">The organization's</Tag>}
          </p>
          <p className="mt-0.5 text-xs text-ink-muted">{usedBy}</p>
          {workflow.description && <p className="mt-2 text-sm text-ink-muted">{workflow.description}</p>}
        </div>
        <div className="flex shrink-0 gap-6">
          <Stat label="Statuses" value={workflow.stepCount} share={statusCount ? workflow.stepCount / statusCount : undefined} />
          <Stat label="Transitions" value={workflow.transitionCount} />
          <Stat label="Schemes" value={workflow.inDefault ? "all" : workflow.schemeCount} share={schemeCount && !workflow.inDefault ? workflow.schemeCount / schemeCount : undefined} tone="done" />
        </div>
      </div>
      {error && <ErrorBanner>{error.message}</ErrorBanner>}
    </Card>
  );
}
