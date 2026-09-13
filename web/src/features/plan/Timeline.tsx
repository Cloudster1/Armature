import { useEffect, useMemo, useRef, useState, type PointerEvent as ReactPointerEvent } from "react";
import { Link } from "@tanstack/react-router";
import { LEVEL_STANDARD, useCreateIssue, useIssueTypes, useSetParent } from "@/api/issues";
import { useAddLink, usePlan, useRemoveLink, useSchedule, type Dependency, type PlanItem, type PlanWarning, type TeamLoad } from "@/api/plan";
import { Button, Card, ErrorBanner, IconButton, cx } from "@/components/ui";
import { Icon } from "@/components/icons";
import { TypeBadge } from "@/features/issues/badges";
import { SprintBands, describe as describeSprint, number as describeSprintNumber } from "./SprintBands";
import { SprintCapacity } from "./SprintCapacity";
import { MilestoneBand, MilestoneLines, MilestoneProgress } from "./MilestoneMarkers";
import { headerLayout } from "./layout";
import { severityOf, wordsOf, worstOf } from "./warnings";
import { AddCalendarRow, AddSidebarRow, type AddDraft, type Ghost } from "./AddRow";
import { dragRange, isRealDrag, placementFor, type Placement } from "./create";
import { decide, rowAt, type DropTarget } from "./reparent";
import { LOAD_GROUP_KEY, barShare, describeLoad, describeWeek, loadRows, utilisation, weekCells, type Utilisation } from "./load";
import {
  NO_CUT,
  NO_FILTERS,
  cutItems,
  filterItems,
  flatten,
  rowsFor,
  type Cut,
  type Filters,
  type PlanView,
  type Row,
} from "./views";
import {
  EDGE_LINGER_MS,
  PLAN_DEFAULT_SPAN_DAYS,
  PLAN_DRAG_THRESHOLD_PX,
  PLAN_INDENT_PER_LEVEL,
  PLAN_LINK_HANDLE_PX,
  PLAN_LOAD_CELL_INSET,
  PLAN_MIN_TICK_LABEL_PX,
  PLAN_ROW_HEIGHT,
  PLAN_SIDEBAR_WIDTH,
} from "@/config";
import {
  addDays,
  applyDrag,
  barFor,
  day,
  filled,
  fitted,
  headingTicks,
  daysBetween,
  isoDay,
  makeScale,
  ticksFor,
  zoomFor,
  type Zoom,
} from "./scale";

/** What the pane is assumed to measure before it has been measured. */
const FALLBACK_PANE_WIDTH = 800;


/** A drag in flight. The origin lives in a ref; only the preview re-renders. */
interface DragOrigin {
  key: string;
  mode: "move" | "start" | "end";
  clientX: number;
  start: Date;
  due: Date;
}

/**
 * A drag across empty calendar: on an add row it makes a ticket with the days
 * dragged, on an unscheduled issue's row it schedules that issue. The anchor
 * is where the pointer went down, in canvas pixels.
 */
type EmptyDrag =
  | { kind: "create"; rowKey: string; anchorX: number; placement: Placement }
  | { kind: "schedule"; key: string; anchorX: number };

/** An empty drag before it has an anchor: Omit on its own would flatten the union. */
type EmptyDragStart = EmptyDrag extends infer T ? (T extends EmptyDrag ? Omit<T, "anchorX"> : never) : never;

interface Preview {
  key: string;
  start: Date;
  due: Date;
}

/** An arrow being drawn from a bar's end to wherever the pointer is, in chart pixels. */
interface LinkDraft {
  fromKey: string;
  x0: number;
  y0: number;
  x: number;
  y: number;
  /** The issue row under the pointer, when it is one that can be blocked. */
  toKey: string | null;
}

/** A row lifted out of the sidebar to be dropped under another. */
interface Lift {
  key: string;
  target: DropTarget | null;
  /** Why the drop where the pointer is would be refused, when it would. */
  refused?: string;
}

/** The summary box left behind by a drag to create, until Enter or Escape. */
interface Draft {
  rowKey: string;
  start: Date;
  due: Date;
  placement: Placement;
  summary: string;
}

