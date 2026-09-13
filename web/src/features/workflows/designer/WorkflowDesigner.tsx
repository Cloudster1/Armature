import { useMemo, useState } from "react";
import { useStatuses, type Status } from "@/api/issues";
import {
  useCreateWorkflow,
  useRuleTypes,
  useSaveWorkflow,
  useWorkflow,
  type Workflow,
} from "@/api/workflows";
import { Button, ErrorBanner, Field } from "@/components/ui";
import { Inspector } from "./Inspector";
import { WorkflowCanvas, type Selection } from "./WorkflowCanvas";
import { connect, emptyDesign, fromWorkflow, moveNode, problems, toInput, type Design } from "./graph";

/**
 * Draws a workflow as the graph it is. A status is a shared noun; a workflow
 * is one opinion about how issues move between them, so the canvas is where
 * the organization's statuses are arranged and the arrows say what is allowed.
 */
export function WorkflowDesigner({
  workflowId,
  onDone,
  onCancel,
}: {
  workflowId?: string;
  onDone: (saved: Workflow) => void;
  onCancel: () => void;
}) {
  const { data: statusData } = useStatuses();
  const { data: existing } = useWorkflow(workflowId ?? "");
  const { data: ruleData } = useRuleTypes();

  // A status coined in the inspector is known here at once, so its node draws
  // before the refetch of the organization's list has come back.
  const [coined, setCoined] = useState<Status[]>([]);
  const statuses = useMemo(
    () => new Map([...(statusData?.statuses ?? []), ...coined].map((status) => [status.id, status])),
    [statusData, coined],
  );
  const catalogue = ruleData?.ruleTypes ?? [];

  const [design, setDesign] = useState<{ loadedFrom?: string } & Design>(() => emptyDesign());
  const [selection, setSelection] = useState<Selection>({ kind: "none" });

  // The canvas is seeded once the workflow arrives, and not again, so a refetch
  // never undoes a drag.
  if (existing && design.loadedFrom !== existing.workflow.id) {
    setDesign({ loadedFrom: existing.workflow.id, ...fromWorkflow(existing) });
  }

  const create = useCreateWorkflow();
  const save = useSaveWorkflow();
  const pending = create.isPending || save.isPending;
  const error = (create.error ?? save.error) as Error | undefined;
  const hints = problems(design, statuses);

  function submit() {
    const input = toInput(design);
    const done = { onSuccess: (result: { workflow: Workflow }) => onDone(result.workflow) };
    if (workflowId) {
      save.mutate({ id: workflowId, ...input }, done);
      return;
    }
    create.mutate(input, done);
  }

  function connectNodes(from: string, to: string) {
    const result = connect(design, from, to, statuses.get(to)?.name ?? "");
    if (!result) return;
    setDesign((current) => ({ ...current, ...result.design }));
    setSelection({ kind: "edge", key: result.key });
  }

  const loading = workflowId !== undefined && !existing;

  return (
    <div className="space-y-3" data-workflow-designer>
      <div className="flex flex-wrap items-end gap-3">
        <div className="min-w-64 flex-1">
          <Field
            label="Name"
            value={design.name}
            placeholder="Bug triage"
            onChange={(event) => setDesign((current) => ({ ...current, name: event.target.value }))}
          />
        </div>
        <div className="flex gap-2">
          <Button loading={pending} disabled={loading} onClick={submit}>
            {workflowId ? "Save workflow" : "Create workflow"}
          </Button>
          <Button variant="ghost" onClick={onCancel}>
            Cancel
          </Button>
        </div>
      </div>

      {hints.length > 0 && (
        <p className="text-sm text-ink-muted" data-design-hints>
          Before this can be saved: {hints.join(" ")}
        </p>
      )}
      {error && <ErrorBanner>{error.message}</ErrorBanner>}

      <div className="flex items-start gap-3">
        <div className="min-w-0 flex-1 overflow-auto rounded-lg border border-border" style={{ maxHeight: "70vh" }}>
          {loading ? (
            <p className="p-4 text-sm text-ink-muted">Loading...</p>
          ) : (
            <WorkflowCanvas
              design={design}
              statuses={statuses}
              selection={selection}
              onSelect={setSelection}
              onMove={(statusId, x, y) => setDesign((current) => ({ ...current, ...moveNode(current, statusId, x, y) }))}
              onConnect={connectNodes}
            />
          )}
        </div>
        <aside className="w-80 shrink-0 rounded-lg border border-border bg-surface p-4">
          <Inspector
            design={design}
            statuses={statuses}
            catalogue={catalogue}
            selection={selection}
            onSelect={setSelection}
            onChange={(next) => setDesign((current) => ({ ...current, ...next }))}
            onCoined={(status) => setCoined((current) => [...current, status])}
          />
        </aside>
      </div>
    </div>
  );
}
