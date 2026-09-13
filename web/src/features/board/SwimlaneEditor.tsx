import { useState, type FormEvent } from "react";
import {
  useAddSwimlane,
  useBoard,
  useDeleteSwimlane,
  useReorderSwimlanes,
  useSetGrouping,
  useUpdateSwimlane,
  type Grouping,
  type Swimlane,
} from "@/api/boards";
import { Button, Card, Checkbox, Chip, ErrorBanner, Field, cx } from "@/components/ui";
import { useConfirm } from "@/features/shell/ConfirmProvider";
import { useStatuses, type Status } from "@/api/issues";

const groupings: Array<{ value: Grouping; label: string }> = [
  { value: "none", label: "No rows" },
  { value: "assignee", label: "Assignee" },
  { value: "priority", label: "Priority" },
  { value: "type", label: "Issue type" },
];

/**
 * Configures a board's swimlanes.
 *
 * A swimlane is nothing but a set of ticket states, so this screen is really a
 * mapping editor: which states belong to which lane, in what order the lanes
 * appear, and how many cards each should hold at once.
 */
export function SwimlaneEditor({ projectKey }: { projectKey: string }) {
  const { data, isLoading } = useBoard(projectKey);
  const { data: statusData } = useStatuses();
  const setGrouping = useSetGrouping();
  const reorder = useReorderSwimlanes();
  const [adding, setAdding] = useState(false);

  if (isLoading) return <p className="text-sm text-ink-muted">Loading...</p>;

  const board = data?.board;
  const statuses = statusData?.statuses ?? [];
  if (!board) return null;

  const swimlanes = board.swimlanes ?? [];

  // A state can only be in one swimlane, so the editor needs to know which are
  // already spoken for and by whom.
  const claimedBy = new Map<string, string>();
  for (const lane of swimlanes) {
    for (const status of lane.statuses) claimedBy.set(status.id, lane.name);
  }
  const unclaimed = statuses.filter((s) => !claimedBy.has(s.id));

  function moveLane(index: number, direction: -1 | 1) {
    const next = [...swimlanes];
    const swapWith = index + direction;
    if (swapWith < 0 || swapWith >= next.length) return;
    [next[index], next[swapWith]] = [next[swapWith]!, next[index]!];
    reorder.mutate({ projectKey, order: next.map((l) => l.id) });
  }

  return (
    <div className="space-y-4">
      {reorder.error && <ErrorBanner>{(reorder.error as Error).message}</ErrorBanner>}

      <Card className="p-4">
        <h3 className="text-sm font-medium text-ink">Rows within each swimlane</h3>
        <p className="mt-1 text-sm text-ink-muted">
          Swimlanes are made of ticket states. This groups the cards inside them a second way.
        </p>
        <div className="mt-3 flex flex-wrap gap-1.5">
          {groupings.map((option) => (
            <Chip key={option.value} pressed={board.groupBy === option.value} onClick={() => setGrouping.mutate({ projectKey, groupBy: option.value })}>
              {option.label}
            </Chip>
          ))}
        </div>
      </Card>

      {unclaimed.length > 0 && (
        <div className="rounded-md border border-warning/40 bg-warning/10 px-3 py-2 text-sm">
          <strong className="font-medium text-ink">
            {unclaimed.length} state{unclaimed.length === 1 ? "" : "s"} not on the board
          </strong>{" "}
          <span className="text-ink-muted">
            ({unclaimed.map((s) => s.name).join(", ")}). Cards in {unclaimed.length === 1 ? "it" : "them"}{" "}
            will not appear in any swimlane.
          </span>
        </div>
      )}

      <ul className="space-y-3">
        {swimlanes.map((swimlane, index) => (
          <li key={swimlane.id}>
            <SwimlaneRow
              projectKey={projectKey}
              swimlane={swimlane}
              statuses={statuses}
              claimedBy={claimedBy}
              canDelete={swimlanes.length > 1}
              isFirst={index === 0}
              isLast={index === swimlanes.length - 1}
              onMove={(direction) => moveLane(index, direction)}
            />
          </li>
        ))}
      </ul>

      {adding ? (
        <AddSwimlaneForm
          projectKey={projectKey}
          available={unclaimed}
          onDone={() => setAdding(false)}
        />
      ) : (
        <Button variant="secondary" onClick={() => setAdding(true)}>
          Add swimlane
        </Button>
      )}
    </div>
  );
}

