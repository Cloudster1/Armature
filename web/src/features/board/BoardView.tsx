import { useRef, useState, type DragEvent } from "react";
import { Link } from "@tanstack/react-router";
import {
  groupCards,
  useBoard,
  useBoardById,
  useMoveCard,
  useSprintBoard,
  type Card as BoardCard,
  type Swimlane,
} from "@/api/boards";
import { Avatar, PriorityBadge, TypeBadge } from "@/features/issues/badges";
import { LabelChips } from "@/features/labels/LabelChip";
import { EmptyState, ErrorBanner, cx, IconButton, useToast } from "@/components/ui";
import { Icon } from "@/components/icons";
import { useIssueDrawer, useIssueList } from "@/features/issues/IssueDrawer";
import { MS_PER_DAY } from "@/features/plan/scale";

/** How a sprint's end reads on the board: in days, since that is how a sprint is felt. */
export function describeEnd(endsOn: string, today = new Date()): string {
  const end = new Date(endsOn);
  const days = Math.round((end.getTime() - today.setHours(0, 0, 0, 0)) / MS_PER_DAY);
  if (days === 0) return "ends today";
  if (days === 1) return "ends tomorrow";
  if (days > 1) return `ends in ${days} days`;
  return `ended ${-days === 1 ? "yesterday" : `${-days} days ago`}`;
}

/** What is being dragged, and where it came from. */
interface Dragging {
  issueKey: string;
  fromSwimlaneId: string;
}

/** Where a card would land if dropped right now. */
interface DropTarget {
  swimlaneId: string;
  /** The card to insert before, or null for the end of the lane. */
  beforeKey: string | null;
}

/**
 * The drag payload is carried on the event's own DataTransfer as well as in
 * component state. State is what the board renders from, but a React state
 * update does not apply until the next render, and dragstart, dragover and drop
 * can all arrive before one happens. Reading the payload back off the event at
 * drop time makes the move independent of render timing.
 */
const DRAG_MIME = "application/x-armature-card";

