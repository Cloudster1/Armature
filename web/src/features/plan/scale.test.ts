import { describe, expect, it } from "vitest";
import { PLAN_MIN_PX_PER_DAY } from "@/config";
import {
  ZOOM_FIT,
  ZOOM_WEEKS,
  addDays,
  applyDrag,
  barFor,
  day,
  daysBetween,
  filled,
  fitted,
  headingTicks,
  isoDay,
  makeScale,
  monthTicks,
  quarterTicks,
  ticksFor,
  weekTicks,
  zoomFor,
} from "./scale";

const from = day("2026-03-01");
const to = day("2026-04-01");
const scale = makeScale(from, to, 10);

describe("filled", () => {
  // Months at seven pixels a day over a two-month plan is a strip 420 pixels
  // wide beside a blank pane; the calendar has to keep going to the edge.
  it("extends a short window to the width of the pane", () => {
    const end = filled(day("2026-03-01"), day("2026-05-01"), 7, 826);
    expect(daysBetween(day("2026-03-01"), end)).toBe(Math.ceil(826 / 7));
  });

  it("leaves a window alone once it is wide enough", () => {
    const end = filled(day("2026-03-01"), day("2026-05-01"), 22, 826);
    expect(end).toEqual(day("2026-05-01"));
  });

  it("rounds up, so the last column reaches the edge rather than stopping short", () => {
    const end = filled(day("2026-03-01"), day("2026-03-02"), 2.6, 100);
    expect(daysBetween(day("2026-03-01"), end) * 2.6).toBeGreaterThanOrEqual(100);
  });
});

describe("makeScale", () => {
  it("puts the first day at zero and each later day a step further along", () => {
    expect(scale.x("2026-03-01")).toBe(0);
    expect(scale.x("2026-03-02")).toBe(10);
    expect(scale.x("2026-03-11")).toBe(100);
  });

  it("is as wide as the window it covers", () => {
    expect(scale.width).toBe(31 * 10);
  });

  it("reads a pixel back as the day it falls in", () => {
    expect(isoDay(scale.dateAt(0))).toBe("2026-03-01");
    expect(isoDay(scale.dateAt(104))).toBe("2026-03-11");
    expect(isoDay(scale.dateAt(95))).toBe("2026-03-11");
  });

  it("gives a one-day task a full day of width rather than none", () => {
    expect(scale.spanWidth("2026-03-05", "2026-03-05")).toBe(10);
  });

  it("counts both ends of a range, because a due date is a day of work", () => {
    expect(scale.spanWidth("2026-03-01", "2026-03-03")).toBe(30);
  });

  it("ignores the time of day in a timestamp", () => {
    expect(scale.x("2026-03-02T23:30:00Z")).toBe(scale.x("2026-03-02T00:00:00Z"));
  });
});

describe("barFor", () => {
  it("has nothing to draw for an issue with only one end", () => {
    expect(barFor(scale, "2026-03-01", null)).toBeNull();
    expect(barFor(scale, null, "2026-03-01")).toBeNull();
    expect(barFor(scale, undefined, undefined)).toBeNull();
  });

  it("places a bar at its start with the width of its range", () => {
    expect(barFor(scale, "2026-03-03", "2026-03-06")).toEqual({ left: 20, width: 40 });
  });
});

describe("applyDrag", () => {
  const start = day("2026-03-10");
  const due = day("2026-03-20");

  it("keeps the length when the whole bar moves", () => {
    const moved = applyDrag(start, due, "move", 3);
    expect(isoDay(moved.start)).toBe("2026-03-13");
    expect(isoDay(moved.due)).toBe("2026-03-23");
  });

  it("moves one end when an edge is dragged", () => {
    expect(isoDay(applyDrag(start, due, "start", -4).start)).toBe("2026-03-06");
    expect(isoDay(applyDrag(start, due, "start", -4).due)).toBe("2026-03-20");
    expect(isoDay(applyDrag(start, due, "end", 5).due)).toBe("2026-03-25");
  });

  it("will not let a range turn inside out", () => {
    const squashed = applyDrag(start, due, "start", 40);
    expect(isoDay(squashed.start)).toBe("2026-03-20");
    expect(isoDay(squashed.due)).toBe("2026-03-20");

    const other = applyDrag(start, due, "end", -40);
    expect(isoDay(other.start)).toBe("2026-03-10");
    expect(isoDay(other.due)).toBe("2026-03-10");
  });

  it("does nothing at all for a drag that did not move a whole day", () => {
    expect(applyDrag(start, due, "move", 0)).toEqual({ start, due });
  });
});