export function Timeline({
  projectKey,
  view = "management",
  cut: scope = NO_CUT,
  query = "",
  filters = NO_FILTERS,
  zoom: zoomId = "fit",
}: {
  projectKey: string;
  view?: PlanView;
  cut?: Cut;
  /** An NQL query; the rows it did not match are cut, their ancestors kept. */
  query?: string;
  /** What the toolbar narrowed the rows to; the toolbar owns the state. */
  filters?: Filters;
  zoom?: Zoom["id"];
}) {
  const { data, isLoading, error } = usePlan(projectKey, query);
  const schedule = useSchedule();
  const [collapsed, setCollapsed] = useState<Set<string>>(new Set());
  const [preview, setPreview] = useState<Preview | null>(null);
  const dragRef = useRef<DragOrigin | null>(null);
  const emptyDragRef = useRef<EmptyDrag | null>(null);
  const chartRef = useRef<HTMLDivElement>(null);
  const [ghost, setGhost] = useState<Ghost | null>(null);
  const [draft, setDraft] = useState<Draft | null>(null);
  const [adding, setAdding] = useState<Map<string, AddDraft>>(new Map());
  const create = useCreateIssue();
  const setParent = useSetParent();
  const reparentRef = useRef<{ key: string; clientY0: number; started: boolean } | null>(null);
  const sidebarRef = useRef<HTMLDivElement>(null);
  const [lift, setLift] = useState<Lift | null>(null);
  const [refusal, setRefusal] = useState<string | null>(null);
  const addLink = useAddLink();
  const removeLink = useRemoveLink();
  const linkDragRef = useRef<{ fromKey: string; x0: number; y0: number } | null>(null);
  const [linkDraft, setLinkDraft] = useState<LinkDraft | null>(null);
  const [selectedDependency, setSelectedDependency] = useState<Dependency | null>(null);
  const { data: typeData } = useIssueTypes();
  // Only standard-level types can stand on their own; a subtask needs a parent.
  const types = useMemo(() => (typeData?.issueTypes ?? []).filter((type) => type.level >= LEVEL_STANDARD), [typeData]);

  // The calendar is sized to the pane it is drawn in, so the pane is measured
  // and re-measured as the window changes.
  const [scroller, setScroller] = useState<HTMLDivElement | null>(null);
  const [available, setAvailable] = useState(0);
  useEffect(() => {
    if (!scroller) return;
    const measure = () => setAvailable(scroller.clientWidth);
    measure();
    const observer = new ResizeObserver(measure);
    observer.observe(scroller);
    return () => observer.disconnect();
  }, [scroller]);

  const pane = available || FALLBACK_PANE_WIDTH;
  const from = day(data?.from ?? Date.now());
  const zoom = fitted(zoomFor(zoomId), from, day(data?.to ?? Date.now()), pane);
  // A fixed zoom shows at least a pane's worth of days, so the calendar is
  // never a strip beside an empty pane.
  const to = filled(from, day(data?.to ?? Date.now()), zoom.pxPerDay, pane);
  const scale = useMemo(
    () => makeScale(from, to, zoom.pxPerDay),
    // The dates are values, not references; their times are what matter.
    [from.getTime(), to.getTime(), zoom.pxPerDay], // eslint-disable-line react-hooks/exhaustive-deps
  );
  const todayX = scale.x(day(Date.now()));

  // A fixed zoom opens on today rather than on the first day of the plan,
  // which is usually the past.
  useEffect(() => {
    if (!scroller || zoom.id === "fit") return;
    scroller.scrollLeft = Math.max(0, todayX - scroller.clientWidth / 3);
  }, [scroller, zoom.id, todayX]);
  // The cut applies to every view; the filters narrow the management view
  // only, and the sprint view shows each iteration's work as it is.
  const cut = useMemo<Cut>(
    () => ({ ...scope, matched: query && data ? new Set(data.matched) : null }),
    [scope, query, data],
  );
  const shown = useMemo(() => {
    const kept = cutItems(data?.items ?? [], cut, new Date());
    return view === "management" ? filterItems(kept, filters) : kept;
  }, [data?.items, cut, filters, view]);
  // The load rows close the management view: what each team is carrying per
  // week, under the work it is carrying.
  const rows = useMemo(
    () => [
      ...rowsFor(view, shown, data?.sprints ?? [], collapsed),
      ...(view === "management" && data ? loadRows(data.load, filters, collapsed) : []),
    ],
    [view, shown, data, filters, collapsed],
  );
  const byKey = useMemo(() => new Map(flatten(data?.items ?? []).map((item) => [item.issue.key, item] as const)), [data?.items]);
  const warningsByKey = useMemo(() => {
    const map = new Map<string, PlanWarning[]>();
    // A sprint warning belongs to the sprint, not to any one row.
    for (const w of data?.warnings ?? []) {
      if (!w.issueKey) continue;
      map.set(w.issueKey, [...(map.get(w.issueKey) ?? []), w]);
    }
    return map;
  }, [data?.warnings]);

  if (isLoading) return <p className="text-sm text-ink-muted">Loading the plan...</p>;
  // A query that fails to compile is reported beside the query box, and the
  // last good plan stays drawn underneath it.
  if (error && !data) return <ErrorBanner>{(error as Error).message}</ErrorBanner>;
  if (!data) return null;

  function toggle(key: string) {
    setCollapsed((current) => {
      const next = new Set(current);
      if (next.has(key)) next.delete(key);
      else next.add(key);
      return next;
    });
  }

  function onPointerDown(
    event: ReactPointerEvent<HTMLElement>,
    item: PlanItem,
    mode: DragOrigin["mode"],
  ) {
    if (!item.start || !item.due) return;
    event.preventDefault();
    event.stopPropagation();
    dragRef.current = {
      key: item.issue.key,
      mode,
      clientX: event.clientX,
      start: day(item.start),
      due: day(item.due),
    };
    // Capture keeps the events coming when the pointer leaves the bar, but it
    // is refused for a pointer the browser does not consider active. The row
    // handles move and release either way, so a refusal is not a failed drag.
    try {
      event.currentTarget.setPointerCapture(event.pointerId);
    } catch {
      // No capture; the container's own handlers carry the drag.
    }
    setPreview({ key: item.issue.key, start: day(item.start), due: day(item.due) });
  }

  /** The pointer's x inside the chart, which is what the scale reads. */
  function canvasX(event: ReactPointerEvent<HTMLElement>): number {
    const left = chartRef.current?.getBoundingClientRect().left ?? 0;
    return event.clientX - left;
  }

  /** The pointer's y inside the chart, which is what names a row. */
  function canvasY(event: ReactPointerEvent<HTMLElement>): number {
    const top = chartRef.current?.getBoundingClientRect().top ?? 0;
    return event.clientY - top;
  }

  /** The issue row at a chart y that a dependency could end on, other than its own start. */
  function rowKeyAt(y: number, notKey: string): string | null {
    const row = rows[Math.floor(y / PLAN_ROW_HEIGHT)];
    return row && row.kind === "issue" && row.key !== notKey ? row.key : null;
  }

  // A dependency is drawn from the dot at the end of a bar to another row. The
  // bar's own drags start on the bar, so the dot stops the press there.
  function onLinkPointerDown(event: ReactPointerEvent<HTMLElement>, item: PlanItem) {
    if (event.button !== 0) return;
    event.preventDefault();
    event.stopPropagation();
    const x0 = canvasX(event);
    const y0 = canvasY(event);
    linkDragRef.current = { fromKey: item.issue.key, x0, y0 };
    setLinkDraft({ fromKey: item.issue.key, x0, y0, x: x0, y: y0, toKey: null });
    try {
      event.currentTarget.setPointerCapture(event.pointerId);
    } catch {
      // No capture; the chart's own handlers carry the drag.
    }
  }

  function onEmptyPointerDown(event: ReactPointerEvent<HTMLElement>, drag: EmptyDragStart) {
    if (event.button !== 0) return;
    event.preventDefault();
    const anchorX = canvasX(event);
    emptyDragRef.current = { ...drag, anchorX } as EmptyDrag;
    try {
      event.currentTarget.setPointerCapture(event.pointerId);
    } catch {
      // No capture; the chart's own handlers carry the drag.
    }
  }

  function onPointerMove(event: ReactPointerEvent<HTMLElement>) {
    const linking = linkDragRef.current;
    if (linking) {
      const x = canvasX(event);
      const y = canvasY(event);
      setLinkDraft({ ...linking, x, y, toKey: rowKeyAt(y, linking.fromKey) });
      return;
    }
    const empty = emptyDragRef.current;
    if (empty) {
      const range = dragRange(scale, empty.anchorX, canvasX(event));
      setGhost({ rowKey: empty.kind === "create" ? empty.rowKey : empty.key, ...range });
      return;
    }
    const origin = dragRef.current;
    if (!origin) return;
    const days = Math.round((event.clientX - origin.clientX) / scale.pxPerDay);
    const moved = applyDrag(origin.start, origin.due, origin.mode, days);
    setPreview({ key: origin.key, ...moved });
  }

  function onPointerUp(event: ReactPointerEvent<HTMLElement>) {
    const linking = linkDragRef.current;
    if (linking) {
      linkDragRef.current = null;
      setLinkDraft(null);
      const toKey = rowKeyAt(canvasY(event), linking.fromKey);
      if (toKey) addLink.mutate({ key: linking.fromKey, type: "Blocks", targetKey: toKey });
      return;
    }
    const empty = emptyDragRef.current;
    if (empty) {
      emptyDragRef.current = null;
      setGhost(null);
      const x = canvasX(event);
      // A click on empty space means nothing; only a drag does.
      if (!isRealDrag(empty.anchorX, x)) return;
      const range = dragRange(scale, empty.anchorX, x);
      if (empty.kind === "schedule") {
        schedule.mutate({ key: empty.key, startDate: isoDay(range.start), dueDate: isoDay(range.due) });
      } else {
        setDraft({ rowKey: empty.rowKey, placement: empty.placement, summary: "", ...range });
      }
      return;
    }
    const origin = dragRef.current;
    dragRef.current = null;
    if (!origin || !preview) {
      setPreview(null);
      return;
    }
    const moved =
      daysBetween(origin.start, preview.start) !== 0 || daysBetween(origin.due, preview.due) !== 0;
    if (moved) {
      schedule.mutate({
        key: origin.key,
        startDate: isoDay(preview.start),
        dueDate: isoDay(preview.due),
      });
    }
    setPreview(null);
  }

  function addDraftFor(rowKey: string): AddDraft {
    return adding.get(rowKey) ?? { typeId: types[0]?.id ?? "", summary: "" };
  }

  function setAddDraft(rowKey: string, next: AddDraft) {
    setAdding((current) => new Map(current).set(rowKey, next));
  }

  /** Makes the ticket an add row or a drag described, where it will sit. */
  function file(rowKey: string, placement: Placement, summary: string, range?: { start: Date; due: Date }) {
    if (!data || !summary.trim()) return;
    const typeId = addDraftFor(rowKey).typeId || undefined;
    create.mutate(
      {
        projectKey: data.projectKey,
        summary: summary.trim(),
        typeId,
        sprintId: placement.sprintId,
        teamId: placement.teamId,
        startDate: range ? isoDay(range.start) : undefined,
        dueDate: range ? isoDay(range.due) : undefined,
      },
      {
        onSuccess: () => {
          setAddDraft(rowKey, { ...addDraftFor(rowKey), summary: "" });
          setDraft(null);
        },
      },
    );
  }

  function putOnPlan(item: PlanItem) {
    const start = day(Date.now());
    schedule.mutate({
      key: item.issue.key,
      startDate: isoDay(start),
      dueDate: isoDay(addDays(start, PLAN_DEFAULT_SPAN_DAYS)),
    });
  }

  // Moving a ticket under another is a drag down the sidebar, in the view
  // that draws the hierarchy. The row's link and chevron keep their jobs: a
  // press on them is theirs, and a press elsewhere is a drag only once it has
  // travelled.
  function onRowPointerDown(event: ReactPointerEvent<HTMLElement>, key: string) {
    if (view !== "management" || event.button !== 0) return;
    if ((event.target as HTMLElement).closest("a,button")) return;
    reparentRef.current = { key, clientY0: event.clientY, started: false };
    try {
      event.currentTarget.setPointerCapture(event.pointerId);
    } catch {
      // No capture; the sidebar's own handlers carry the drag.
    }
  }

  /** The pointer's y measured from the top of the first row. */
  function rowsY(event: ReactPointerEvent<HTMLElement>): number {
    const top = sidebarRef.current?.getBoundingClientRect().top ?? 0;
    return event.clientY - top - headerHeight;
  }

  function onSidebarPointerMove(event: ReactPointerEvent<HTMLElement>) {
    const drag = reparentRef.current;
    if (!drag) return;
    if (!drag.started) {
      if (Math.abs(event.clientY - drag.clientY0) < PLAN_DRAG_THRESHOLD_PX) return;
      drag.started = true;
    }
    event.preventDefault();
    const target = rowAt(rows, rowsY(event));
    const dragged = byKey.get(drag.key);
    const decision = target && dragged ? decide(dragged, target) : null;
    setLift({ key: drag.key, target, refused: decision && "refused" in decision ? decision.refused : undefined });
  }

  function onSidebarPointerUp(event: ReactPointerEvent<HTMLElement>) {
    const drag = reparentRef.current;
    reparentRef.current = null;
    setLift(null);
    if (!drag?.started) return;
    const target = rowAt(rows, rowsY(event));
    const dragged = byKey.get(drag.key);
    if (!target || !dragged) return;
    const decision = decide(dragged, target);
    if ("refused" in decision) {
      setRefusal(decision.refused);
      return;
    }
    setRefusal(null);
    if ((dragged.issue.parentKey ?? null) === decision.parentKey) return;
    setParent.mutate({ key: drag.key, parentKey: decision.parentKey });
  }

  // Sprints are the sprint view's business; the management view reads the
  // plan without them, so the same line is not drawn on two views at once.
  const showSprints = view === "sprints";
  const warnings = showSprints
    ? data.warnings.filter((w) => !w.team)
    : data.warnings.filter((w) => !w.sprint && w.kind !== "outside-sprint");
  // The sidebar's own header has to match the calendar's, bands included, or
  // the rows either side of the divider stop lining up.
  const headerHeight = headerLayout({
    sprints: showSprints && data.sprints.some((s) => s.sprint.startsOn && s.sprint.endsOn),
    milestones: data.milestones.some((m) => m.dueOn),
  }).total;
  const chartHeight = rows.length * PLAN_ROW_HEIGHT;
  const rowIndex = new Map(rows.flatMap((r, i) => (r.kind === "issue" ? [[r.key, i] as const] : [])));

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-center gap-3 text-sm" data-testid="plan-notes">
        <p className="text-ink-muted">
          {data.unscheduled > 0 ? `${data.unscheduled} not on the plan yet` : "Everything is scheduled"}
          {data.unestimated > 0 && `, ${data.unestimated} unestimated`}
        </p>
        {schedule.error && <ErrorBanner>{(schedule.error as Error).message}</ErrorBanner>}
        {(refusal || setParent.error) && <ErrorBanner>{refusal ?? (setParent.error as Error).message}</ErrorBanner>}
        {addLink.error && <ErrorBanner>{(addLink.error as Error).message}</ErrorBanner>}
        {removeLink.error && <ErrorBanner>{(removeLink.error as Error).message}</ErrorBanner>}
      </div>

      <Card className="overflow-hidden">
        <div className="flex">
          {/* The issue column does not scroll sideways with the calendar. */}
          <div
            ref={sidebarRef}
            className={cx("shrink-0 border-r border-border", lift && "select-none")}
            style={{ width: PLAN_SIDEBAR_WIDTH }}
            data-testid="plan-sidebar"
            onPointerMove={onSidebarPointerMove}
            onPointerUp={onSidebarPointerUp}
            onPointerCancel={onSidebarPointerUp}
          >
            <div
              className="relative flex items-end border-b border-border px-3 pb-1 text-xs font-medium text-ink-muted"
              style={{ height: headerHeight }}
            >
              {view === "sprints" ? "Sprint and its work" : "Issue"}
              {lift && (
                <span
                  data-plan-drop-top
                  className={cx(
                    "absolute inset-1 flex items-center justify-center rounded border border-dashed text-2xs",
                    lift.target?.kind === "top"
                      ? lift.refused
                        ? "border-danger bg-danger/10 text-danger"
                        : "border-accent bg-accent-subtle text-accent"
                      : "border-border-strong text-ink-subtle",
                  )}
                >
                  {lift.target?.kind === "top" && lift.refused ? lift.refused : "Drop here for the top level"}
                </span>
              )}
            </div>
            {rows.map((row) =>
              row.kind === "group" ? (
                <GroupSidebarRow key={row.key} row={row} onToggle={() => toggle(row.key)} />
              ) : row.kind === "load" ? (
                <LoadSidebarRow key={row.key} row={row.row} />
              ) : row.kind === "add" ? (
                <AddSidebarRow
                  key={row.key}
                  label={row.label}
                  types={types}
                  draft={addDraftFor(row.key)}
                  onChange={(next) => setAddDraft(row.key, next)}
                  onSubmit={() => file(row.key, placementFor(row.group), addDraftFor(row.key).summary)}
                  pending={create.isPending}
                  error={create.error ? (create.error as Error).message : undefined}
                />
              ) : (
                <SidebarRow
                  key={row.key}
                  row={row}
                  onToggle={() => toggle(row.key)}
                  draggable={view === "management"}
                  lifted={lift?.key === row.key}
                  drop={lift?.target?.kind === "row" && lift.target.item.issue.key === row.key ? (lift.refused ? "refused" : "into") : null}
                  warnings={warningsByKey.get(row.key) ?? []}
                  onPointerDown={(event) => onRowPointerDown(event, row.key)}
                />
              ),
            )}
          </div>

          <div ref={setScroller} className="min-w-0 flex-1 overflow-x-auto" data-testid="plan-calendar">
            {/* Clipped, so a band or a flag past the last day cannot widen the
                calendar and put a scrollbar under a view that was meant to fit. */}
            <div className="relative overflow-hidden" style={{ width: scale.width }} data-testid="plan-canvas">
              <TimelineHeader scale={scale} zoom={zoom} todayX={todayX} />
              {showSprints && <SprintBands scale={scale} sprints={data.sprints} />}
              <MilestoneBand scale={scale} milestones={data.milestones} />

              <div
                ref={chartRef}
                className="relative"
                style={{ height: chartHeight }}
                data-px-per-day={scale.pxPerDay}
                onPointerMove={onPointerMove}
                onPointerUp={onPointerUp}
                onPointerCancel={onPointerUp}
              >
                <span
                  aria-hidden="true"
                  className="pointer-events-none absolute top-0 bottom-0 w-px bg-accent/50"
                  style={{ left: todayX }}
                />
                <MilestoneLines scale={scale} milestones={data.milestones} />

                {rows.map((row, index) =>
                  row.kind === "group" ? (
                    <GroupBand key={row.key} row={row} top={index * PLAN_ROW_HEIGHT} scale={scale} />
                  ) : row.kind === "load" ? (
                    <LoadRow key={row.key} row={row.row} top={index * PLAN_ROW_HEIGHT} scale={scale} />
                  ) : row.kind === "add" ? (
                    <AddCalendarRow
                      key={row.key}
                      rowKey={row.key}
                      label={row.label}
                      top={index * PLAN_ROW_HEIGHT}
                      scale={scale}
                      ghost={ghost}
                      draft={draft?.rowKey === row.key ? draft : null}
                      onPointerDown={(event) => onEmptyPointerDown(event, { kind: "create", rowKey: row.key, placement: placementFor(row.group) })}
                      onDraftChange={(summary) => setDraft((current) => (current ? { ...current, summary } : current))}
                      onDraftSubmit={() => draft && file(draft.rowKey, draft.placement, draft.summary, draft)}
                      onDraftCancel={() => setDraft(null)}
                    />
                  ) : (
                    <BarRow
                      key={row.key}
                      item={row.item}
                      top={index * PLAN_ROW_HEIGHT}
                      scale={scale}
                      preview={preview?.key === row.key ? preview : null}
                      ghost={ghost?.rowKey === row.key ? ghost : null}
                      warnings={warningsByKey.get(row.key) ?? []}
                      onPointerDown={onPointerDown}
                      onLinkPointerDown={onLinkPointerDown}
                      linkTarget={linkDraft?.toKey === row.key}
                      onEmptyPointerDown={(event) => onEmptyPointerDown(event, { kind: "schedule", key: row.key })}
                      onSchedule={() => putOnPlan(row.item)}
                      todayX={todayX}
                    />
                  ),
                )}

                <DependencyArrows
                  dependencies={data.dependencies}
                  rows={rows}
                  rowIndex={rowIndex}
                  scale={scale}
                  height={chartHeight}
                  selected={selectedDependency}
                  onSelect={setSelectedDependency}
                  removing={removeLink.isPending}
                  onRemove={(dependency) =>
                    removeLink.mutate({ key: dependency.blockerKey, linkId: dependency.linkId }, { onSuccess: () => setSelectedDependency(null) })
                  }
                />
                {linkDraft && (
                  <svg
                    className="pointer-events-none absolute top-0 left-0 overflow-visible"
                    width={scale.width}
                    height={chartHeight}
                    aria-hidden="true"
                    data-plan-link-draft={linkDraft.fromKey}
                  >
                    <line
                      x1={linkDraft.x0}
                      y1={linkDraft.y0}
                      x2={linkDraft.x}
                      y2={linkDraft.y}
                      strokeWidth="1.5"
                      strokeDasharray="4 3"
                      className={linkDraft.toKey ? "stroke-accent" : "stroke-ink-subtle"}
                    />
                  </svg>
                )}
              </div>
            </div>
          </div>
        </div>
      </Card>

      {showSprints && <SprintCapacity sprints={data.sprints} />}

      <MilestoneProgress projectKey={data.projectKey} milestones={data.milestones} />

      <WarningList warnings={warnings} />
    </div>
  );
}

