import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import type { ReactNode } from "react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { meQueryKey } from "@/api/auth";
import { calendarQueryKey } from "@/api/calendar";
import { issuesQueryKey, type HistoryEntry } from "@/api/issues";
import type { Milestone } from "@/api/milestones";

// The router only draws links here; nothing navigates.
vi.mock("@tanstack/react-router", () => ({
  Link: ({ children }: { children: ReactNode }) => <a>{children}</a>,
}));

const { MilestoneProgress } = await import("@/features/plan/MilestoneMarkers");
const { MonthGrid } = await import("@/features/calendar/MonthGrid");
const { History } = await import("@/features/issues/IssueHistory");

const NOW = new Date("2026-03-10T12:00:00Z");

/** A session whose profile says German, in Berlin, whatever the browser says. */
function signedInAsGerman(): QueryClient {
  const client = new QueryClient({ defaultOptions: { queries: { staleTime: Infinity, retry: false } } });
  client.setQueryData(meQueryKey, {
    principal: {
      user: { id: "u1", email: "greta@example.com", name: "Greta", timezone: "Europe/Berlin", locale: "de-DE", isActive: true },
    },
  });
  return client;
}

function show(client: QueryClient, node: ReactNode) {
  return render(<QueryClientProvider client={client}>{node}</QueryClientProvider>);
}

const milestone: Milestone = {
  id: "m1",
  projectId: "p1",
  projectKey: "ALP",
  name: "Release 1",
  dueOn: "2026-09-24T00:00:00Z",
  position: 0,
  progress: { issues: 4, done: 1, inProgress: 1, todo: 2, percent: 25 },
  createdAt: "2026-01-01T00:00:00Z",
  updatedAt: "2026-01-01T00:00:00Z",
};

describe("a profile set to German", () => {
  beforeEach(() => vi.useFakeTimers({ toFake: ["Date"], now: NOW }));
  afterEach(() => vi.useRealTimers());

  it("dates a milestone in German", () => {
    show(signedInAsGerman(), <MilestoneProgress projectKey="ALP" milestones={[milestone]} />);
    expect(screen.getByText("due 24.09.2026")).toBeTruthy();
  });

  it("names the calendar's month in German", () => {
    const client = signedInAsGerman();
    client.setQueryData([...calendarQueryKey, "ALP", "2026-03"], { month: { year: 2026, month: 3, items: [], truncated: false } });
    const { container } = show(client, <MonthGrid projectKey="ALP" canWrite={false} />);
    expect(container.querySelector("[data-calendar-title]")?.textContent).toBe("März 2026");
  });

  it("writes a change older than a month as a German date", () => {
    const client = signedInAsGerman();
    const entry: HistoryEntry = {
      id: "h1",
      issueId: "i1",
      actor: { id: "u1", name: "Greta" },
      changes: [{ field: "status", from: "To do", to: "Done" }],
      createdAt: "2026-01-05T12:00:00Z",
    };
    client.setQueryData([...issuesQueryKey, "history", "ALP-1"], { history: [entry] });
    const { container } = show(client, <History issueKey="ALP-1" />);
    expect(container.querySelector("[data-activity=history]")?.textContent).toContain("05.01.2026");
  });
});
