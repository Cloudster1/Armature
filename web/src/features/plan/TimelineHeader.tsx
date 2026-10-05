import { useMemo } from "react";
import type { Holiday } from "@/api/availability";
import { cx } from "@/components/ui";
import { PLAN_MIN_TICK_LABEL_PX, PLAN_ROW_HEIGHT } from "@/config";
import { headerLayout } from "./layout";
import { headingTicks, holidayBands, makeScale, ticksFor, type Zoom } from "./scale";

const NO_HOLIDAYS: Holiday[] = [];

/**
 * The months and weeks across the top of the timeline, and the default
 * calendar's holidays shaded down the whole calendar beneath them.
 */
export function TimelineHeader({
  scale,
  zoom,
  todayX,
  holidays = NO_HOLIDAYS,
}: {
  scale: ReturnType<typeof makeScale>;
  zoom: Zoom;
  todayX: number;
  holidays?: Holiday[];
}) {
  const heading = useMemo(() => headingTicks(scale, zoom), [scale, zoom]);
  const ticks = useMemo(() => ticksFor(scale, zoom), [scale, zoom]);
  const daysOff = useMemo(() => holidayBands(scale, holidays), [scale, holidays]);

  // The shading sits on the canvas, below the header too. The header is sized
  // border-box like the bands beneath it, its bottom border inside its height.
  return (
    <>
      {daysOff.map((band) => (
        <span
          key={band.key}
          aria-hidden="true"
          className="pointer-events-none absolute top-0 bottom-0 bg-surface-raised/70"
          style={{ left: band.x, width: band.width }}
          data-plan-holiday={band.key}
        />
      ))}
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
          {daysOff.map((band) => (
            <span key={band.key} className="absolute inset-y-0" style={{ left: band.x, width: band.width }} title={band.label} data-plan-holiday-label={band.label} />
          ))}
          <span
            aria-hidden="true"
            className="absolute -top-1 w-px bg-accent"
            style={{ left: todayX, height: PLAN_ROW_HEIGHT }}
          />
        </div>
      </div>
    </>
  );
}