/** A sprint's line in the sprint view: its name, what it holds, and when it runs. */
function GroupSidebarRow({ row, onToggle }: { row: Extract<Row, { kind: "group" }>; onToggle: () => void }) {
  const sprint = row.sprint?.sprint;
  // The load group is not a set of issues, so it is not counted as one.
  const isLoad = row.key === LOAD_GROUP_KEY;
  return (
    <div
      className="flex items-center gap-1.5 border-b border-border bg-surface-raised/60 px-3 text-sm"
      style={{ height: PLAN_ROW_HEIGHT }}
      data-plan-group={isLoad ? undefined : row.label}
      data-plan-load-group={isLoad ? row.label : undefined}
    >
      <IconButton
        size="xs"
        icon={<Icon.ChevronRight className={cx("transition-transform", !row.collapsed && "rotate-90")} />}
        label={`${row.collapsed ? "Expand" : "Collapse"} ${row.label}`}
        onClick={onToggle}
        aria-expanded={!row.collapsed}
        className="text-ink-subtle"
      />
      <span className="min-w-0 truncate font-semibold text-ink">{row.label}</span>
      {sprint?.state === "active" && (
        <span className="rounded-full bg-accent-subtle px-1.5 text-2xs font-medium text-accent">Running</span>
      )}
      <span className="ml-auto shrink-0 text-xs text-ink-muted tabular-nums">
        {row.detail ?? `${row.count} ${row.count === 1 ? "issue" : "issues"}`}
        {row.sprint && `, ${describeSprint(row.sprint)}`}
      </span>
      {sprint && (
        <Link
          to="/projects/$projectKey/sprints/$sprintId/board"
          params={{ projectKey: sprint.projectKey, sprintId: sprint.id }}
          data-sprint-board={sprint.name}
          className="shrink-0 text-xs text-ink-muted underline-offset-2 hover:text-accent hover:underline"
        >
          Board
        </Link>
      )}
    </div>
  );
}

