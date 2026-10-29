import { useMemo, useRef, useState, type DragEvent } from "react";
import { Link } from "@tanstack/react-router";
import { groupCards, useBoard, useBoardById, useMoveCard, useSprintBoard, type Card as BoardCard, type Swimlane } from "@/api/boards";
import { Avatar, PriorityBadge, TypeBadge } from "@/features/issues/badges";
import { LabelChips } from "@/features/labels/LabelChip";
import { cx, EmptyState, ErrorBanner, IconButton, Skeleton, useToast } from "@/components/ui";
import { Icon } from "@/components/icons";
import { useIssueDrawer, useIssueList } from "@/features/issues/IssueDrawer";
import { MS_PER_DAY } from "@/features/plan/scale";
import { useMe } from "@/api/auth";
import { QuickFilters } from "./QuickFilters";
import { filterCards, isFiltered, optionsFrom, type QuickFilter } from "./quickFilter";
import { t } from "@/i18n";

/** How a sprint's end reads on the board: in days, since that is how a sprint is felt. */
export function describeEnd(endsOn: string, today = new Date()): string {
  const end = new Date(endsOn);
  const days = Math.round((end.getTime() - today.setHours(0, 0, 0, 0)) / MS_PER_DAY);
  if (days === 0) return t.board.view.endsToday;
  if (days === 1) return t.board.view.endsTomorrow;
  if (days > 1) return t.board.view.endsIn(days);
  return -days === 1 ? t.board.view.endedYesterday : t.board.view.endedAgo(-days);
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
  filter,
  onFilter,
}: {
  projectKey: string;
  boardId?: string;
  /** Set, the board is one sprint's: its stream's board showing only that sprint's work. */
  sprintId?: string;
  filter: QuickFilter;
  onFilter: (filter: QuickFilter) => void;
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
  const { data: me } = useMe();
  const meId = me?.principal.user.id ?? "";

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

  // Each lane's cards the filter lets through. The lane keeps all of them, for
  // its limit and for ranking a drop beside a card the filter hides.
  const lanes = data?.board?.swimlanes;
  const visible = useMemo(() => new Map((lanes ?? []).map((lane) => [lane.id, filterCards(lane.cards, filter, meId)])), [lanes, filter, meId]);
  const options = useMemo(() => optionsFrom((lanes ?? []).flatMap((lane) => lane.cards)), [lanes]);

  // Cards in column order, top to bottom, so the panel's next is the card below.
  useIssueList((lanes ?? []).flatMap((lane) => (visible.get(lane.id) ?? []).map((card) => card.key)));

  if (isLoading) return <Skeleton lines={4} />;
  if (error) return <ErrorBanner>{(error as Error).message}</ErrorBanner>;

  const board = data?.board;
  if (!board) return null;

  const swimlanes = board.swimlanes ?? [];
  const unmapped = board.unmapped ?? [];
  const filtered = isFiltered(filter);
  const hidden = swimlanes.reduce((sum, lane) => sum + lane.cards.length - (visible.get(lane.id)?.length ?? 0), 0);

  // A scrum board between sprints has nothing to show, and says why rather
  // than drawing four empty columns.
  if (board.type === "scrum" && !board.sprint) {
    return (
      <div data-board-showing={board.name}>
        <EmptyState
          title={t.board.view.noSprint}
          description={t.board.view.noSprintBody}
          action={
            <Link
              to="/projects/$projectKey/sprints"
              params={{ projectKey }}
              className="inline-flex h-10 items-center rounded-md bg-accent px-4 text-sm font-medium text-on-accent hover:bg-accent-hover"
            >
              {t.board.view.goToSprints}
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
    // The lane as it would be without the moved card, and where in it the card
    // lands. Dropped on itself, it lands where it already is.
    const others = cards.filter((c) => c.key !== current.issueKey);
    const from = cards.findIndex((c) => c.key === current.issueKey);
    const found = beforeKey === current.issueKey ? from : beforeKey ? others.findIndex((c) => c.key === beforeKey) : -1;
    const at = found < 0 ? others.length : found;
    if (from >= 0 && at === from) return; // dropped exactly where it already was

    const above = others[at - 1];
    const below = others[at]?.key;

    move.mutate(
      {
        projectKey,
        issueKey: current.issueKey,
        swimlaneId: swimlane.id,
        afterKey: above?.key,
        beforeKey: below,
      },
      { onSuccess: (moved) => toast.success(`${t.board.view.moved(current.issueKey, swimlane.name)}${moved?.transition ? ` (${moved.transition})` : ""}`) },
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
          {board.sprint.endsOn && <span className="ml-auto text-xs text-ink-subtle">{describeEnd(board.sprint.endsOn)}</span>}
        </div>
      )}

      {move.error && <ErrorBanner>{(move.error as Error).message}</ErrorBanner>}
      {move.data?.transition && (
        <p className="text-xs text-ink-subtle" role="status">
          {t.board.view.movedThrough} {move.data.transition}
        </p>
      )}

      {unmapped.length > 0 && (
        <div className="rounded-md border border-warning/40 bg-warning/10 px-3 py-2 text-sm text-ink">
          <strong className="font-medium">{t.board.view.unmapped(unmapped.length)}</strong>{" "}
          <span className="text-ink-muted">{t.board.view.unmappedHint(unmapped.map((c) => c.key).join(", "))}</span>
        </div>
      )}

      <QuickFilters
        filter={filter}
        onChange={onFilter}
        options={options}
        hidden={hidden}
        leftOut={board.leftOut > 0 && board.leftOutQuery ? { count: board.leftOut, query: board.leftOutQuery, days: board.doneForDays } : undefined}
      />

      <div className="flex min-h-112 gap-3 overflow-x-auto pb-2" data-issue-list="">
        {swimlanes.map((swimlane) => (
          <SwimlaneColumn
            key={swimlane.id}
            swimlane={swimlane}
            cards={visible.get(swimlane.id) ?? swimlane.cards}
            filtered={filtered}
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
  cards,
  filtered,
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
  /** The cards to draw: the lane's own, less what the filter hides. */
  cards: BoardCard[];
  filtered: boolean;
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
  const rows = groupCards(cards, groupBy);
  const total = swimlane.cards.length;

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
        <h3 className="truncate text-xs font-semibold tracking-wide text-ink uppercase">{swimlane.name}</h3>
        <span
          className={cx("shrink-0 rounded px-1.5 py-0.5 text-xs tabular-nums", overWip ? "bg-danger/15 font-medium text-danger" : "text-ink-muted")}
          title={
            filtered
              ? swimlane.wipLimit > 0
                ? t.board.view.shownOfLimit(cards.length, swimlane.wipLimit, total)
                : t.board.view.shownOf(cards.length, total)
              : swimlane.wipLimit > 0
                ? t.board.view.ofLimit(total, swimlane.wipLimit)
                : t.board.view.cards(total)
          }
          data-lane-count={filtered ? `${cards.length} of ${total}` : String(total)}
        >
          {filtered ? t.board.view.count(cards.length, total) : total}
          {!filtered && swimlane.wipLimit > 0 && ` / ${swimlane.wipLimit}`}
        </span>
      </header>

      {/* The states a swimlane is made of are shown, because they are what
          decides which cards land here and which drags are possible. */}
      {!(swimlane.statuses.length === 1 && swimlane.statuses[0]!.name === swimlane.name) && (
        <p className="border-b border-border px-3 py-1.5 text-2xs text-ink-subtle">
          {swimlane.statuses.length === 0 ? t.board.view.noStates : swimlane.statuses.map((s) => s.name).join(" · ")}
        </p>
      )}

      <div className={cx("flex-1 space-y-2 p-2", busy && "opacity-60")}>
        {cards.length === 0 && <p className="px-1 py-6 text-center text-xs text-ink-subtle">{total === 0 ? t.board.view.dropHere : t.board.view.noMatch}</p>}

        {rows.map((row) => (
          <div key={row.name || "all"} className="space-y-2">
            {row.name && <p className="px-1 pt-1 text-2xs font-medium text-ink-muted capitalize">{row.name}</p>}
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
        <IconButton
          icon={<Icon.ChevronRight />}
          label={t.board.view.openBeside(card.key)}
          size="sm"
          onClick={() => drawer.open(card.key)}
          data-open-drawer={card.key}
        />
      </div>
    </article>
  );
}
