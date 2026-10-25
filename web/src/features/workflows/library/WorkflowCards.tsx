import { useEffect } from "react";
import { useNavigate } from "@tanstack/react-router";
import { useStatuses } from "@/api/issues";
import { useCopyWorkflow, useDeleteWorkflow, useSchemes, useWorkflows, type LibraryParams, type WorkflowSummary } from "@/api/workflows";
import { Button, Card, EmptyState, ErrorBanner, IconButton, ListSearch, Pager, Stat, Tag, clampOffset, shownOf } from "@/components/ui";
import { Icon } from "@/components/icons";
import { useConfirm } from "@/features/shell/ConfirmProvider";
import { LIBRARY_PAGE_SIZE } from "@/config";
import { t } from "@/i18n";

interface WorkflowCardsProps {
  focused?: string;
  q: string;
  offset: number;
  /** Shows another search or page; replace keeps the address's history as it is. */
  onShow: (next: LibraryParams, replace?: boolean) => void;
  canConfigure: boolean;
}

/** The organization's workflows, a page of cards at a time: what each is, who uses it, and what can be done to it. */
export function WorkflowCards({ focused, q, offset, onShow, canConfigure }: WorkflowCardsProps) {
  const { data, isLoading } = useWorkflows({ q, offset });
  // One scheme is enough to learn how many there are, which each card's share is of.
  const { data: schemeData } = useSchemes({ limit: 1 });
  const { data: statusData } = useStatuses();
  const navigate = useNavigate();
  const workflows = data?.workflows ?? [];
  const schemeCount = schemeData?.total ?? 0;
  const statusCount = statusData?.statuses.length ?? 0;

  // Workflows leave the list while it is open; a page past the end steps back.
  useEffect(() => {
    if (data && offset > 0 && offset >= data.total) onShow({ q, offset: clampOffset(offset, data.total, LIBRARY_PAGE_SIZE) }, true);
  }, [data, offset, q, onShow]);

  return (
    <section className="space-y-3">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <div className="flex items-center gap-3">
          <ListSearch
            aria-label="Find workflows"
            placeholder="Find by name"
            value={q}
            onChange={(text) => onShow({ q: text })}
            className="w-56"
            data-library-search=""
          />
          {data && <span className="text-sm text-ink-muted tabular-nums">{shownOf(data.total, data.offset, workflows.length, t.kit.counted.workflow)}</span>}
        </div>
        {canConfigure && (
          <Button size="sm" variant="secondary" onClick={() => navigate({ to: "/settings/workflows/new" })}>
            New workflow
          </Button>
        )}
      </div>
      {isLoading && <p className="text-sm text-ink-muted">Loading...</p>}
      {data && workflows.length === 0 && q && (
        <EmptyState icon={<Icon.Search />} title="No workflow matches that" description="Try part of its name, or clear the search." />
      )}
      {workflows.map((workflow) => (
        <WorkflowCard
          key={workflow.id}
          workflow={workflow}
          highlighted={workflow.id === focused}
          canConfigure={canConfigure}
          schemeCount={schemeCount}
          statusCount={statusCount}
        />
      ))}
      {data && (
        <Pager label="Workflow pages" total={data.total} offset={data.offset} pageSize={LIBRARY_PAGE_SIZE} onChange={(next) => onShow({ q, offset: next })} />
      )}
    </section>
  );
}

function WorkflowCard({
  workflow,
  highlighted,
  canConfigure,
  schemeCount,
  statusCount,
}: {
  workflow: WorkflowSummary;
  highlighted: boolean;
  canConfigure: boolean;
  schemeCount: number;
  statusCount: number;
}) {
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
            <IconButton
              icon={<Icon.Edit />}
              label={`Edit ${workflow.name}`}
              size="sm"
              variant="ghost"
              data-action="edit"
              onClick={() => navigate({ to: "/settings/workflows/$workflowId/design", params: { workflowId: workflow.id } })}
            />
            <IconButton
              icon={<Icon.Copy />}
              label={`Duplicate ${workflow.name}`}
              size="sm"
              variant="ghost"
              data-action="duplicate"
              aria-busy={copy.isPending}
              onClick={() => copy.mutate({ id: workflow.id, name: `${workflow.name} copy` })}
            />
            {workflow.schemeCount === 0 && (
              <IconButton
                icon={<Icon.Trash />}
                label={`Delete ${workflow.name}`}
                size="sm"
                variant="ghost"
                data-action="delete"
                aria-busy={remove.isPending}
                onClick={async () =>
                  (await confirm({ noun: t.kit.confirm.things.workflow, body: `${workflow.name} goes, if nothing still uses it.` })) &&
                  remove.mutate(workflow.id)
                }
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
            {workflow.inDefault && <Tag className="text-accent">In the default</Tag>}
          </p>
          <p className="mt-0.5 text-xs text-ink-muted">{usedBy}</p>
          {workflow.description && <p className="mt-2 text-sm text-ink-muted">{workflow.description}</p>}
        </div>
        <div className="flex shrink-0 gap-6">
          <Stat label="Statuses" value={workflow.stepCount} share={statusCount ? workflow.stepCount / statusCount : undefined} />
          <Stat label="Transitions" value={workflow.transitionCount} />
          <Stat
            label="Schemes"
            value={workflow.inDefault ? "all" : workflow.schemeCount}
            share={schemeCount && !workflow.inDefault ? workflow.schemeCount / schemeCount : undefined}
            tone="done"
          />
        </div>
      </div>
      {error && <ErrorBanner>{error.message}</ErrorBanner>}
    </Card>
  );
}