export function BoardView({
  projectKey,
  boardId,
  sprintId,
}: {
  projectKey: string;
  boardId?: string;
  /** Set, the board is one sprint's: its stream's board showing only that sprint's work. */
  sprintId?: string;
}) {
  // A project's first board is addressed by the project, the rest by id, and a
  // sprint's by the sprint. All read the same shape, so the view does not care
  // which it was given.
  const byProject = useBoard(boardId || sprintId ? "" : projectKey);
  const byID = useBoardById(sprintId ? "" : (boardId ?? ""));
  const bySprint = useSprintBoard(sprintId ?? "");
  const { data, isLoading, error } = sprintId ? bySprint : boardId ? byID : byProject;
  const move = useMoveCard();
  const toast = useToast();

  // Refs hold the live drag for the logic, because they update synchronously.
  // The matching state exists only so the board can show what is happening.
  const draggingRef = useRef<Dragging | null>(null);
  const targetRef = useRef<DropTarget | null>(null);
  const [dragging, setDragging] = useState<Dragging | null>(null);
  const [target, setTarget] = useState<DropTarget | null>(null);

  function beginDrag(next: Dragging) {
    draggingRef.current = next;
    setDragging(next);
  }

  function hoverOver(next: DropTarget) {
    targetRef.current = next;
    setTarget(next);
  }

  function endDrag() {
    draggingRef.current = null;
    targetRef.current = null;
    setDragging(null);
    setTarget(null);
  }

  // Cards in column order, top to bottom, so the panel's next is the card below.
  useIssueList((data?.board?.swimlanes ?? []).flatMap((lane) => lane.cards.map((card) => card.key)));

  if (isLoading) return <p className="text-sm text-ink-muted">Loading the board...</p>;
  if (error) return <ErrorBanner>{(error as Error).message}</ErrorBanner>;

  const board = data?.board;
  if (!board) return null;

  const swimlanes = board.swimlanes ?? [];
  const unmapped = board.unmapped ?? [];

  // A scrum board between sprints has nothing to show, and says why rather
  // than drawing four empty columns.
  if (board.type === "scrum" && !board.sprint) {
    return (
      <div data-board-showing={board.name}>
        <EmptyState
          title="No sprint is running"
          description="A scrum board shows the sprint that is running. Plan one from the backlog and start it, and its work appears here."
          action={
            <Link
              to="/projects/$projectKey/sprints"
              params={{ projectKey }}
              className="inline-flex h-10 items-center rounded-md bg-accent px-4 text-sm font-medium text-on-accent hover:bg-accent-hover"
            >
              Go to sprints
            </Link>
          }
        />
      </div>
    );
  }

  function drop(swimlane: Swimlane, event: DragEvent) {
    // Prefer the event's own payload; fall back to the ref for browsers that
    // decline to hand it back.
    let current = draggingRef.current;
    const carried = event.dataTransfer.getData(DRAG_MIME);
    if (carried) {
      try {
        current = JSON.parse(carried) as Dragging;
      } catch {
        // Malformed payload: the ref is still the better answer.
      }
    }

    const hovering = targetRef.current;
    const beforeKey = hovering?.swimlaneId === swimlane.id ? hovering.beforeKey : null;
    endDrag();
    if (!current) return;

    const cards = swimlane.cards;
    const beforeIndex = beforeKey ? cards.findIndex((c) => c.key === beforeKey) : cards.length;

    // The card immediately above the drop point, skipping the card being moved
    // so it is not treated as its own neighbour.
    const above = cards.slice(0, beforeIndex).filter((c) => c.key !== current.issueKey).at(-1);
    const below = beforeKey && beforeKey !== current.issueKey ? beforeKey : undefined;

    if (current.fromSwimlaneId === swimlane.id && above?.key === undefined && below === undefined) {
      return; // dropped exactly where it already was
    }

    move.mutate(
      {
        projectKey,
        issueKey: current.issueKey,
        swimlaneId: swimlane.id,
        afterKey: above?.key,
        beforeKey: below,
      },
      { onSuccess: (moved) => toast.success(`${current.issueKey} moved to ${swimlane.name}${moved?.transition ? ` (${moved.transition})` : ""}`) },
    );
  }

  return (
    // The name of the board whose cards are actually on screen. While a
    // different board is being fetched this still names the old one, which is
    // the honest answer to "what am I looking at".
    <div className="space-y-3" data-board-showing={board.name}>
      {board.sprint && (
        <div
          data-sprint-showing={board.sprint.name}
          className="flex flex-wrap items-baseline gap-x-3 gap-y-1 rounded-md border border-border bg-surface-raised px-3 py-2 text-sm"
        >
          <span className="font-medium text-ink">{board.sprint.name}</span>
          {board.sprint.goal && <span className="text-ink-muted">{board.sprint.goal}</span>}
          {board.sprint.endsOn && (
            <span className="ml-auto text-xs text-ink-subtle">{describeEnd(board.sprint.endsOn)}</span>
          )}
        </div>
      )}

      {move.error && <ErrorBanner>{(move.error as Error).message}</ErrorBanner>}
      {move.data?.transition && (
        <p className="text-xs text-ink-subtle" role="status">
          Moved through the workflow: {move.data.transition}
        </p>
      )}

      {unmapped.length > 0 && (
        <div className="rounded-md border border-warning/40 bg-warning/10 px-3 py-2 text-sm text-ink">
          <strong className="font-medium">
            {unmapped.length} card{unmapped.length === 1 ? "" : "s"} in states no swimlane
            covers
          </strong>{" "}
          <span className="text-ink-muted">
            ({unmapped.map((c) => c.key).join(", ")}). Add those states to a swimlane to see them
            on the board.
          </span>
        </div>
      )}

      <div className="flex min-h-[28rem] gap-3 overflow-x-auto pb-2" data-issue-list="">
        {swimlanes.map((swimlane) => (
          <SwimlaneColumn
            key={swimlane.id}
            swimlane={swimlane}
            groupBy={board.groupBy}
            dragging={dragging}
            target={target}
            onDragStart={beginDrag}
            onDragEnd={endDrag}
            onDragOver={hoverOver}
            onDrop={drop}
            busy={move.isPending}
          />
        ))}
      </div>
    </div>
  );
}

