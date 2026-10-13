import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, render } from "@testing-library/react";
import type { MouseEvent, ReactNode } from "react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { calendarQueryKey, type CalendarItem } from "@/api/calendar";

// An entry on the calendar opens what it stands for; a drag only moves it.

/** Where a link would have taken the page, had its click not been taken over. */
const navigated = vi.fn();

// The router's link, as far as the calendar uses it: an address, and a click
// that navigates unless a handler of the page prevented it.
vi.mock("@tanstack/react-router", () => ({
  Link: ({ to, params, onClick, children, ...rest }: { to: string; params?: Record<string, string>; onClick?: (e: MouseEvent) => void; children: ReactNode }) => {
    const href = Object.entries(params ?? {}).reduce((path, [name, value]) => path.replace(`$${name}`, value), to);
    return (
      <a
        {...rest}
        href={href}
        onClick={(e) => {
          onClick?.(e);
          if (!e.defaultPrevented) navigated(href);
          e.preventDefault();
        }}
      >
        {children}
      </a>
    );
  },
  useLocation: () => ({ pathname: "/projects/CP/calendar" }),
}));

const scheduled = vi.fn();
vi.mock("@/api/plan", async () => ({
  ...(await vi.importActual<typeof import("@/api/plan")>("@/api/plan")),
  useSchedule: () => ({ mutate: scheduled, error: null }),
}));

const { MonthGrid } = await import("./MonthGrid");
const { IssueDrawerProvider, useIssueDrawer } = await import("@/features/issues/IssueDrawer");

const NOW = new Date("2026-03-10T12:00:00Z");

const items: CalendarItem[] = [
  { kind: "sprint", id: "s1", title: "Sprint 1", from: "2026-03-02", to: "2026-03-06" },
  { kind: "issue", id: "i1", key: "CP-1", title: "Paint the fence", from: "2026-03-09", to: "2026-03-09", category: "todo" },
  { kind: "issue", id: "i2", key: "CP-2", title: "Mow the lawn", from: "2026-03-12", to: "2026-03-12", category: "todo" },
  { kind: "absence", id: "a1", title: "Greta", from: "2026-03-17", to: "2026-03-17" },
  { kind: "milestone", id: "m1", title: "Beta", from: "2026-03-20", to: "2026-03-20" },
  { kind: "version", id: "v1", title: "1.0", from: "2026-03-25", to: "2026-03-25" },
];

/** What the drawer shows and which issues it can step through. */
function Drawer() {
  const drawer = useIssueDrawer();
  return <output data-current={drawer.current ?? ""} data-keys={drawer.keys.join(",")} />;
}

function show(canWrite = true) {
  const client = new QueryClient({ defaultOptions: { queries: { staleTime: Infinity, retry: false } } });
  client.setQueryData([...calendarQueryKey, "CP", "2026-03"], { month: { year: 2026, month: 3, items, truncated: false } });
  return render(
    <QueryClientProvider client={client}>
      <IssueDrawerProvider>
        <MonthGrid projectKey="CP" canWrite={canWrite} />
        <Drawer />
      </IssueDrawerProvider>
    </QueryClientProvider>,
  );
}

function opened(): string | null | undefined {
  return document.querySelector("output")?.getAttribute("data-current");
}

/** The day whose cell lies under the pointer; jsdom lays nothing out, so the test says. */
let pointerOver = "";

function entry(key: string): HTMLElement {
  return document.querySelector<HTMLElement>(`[data-calendar-item="${key}"] a`)!;
}

