import { useState, type FormEvent } from "react";
import {
  useAddSwimlane,
  useBoardById,
  useDeleteSwimlane,
  useReorderSwimlanes,
  useUpdateBoard,
  useUpdateSwimlane,
  type Grouping,
  type Swimlane,
} from "@/api/boards";
import { Button, Card, Checkbox, Chip, cx, ErrorBanner, Field, Input, Skeleton } from "@/components/ui";
import { useConfirm } from "@/features/shell/ConfirmProvider";
import { useProjectStatuses, type Status } from "@/api/issues";
import { t } from "@/i18n";

// Read while rendering, so the chips speak the language on screen.
const groupings = (): Array<{ value: Grouping; label: string }> => [
  { value: "none", label: t.board.swimlanes.none },
  { value: "assignee", label: t.board.swimlanes.assignee },
  { value: "priority", label: t.board.swimlanes.priority },
  { value: "type", label: t.board.swimlanes.type },
];

/** Maps ticket states onto the lanes of the board it is handed, the one on
 * screen; a project can have several, so it never looks the board up itself. */
export function SwimlaneEditor({ boardId }: { boardId: string }) {
  const { data, isLoading } = useBoardById(boardId);
  // Only the project's workflows can put a card in a state, so only theirs are offered.
  const { data: statusData, isError: statusesFailed } = useProjectStatuses(data?.board?.projectKey ?? "");
  const updateBoard = useUpdateBoard();
  const reorder = useReorderSwimlanes();
  const [adding, setAdding] = useState(false);

  // The project's states come after the board; drawn without them, every lane grows under the pointer when they land.
  if (isLoading || (data?.board && !statusData && !statusesFailed)) return <Skeleton lines={4} />;

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
    reorder.mutate({ boardId, order: next.map((l) => l.id) });
  }

  return (
    <div className="space-y-4">
      {reorder.error && <ErrorBanner>{(reorder.error as Error).message}</ErrorBanner>}
      {updateBoard.error && <ErrorBanner>{(updateBoard.error as Error).message}</ErrorBanner>}

      <Card className="p-4">
        <h3 className="text-sm font-medium text-ink">{t.board.swimlanes.rowsTitle}</h3>
        <p className="mt-1 text-sm text-ink-muted">{t.board.swimlanes.rowsHint}</p>
        <div className="mt-3 flex flex-wrap gap-1.5">
          {groupings().map((option) => (
            <Chip key={option.value} pressed={board.groupBy === option.value} onClick={() => updateBoard.mutate({ id: boardId, groupBy: option.value })}>
              {option.label}
            </Chip>
          ))}
        </div>
      </Card>

      {board.type === "kanban" && <FinishedWork key={board.doneForDays} boardId={boardId} days={board.doneForDays} />}

      {unclaimed.length > 0 && (
        <div className="rounded-md border border-warning/40 bg-warning/10 px-3 py-2 text-sm">
          <strong className="font-medium text-ink">{t.board.swimlanes.unclaimed(unclaimed.length)}</strong>{" "}
          <span className="text-ink-muted">{t.board.swimlanes.unclaimedHint(unclaimed.map((s) => s.name).join(", "), unclaimed.length)}</span>
        </div>
      )}

      <ul className="space-y-3">
        {swimlanes.map((swimlane, index) => (
          <li key={swimlane.id}>
            <SwimlaneRow
              boardId={boardId}
              swimlane={swimlane}
              statuses={statuses}
              claimedBy={claimedBy}
              canDelete={swimlanes.length > 1}
              isFirst={index === 0}
              isLast={index === swimlanes.length - 1}
              moving={reorder.isPending}
              onMove={(direction) => moveLane(index, direction)}
            />
          </li>
        ))}
      </ul>

      {adding ? (
        <AddSwimlaneForm boardId={boardId} available={unclaimed} onDone={() => setAdding(false)} />
      ) : (
        <Button variant="secondary" onClick={() => setAdding(true)}>
          {t.board.swimlanes.add}
        </Button>
      )}
    </div>
  );
}

/** How long a kanban board keeps finished work, so its last lane stops growing. */
function FinishedWork({ boardId, days }: { boardId: string; days: number }) {
  const update = useUpdateBoard();
  const [text, setText] = useState(String(days));

  function save(event: FormEvent) {
    event.preventDefault();
    update.mutate({ id: boardId, doneForDays: Math.max(0, Math.floor(Number(text) || 0)) });
  }

  return (
    <Card className="p-4" data-done-for-days-editor="">
      <h3 className="text-sm font-medium text-ink">{t.board.swimlanes.finishedTitle}</h3>
      <p className="mt-1 text-sm text-ink-muted">{t.board.swimlanes.finishedHint}</p>
      {update.error && (
        <div className="mt-3">
          <ErrorBanner>{(update.error as Error).message}</ErrorBanner>
        </div>
      )}
      <form onSubmit={save} className="mt-3 flex flex-wrap items-center gap-2">
        <label htmlFor="board-done-for-days" className="text-sm text-ink-muted">
          {t.board.swimlanes.staysFor}
        </label>
        <Input
          id="board-done-for-days"
          type="number"
          min={0}
          step={1}
          inputMode="numeric"
          aria-describedby="board-done-for-days-hint"
          value={text}
          onChange={(event) => setText(event.target.value)}
          className="max-w-20 text-right tabular-nums"
        />
        <span className="text-sm text-ink-muted">{t.board.swimlanes.days}</span>
        <Button type="submit" size="sm" variant="secondary" loading={update.isPending}>
          {t.board.swimlanes.save}
        </Button>
      </form>
      <p id="board-done-for-days-hint" className="mt-1 text-xs text-ink-subtle">
        {t.board.swimlanes.zeroKeeps}
      </p>
    </Card>
  );
}

