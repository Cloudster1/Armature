import { useMemo, useRef, useState, type MouseEvent as ReactMouseEvent, type PointerEvent as ReactPointerEvent } from "react";
import { Link } from "@tanstack/react-router";
import { useCalendarMonth, type CalendarItem } from "@/api/calendar";
import { useSchedule } from "@/api/plan";
import { Button, ErrorBanner, Switch } from "@/components/ui";
import { cx } from "@/components/ui/cx";
import { CALENDAR_DRAG_THRESHOLD_PX } from "@/config";
import { useIssueDrawer, useIssueList } from "@/features/issues/IssueDrawer";
import { draggedSchedule, isoDay, labelOf, listedItems, monthGrid, monthTitle, placeItems, shiftMonth, withHolidays, type Placed } from "./grid";
import { useShowDaysOff } from "./settings";
import { useFormat } from "@/lib/format";
import { t } from "@/i18n";

// Read while drawing, so the weekdays follow the language.
const weekdays = (): string[] => t.schedule.calendar.weekdays;

/** The colour a kind of item wears; a done issue goes quiet. */
function toneOf(item: CalendarItem): string {
  if (item.kind === "sprint") return "bg-chart-2/20 text-ink border-chart-2/40";
  if (item.kind === "milestone") return "bg-chart-4/20 text-ink border-chart-4/40";
  if (item.kind === "version") return "bg-chart-5/20 text-ink border-chart-5/40";
  if (item.kind === "absence") return "bg-chart-3/15 text-ink border-chart-3/40";
  if (item.kind === "holiday") return "bg-surface-raised text-ink-muted border-border";
  if (item.category === "done" || item.done) return "bg-surface-raised text-ink-subtle border-border line-through";
  return "bg-accent/15 text-ink border-accent/30";
}

/** A click that asks for a new tab or window is the link's own business. */
function opensElsewhere(event: ReactMouseEvent): boolean {
  return event.button !== 0 || event.metaKey || event.ctrlKey || event.shiftKey || event.altKey;
}

/**
 * A month of the project's dated things. A click on an issue opens it beside
 * the calendar, and its bar is dragged to another day to move both its dates.
 */
