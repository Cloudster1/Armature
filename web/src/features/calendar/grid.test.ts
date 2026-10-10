import { describe, expect, it } from "vitest";
import type { CalendarItem } from "@/api/calendar";
import { daysBetween, draggedSchedule, labelOf, listedItems, monthGrid, placeItems, shiftDay, shiftMonth, withHolidays } from "./grid";

function item(over: Partial<CalendarItem>): CalendarItem {
  return { kind: "issue", id: over.id ?? over.key ?? "x", title: over.title ?? "t", from: "2026-09-10", to: "2026-09-10", ...over };
}

describe("the month grid", () => {
  it("starts on the Monday before the first and shows six weeks", () => {
    // September 2026 begins on a Tuesday.
    const cells = monthGrid(2026, 9);
    expect(cells).toHaveLength(42);
    expect(cells[0]!.day).toBe("2026-08-31");
    expect(cells[0]!.inMonth).toBe(false);
    expect(cells[1]!.day).toBe("2026-09-01");
    expect(cells[1]!.inMonth).toBe(true);
    expect(cells.filter((c) => c.inMonth)).toHaveLength(30);
    expect(cells[5]!.weekend).toBe(true);
  });

  it("walks months and days across year ends", () => {
    expect(shiftMonth(2026, 12, 1)).toEqual({ year: 2027, month: 1 });
    expect(shiftMonth(2026, 1, -1)).toEqual({ year: 2025, month: 12 });
    expect(shiftDay("2026-12-31", 1)).toBe("2027-01-01");
    expect(daysBetween("2026-09-01", "2026-09-04")).toBe(3);
    expect(daysBetween("2026-09-04", "2026-09-01")).toBe(-3);
  });

  it("lays a range on every day it covers, longest first, and counts the rest", () => {
    const cells = monthGrid(2026, 9);
    const items = [
      item({ key: "CP-1", title: "one day", from: "2026-09-10", to: "2026-09-10" }),
      item({ key: "CP-2", title: "a week", from: "2026-09-08", to: "2026-09-14" }),
      item({ key: "CP-3", from: "2026-09-10", to: "2026-09-10" }),
      item({ key: "CP-4", from: "2026-09-10", to: "2026-09-10" }),
    ];
    const placed = placeItems(cells, items, 2);
    const tenth = placed.get("2026-09-10")!;
    expect(tenth.shown.map((p) => p.item.key)).toEqual(["CP-2", "CP-1"]);
    expect(tenth.more).toBe(2);
    expect(tenth.shown[0]).toMatchObject({ starts: false, ends: false });
    expect(placed.get("2026-09-08")!.shown[0]).toMatchObject({ starts: true, ends: false });
    expect(placed.get("2026-09-14")!.shown[0]).toMatchObject({ starts: false, ends: true });
    expect(placed.get("2026-09-20")!.shown).toHaveLength(0);
  });

  it("moves both ends of a dragged bar by the days the pointer travelled", () => {
    const bar = item({ key: "CP-2", from: "2026-09-08", to: "2026-09-14" });
    expect(draggedSchedule(bar, "2026-09-12", "2026-09-10")).toEqual({ startDate: "2026-09-10", dueDate: "2026-09-16" });
    expect(draggedSchedule(bar, "2026-09-01", "2026-09-08")).toEqual({ startDate: "2026-09-01", dueDate: "2026-09-07" });
  });
});

describe("holidays and absences", () => {
  const holiday = item({ kind: "holiday", id: "h1", title: "Founders' Day", from: "2026-09-16", to: "2026-09-16" });
  const half = item({ kind: "holiday", id: "h2", title: "Fair", from: "2026-09-18", to: "2026-09-18", halfDay: true });
  const lisbon = item({ kind: "holiday", id: "h3", title: "Lisbon day (Lisbon)", calendar: "Lisbon", from: "2026-09-17", to: "2026-09-17" });
  const away = item({ kind: "absence", id: "a1", title: "Ada Lovelace", from: "2026-09-14", to: "2026-09-15" });
  const issue = item({ key: "CP-1", title: "work" });
  const all = [holiday, half, lisbon, away, issue];

  it("shades the default calendar's days by name and lists nothing for them", () => {
    const cells = withHolidays(monthGrid(2026, 9), all);
    expect(cells.find((c) => c.day === "2026-09-16")!.holiday).toBe("Founders' Day");
    expect(cells.find((c) => c.day === "2026-09-18")!.holiday).toBe("Fair (half day)");
    expect(cells.find((c) => c.day === "2026-09-17")!.holiday).toBeUndefined();
    expect(listedItems(all, true).map((it) => it.id)).toEqual(["h3", "a1", "CP-1"]);
  });

  it("draws an absence on every day it covers, saying who and that they are away", () => {
    const placed = placeItems(monthGrid(2026, 9), listedItems(all, true));
    expect(placed.get("2026-09-14")!.shown.map((p) => labelOf(p.item))).toEqual(["Ada Lovelace away"]);
    expect(placed.get("2026-09-15")!.shown[0]).toMatchObject({ starts: false, ends: true });
    expect(labelOf({ ...away, halfDay: true })).toBe("Ada Lovelace away half the day");
    expect(labelOf(issue)).toBe("CP-1 work");
  });

  it("draws an absence that runs past the grid as continuing, on the neighbouring months' days too", () => {
    // September 2026's grid runs from Monday 31 August to Sunday 11 October.
    const cells = monthGrid(2026, 9);
    const bob = item({ kind: "absence", id: "a2", title: "Bob", from: "2026-09-28", to: "2026-10-20" });
    const ada = item({ kind: "absence", id: "a3", title: "Ada", from: "2026-08-17", to: "2026-09-02" });
    const placed = placeItems(cells, [bob, ada]);
    expect(placed.get("2026-09-28")!.shown[0]).toMatchObject({ starts: true, ends: false });
    expect(placed.get("2026-10-01")!.shown.map((p) => p.item.id)).toEqual(["a2"]);
    expect(placed.get("2026-10-11")!.shown[0]).toMatchObject({ starts: false, ends: false });
    expect(placed.get("2026-08-31")!.shown[0]).toMatchObject({ starts: false, ends: false });
    expect(placed.get("2026-09-02")!.shown[0]).toMatchObject({ starts: false, ends: true });
  });

  it("names a neighbouring month's holiday on its padding day", () => {
    const harvest = item({ kind: "holiday", id: "h4", title: "Harvest Day", from: "2026-10-02", to: "2026-10-02" });
    const cells = withHolidays(monthGrid(2026, 9), [harvest]);
    expect(cells.find((c) => c.day === "2026-10-02")).toMatchObject({ inMonth: false, holiday: "Harvest Day" });
  });

  it("hides them all when the reader says so, the rest staying", () => {
    expect(listedItems(all, false).map((it) => it.id)).toEqual(["CP-1"]);
    expect(withHolidays(monthGrid(2026, 9), []).some((c) => c.holiday)).toBe(false);
  });
});
