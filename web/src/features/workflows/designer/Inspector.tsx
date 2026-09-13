import { useState } from "react";
import type { Status } from "@/api/issues";
import type { RuleType } from "@/api/workflows";
import { Button, Field, Select } from "@/components/ui";
import { StatusBadge } from "@/features/issues/badges";
import { NewStatusForm } from "./NewStatusForm";
import { RuleEditor } from "./RuleEditor";
import {
  ANYWHERE,
  addStatus,
  connect,
  removeEdge,
  removeStatus,
  setInitial,
  updateEdge,
  type Design,
  type Edge,
} from "./graph";
import type { Selection } from "./WorkflowCanvas";

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
      return (
        <NodePanel
          status={status}
          design={design}
          statuses={statuses}
          onChange={onChange}
          onSelect={onSelect}
        />
      );
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
  const available = [...statuses.values()].filter(
    (status) => !design.nodes.some((node) => node.statusId === status.id),
  );

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
        label="Description"
        id="field-workflow-description"
        value={design.description}
        rows={2}
        placeholder="What this workflow is for. Optional."
        onChange={(event) => onChange({ ...design, description: event.target.value })}
      />

      <div className="space-y-2">
        <Select label="Add a status" id="field-add-status" value={adding} onChange={(event) => setAdding(event.target.value)}>
          <option value="">{available.length ? "Choose a status" : "Every status is on the canvas"}</option>
          {available.map((status) => (
            <option key={status.id} value={status.id}>
              {status.name}
            </option>
          ))}
        </Select>
        <Button size="sm" variant="secondary" disabled={!adding} onClick={add}>
          Add status
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
        <p>Drag a status to move it. Drag from the ring on its right edge onto another status to add a transition.</p>
        <p>Transitions drawn from the dashed Any status box are available from everywhere, which is how Close or Reopen is expressed.</p>
        <p>Click a status or a transition to change it here.</p>
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
  const others = design.nodes
    .map((node) => statuses.get(node.statusId))
    .filter((each): each is Status => each !== undefined && each.id !== status.id);
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
        <p className="rounded-md bg-accent-subtle px-2 py-1.5 text-sm text-accent">New issues open here.</p>
      ) : (
        <Button size="sm" variant="secondary" onClick={() => onChange(setInitial(design, status.id))}>
          New issues open here
        </Button>
      )}

      <div className="space-y-2">
        <Select label="Add a transition to" id="field-to-status" value={target} onChange={(event) => setTarget(event.target.value)}>
          <option value="">{others.length ? "Choose a status" : "Add another status first"}</option>
          {others.map((each) => (
            <option key={each.id} value={each.id}>
              {each.name}
            </option>
          ))}
        </Select>
        <Button size="sm" variant="secondary" disabled={!target} onClick={addTransition}>
          Add transition
        </Button>
      </div>

      {leaving.length > 0 && (
        <div>
          <p className="text-sm font-medium text-ink-muted">Leaving here</p>
          <ul className="mt-1 space-y-0.5">
            {leaving.map((edge) => (
              <li key={edge.key}>
                <Button variant="link" onClick={() => onSelect({ kind: "edge", key: edge.key })} className="text-ink hover:text-accent">
                  {edge.name || "unnamed"} to {statuses.get(edge.to)?.name ?? "?"}
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
        Remove from workflow
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
  const from = edge.from === ANYWHERE ? "any status" : (statuses.get(edge.from)?.name ?? "?");
  const to = statuses.get(edge.to)?.name ?? "?";
  return (
    <div className="space-y-4" data-edge-panel>
      <Field
        label="Transition name"
        id="field-transition-name"
        value={edge.name}
        placeholder="Start progress"
        onChange={(event) => onChange({ name: event.target.value })}
      />
      <p className="text-sm text-ink-muted">
        From <span className="text-ink">{from}</span> to <span className="text-ink">{to}</span>.
      </p>
      <Field
        label="Description"
        id="field-transition-description"
        value={edge.description}
        placeholder="When to take it. Optional."
        onChange={(event) => onChange({ description: event.target.value })}
      />

      <RuleEditor rules={edge.rules} catalogue={catalogue} onChange={(rules) => onChange({ rules })} />

      <Button size="sm" variant="ghost" onClick={onRemove}>
        Remove transition
      </Button>
    </div>
  );
}
