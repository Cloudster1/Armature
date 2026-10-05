import { beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { CalendarDays } from "./CalendarDays";

const setDays = vi.fn();
const importDays = vi.fn();
const calendar = {
  id: "c1",
  name: "Germany",
  default: true,
  dayCount: 2,
  peopleCount: 0,
  createdAt: "2026-01-01T00:00:00Z",
  updatedAt: "2026-01-01T00:00:00Z",
  days: [
    { day: "2026-12-24", name: "Heiligabend", halfDay: true },
    { day: "2026-12-25", name: "Erster Weihnachtstag", halfDay: false },
  ],
};
vi.mock("@/api/availability", () => ({
  useHolidayCalendar: () => ({ data: { calendar }, error: null }),
  useSetHolidays: () => ({ mutate: setDays, isPending: false, error: null }),
  useImportHolidays: () => ({ mutate: importDays, isPending: false, error: null }),
}));
vi.mock("@/lib/format", () => ({ useFormat: () => ({ locale: "en-GB" }) }));
vi.mock("@/components/ui", async () => {
  const actual = await vi.importActual<typeof import("@/components/ui")>("@/components/ui");
  return { ...actual, useToast: () => ({ success: vi.fn(), info: vi.fn(), error: vi.fn() }) };
});

describe("a calendar's days off", () => {
  beforeEach(() => vi.clearAllMocks());

  it("read as dates in the reader's language for somebody who cannot change them", () => {
    const { container } = render(<CalendarDays id="c1" canAdminister={false} />);
    expect(screen.getByText("25 Dec 2026")).toBeTruthy();
    expect(screen.getAllByText("Half day")).toHaveLength(2);
    expect(container.querySelector("[data-holiday-file]")).toBeNull();
    expect(screen.queryByRole("button", { name: "Add a day" })).toBeNull();
  });

  it("are edited in place and saved as one list", async () => {
    render(<CalendarDays id="c1" canAdminister />);
    expect(screen.queryByRole("button", { name: "Save days" })).toBeNull();

    const names = screen.getAllByLabelText("Name");
    await userEvent.clear(names[1]!);
    await userEvent.type(names[1]!, "Weihnachten");
    await userEvent.click(screen.getByRole("button", { name: "Remove Heiligabend" }));
    await userEvent.click(screen.getByRole("button", { name: "Save days" }));

    expect(setDays).toHaveBeenCalledWith({ id: "c1", days: [{ day: "2026-12-25", name: "Weihnachten", halfDay: false }] }, expect.anything());
  });

  it("take a file's worth from an .ics", () => {
    const { container } = render(<CalendarDays id="c1" canAdminister />);
    const file = new File(["BEGIN:VCALENDAR"], "holidays.ics", { type: "text/calendar" });
    fireEvent.change(container.querySelector("[data-holiday-file]")!, { target: { files: [file] } });
    expect(importDays).toHaveBeenCalledWith({ id: "c1", file }, expect.anything());
  });
});