export function MonthGrid({ projectKey, canWrite }: { projectKey: string; canWrite: boolean }) {
  const today = new Date();
  const [{ year, month }, setMonth] = useState({ year: today.getFullYear(), month: today.getMonth() + 1 });
  const { data, error } = useCalendarMonth(projectKey, year, month);
  const schedule = useSchedule();
  const [showDaysOff, setShowDaysOff] = useShowDaysOff();
  const { locale } = useFormat();
  const items = useMemo(() => data?.month.items ?? [], [data]);
  const cells = useMemo(() => withHolidays(monthGrid(year, month), showDaysOff ? items : []), [year, month, items, showDaysOff]);
  const byDay = useMemo(() => placeItems(cells, listedItems(items, showDaysOff)), [cells, items, showDaysOff]);
  const drawer = useIssueDrawer();
  useIssueList(items.flatMap((item) => (item.kind === "issue" && item.key ? [item.key] : [])));

  // The drag: which issue was grabbed on which day and where, whether it has
  // gone far enough to be a drag, and which day the pointer is over now.
  const drag = useRef<{ item: CalendarItem; grabbed: string; x: number; y: number; moved: boolean } | null>(null);
  const [over, setOver] = useState<string | null>(null);
  // A drag still ends in a click on the bar it started from; that one opens nothing.
  const dragged = useRef(false);

  function dayUnder(event: ReactPointerEvent): string | null {
    const el = document.elementFromPoint(event.clientX, event.clientY)?.closest("[data-day]");
    return el?.getAttribute("data-day") ?? null;
  }

  function onPointerDown(event: ReactPointerEvent<HTMLElement>, item: CalendarItem, day: string) {
    if (!canWrite || item.kind !== "issue") return;
    event.preventDefault();
    drag.current = { item, grabbed: day, x: event.clientX, y: event.clientY, moved: false };
    dragged.current = false;
    // Capture keeps the moves coming when the pointer leaves the bar; a
    // synthetic pointer has no id to capture, and that is fine.
    try {
      event.currentTarget.setPointerCapture(event.pointerId);
    } catch {
      // nothing to capture
    }
    setOver(day);
  }

  function onPointerMove(event: ReactPointerEvent<HTMLElement>) {
    const current = drag.current;
    if (!current) return;
    if (Math.hypot(event.clientX - current.x, event.clientY - current.y) >= CALENDAR_DRAG_THRESHOLD_PX) current.moved = true;
    setOver(dayUnder(event));
  }

  function onPointerUp(event: ReactPointerEvent<HTMLElement>) {
    const current = drag.current;
    drag.current = null;
    setOver(null);
    if (!current) return;
    const target = dayUnder(event);
    dragged.current = current.moved || target !== current.grabbed;
    if (!target || target === current.grabbed || !current.item.key) return;
    schedule.mutate({ key: current.item.key, ...draggedSchedule(current.item, target, current.grabbed) });
  }

  // On the bar rather than its link: the bar holds the pointer while it is
  // down, so the click after it lands here. Enter on the link comes here too.
  function onClick(event: ReactMouseEvent<HTMLElement>, item: CalendarItem) {
    const afterDrag = dragged.current;
    dragged.current = false;
    if (item.kind !== "issue" || !item.key || opensElsewhere(event)) return;
    event.preventDefault();
    // A click from the keyboard has no pointer behind it, so no drag either.
    if (afterDrag && event.detail !== 0) return;
    drawer.open(item.key);
  }

  return (
    <div className="space-y-3" data-calendar={`${year}-${String(month).padStart(2, "0")}`}>
      <div className="flex items-center justify-between">
        <h2 className="text-base font-semibold text-ink" data-calendar-title>
          {monthTitle(year, month, locale)}
        </h2>
        <div className="flex items-center gap-1">
          <label className="mr-3 flex items-center gap-2 text-xs text-ink-muted">
            <Switch checked={showDaysOff} onChange={setShowDaysOff} label={t.schedule.calendar.showDaysOff} data-action="calendar-days-off" />
            {t.schedule.calendar.daysOff}
          </label>
          <Button size="sm" variant="ghost" onClick={() => setMonth(shiftMonth(year, month, -1))} data-action="calendar-prev">
            {t.schedule.calendar.previous}
          </Button>
          <Button size="sm" variant="ghost" onClick={() => setMonth({ year: today.getFullYear(), month: today.getMonth() + 1 })} data-action="calendar-today">
            {t.schedule.calendar.today}
          </Button>
          <Button size="sm" variant="ghost" onClick={() => setMonth(shiftMonth(year, month, 1))} data-action="calendar-next">
            {t.schedule.calendar.next}
          </Button>
        </div>
      </div>
      {error && <ErrorBanner>{(error as Error).message}</ErrorBanner>}
      {schedule.error && <ErrorBanner>{(schedule.error as Error).message}</ErrorBanner>}
      {data?.month.truncated && <p className="text-sm text-ink-muted">{t.schedule.calendar.truncated}</p>}
      <div
        className="grid grid-cols-7 gap-px rounded-control border border-border bg-border text-xs"
        role="grid"
        aria-label={monthTitle(year, month, locale)}
        data-issue-list=""
      >
        {weekdays().map((d) => (
          <div key={d} className="bg-surface px-2 py-1 font-medium text-ink-muted" role="columnheader">
            {d}
          </div>
        ))}
        {cells.map((cell) => {
          const onDay = byDay.get(cell.day) ?? { shown: [], more: 0 };
          const isToday = cell.day === isoDay(today);
          return (
            <div
              key={cell.day}
              role="gridcell"
              data-day={cell.day}
              data-drop={over === cell.day ? "true" : undefined}
              data-holiday={cell.holiday}
              title={cell.holiday}
              className={cx(
                "min-h-24 bg-surface p-1",
                !cell.inMonth && "bg-surface-raised/60 text-ink-subtle",
                (cell.weekend || cell.holiday) && cell.inMonth && "bg-surface-raised/30",
                over === cell.day && "ring-2 ring-inset ring-accent",
              )}
            >
              <div className="mb-1 flex items-baseline justify-between gap-1">
                <span className="truncate text-2xs text-ink-subtle">{cell.holiday}</span>
                <span className={cx("tabular-nums", isToday && "font-semibold text-accent")}>{Number(cell.day.slice(-2))}</span>
              </div>
              <ul className="space-y-0.5">
                {onDay.shown.map((placed) => (
                  <Item
                    key={`${placed.item.kind}-${placed.item.id}`}
                    placed={placed}
                    day={cell.day}
                    projectKey={projectKey}
                    draggable={canWrite && placed.item.kind === "issue"}
                    onPointerDown={onPointerDown}
                    onPointerMove={onPointerMove}
                    onPointerUp={onPointerUp}
                    onClick={onClick}
                  />
                ))}
                {onDay.more > 0 && (
                  <li className="px-1 text-ink-subtle" data-calendar-more={onDay.more}>
                    {t.schedule.calendar.more(onDay.more)}
                  </li>
                )}
              </ul>
            </div>
          );
        })}
      </div>
    </div>
  );
}

