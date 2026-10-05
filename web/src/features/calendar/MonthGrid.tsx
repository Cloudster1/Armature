import { useMemo, useRef, useState, type PointerEvent as ReactPointerEvent } from "react";
import { Link } from "@tanstack/react-router";
import { useCalendarMonth, type CalendarItem } from "@/api/calendar";
import { useSchedule } from "@/api/plan";
import { Button, ErrorBanner, Switch } from "@/components/ui";
import { cx } from "@/components/ui/cx";
import { draggedSchedule, isoDay, labelOf, listedItems, monthGrid, monthTitle, placeItems, shiftMonth, withHolidays, type Placed } from "./grid";
import { useShowDaysOff } from "./settings";

const WEEKDAYS = ["Mon", "Tue", "Wed", "Thu", "Fri", "Sat", "Sun"];

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

/**
 * A month of the project's dated things. An issue's bar is dragged by the
 * pointer to another day and both its dates move together; the rest is read.
 */
export function MonthGrid({ projectKey, canWrite }: { projectKey: string; canWrite: boolean }) {
  const today = new Date();
  const [{ year, month }, setMonth] = useState({ year: today.getFullYear(), month: today.getMonth() + 1 });
  const { data, error } = useCalendarMonth(projectKey, year, month);
  const schedule = useSchedule();
  const [showDaysOff, setShowDaysOff] = useShowDaysOff();
  const items = useMemo(() => data?.month.items ?? [], [data]);
  const cells = useMemo(() => withHolidays(monthGrid(year, month), showDaysOff ? items : []), [year, month, items, showDaysOff]);
  const byDay = useMemo(() => placeItems(cells, listedItems(items, showDaysOff)), [cells, items, showDaysOff]);

  // The drag: which issue was grabbed on which day, and which day the pointer is over now.
  const drag = useRef<{ item: CalendarItem; grabbed: string } | null>(null);
  const [over, setOver] = useState<string | null>(null);

  function dayUnder(event: ReactPointerEvent): string | null {
    const el = document.elementFromPoint(event.clientX, event.clientY)?.closest("[data-day]");
    return el?.getAttribute("data-day") ?? null;
  }

  function onPointerDown(event: ReactPointerEvent<HTMLElement>, item: CalendarItem, day: string) {
    if (!canWrite || item.kind !== "issue") return;
    event.preventDefault();
    drag.current = { item, grabbed: day };
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
    if (!drag.current) return;
    setOver(dayUnder(event));
  }

  function onPointerUp(event: ReactPointerEvent<HTMLElement>) {
    const current = drag.current;
    drag.current = null;
    setOver(null);
    if (!current) return;
    const target = dayUnder(event);
    if (!target || target === current.grabbed || !current.item.key) return;
    schedule.mutate({ key: current.item.key, ...draggedSchedule(current.item, target, current.grabbed) });
  }

  return (
    <div className="space-y-3" data-calendar={`${year}-${String(month).padStart(2, "0")}`}>
      <div className="flex items-center justify-between">
        <h2 className="text-base font-semibold text-ink" data-calendar-title>
          {monthTitle(year, month)}
        </h2>
        <div className="flex items-center gap-1">
          <label className="mr-3 flex items-center gap-2 text-xs text-ink-muted">
            <Switch checked={showDaysOff} onChange={setShowDaysOff} label="Show holidays and absences" data-action="calendar-days-off" />
            Holidays and absences
          </label>
          <Button size="sm" variant="ghost" onClick={() => setMonth(shiftMonth(year, month, -1))} data-action="calendar-prev">
            Previous
          </Button>
          <Button size="sm" variant="ghost" onClick={() => setMonth({ year: today.getFullYear(), month: today.getMonth() + 1 })} data-action="calendar-today">
            Today
          </Button>
          <Button size="sm" variant="ghost" onClick={() => setMonth(shiftMonth(year, month, 1))} data-action="calendar-next">
            Next
          </Button>
        </div>
      </div>
      {error && <ErrorBanner>{(error as Error).message}</ErrorBanner>}
      {schedule.error && <ErrorBanner>{(schedule.error as Error).message}</ErrorBanner>}
      {data?.month.truncated && <p className="text-sm text-ink-muted">More is dated this month than the calendar draws; the plan has all of it.</p>}
      <div className="grid grid-cols-7 gap-px rounded-control border border-border bg-border text-xs" role="grid" aria-label={monthTitle(year, month)}>
        {WEEKDAYS.map((d) => (
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
                  <Item key={`${placed.item.kind}-${placed.item.id}`} placed={placed} day={cell.day} draggable={canWrite && placed.item.kind === "issue"} onPointerDown={onPointerDown} onPointerMove={onPointerMove} onPointerUp={onPointerUp} />
                ))}
                {onDay.more > 0 && <li className="px-1 text-ink-subtle" data-calendar-more={onDay.more}>{`+${onDay.more} more`}</li>}
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
  draggable,
  onPointerDown,
  onPointerMove,
  onPointerUp,
}: {
  placed: Placed;
  day: string;
  draggable: boolean;
  onPointerDown: (event: ReactPointerEvent<HTMLElement>, item: CalendarItem, day: string) => void;
  onPointerMove: (event: ReactPointerEvent<HTMLElement>) => void;
  onPointerUp: (event: ReactPointerEvent<HTMLElement>) => void;
}) {
  const { item, starts, ends } = placed;
  const label = labelOf(item);
  const shape = cx("block truncate border px-1 py-0.5 leading-4", toneOf(item), starts ? "rounded-l" : "-ml-1 border-l-0", ends ? "rounded-r" : "-mr-1 border-r-0");
  return (
    <li
      data-calendar-item={item.key ?? item.id}
      data-calendar-kind={item.kind}
      title={label}
      className={cx(draggable && "cursor-grab touch-none select-none")}
      onPointerDown={(e) => onPointerDown(e, item, day)}
      onPointerMove={onPointerMove}
      onPointerUp={onPointerUp}
    >
      {item.kind === "issue" && item.key ? (
        <Link to="/issues/$issueKey" params={{ issueKey: item.key }} className={shape} draggable={false} onClick={(e) => e.preventDefault()} onDoubleClick={(e) => e.currentTarget.click()}>
          {starts ? label : " "}
        </Link>
      ) : (
        <span className={shape}>{starts ? label : " "}</span>
      )}
    </li>
  );
}
