import { useState } from "react";
import type { Status } from "@/api/issues";
import type { RuleType } from "@/api/workflows";
import { Button, Field, Select } from "@/components/ui";
import { StatusBadge } from "@/features/issues/badges";
import { NewStatusForm } from "./NewStatusForm";
import { RuleEditor } from "./RuleEditor";
import { ANYWHERE, addStatus, connect, removeEdge, removeStatus, setInitial, updateEdge, type Design, type Edge } from "./graph";
import type { Selection } from "./WorkflowCanvas";
import { t } from "@/i18n";

/**
 * The panel beside the canvas. It shows whatever is selected and is the
 * keyboard's way of doing what the pointer does on the canvas: adding a
 * status, drawing a transition, choosing where issues open.
 */
export function Inspector({
  design,
  statuses,
  catalogue,
  selection,
  onSelect,
  onChange,
  onCoined,
}: {
  design: Design;
  statuses: Map<string, Status>;
  catalogue: RuleType[];
  selection: Selection;
  onSelect: (selection: Selection) => void;
  onChange: (design: Design) => void;
  onCoined: (status: Status) => void;
}) {
  if (selection.kind === "edge") {
    const edge = design.edges.find((each) => each.key === selection.key);
    if (edge) {
      return (
        <EdgePanel
          edge={edge}
          statuses={statuses}
          catalogue={catalogue}
          onChange={(patch) => onChange(updateEdge(design, edge.key, patch))}
          onRemove={() => {
            onChange(removeEdge(design, edge.key));
            onSelect({ kind: "none" });
          }}
        />
      );
    }
  }
  if (selection.kind === "node") {
    const status = statuses.get(selection.statusId);
    if (status && design.nodes.some((node) => node.statusId === status.id)) {
      return <NodePanel status={status} design={design} statuses={statuses} onChange={onChange} onSelect={onSelect} />;
    }
  }
  return <DesignPanel design={design} statuses={statuses} onChange={onChange} onSelect={onSelect} onCoined={onCoined} />;
}

function DesignPanel({
  design,
  statuses,
  onChange,
  onSelect,
  onCoined,
}: {
  design: Design;
  statuses: Map<string, Status>;
  onChange: (design: Design) => void;
  onSelect: (selection: Selection) => void;
  onCoined: (status: Status) => void;
}) {
  const [adding, setAdding] = useState("");
  const available = [...statuses.values()].filter((status) => !design.nodes.some((node) => node.statusId === status.id));

  // Adding leaves this panel open, because the next thing is usually another.
  function add() {
    const status = statuses.get(adding);
    if (!status) return;
    onChange(addStatus(design, status));
    setAdding("");
  }

  return (
    <div className="space-y-4">
      <Field
        label={t.workflows.inspector.description}
        id="field-workflow-description"
        value={design.description}
        rows={2}
        placeholder={t.workflows.inspector.descriptionPlaceholder}
        onChange={(event) => onChange({ ...design, description: event.target.value })}
      />

      <div className="space-y-2">
        <Select label={t.workflows.inspector.addStatus} id="field-add-status" value={adding} onChange={(event) => setAdding(event.target.value)}>
          <option value="">{available.length ? t.workflows.inspector.chooseStatus : t.workflows.inspector.everyStatusOn}</option>
          {available.map((status) => (
            <option key={status.id} value={status.id}>
              {status.name}
            </option>
          ))}
        </Select>
        <Button size="sm" variant="secondary" disabled={!adding} onClick={add}>
          {t.workflows.inspector.addStatusButton}
        </Button>
      </div>

      <NewStatusForm
        onCreated={(status) => {
          onCoined(status);
          onChange(addStatus(design, status));
          onSelect({ kind: "node", statusId: status.id });
        }}
      />

      <div className="space-y-1 text-xs text-ink-subtle">
        <p>{t.workflows.inspector.helpDrag}</p>
        <p>{t.workflows.inspector.helpAnywhere}</p>
        <p>{t.workflows.inspector.helpClick}</p>
      </div>
    </div>
  );
}

