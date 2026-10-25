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
            aria-label={t.workflows.library.findWorkflows}
            placeholder={t.workflows.library.findByName}
            value={q}
            onChange={(text) => onShow({ q: text })}
            className="w-56"
            data-library-search=""
          />
          {data && <span className="text-sm text-ink-muted tabular-nums">{shownOf(data.total, data.offset, workflows.length, t.kit.counted.workflow)}</span>}
        </div>
        {canConfigure && (
          <Button size="sm" variant="secondary" onClick={() => navigate({ to: "/settings/workflows/new" })}>
            {t.workflows.library.newWorkflow}
          </Button>
        )}
      </div>
      {isLoading && <p className="text-sm text-ink-muted">{t.workflows.library.loading}</p>}
      {data && workflows.length === 0 && q && (
        <EmptyState icon={<Icon.Search />} title={t.workflows.library.noWorkflow} description={t.workflows.library.noMatchDetail} />
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
        <Pager
          label={t.workflows.library.workflowPages}
          total={data.total}
          offset={data.offset}
          pageSize={LIBRARY_PAGE_SIZE}
          onChange={(next) => onShow({ q, offset: next })}
        />
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
    ? t.workflows.library.usedByEvery
    : workflow.schemeCount === 0
      ? t.workflows.library.usedByNothing
      : t.workflows.library.usedBySchemes(workflow.schemeCount);

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
              label={t.workflows.library.edit(workflow.name)}
              size="sm"
              variant="ghost"
              data-action="edit"
              onClick={() => navigate({ to: "/settings/workflows/$workflowId/design", params: { workflowId: workflow.id } })}
            />
            <IconButton
              icon={<Icon.Copy />}
              label={t.workflows.library.duplicate(workflow.name)}
              size="sm"
              variant="ghost"
              data-action="duplicate"
              aria-busy={copy.isPending}
              onClick={() => copy.mutate({ id: workflow.id, name: t.workflows.library.copyName(workflow.name) })}
            />
            {workflow.schemeCount === 0 && (
              <IconButton
                icon={<Icon.Trash />}
                label={t.workflows.library.delete(workflow.name)}
                size="sm"
                variant="ghost"
                data-action="delete"
                aria-busy={remove.isPending}
                onClick={async () =>
                  (await confirm({ noun: t.kit.confirm.things.workflow, body: t.workflows.library.deleteWorkflowBody(workflow.name) })) &&
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
            {workflow.inDefault && <Tag className="text-accent">{t.workflows.library.inDefault}</Tag>}
          </p>
          <p className="mt-0.5 text-xs text-ink-muted">{usedBy}</p>
          {workflow.description && <p className="mt-2 text-sm text-ink-muted">{workflow.description}</p>}
        </div>
        <div className="flex shrink-0 gap-6">
          <Stat label={t.workflows.library.statuses} value={workflow.stepCount} share={statusCount ? workflow.stepCount / statusCount : undefined} />
          <Stat label={t.workflows.library.transitions} value={workflow.transitionCount} />
          <Stat
            label={t.workflows.library.schemes}
            value={workflow.inDefault ? t.workflows.library.all : workflow.schemeCount}
            share={schemeCount && !workflow.inDefault ? workflow.schemeCount / schemeCount : undefined}
            tone="done"
          />
        </div>
      </div>
      {error && <ErrorBanner>{error.message}</ErrorBanner>}
    </Card>
  );
}
