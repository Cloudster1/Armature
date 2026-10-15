import { beforeEach, describe, expect, it, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { ReactNode } from "react";
import type { HolidayCalendar } from "@/api/availability";
import { ToastProvider } from "@/components/ui";
import { ConfirmProvider } from "@/features/shell/ConfirmProvider";

const remove = vi.fn();
let removeError: Error | null = null;
const idle = { mutate: vi.fn(), isPending: false, error: null };
const calendar = (over: Partial<HolidayCalendar>): HolidayCalendar => ({
  id: "c1",
  name: "Holidays",
  default: true,
  dayCount: 0,
  peopleCount: 0,
  desks: [],
  createdAt: "2026-01-01T00:00:00Z",
  updatedAt: "2026-01-01T00:00:00Z",
  ...over,
});
let calendars: HolidayCalendar[] = [];

vi.mock("@tanstack/react-router", () => ({
  Link: ({ children, to }: { children: ReactNode; to: string }) => <a href={to}>{children}</a>,
  createRoute: (options: unknown) => options,
}));
vi.mock("./app", () => ({ appRoute: {} }));
vi.mock("@/api/access", () => ({ useAccess: () => ({ data: { canAdministerOrg: true } }) }));
vi.mock("@/api/availability", () => ({
  useHolidayCalendars: () => ({ data: { calendars }, isLoading: false, error: null }),
  useCreateHolidayCalendar: () => idle,
  useUpdateHolidayCalendar: () => idle,
  useDeleteHolidayCalendar: () => ({ mutate: remove, isPending: false, error: removeError }),
}));
vi.mock("@/features/availability/CalendarDays", () => ({ CalendarDays: () => null }));

const { holidaysRoute } = await import("./holidays");
const HolidaysPage = (holidaysRoute as unknown as { component: () => ReactNode }).component;

function renderPage() {
  render(
    <ToastProvider>
      <ConfirmProvider>
        <HolidaysPage />
      </ConfirmProvider>
    </ToastProvider>,
  );
}

async function askToDelete(name: string) {
  await userEvent.click(screen.getByRole("button", { name: `Actions for ${name}` }));
  await userEvent.click(screen.getByRole("menuitem", { name: "Delete" }));
  return screen.findByRole("dialog", { name: "Delete calendar?" });
}

beforeEach(() => {
  remove.mockClear();
  removeError = null;
  calendars = [calendar({}), calendar({ id: "c2", name: "Bank holidays", default: false, dayCount: 9, peopleCount: 2, desks: ["Front", "People"] })];
});

describe("deleting a holiday calendar", () => {
  it("names the desks that take their days off from it", async () => {
    renderPage();
    const dialog = await askToDelete("Bank holidays");
    expect(dialog).toHaveTextContent("The Front and People desks take their days off from it");
  });

  it("says nothing of desks when none takes its days", async () => {
    calendars[1] = { ...calendars[1]!, desks: [] };
    renderPage();
    const dialog = await askToDelete("Bank holidays");
    expect(dialog).not.toHaveTextContent("desk");
  });

  it("shows the server's refusal as its sentence", () => {
    removeError = new Error("Bank holidays is the holiday calendar of the Front and People desks; choose another one on their service desk pages first.");
    renderPage();
    expect(screen.getByText(removeError.message)).toBeInTheDocument();
  });
});