describe("ticks", () => {
  it("labels every month the window touches", () => {
    // The right edge is exclusive, so a window ending on 1 April shows no April.
    expect(monthTicks(scale).map((t) => t.label)).toEqual(["Mar 2026"]);

    const intoApril = makeScale(day("2026-03-01"), day("2026-04-02"), 10);
    expect(monthTicks(intoApril).map((t) => t.label)).toEqual(["Mar 2026", "Apr 2026"]);
  });

  it("keeps a bar ending on the last visible day inside the width", () => {
    const bar = barFor(scale, "2026-03-30", "2026-03-31");
    expect(bar!.left + bar!.width).toBeLessThanOrEqual(scale.width);
  });

  it("clips the first and last column to the window", () => {
    const wide = makeScale(day("2026-03-15"), day("2026-04-10"), 10);
    const march = monthTicks(wide)[0];
    expect(march).toMatchObject({ x: 0, width: 17 * 10 });
  });

  it("starts weeks on Monday", () => {
    // 1 March 2026 is a Sunday, so the first column reaches back to 23 February.
    const labels = weekTicks(scale).map((t) => t.label);
    expect(labels.slice(0, 2)).toEqual(["23 Feb", "2 Mar"]);
  });

  it("never draws a column starting left of the window", () => {
    for (const tick of weekTicks(scale)) expect(tick.x).toBeGreaterThanOrEqual(0);
  });

  it("follows the zoom in choosing which row of ticks to draw", () => {
    expect(ticksFor(scale, ZOOM_WEEKS)).toEqual(weekTicks(scale));
    expect(ticksFor(scale, { ...ZOOM_WEEKS, ticks: "month" })).toEqual(monthTicks(scale));
  });

  it("labels quarters from January, not from the start of the window", () => {
    const year = makeScale(day("2026-02-01"), day("2026-12-31"), 2);
    expect(quarterTicks(year).map((t) => t.label)).toEqual([
      "Q1 2026",
      "Q2 2026",
      "Q3 2026",
      "Q4 2026",
    ]);
  });

  it("keeps the two header rows from saying the same thing twice", () => {
    const monthly = { ...ZOOM_WEEKS, ticks: "month" as const };
    expect(headingTicks(scale, monthly)).not.toEqual(ticksFor(scale, monthly));
    expect(headingTicks(scale, ZOOM_WEEKS)).toEqual(monthTicks(scale));
  });
});

describe("dates", () => {
  it("counts whole days between two dates", () => {
    expect(daysBetween(day("2026-03-01"), day("2026-03-31"))).toBe(30);
    expect(daysBetween(day("2026-03-31"), day("2026-03-01"))).toBe(-30);
  });

  it("counts across a daylight saving change, which is not a whole day", () => {
    // Europe springs forward on 29 March 2026; UTC days are unaffected.
    expect(daysBetween(day("2026-03-28"), day("2026-03-30"))).toBe(2);
  });

  it("adds days without drifting through midnight", () => {
    expect(isoDay(addDays(day("2026-02-28"), 1))).toBe("2026-03-01");
  });
});

describe("zoomFor", () => {
  it("finds a zoom by id", () => {
    expect(zoomFor("months").pxPerDay).toBe(7);
  });

  it("falls back to the fitted view rather than nothing", () => {
    expect(zoomFor("nonsense")).toEqual(ZOOM_FIT);
  });
});

describe("fitted", () => {
  const from = new Date("2026-03-01T00:00:00Z");
  const to = new Date("2026-04-10T00:00:00Z"); // 40 days

  it("spreads the whole range over the width on offer", () => {
    const zoom = fitted(ZOOM_FIT, from, to, 800);
    expect(zoom.pxPerDay).toBe(20);
    expect(zoom.ticks).toBe("week");
  });

  // A year in a narrow pane would make a week a few pixels wide; then the
  // calendar scrolls a little rather than labelling nothing legible.
  it("stops at the floor and labels months once weeks are too narrow", () => {
    const year = new Date("2027-03-01T00:00:00Z");
    const zoom = fitted(ZOOM_FIT, from, year, 600);
    expect(zoom.pxPerDay).toBe(PLAN_MIN_PX_PER_DAY);
    expect(zoom.ticks).toBe("month");
  });

  it("leaves a fixed zoom alone", () => {
    expect(fitted(ZOOM_WEEKS, from, to, 100)).toEqual(ZOOM_WEEKS);
  });

  it("is the zoom a plan opens on", () => {
    expect(zoomFor("nonsense").id).toBe("fit");
  });
});