/** The sprint's span across the calendar, as a band on its own line. */
function GroupBand({ row, top, scale }: { row: Extract<Row, { kind: "group" }>; top: number; scale: ReturnType<typeof makeScale> }) {
  const sprint = row.sprint?.sprint;
  const bar = sprint ? barFor(scale, sprint.startsOn, sprint.endsOn) : null;
  return (
    <div
      className="absolute right-0 left-0 border-b border-border bg-surface-raised/40"
      style={{ top, height: PLAN_ROW_HEIGHT }}
      data-plan-group-row={row.label}
    >
      {bar && (
        <span
          data-plan-group-band={row.label}
          title={`${row.label}: ${sprint?.startsOn} to ${sprint?.endsOn}`}
          className={cx(
            "absolute top-1.5 bottom-1.5 rounded ring-1",
            sprint?.state === "active" ? "bg-accent-subtle ring-accent/40" : "bg-surface ring-border",
          )}
          style={{ left: bar.left, width: bar.width }}
        />
      )}
    </div>
  );
}

/** A team's line in the load: who, and what it said it could take. */
function LoadSidebarRow({ row }: { row: TeamLoad }) {
  return (
    <div
      className={cx(
        "flex items-center gap-1.5 border-b border-border/60 px-3 text-sm last:border-b-0",
        row.kind === "total" && "font-semibold",
      )}
      style={{ height: PLAN_ROW_HEIGHT, paddingLeft: PLAN_INDENT_PER_LEVEL * 2 }}
      data-plan-load-row={row.team}
    >
      <span className={cx("min-w-0 flex-1 truncate", row.kind === "unassigned" ? "text-ink-muted" : "text-ink")}>{row.team}</span>
      <span className="shrink-0 text-xs text-ink-muted tabular-nums">{describeLoad(row)}</span>
    </div>
  );
}