function SwimlaneRow({
  boardId,
  swimlane,
  statuses,
  claimedBy,
  canDelete,
  isFirst,
  isLast,
  moving,
  onMove,
}: {
  boardId: string;
  swimlane: Swimlane;
  statuses: Status[];
  claimedBy: Map<string, string>;
  canDelete: boolean;
  isFirst: boolean;
  isLast: boolean;
  /** A move builds on the order on screen, so a second one waits for the first to land. */
  moving: boolean;
  onMove: (direction: -1 | 1) => void;
}) {
  const update = useUpdateSwimlane();
  const remove = useDeleteSwimlane();
  const confirm = useConfirm();
  const [name, setName] = useState(swimlane.name);
  const [wipLimit, setWipLimit] = useState(String(swimlane.wipLimit || ""));

  const selected = new Set(swimlane.statuses.map((s) => s.id));
  // A state the project no longer reaches stays on offer while the lane holds it, so it can be let go.
  const offered = [...statuses, ...swimlane.statuses.filter((held) => !statuses.some((s) => s.id === held.id))];

  function toggleStatus(statusId: string) {
    const next = new Set(selected);
    if (next.has(statusId)) next.delete(statusId);
    else next.add(statusId);
    update.mutate({ boardId, swimlaneId: swimlane.id, statusIds: [...next] });
  }

  function saveDetails(event: FormEvent) {
    event.preventDefault();
    update.mutate({
      boardId,
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
            <Field label={t.board.swimlanes.laneName(swimlane.position + 1)} value={name} onChange={(e) => setName(e.target.value)} />
          </div>
          <div className="w-28">
            <Field
              label={t.board.swimlanes.limit}
              type="number"
              min={0}
              value={wipLimit}
              onChange={(e) => setWipLimit(e.target.value)}
              placeholder={t.board.swimlanes.noLimit}
            />
          </div>
          <Button type="submit" size="sm" variant="secondary" loading={update.isPending}>
            {t.board.swimlanes.save}
          </Button>
        </form>

        <div className="flex shrink-0 items-center gap-1 pt-6">
          <Button size="sm" variant="ghost" disabled={isFirst || moving} onClick={() => onMove(-1)} aria-label={t.board.swimlanes.moveLeft(swimlane.name)}>
            &larr;
          </Button>
          <Button size="sm" variant="ghost" disabled={isLast || moving} onClick={() => onMove(1)} aria-label={t.board.swimlanes.moveRight(swimlane.name)}>
            &rarr;
          </Button>
          <Button
            size="sm"
            variant="ghost"
            disabled={!canDelete}
            loading={remove.isPending}
            onClick={async () =>
              (await confirm({
                noun: t.kit.confirm.things.swimlane,
                body: t.board.swimlanes.deleteBody(swimlane.name),
              })) && remove.mutate({ boardId, swimlaneId: swimlane.id })
            }
            title={canDelete ? t.board.swimlanes.deleteThis : t.board.swimlanes.needsOne}
          >
            {t.board.swimlanes.delete}
          </Button>
        </div>
      </div>

      <fieldset className="mt-3">
        <legend className="text-xs font-medium text-ink-muted">{t.board.swimlanes.states}</legend>
        <div className="mt-2 flex flex-wrap gap-1.5">
          {offered.map((status) => {
            const mine = selected.has(status.id);
            const owner = claimedBy.get(status.id);
            const takenByAnother = Boolean(owner) && !mine;
            return (
              <Checkbox
                key={status.id}
                label={status.name}
                checked={mine}
                disabled={takenByAnother}
                title={takenByAnother ? t.board.swimlanes.alreadyIn(owner ?? "") : undefined}
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

function AddSwimlaneForm({ boardId, available, onDone }: { boardId: string; available: Status[]; onDone: () => void }) {
  const add = useAddSwimlane();
  const [name, setName] = useState("");
  const [chosen, setChosen] = useState<string[]>([]);

  function onSubmit(event: FormEvent) {
    event.preventDefault();
    add.mutate({ boardId, name, statusIds: chosen }, { onSuccess: onDone });
  }

  return (
    <Card className="p-4">
      <form onSubmit={onSubmit} className="space-y-3" noValidate>
        {add.error && <ErrorBanner>{(add.error as Error).message}</ErrorBanner>}
        <Field
          label={t.board.swimlanes.newName}
          autoFocus
          required
          value={name}
          onChange={(e) => setName(e.target.value)}
          placeholder={t.board.swimlanes.newPlaceholder}
        />
        {available.length > 0 && (
          <fieldset>
            <legend className="text-xs font-medium text-ink-muted">{t.board.swimlanes.newStates}</legend>
            <div className="mt-2 flex flex-wrap gap-1.5">
              {available.map((status) => (
                <Checkbox
                  key={status.id}
                  label={status.name}
                  checked={chosen.includes(status.id)}
                  onChange={() => setChosen((current) => (current.includes(status.id) ? current.filter((id) => id !== status.id) : [...current, status.id]))}
                  className={cx(
                    "rounded border px-2 py-1 text-xs",
                    chosen.includes(status.id) ? "border-accent bg-accent-subtle text-accent" : "border-border",
                  )}
                />
              ))}
            </div>
          </fieldset>
        )}
        <div className="flex gap-2">
          <Button type="submit" loading={add.isPending} disabled={!name.trim()}>
            {t.board.swimlanes.add}
          </Button>
          <Button type="button" variant="secondary" onClick={onDone}>
            {t.board.swimlanes.cancel}
          </Button>
        </div>
      </form>
    </Card>
  );
}
