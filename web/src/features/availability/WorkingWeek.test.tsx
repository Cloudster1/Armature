import { beforeEach, describe, expect, it, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { WorkingWeekDialog, WorkingWeekTable } from "./WorkingWeek";

const save = vi.fn();
const standard = { mon: 480, tue: 480, wed: 480, thu: 480, fri: 480, sat: 0, sun: 0 };
const week = { userId: "u1", calendarId: null, calendar: { id: "c1", name: "Holidays" }, minutes: standard, saved: false };
vi.mock("@/api/availability", async () => {
  const actual = await vi.importActual<typeof import("@/api/availability")>("@/api/availability");
  return {
    ...actual,
    useWorkingWeek: () => ({ data: { week }, error: null }),
    useSetWorkingWeek: () => ({ mutate: save, isPending: false, error: null }),
    useHolidayCalendars: () => ({
      data: {
        calendars: [
          { id: "c1", name: "Holidays", default: true },
          { id: "c2", name: "Portugal", default: false },
        ],
      },
    }),
  };
});
vi.mock("@/lib/format", () => ({ useFormat: () => ({ locale: "en-GB" }) }));
vi.mock("@/components/ui", async () => {
  const actual = await vi.importActual<typeof import("@/components/ui")>("@/components/ui");
  return { ...actual, useToast: () => ({ success: vi.fn(), info: vi.fn(), error: vi.fn() }) };
});

describe("a working week", () => {
  beforeEach(() => vi.clearAllMocks());

  it("reads as hours a day and a total, with the calendar it keeps", () => {
    render(<WorkingWeekTable week={week} />);
    expect(screen.getByText("40 h")).toBeTruthy();
    expect(screen.getByText("Holidays")).toBeTruthy();
    expect(screen.getByText(/the standard one/)).toBeTruthy();
  });

  it("is set in hours and saved in minutes, with the calendar chosen", async () => {
    render(<WorkingWeekDialog userId="u1" name="Grace Hopper" onClose={() => {}} />);
    const friday = screen.getByLabelText("Hours on Friday");
    await userEvent.clear(friday);
    await userEvent.type(friday, "4.5");
    await userEvent.selectOptions(screen.getByLabelText("Holiday calendar"), "c2");
    expect(screen.getByText(/36.5 hours a week/)).toBeTruthy();
    await userEvent.click(screen.getByRole("button", { name: "Save week" }));
    expect(save).toHaveBeenCalledWith({ userId: "u1", calendarId: "c2", minutes: { ...standard, fri: 270 } }, expect.anything());
  });

  it("refuses a day longer than a day, and says which", async () => {
    render(<WorkingWeekDialog userId="u1" name="Grace Hopper" onClose={() => {}} />);
    const monday = screen.getByLabelText("Hours on Monday");
    await userEvent.clear(monday);
    await userEvent.type(monday, "25");
    expect(screen.getByText(/Fix Monday\./)).toBeTruthy();
    expect(screen.getByRole("button", { name: "Save week" })).toBeDisabled();
    expect(save).not.toHaveBeenCalled();
  });
});