const loadTone: Record<Utilisation, string> = {
  unmeasured: "bg-status-todo/70",
  under: "bg-status-done/70",
  full: "bg-accent/70",
  over: "bg-danger/80",
};

/** A team's weeks across the calendar: a bar per week, as tall as its share of the capacity, red where it is over. */
function LoadRow({ row, top, scale }: { row: TeamLoad; top: number; scale: ReturnType<typeof makeScale> }) {
  const inner = PLAN_ROW_HEIGHT - 2 * PLAN_LOAD_CELL_INSET;
  return (
    <div
      className="absolute right-0 left-0 border-b border-border/60"
      style={{ top, height: PLAN_ROW_HEIGHT }}
      data-plan-load-cells={row.team}
    >
      {weekCells(scale, row).map((cell) => {
        const state = utilisation(cell.week.load, cell.week.capacity);
        const height = Math.round(inner * barShare(row, cell.week));
        return (
          <span
            key={cell.key}
            title={describeWeek(row, cell.week)}
            className="absolute"
            style={{ left: cell.x, width: cell.width, top: PLAN_LOAD_CELL_INSET, height: inner }}
            data-plan-load-week={cell.key}
            data-load={cell.week.load}
            data-capacity={cell.week.capacity ?? ""}
            data-over={state === "over" ? "true" : "false"}
          >
            {cell.week.capacity !== undefined && (
              <span aria-hidden="true" className="absolute inset-x-0.5 bottom-0 border-t border-dashed border-border-strong" style={{ height: inner }} />
            )}
            <span
              aria-hidden="true"
              className={cx("absolute inset-x-0.5 bottom-0 rounded-t-sm", loadTone[state])}
              style={{ height: Math.max(cell.week.load > 0 ? 2 : 0, height) }}
            />
            {cell.width >= 40 && cell.week.load > 0 && (
              <span className="absolute inset-x-0 bottom-0.5 text-center text-2xs leading-none text-ink tabular-nums">
                {describeSprintNumber(cell.week.load)}
              </span>
            )}
          </span>
        );
      })}
    </div>
  );
}