function SwimlaneRow({
  projectKey,
  swimlane,
  statuses,
  claimedBy,
  canDelete,
  isFirst,
  isLast,
  onMove,
}: {
  projectKey: string;
  swimlane: Swimlane;
  statuses: Status[];
  claimedBy: Map<string, string>;
  canDelete: boolean;
  isFirst: boolean;
  isLast: boolean;
  onMove: (direction: -1 | 1) => void;
}) {
  const update = useUpdateSwimlane();
  const remove = useDeleteSwimlane();
  const confirm = useConfirm();
  const [name, setName] = useState(swimlane.name);
  const [wipLimit, setWipLimit] = useState(String(swimlane.wipLimit || ""));

  const selected = new Set(swimlane.statuses.map((s) => s.id));

  function toggleStatus(statusId: string) {
    const next = new Set(selected);
    if (next.has(statusId)) next.delete(statusId);
    else next.add(statusId);
    update.mutate({ projectKey, swimlaneId: swimlane.id, statusIds: [...next] });
  }

  function saveDetails(event: FormEvent) {
    event.preventDefault();
    update.mutate({
      projectKey,
      swimlaneId: swimlane.id,
      name,
      wipLimit: Number(wipLimit) || 0,
    });
  }

  return (
    <Card className="p-4" data-swimlane-editor={swimlane.name}>
      {update.error && (
        <div className="mb-3">
          <ErrorBanner>{(update.error as Error).message}</ErrorBanner>
        </div>
      )}

      <div className="flex items-start gap-3">
        <form onSubmit={saveDetails} className="flex flex-1 flex-wrap items-end gap-3">
          <div className="min-w-40 flex-1">
            <Field
              label={`Swimlane ${swimlane.position + 1} name`}
              value={name}
              onChange={(e) => setName(e.target.value)}
            />
          </div>
          <div className="w-28">
            <Field
              label="Card limit"
              type="number"
              min={0}
              value={wipLimit}
              onChange={(e) => setWipLimit(e.target.value)}
              placeholder="None"
            />
          </div>
          <Button type="submit" size="sm" variant="secondary" loading={update.isPending}>
            Save
          </Button>
        </form>

        <div className="flex shrink-0 items-center gap-1 pt-6">
          <Button
            size="sm"
            variant="ghost"
            disabled={isFirst}
            onClick={() => onMove(-1)}
            aria-label={`Move ${swimlane.name} left`}
          >
            &larr;
          </Button>
          <Button
            size="sm"
            variant="ghost"
            disabled={isLast}
            onClick={() => onMove(1)}
            aria-label={`Move ${swimlane.name} right`}
          >
            &rarr;
          </Button>
          <Button
            size="sm"
            variant="ghost"
            disabled={!canDelete}
            loading={remove.isPending}
            onClick={async () => (await confirm({ noun: "swimlane", body: `${swimlane.name} goes from the board; the statuses it showed move to the next one.` })) && remove.mutate({ projectKey, swimlaneId: swimlane.id })}
            title={canDelete ? "Delete this swimlane" : "A board needs at least one swimlane"}
          >
            Delete
          </Button>
        </div>
      </div>

      <fieldset className="mt-3">
        <legend className="text-xs font-medium text-ink-muted">States in this swimlane</legend>
        <div className="mt-2 flex flex-wrap gap-1.5">
          {statuses.map((status) => {
            const mine = selected.has(status.id);
            const owner = claimedBy.get(status.id);
            const takenByAnother = Boolean(owner) && !mine;
            return (
              <Checkbox
                key={status.id}
                label={status.name}
                checked={mine}
                disabled={takenByAnother}
                title={takenByAnother ? `Already in ${owner}` : undefined}
                onChange={() => toggleStatus(status.id)}
                className={cx("rounded border px-2 py-1 text-xs", mine ? "border-accent bg-accent-subtle text-accent" : "border-border")}
              />
            );
          })}
        </div>
      </fieldset>
    </Card>
  );
}

function AddSwimlaneForm({
  projectKey,
  available,
  onDone,
}: {
  projectKey: string;
  available: Status[];
  onDone: () => void;
}) {
  const add = useAddSwimlane();
  const [name, setName] = useState("");
  const [chosen, setChosen] = useState<string[]>([]);

  function onSubmit(event: FormEvent) {
    event.preventDefault();
    add.mutate({ projectKey, name, statusIds: chosen }, { onSuccess: onDone });
  }

  return (
    <Card className="p-4">
      <form onSubmit={onSubmit} className="space-y-3" noValidate>
        {add.error && <ErrorBanner>{(add.error as Error).message}</ErrorBanner>}
        <Field
          label="New swimlane name"
          autoFocus
          required
          value={name}
          onChange={(e) => setName(e.target.value)}
          placeholder="Blocked"
        />
        {available.length > 0 && (
          <fieldset>
            <legend className="text-xs font-medium text-ink-muted">
              States for it (only ones not already on the board)
            </legend>
            <div className="mt-2 flex flex-wrap gap-1.5">
              {available.map((status) => (
                <Checkbox
                  key={status.id}
                  label={status.name}
                  checked={chosen.includes(status.id)}
                  onChange={() => setChosen((current) => (current.includes(status.id) ? current.filter((id) => id !== status.id) : [...current, status.id]))}
                  className={cx("rounded border px-2 py-1 text-xs", chosen.includes(status.id) ? "border-accent bg-accent-subtle text-accent" : "border-border")}
                />
              ))}
            </div>
          </fieldset>
        )}
        <div className="flex gap-2">
          <Button type="submit" loading={add.isPending} disabled={!name.trim()}>
            Add swimlane
          </Button>
          <Button type="button" variant="secondary" onClick={onDone}>
            Cancel
          </Button>
        </div>
      </form>
    </Card>
  );
}