function NodePanel({
  status,
  design,
  statuses,
  onChange,
  onSelect,
}: {
  status: Status;
  design: Design;
  statuses: Map<string, Status>;
  onChange: (design: Design) => void;
  onSelect: (selection: Selection) => void;
}) {
  const [target, setTarget] = useState("");
  const others = design.nodes.map((node) => statuses.get(node.statusId)).filter((each): each is Status => each !== undefined && each.id !== status.id);
  const leaving = design.edges.filter((edge) => edge.from === status.id);
  const isInitial = design.initialId === status.id;

  function addTransition() {
    const to = statuses.get(target);
    if (!to) return;
    const result = connect(design, status.id, to.id, to.name);
    if (!result) return;
    onChange(result.design);
    onSelect({ kind: "edge", key: result.key });
    setTarget("");
  }

  return (
    <div className="space-y-4" data-node-panel={status.name}>
      <div>
        <StatusBadge name={status.name} category={status.category} className="text-sm font-medium" />
        {status.description && <p className="mt-1 text-xs text-ink-muted">{status.description}</p>}
      </div>

      {isInitial ? (
        <p className="rounded-md bg-accent-subtle px-2 py-1.5 text-sm text-accent">{t.workflows.inspector.opensHere}</p>
      ) : (
        <Button size="sm" variant="secondary" onClick={() => onChange(setInitial(design, status.id))}>
          {t.workflows.inspector.makeInitial}
        </Button>
      )}

      <div className="space-y-2">
        <Select label={t.workflows.inspector.addTransitionTo} id="field-to-status" value={target} onChange={(event) => setTarget(event.target.value)}>
          <option value="">{others.length ? t.workflows.inspector.chooseStatus : t.workflows.inspector.addAnotherFirst}</option>
          {others.map((each) => (
            <option key={each.id} value={each.id}>
              {each.name}
            </option>
          ))}
        </Select>
        <Button size="sm" variant="secondary" disabled={!target} onClick={addTransition}>
          {t.workflows.inspector.addTransition}
        </Button>
      </div>

      {leaving.length > 0 && (
        <div>
          <p className="text-sm font-medium text-ink-muted">{t.workflows.inspector.leaving}</p>
          <ul className="mt-1 space-y-0.5">
            {leaving.map((edge) => (
              <li key={edge.key}>
                <Button variant="link" onClick={() => onSelect({ kind: "edge", key: edge.key })} className="text-ink hover:text-accent">
                  {t.workflows.inspector.leavingTo(edge.name || t.workflows.inspector.unnamed, statuses.get(edge.to)?.name ?? "?")}
                </Button>
              </li>
            ))}
          </ul>
        </div>
      )}

      <Button
        size="sm"
        variant="ghost"
        onClick={() => {
          onChange(removeStatus(design, status.id));
          onSelect({ kind: "none" });
        }}
      >
        {t.workflows.inspector.removeStatus}
      </Button>
    </div>
  );
}

function EdgePanel({
  edge,
  statuses,
  catalogue,
  onChange,
  onRemove,
}: {
  edge: Edge;
  statuses: Map<string, Status>;
  catalogue: RuleType[];
  onChange: (patch: Partial<Edge>) => void;
  onRemove: () => void;
}) {
  const from = edge.from === ANYWHERE ? t.workflows.inspector.anyStatus : (statuses.get(edge.from)?.name ?? "?");
  const to = statuses.get(edge.to)?.name ?? "?";
  return (
    <div className="space-y-4" data-edge-panel>
      <Field
        label={t.workflows.inspector.transitionName}
        id="field-transition-name"
        value={edge.name}
        placeholder={t.workflows.inspector.transitionPlaceholder}
        onChange={(event) => onChange({ name: event.target.value })}
      />
      <p className="text-sm text-ink-muted">
        {t.workflows.inspector.from} <span className="text-ink">{from}</span> {t.workflows.inspector.to} <span className="text-ink">{to}</span>.
      </p>
      <Field
        label={t.workflows.inspector.description}
        id="field-transition-description"
        value={edge.description}
        placeholder={t.workflows.inspector.transitionDescriptionPlaceholder}
        onChange={(event) => onChange({ description: event.target.value })}
      />

      <RuleEditor rules={edge.rules} catalogue={catalogue} onChange={(rules) => onChange({ rules })} />

      <Button size="sm" variant="ghost" onClick={onRemove}>
        {t.workflows.inspector.removeTransition}
      </Button>
    </div>
  );
}