function SidebarRow({
  row,
  onToggle,
  draggable = false,
  lifted = false,
  drop = null,
  warnings = [],
  onPointerDown,
}: {
  row: Extract<Row, { kind: "issue" }>;
  onToggle: () => void;
  /** Whether the row can be lifted and dropped under another. */
  draggable?: boolean;
  lifted?: boolean;
  /** How this row reads as a drop target while another is lifted over it. */
  drop?: "into" | "refused" | null;
  warnings?: PlanWarning[];
  onPointerDown?: (event: ReactPointerEvent<HTMLElement>) => void;
}) {
  const { item } = row;
  // The row wears its warning; a drop in progress says more, so it wins.
  const trouble = worstOf(warnings);
  const words = wordsOf(warnings);
  return (
    <div
      className={cx(
        "flex items-center gap-1.5 border-b border-border/60 px-3 text-sm last:border-b-0",
        draggable && "cursor-grab touch-none",
        lifted && "opacity-50",
        !drop && trouble === "warning" && "bg-warning-subtle",
        !drop && trouble === "error" && "bg-danger-subtle",
        drop === "into" && "bg-accent-subtle/60 ring-1 ring-accent ring-inset",
        drop === "refused" && "bg-danger/10 ring-1 ring-danger ring-inset",
      )}
      style={{ height: PLAN_ROW_HEIGHT, paddingLeft: PLAN_INDENT_PER_LEVEL * (item.depth + 1) }}
      title={words || undefined}
      data-plan-row={item.issue.key}
      data-plan-depth={item.depth}
      data-plan-lifted={lifted || undefined}
      data-plan-drop={drop ?? undefined}
      data-plan-trouble={trouble ?? undefined}
      onPointerDown={onPointerDown}
    >
      {row.hasChildren ? (
        <IconButton
          size="xs"
          icon={<Icon.ChevronRight className={cx("transition-transform", !row.collapsed && "rotate-90")} />}
          label={`${row.collapsed ? "Expand" : "Collapse"} ${item.issue.key}`}
          onClick={onToggle}
          aria-expanded={!row.collapsed}
          className="text-ink-subtle"
        />
      ) : (
        <span className="size-5 shrink-0" />
      )}
      <TypeBadge icon={item.issue.type.icon} name={item.issue.type.name} />
      <Link
        to="/issues/$issueKey"
        params={{ issueKey: item.issue.key }}
        className="shrink-0 font-mono text-2xs text-ink-muted hover:text-accent"
      >
        {item.issue.key}
      </Link>
      {trouble && (
        <Icon.Warning
          className={cx("size-3.5 shrink-0", trouble === "error" ? "text-danger" : "text-warning")}
          role="img"
          aria-label={words}
          data-plan-trouble-icon={trouble}
        />
      )}
      <span className="min-w-0 flex-1 truncate text-ink" title={words || item.issue.summary}>
        {item.issue.summary}
      </span>
    </div>
  );
}

function TimelineHeader({
  scale,
  zoom,
  todayX,
}: {
  scale: ReturnType<typeof makeScale>;
  zoom: Zoom;
  todayX: number;
}) {
  const heading = useMemo(() => headingTicks(scale, zoom), [scale, zoom]);
  const ticks = useMemo(() => ticksFor(scale, zoom), [scale, zoom]);

  // Sized border-box like the bands beneath it, so its own bottom border is
  // part of the height the sidebar was told about rather than an extra pixel.
  return (
    <div
      className="relative flex flex-col border-b border-border"
      style={{ height: headerLayout({ sprints: false, milestones: false }).header }}
      data-testid="plan-header"
    >
      <div className="relative flex-1">
        {heading.map((tick) => (
          <span
            key={tick.key}
            className="absolute truncate border-l border-border px-1.5 text-2xs font-medium text-ink"
            style={{ left: tick.x, width: tick.width }}
          >
            {tick.width >= PLAN_MIN_TICK_LABEL_PX && tick.label}
          </span>
        ))}
      </div>
      <div className="relative flex-1">
        {ticks.map((tick) => (
          <span
            key={tick.key}
            className={cx(
              "absolute truncate border-l px-1.5 text-2xs text-ink-subtle",
              tick.emphasis ? "border-border-strong" : "border-border/60",
            )}
            style={{ left: tick.x, width: tick.width }}
          >
            {tick.width >= PLAN_MIN_TICK_LABEL_PX && tick.label}
          </span>
        ))}
        <span
          aria-hidden="true"
          className="absolute -top-1 w-px bg-accent"
          style={{ left: todayX, height: PLAN_ROW_HEIGHT }}
        />
      </div>
    </div>
  );
}

