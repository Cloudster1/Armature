import { beforeEach, describe, expect, it, vi } from "vitest";
import { render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { AbsenceEditor } from "./Absences";
import type { Absence } from "@/api/availability";

const record = vi.fn();
const update = vi.fn();
const remove = vi.fn();
vi.mock("@/api/availability", async () => {
  const actual = await vi.importActual<typeof import("@/api/availability")>("@/api/availability");
  return {
    ...actual,
    useRecordAbsence: () => ({ mutate: record, isPending: false, error: null }),
    useUpdateAbsence: () => ({ mutate: update, isPending: false, error: null }),
    useRemoveAbsence: () => ({ mutate: remove, isPending: false, error: null }),
  };
});
vi.mock("@/lib/format", () => ({ useFormat: () => ({ locale: "en-GB" }) }));
vi.mock("@/features/shell/ConfirmProvider", () => ({ useConfirm: () => () => Promise.resolve(true) }));
vi.mock("@/components/ui", async () => {
  const actual = await vi.importActual<typeof import("@/components/ui")>("@/components/ui");
  return { ...actual, useToast: () => ({ success: vi.fn(), info: vi.fn(), error: vi.fn() }) };
});

const fortnight: Absence = { id: "a1", userId: "u1", userName: "Ada Away", startsOn: "2099-08-03", endsOn: "2099-08-14", halfDay: false };
const halfDay: Absence = { id: "a2", userId: "u1", userName: "Ada Away", startsOn: "2099-12-24", endsOn: "2099-12-24", halfDay: true };
const past: Absence = { id: "a3", userId: "u1", userName: "Ada Away", startsOn: "2000-01-03", endsOn: "2000-01-04", halfDay: false };

describe("the absences section", () => {
  beforeEach(() => vi.clearAllMocks());

  it("lists the days only, a half day and a past one marked as such", () => {
    render(<AbsenceEditor userId="u1" absences={[past, fortnight, halfDay]} empty="You are not away." />);
    expect(screen.getByText("3 Aug 2099 to 14 Aug 2099")).toBeTruthy();
    expect(screen.getByText("24 Dec 2099, half the day")).toBeTruthy();
    expect(screen.getByText("Past")).toBeTruthy();
    expect(screen.queryByLabelText(/reason/i)).toBeNull();
  });

  it("says so when nobody is away", () => {
    render(<AbsenceEditor userId="u1" absences={[]} empty="You are not away." />);
    expect(screen.getByText("You are not away.")).toBeTruthy();
  });

  it("records one day when the last day is left empty", async () => {
    render(<AbsenceEditor userId="u1" absences={[]} empty="You are not away." />);
    const button = screen.getByRole("button", { name: "Record absence" });
    expect(button).toBeDisabled();
    await userEvent.type(screen.getByLabelText("First day"), "2099-11-02");
    await userEvent.click(screen.getByLabelText("Half of the day"));
    await userEvent.click(button);
    expect(record).toHaveBeenCalledWith({ userId: "u1", startsOn: "2099-11-02", endsOn: "2099-11-02", halfDay: true }, expect.anything());
  });

  it("refuses a half day over two days, and a last day before the first, before sending", async () => {
    render(<AbsenceEditor userId="u1" absences={[]} empty="You are not away." />);
    await userEvent.type(screen.getByLabelText("First day"), "2099-11-03");
    await userEvent.type(screen.getByLabelText(/Last day/), "2099-11-04");
    await userEvent.click(screen.getByLabelText("Half of the day"));
    expect(screen.getByRole("alert").textContent).toMatch(/Only a single day can be a half day\./);
    expect(screen.getByRole("button", { name: "Record absence" })).toBeDisabled();

    await userEvent.click(screen.getByLabelText("Half of the day"));
    await userEvent.clear(screen.getByLabelText(/Last day/));
    await userEvent.type(screen.getByLabelText(/Last day/), "2099-11-01");
    expect(screen.getByRole("alert").textContent).toMatch(/before the first\. Swap them\./);
    expect(record).not.toHaveBeenCalled();
  });

  it("changes an absence in the same form, and takes one back", async () => {
    render(<AbsenceEditor userId="u1" absences={[fortnight]} empty="You are not away." />);
    const row = screen.getByText("3 Aug 2099 to 14 Aug 2099").closest("li") as HTMLElement;
    await userEvent.click(within(row).getByRole("button", { name: "Change" }));
    const last = screen.getByLabelText(/Last day/);
    await userEvent.clear(last);
    await userEvent.type(last, "2099-08-07");
    await userEvent.click(screen.getByRole("button", { name: "Save absence" }));
    expect(update).toHaveBeenCalledWith({ id: "a1", startsOn: "2099-08-03", endsOn: "2099-08-07", halfDay: false }, expect.anything());

    await userEvent.click(within(row).getByRole("button", { name: "Remove" }));
    expect(remove).toHaveBeenCalledWith("a1", expect.anything());
  });
});
