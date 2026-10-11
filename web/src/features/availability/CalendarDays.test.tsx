import { beforeEach, describe, expect, it, vi } from "vitest";
import { act, fireEvent, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { ConfirmProvider } from "@/features/shell/ConfirmProvider";
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

// The page sits inside the shell's ConfirmProvider; so does the table here.
const renderDays = (canAdminister: boolean) =>
  render(
    <ConfirmProvider>
      <CalendarDays id="c1" canAdminister={canAdminister} />
    </ConfirmProvider>,
  );

describe("a calendar's days off", () => {
  beforeEach(() => vi.clearAllMocks());

  it("read as dates in the reader's language for somebody who cannot change them", () => {
    const { container } = renderDays(false);
    expect(screen.getByText("25 Dec 2026")).toBeTruthy();
    expect(screen.getAllByText("Half day")).toHaveLength(2);
    expect(container.querySelector("[data-holiday-file]")).toBeNull();
    expect(screen.queryByRole("button", { name: "Add a day" })).toBeNull();
  });

  it("are edited in place and saved as one list", async () => {
    renderDays(true);
    expect(screen.queryByRole("button", { name: "Save days" })).toBeNull();

    const names = screen.getAllByLabelText("Name");
    await userEvent.clear(names[1]!);
    await userEvent.type(names[1]!, "Weihnachten");
    await userEvent.click(screen.getByRole("button", { name: "Remove Heiligabend" }));
    await userEvent.click(screen.getByRole("button", { name: "Save days" }));

    expect(setDays).toHaveBeenCalledWith({ id: "c1", days: [{ day: "2026-12-25", name: "Weihnachten", halfDay: false }] }, expect.anything());
  });

  it("take a file's worth from an .ics", () => {
    const { container } = renderDays(true);
    const file = new File(["BEGIN:VCALENDAR"], "holidays.ics", { type: "text/calendar" });
    fireEvent.change(container.querySelector("[data-holiday-file]")!, { target: { files: [file] } });
    expect(importDays).toHaveBeenCalledWith({ id: "c1", file }, expect.anything());
  });

  describe("while the table holds unsaved edits", () => {
    const file = new File(["BEGIN:VCALENDAR"], "holidays.ics", { type: "text/calendar" });

    async function typeThreeDays() {
      const view = renderDays(true);
      for (const name of ["Brueckentag", "Betriebsausflug", "Inventur"]) {
        await userEvent.click(screen.getByRole("button", { name: "Add a day" }));
        await userEvent.type(screen.getAllByLabelText("Name").at(-1)!, name);
      }
      fireEvent.change(view.container.querySelector("[data-holiday-file]")!, { target: { files: [file] } });
      return view;
    }

    const typedNames = () => screen.getAllByLabelText<HTMLInputElement>("Name").map((input) => input.value);

    it("asks before an import discards them, and keeps them when the answer is no", async () => {
      await typeThreeDays();
      expect(await screen.findByRole("dialog", { name: "Import and discard 3 unsaved days?" })).toBeInTheDocument();
      expect(importDays).not.toHaveBeenCalled();

      await userEvent.click(screen.getByRole("button", { name: "Cancel" }));
      expect(importDays).not.toHaveBeenCalled();
      expect(typedNames()).toEqual(["Heiligabend", "Erster Weihnachtstag", "Brueckentag", "Betriebsausflug", "Inventur"]);
      expect(screen.getByRole("button", { name: "Save days" })).toBeInTheDocument();
    });

    it("imports once the answer is yes, and lets the typed days go only when the import arrives", async () => {
      await typeThreeDays();
      await userEvent.click(await screen.findByRole("button", { name: "Import and discard 3 unsaved days" }));
      expect(importDays).toHaveBeenCalledWith({ id: "c1", file }, expect.anything());
      expect(typedNames()).toContain("Inventur");

      const [, options] = importDays.mock.calls[0]!;
      act(() => options.onSuccess({ imported: 4 }));
      expect(typedNames()).toEqual(["Heiligabend", "Erster Weihnachtstag"]);
    });

    it("counts one edited day as one", async () => {
      const { container } = renderDays(true);
      await userEvent.type(screen.getAllByLabelText("Name")[1]!, " (Ost)");
      fireEvent.change(container.querySelector("[data-holiday-file]")!, { target: { files: [file] } });
      expect(await screen.findByRole("dialog", { name: "Import and discard 1 unsaved day?" })).toBeInTheDocument();
    });

    it("does not ask once the edits are undone by hand", async () => {
      const { container } = renderDays(true);
      await userEvent.click(screen.getByRole("button", { name: "Add a day" }));
      await userEvent.click(screen.getByRole("button", { name: "Remove this day" }));
      fireEvent.change(container.querySelector("[data-holiday-file]")!, { target: { files: [file] } });
      await vi.waitFor(() => expect(importDays).toHaveBeenCalledWith({ id: "c1", file }, expect.anything()));
      expect(screen.queryByRole("dialog")).toBeNull();
    });
  });
});