function BarRow({
  item,
  top,
  scale,
  preview,
  ghost,
  warnings,
  onPointerDown,
  onLinkPointerDown,
  linkTarget = false,
  onEmptyPointerDown,
  onSchedule,
  todayX,
}: {
  item: PlanItem;
  top: number;
  scale: ReturnType<typeof makeScale>;
  preview: Preview | null;
  ghost: Ghost | null;
  warnings: PlanWarning[];
  onPointerDown: (e: ReactPointerEvent<HTMLElement>, item: PlanItem, mode: DragOrigin["mode"]) => void;
  onLinkPointerDown?: (e: ReactPointerEvent<HTMLElement>, item: PlanItem) => void;
  /** Whether a dependency being drawn would end on this row. */
  linkTarget?: boolean;
  onEmptyPointerDown: (e: ReactPointerEvent<HTMLElement>) => void;
  onSchedule: () => void;
  todayX: number;
}) {
  const start = preview ? isoDay(preview.start) : item.start;
  const due = preview ? isoDay(preview.due) : item.due;
  const bar = barFor(scale, start, due);
  const trouble = worstOf(warnings);
  const words = wordsOf(warnings);

  // Only an unscheduled row takes a drag on its empty space: it can mean
  // nothing but "schedule this". A scheduled row's space is where bar drags
  // start and end.
  return (
    <div
      className={cx(
        "absolute right-0 left-0 border-b border-border/60",
        !bar && "cursor-crosshair touch-none select-none",
        linkTarget && "bg-accent-subtle/50",
      )}
      style={{ top, height: PLAN_ROW_HEIGHT }}
      data-plan-bar-row={item.issue.key}
      data-plan-link-target={linkTarget || undefined}
      onPointerDown={bar ? undefined : onEmptyPointerDown}
    >
      {!bar && ghost && (
        <span
          aria-hidden="true"
          data-plan-ghost
          className="absolute top-1/2 h-4 -translate-y-1/2 rounded border border-dashed border-accent bg-accent-subtle/60"
          style={{ left: scale.x(ghost.start), width: scale.spanWidth(ghost.start, ghost.due) }}
        />
      )}
      {bar ? (
        <div
          role="button"
          tabIndex={0}
          data-plan-bar={item.issue.key}
          data-plan-start={start}
          data-plan-due={due}
          title={`${item.issue.key} · ${start} to ${due}${item.derived ? " (from its children)" : ""}${words ? `\n${words}` : ""}`}
          onPointerDown={(event) => onPointerDown(event, item, "move")}
          className={cx(
            "group absolute flex cursor-grab items-center rounded active:cursor-grabbing",
            "top-1/2 h-4 -translate-y-1/2 touch-none select-none",
            item.derived
              ? "border border-dashed border-epic/70 bg-epic/25"
              : trouble === "error"
                ? "bg-danger/80"
                : trouble === "warning"
                  ? "bg-warning/80"
                  : "bg-accent",
          )}
          data-plan-trouble={trouble ?? undefined}
          style={{ left: bar.left, width: bar.width }}
        >
          <span
            aria-hidden="true"
            onPointerDown={(event) => onPointerDown(event, item, "start")}
            className="absolute -left-0.5 h-full w-1.5 cursor-ew-resize rounded-l opacity-0 group-hover:bg-ink/30 group-hover:opacity-100"
          />
          {item.progress.total > 0 && !item.derived && (
            <span
              aria-hidden="true"
              className="absolute top-0 left-0 h-full rounded-l bg-success/70"
              style={{ width: `${(item.progress.done / item.progress.total) * 100}%` }}
            />
          )}
          {/* The handle comes before the end grip, which stays the bar's last span for anything that reaches for it. */}
          <span
            role="button"
            aria-label={`Drag to make ${item.issue.key} block another issue`}
            title="Drag onto another row: this must finish before that starts"
            data-plan-link-handle={item.issue.key}
            onPointerDown={(event) => onLinkPointerDown?.(event, item)}
            className="absolute top-1/2 -translate-y-1/2 cursor-crosshair rounded-full border-2 border-accent bg-surface opacity-0 group-hover:opacity-100 hover:bg-accent-subtle"
            style={{ right: -(PLAN_LINK_HANDLE_PX + 4), width: PLAN_LINK_HANDLE_PX, height: PLAN_LINK_HANDLE_PX }}
          />
          <span
            aria-hidden="true"
            onPointerDown={(event) => onPointerDown(event, item, "end")}
            className="absolute -right-0.5 h-full w-1.5 cursor-ew-resize rounded-r opacity-0 group-hover:bg-ink/30 group-hover:opacity-100"
          />
        </div>
      ) : (
        <Button
          variant="secondary"
          size="sm"
          onClick={onSchedule}
          onPointerDown={(event) => event.stopPropagation()}
          data-plan-schedule={item.issue.key}
          className="absolute top-1/2 -translate-y-1/2 border-dashed text-ink-subtle hover:text-accent"
          style={{ left: Math.max(0, todayX) }}
        >
          Schedule
        </Button>
      )}
    </div>
  );
}

const ARROW_INSET = 8;


/** An arrow's elbow path, and where its control sits: the middle of the vertical leg. */
export function arrowPath(fromX: number, fromY: number, toX: number, toY: number) {
  // The elbow steps clear of both bars before turning, so an arrow between
  // two touching bars is still visible.
  const elbow = Math.max(fromX + ARROW_INSET, toX - ARROW_INSET);
  return { d: `M ${fromX} ${fromY} H ${elbow} V ${toY} H ${toX}`, handle: { x: elbow, y: (fromY + toY) / 2 } };
}

/**
 * Arrows from a blocker's end to the blocked issue's start. Drawn over the bars
 * rather than between them, so a collapsed branch simply hides its own arrows.
 * Each arrow has a wide invisible twin to point at, which shows its remove
 * control where the arrow is, and to click on, which keeps the control there.
 */