function Item({
  placed,
  day,
  projectKey,
  draggable,
  onPointerDown,
  onPointerMove,
  onPointerUp,
  onClick,
}: {
  placed: Placed;
  day: string;
  projectKey: string;
  draggable: boolean;
  onPointerDown: (event: ReactPointerEvent<HTMLElement>, item: CalendarItem, day: string) => void;
  onPointerMove: (event: ReactPointerEvent<HTMLElement>) => void;
  onPointerUp: (event: ReactPointerEvent<HTMLElement>) => void;
  onClick: (event: ReactMouseEvent<HTMLElement>, item: CalendarItem) => void;
}) {
  const { item, starts, ends } = placed;
  const label = labelOf(item);
  const shape = cx(
    "block truncate border px-1 py-0.5 leading-4",
    toneOf(item),
    starts ? "rounded-l" : "-ml-1 border-l-0",
    ends ? "rounded-r" : "-mr-1 border-r-0",
  );
  const text = starts ? label : " ";
  return (
    <li
      data-calendar-item={item.key ?? item.id}
      data-calendar-kind={item.kind}
      title={label}
      className={cx(draggable && "cursor-grab touch-none select-none")}
      onPointerDown={(e) => onPointerDown(e, item, day)}
      onPointerMove={onPointerMove}
      onPointerUp={onPointerUp}
      onClick={(e) => onClick(e, item)}
    >
      <Entry item={item} projectKey={projectKey} className={shape}>
        {text}
      </Entry>
    </li>
  );
}

/** Where an entry leads: an issue to its page, the rest to where they are kept; days off lead nowhere. */
function Entry({ item, projectKey, className, children }: { item: CalendarItem; projectKey: string; className: string; children: string }) {
  if (item.kind === "issue" && item.key) {
    // The bar opens the drawer; the link stays for a new tab and the keyboard.
    return (
      <Link
        to="/issues/$issueKey"
        params={{ issueKey: item.key }}
        className={className}
        draggable={false}
        onClick={(e) => {
          if (!opensElsewhere(e)) e.preventDefault();
        }}
      >
        {children}
      </Link>
    );
  }
  if (item.kind === "sprint") {
    return (
      <Link to="/projects/$projectKey/sprints/$sprintId/board" params={{ projectKey, sprintId: item.id }} className={className}>
        {children}
      </Link>
    );
  }
  if (item.kind === "milestone") {
    return (
      <Link to="/projects/$projectKey/milestones" params={{ projectKey }} className={className}>
        {children}
      </Link>
    );
  }
  if (item.kind === "version") {
    return (
      <Link to="/projects/$projectKey/releases" params={{ projectKey }} className={className}>
        {children}
      </Link>
    );
  }
  return <span className={className}>{children}</span>;
}
