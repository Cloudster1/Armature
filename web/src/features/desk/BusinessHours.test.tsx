import { beforeEach, describe, expect, it, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { BusinessCalendar } from "@/api/desk";
import { ToastProvider } from "@/components/ui";
import { BusinessHours } from "./DeskExtras";

const save = vi.fn();
let calendar: BusinessCalendar;

vi.mock("@/api/desk", async (actual) => ({
  ...(await actual<typeof import("@/api/desk")>()),
  useBusinessCalendar: () => ({ data: { calendar }, error: null }),
  useSaveBusinessCalendar: () => ({ mutate: save, isPending: false, error: null }),
  usePolicies: () => ({ data: { policies: [] } }),
  useUpdatePolicy: () => ({ mutate: vi.fn() }),
}));
vi.mock("@/api/availability", () => ({
  useHolidayCalendars: () => ({
    data: {
      calendars: [
        { id: "c1", name: "Holidays", default: true, dayCount: 0, peopleCount: 0, desks: [], createdAt: "", updatedAt: "" },
        { id: "c2", name: "Bank holidays", default: false, dayCount: 9, peopleCount: 0, desks: [], createdAt: "", updatedAt: "" },
      ],
    },
  }),
}));

const renderHours = () =>
  render(
    <ToastProvider>
      <BusinessHours projectKey="CP" />
    </ToastProvider>,
  );

beforeEach(() => {
  save.mockClear();
  calendar = {
    timezone: "UTC",
    hours: { mon: [{ from: "09:00", to: "17:00" }] },
    holidays: ["2026-12-31"],
    holidayCalendarId: null,
    holidayCalendarName: null,
  };
});

describe("a desk's holidays", () => {
  it("are taken from the holiday calendar chosen, beside its own days", async () => {
    renderHours();
    await userEvent.selectOptions(screen.getByLabelText("Holidays from"), "Bank holidays");
    await userEvent.click(screen.getByRole("button", { name: "Save business hours" }));
    expect(save).toHaveBeenCalledWith(expect.objectContaining({ holidayCalendarId: "c2", holidays: ["2026-12-31"] }), expect.anything());
  });

  it("show the calendar saved, and None takes it away", async () => {
    calendar = { ...calendar, holidayCalendarId: "c2", holidayCalendarName: "Bank holidays" };
    renderHours();
    const select = screen.getByLabelText<HTMLSelectElement>("Holidays from");
    expect(select.value).toBe("c2");
    await userEvent.selectOptions(select, "None");
    await userEvent.click(screen.getByRole("button", { name: "Save business hours" }));
    expect(save).toHaveBeenCalledWith(expect.objectContaining({ holidayCalendarId: null }), expect.anything());
  });

  it("keep the desk's own days as extra days off", () => {
    renderHours();
    expect(screen.getByLabelText<HTMLInputElement>("Extra days off").value).toBe("2026-12-31");
  });
});