// A click from a pointer counts its clicks in detail; one from the keyboard says 0.
describe("an entry on the calendar", () => {
  beforeEach(() => {
    vi.useFakeTimers({ toFake: ["Date"], now: NOW });
    navigated.mockReset();
    scheduled.mockReset();
    // jsdom has no pointer events; a mouse event carries the coordinates.
    vi.stubGlobal("PointerEvent", class extends MouseEvent {});
    vi.spyOn(document, "elementFromPoint").mockImplementation(() => document.querySelector(`[data-day="${pointerOver}"]`));
  });
  afterEach(() => {
    vi.useRealTimers();
    vi.unstubAllGlobals();
    vi.restoreAllMocks();
  });

  it("opens its issue beside the calendar on a click", () => {
    show();
    pointerOver = "2026-03-09";
    const bar = entry("CP-1");
    fireEvent.pointerDown(bar, { clientX: 10, clientY: 10 });
    fireEvent.pointerUp(bar, { clientX: 11, clientY: 10 });
    fireEvent.click(bar, { clientX: 11, clientY: 10, detail: 1 });
    expect(opened()).toBe("CP-1");
    expect(navigated).not.toHaveBeenCalled();
    expect(scheduled).not.toHaveBeenCalled();
  });

  it("opens its issue for a reader who cannot drag it", () => {
    show(false);
    fireEvent.click(entry("CP-2"), { detail: 1 });
    expect(opened()).toBe("CP-2");
  });

  it("lets the drawer step through the month's issues in order", () => {
    show();
    expect(document.querySelector("output")?.getAttribute("data-keys")).toBe("CP-1,CP-2");
  });

  it("opens its issue from the keyboard", () => {
    show();
    fireEvent.click(entry("CP-1"), { detail: 0 });
    expect(opened()).toBe("CP-1");
  });

  it("leaves a click with a modifier to the link", () => {
    show();
    fireEvent.click(entry("CP-1"), { ctrlKey: true, detail: 1 });
    expect(opened()).toBe("");
    expect(navigated).toHaveBeenCalledWith("/issues/CP-1");
  });

  it("does not open its issue when it was dragged to another day", () => {
    show();
    const bar = entry("CP-1");
    pointerOver = "2026-03-09";
    fireEvent.pointerDown(bar, { clientX: 10, clientY: 10 });
    pointerOver = "2026-03-11";
    fireEvent.pointerMove(bar, { clientX: 210, clientY: 10 });
    fireEvent.pointerUp(bar, { clientX: 210, clientY: 10 });
    fireEvent.click(bar, { clientX: 210, clientY: 10, detail: 1 });
    expect(scheduled).toHaveBeenCalledWith({ key: "CP-1", startDate: "2026-03-11", dueDate: "2026-03-11" });
    expect(opened()).toBe("");
    expect(navigated).not.toHaveBeenCalled();
  });

  it("does not open its issue when the pointer wandered off and came back", () => {
    show();
    const bar = entry("CP-1");
    pointerOver = "2026-03-09";
    fireEvent.pointerDown(bar, { clientX: 10, clientY: 10 });
    fireEvent.pointerMove(bar, { clientX: 60, clientY: 10 });
    fireEvent.pointerUp(bar, { clientX: 12, clientY: 10 });
    fireEvent.click(bar, { clientX: 12, clientY: 10, detail: 1 });
    expect(scheduled).not.toHaveBeenCalled();
    expect(opened()).toBe("");
  });

  it("opens on the next click after a drag", () => {
    show();
    const bar = entry("CP-1");
    pointerOver = "2026-03-09";
    fireEvent.pointerDown(bar, { clientX: 10, clientY: 10 });
    fireEvent.pointerMove(bar, { clientX: 60, clientY: 10 });
    fireEvent.pointerUp(bar, { clientX: 10, clientY: 10 });
    fireEvent.click(bar, { detail: 1 });
    fireEvent.pointerDown(bar, { clientX: 10, clientY: 10 });
    fireEvent.pointerUp(bar, { clientX: 10, clientY: 10 });
    fireEvent.click(bar, { detail: 1 });
    expect(opened()).toBe("CP-1");
  });

  it("links a sprint to its board, a milestone to the milestones and a version to the releases", () => {
    show();
    expect(entry("s1").getAttribute("href")).toBe("/projects/CP/sprints/s1/board");
    expect(entry("m1").getAttribute("href")).toBe("/projects/CP/milestones");
    expect(entry("v1").getAttribute("href")).toBe("/projects/CP/releases");
  });

  it("leaves an absence as plain text", () => {
    show();
    const absence = document.querySelector('[data-calendar-item="a1"]')!;
    expect(absence.querySelector("a")).toBeNull();
    expect(absence.className).not.toContain("cursor");
  });
});
