import { describe, expect, it } from "vitest";
import type { CalendarItem } from "@/api/calendar";
import { daysBetween, draggedSchedule, monthGrid, placeItems, shiftDay, shiftMonth } from "./grid";

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