export function DependencyArrows({
  dependencies,
  rows,
  rowIndex,
  scale,
  height,
  selected,
  onSelect,
  onRemove,
  removing = false,
}: {
  dependencies: Dependency[];
  rows: Row[];
  rowIndex: Map<string, number>;
  scale: ReturnType<typeof makeScale>;
  height: number;
  selected: Dependency | null;
  onSelect: (dependency: Dependency) => void;
  onRemove: (dependency: Dependency) => void;
  removing?: boolean;
}) {
  const [hovered, setHovered] = useState<string | null>(null);
  const linger = useRef<ReturnType<typeof setTimeout> | null>(null);
  const hot = (linkId: string) => {
    if (linger.current) clearTimeout(linger.current);
    setHovered(linkId);
  };
  const cool = () => {
    if (linger.current) clearTimeout(linger.current);
    linger.current = setTimeout(() => setHovered(null), EDGE_LINGER_MS);
  };

  const byKey = new Map(rows.flatMap((r) => (r.kind === "issue" ? [[r.key, r.item] as const] : [])));
  const paths: Array<{ key: string; d: string; handle: { x: number; y: number }; dependency: Dependency }> = [];

  for (const dep of dependencies) {
    const blocker = byKey.get(dep.blockerKey);
    const blocked = byKey.get(dep.blockedKey);
    if (!blocker?.due || !blocked?.start) continue;

    const fromY = (rowIndex.get(dep.blockerKey) ?? 0) * PLAN_ROW_HEIGHT + PLAN_ROW_HEIGHT / 2;
    const toY = (rowIndex.get(dep.blockedKey) ?? 0) * PLAN_ROW_HEIGHT + PLAN_ROW_HEIGHT / 2;
    const fromX = scale.x(blocker.due) + scale.pxPerDay;
    const toX = scale.x(blocked.start);
    const { d, handle } = arrowPath(fromX, fromY, toX, toY);
    paths.push({ key: `${dep.blockerKey}->${dep.blockedKey}`, d, handle, dependency: dep });
  }

  if (paths.length === 0) return null;
  const shown = paths.find((p) => p.dependency.linkId === (hovered ?? selected?.linkId));

  return (
    <>
      <svg
        className="pointer-events-none absolute top-0 left-0 overflow-visible"
        width={scale.width}
        height={height}
        aria-hidden="true"
        data-testid="plan-dependencies"
      >
        <defs>
          <marker id="plan-arrow" markerWidth="6" markerHeight="6" refX="5" refY="3" orient="auto">
            <path d="M0,0 L6,3 L0,6 z" className="fill-ink-subtle" />
          </marker>
          <marker id="plan-arrow-selected" markerWidth="6" markerHeight="6" refX="5" refY="3" orient="auto">
            <path d="M0,0 L6,3 L0,6 z" className="fill-accent" />
          </marker>
        </defs>
        {paths.map((path) => {
          const isSelected = selected?.linkId === path.dependency.linkId;
          const isHot = isSelected || hovered === path.dependency.linkId;
          return (
            <g key={path.key}>
              <path
                d={path.d}
                fill="none"
                strokeWidth={isHot ? 2.5 : 1.5}
                markerEnd={isHot ? "url(#plan-arrow-selected)" : "url(#plan-arrow)"}
                className={isHot ? "stroke-accent" : "stroke-ink-subtle"}
                data-plan-arrow={path.key}
                data-selected={isSelected || undefined}
              />
              <path
                d={path.d}
                fill="none"
                stroke="transparent"
                strokeWidth="12"
                className="pointer-events-auto cursor-pointer"
                data-plan-arrow-hit={path.key}
                onClick={() => onSelect(path.dependency)}
                onPointerEnter={() => hot(path.dependency.linkId)}
                onPointerLeave={cool}
              >
                <title>
                  {path.dependency.blockerKey} blocks {path.dependency.blockedKey}
                </title>
              </path>
            </g>
          );
        })}
      </svg>
      {shown && (
        <IconButton
          size="xs"
          icon={<Icon.X />}
          label={`Remove dependency ${shown.dependency.blockerKey} blocks ${shown.dependency.blockedKey}`}
          className="absolute z-10 -translate-x-1/2 -translate-y-1/2 border border-accent bg-surface text-accent shadow-sm hover:bg-accent-subtle"
          style={{ left: shown.handle.x, top: shown.handle.y }}
          data-plan-remove-dependency
          data-plan-unlink={shown.dependency.linkId}
          disabled={removing}
          onPointerEnter={() => hot(shown.dependency.linkId)}
          onPointerLeave={cool}
          onClick={() => onRemove(shown.dependency)}
        />
      )}
    </>
  );
}

function WarningList({ warnings }: { warnings: PlanWarning[] }) {
  if (warnings.length === 0) return null;

  return (
    <section>
      <h2 className="mb-2 text-xs font-semibold tracking-wide text-ink-muted uppercase">
        Where the plan disagrees with itself ({warnings.length})
      </h2>
      <ul className="space-y-1.5" data-testid="plan-warnings">
        {warnings.map((warning) => (
          <li
            key={`${warning.issueKey ?? warning.sprint ?? warning.team}-${warning.kind}-${warning.message}`}
            className="flex items-baseline gap-2 text-sm"
            data-plan-warning-for={warning.issueKey ?? warning.sprint ?? warning.team}
            data-plan-severity={severityOf(warning.kind)}
          >
            <Icon.Warning className={cx("size-3.5 shrink-0 self-center", severityOf(warning.kind) === "error" ? "text-danger" : "text-warning")} aria-hidden="true" />
            {warning.issueKey ? (
              <Link
                to="/issues/$issueKey"
                params={{ issueKey: warning.issueKey }}
                className="shrink-0 font-mono text-xs text-accent hover:underline"
              >
                {warning.issueKey}
              </Link>
            ) : (
              <span className="shrink-0 text-xs font-medium text-ink">{warning.sprint ?? warning.team}</span>
            )}
            <span className="text-ink-muted">{warning.message}</span>
          </li>
        ))}
      </ul>
    </section>
  );
}