function SwimlaneColumn({
  swimlane,
  groupBy,
  dragging,
  target,
  onDragStart,
  onDragEnd,
  onDragOver,
  onDrop,
  busy,
}: {
  swimlane: Swimlane;
  groupBy: "none" | "assignee" | "priority" | "type";
  dragging: Dragging | null;
  target: DropTarget | null;
  onDragStart: (dragging: Dragging) => void;
  onDragEnd: () => void;
  onDragOver: (t: DropTarget) => void;
  onDrop: (swimlane: Swimlane, event: DragEvent) => void;
  busy: boolean;
}) {
  const overWip = swimlane.wipLimit > 0 && swimlane.cards.length > swimlane.wipLimit;
  const isTarget = target?.swimlaneId === swimlane.id;
  const rows = groupCards(swimlane.cards, groupBy);

  return (
    <section
      aria-label={swimlane.name}
      data-swimlane={swimlane.name}
      className={cx(
        "flex w-72 shrink-0 flex-col rounded-lg border bg-surface-raised transition-colors",
        isTarget && dragging ? "border-accent" : "border-border",
      )}
      onDragOver={(event) => {
        // Preventing the default is what marks this as a valid drop target.
        event.preventDefault();
        if (!isTarget) onDragOver({ swimlaneId: swimlane.id, beforeKey: null });
      }}
      onDrop={(event) => {
        event.preventDefault();
        onDrop(swimlane, event);
      }}
    >
      <header className="flex items-baseline justify-between gap-2 border-b border-border px-3 py-2">
        <h3 className="truncate text-xs font-semibold tracking-wide text-ink uppercase">
          {swimlane.name}
        </h3>
        <span
          className={cx(
            "shrink-0 rounded px-1.5 py-0.5 text-xs tabular-nums",
            overWip ? "bg-danger/15 font-medium text-danger" : "text-ink-muted",
          )}
          title={
            swimlane.wipLimit > 0
              ? `${swimlane.cards.length} of a ${swimlane.wipLimit} card limit`
              : `${swimlane.cards.length} cards`
          }
        >
          {swimlane.cards.length}
          {swimlane.wipLimit > 0 && ` / ${swimlane.wipLimit}`}
        </span>
      </header>

      {/* The states a swimlane is made of are shown, because they are what
          decides which cards land here and which drags are possible. */}
      {!(swimlane.statuses.length === 1 && swimlane.statuses[0]!.name === swimlane.name) && (
        <p className="border-b border-border px-3 py-1.5 text-2xs text-ink-subtle">
          {swimlane.statuses.length === 0
            ? "No states: nothing can be here"
            : swimlane.statuses.map((s) => s.name).join(" · ")}
        </p>
      )}

      <div className={cx("flex-1 space-y-2 p-2", busy && "opacity-60")}>
        {swimlane.cards.length === 0 && (
          <p className="px-1 py-6 text-center text-xs text-ink-subtle">Drop cards here</p>
        )}

        {rows.map((row) => (
          <div key={row.name || "all"} className="space-y-2">
            {row.name && (
              <p className="px-1 pt-1 text-2xs font-medium text-ink-muted capitalize">
                {row.name}
              </p>
            )}
            {row.cards.map((card) => (
              <IssueCard
                key={card.id}
                card={card}
                isDragging={dragging?.issueKey === card.key}
                isDropBefore={isTarget && target?.beforeKey === card.key}
                onDragStart={(event) => {
                  const payload: Dragging = { issueKey: card.key, fromSwimlaneId: swimlane.id };
                  event.dataTransfer.setData(DRAG_MIME, JSON.stringify(payload));
                  // Some browsers refuse to start a drag with no plain text.
                  event.dataTransfer.setData("text/plain", card.key);
                  event.dataTransfer.effectAllowed = "move";
                  onDragStart(payload);
                }}
                onDragEnd={onDragEnd}
                onDragOver={() => onDragOver({ swimlaneId: swimlane.id, beforeKey: card.key })}
              />
            ))}
          </div>
        ))}
      </div>
    </section>
  );
}

function IssueCard({
  card,
  isDragging,
  isDropBefore,
  onDragStart,
  onDragEnd,
  onDragOver,
}: {
  card: BoardCard;
  isDragging: boolean;
  isDropBefore: boolean;
  onDragStart: (event: DragEvent) => void;
  onDragEnd: () => void;
  onDragOver: () => void;
}) {
  const drawer = useIssueDrawer();
  const selected = drawer.current === card.key;
  return (
    <article
      draggable
      data-card={card.key}
      data-selected={selected ? "" : undefined}
      aria-current={selected || undefined}
      onDragStart={onDragStart}
      onDragEnd={onDragEnd}
      onDragOver={(event) => {
        event.preventDefault();
        event.stopPropagation();
        onDragOver();
      }}
      // A finished drag fires no click, so opening on click leaves dragging alone.
      onClick={(event) => {
        if ((event.target as HTMLElement).closest("a, button")) return;
        drawer.open(card.key);
      }}
      className={cx(
        "cursor-grab rounded-md border bg-surface p-2.5 shadow-sm active:cursor-grabbing",
        isDragging ? "border-accent opacity-40" : selected ? "border-accent" : "border-border hover:border-border-strong",
        isDropBefore && "border-t-2 border-t-accent",
      )}
    >
      <Link
        to="/issues/$issueKey"
        params={{ issueKey: card.key }}
        className="block text-sm text-ink hover:text-accent"
        // The link must not hijack the drag.
        draggable={false}
      >
        {card.summary}
      </Link>
      <LabelChips labels={card.labels ?? []} className="mt-1" />
      {card.parent && (
        <Link
          to="/issues/$issueKey"
          params={{ issueKey: card.parent.key }}
          draggable={false}
          title={`${card.parent.type.name} ${card.parent.key}: ${card.parent.summary}`}
          className="mt-2 inline-flex max-w-full items-center gap-1 rounded bg-epic/10 px-1.5 py-0.5 text-2xs font-medium text-epic hover:bg-epic/20"
        >
          <span className="truncate">{card.parent.summary}</span>
        </Link>
      )}
      <div className="mt-2 flex items-center gap-2">
        <TypeBadge icon={card.type.icon} name={card.type.name} />
        <span className="font-mono text-2xs text-ink-muted">{card.key}</span>
        <span className="flex-1" />
        <PriorityBadge priority={card.priority} />
        <Avatar name={card.assignee?.name} src={card.assignee?.avatarUrl} size="sm" />
        <IconButton icon={<Icon.ChevronRight />} label={`Open ${card.key} beside the board`} size="sm" onClick={() => drawer.open(card.key)} data-open-drawer={card.key} />
      </div>
    </article>
  );
}
