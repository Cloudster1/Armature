import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { ReportSourceProvider, type ReportSource, type SprintHistoryReport, type Widget } from "@/api/reports";
import { WidgetBody } from "./WidgetBody";

const history: SprintHistoryReport = {
  sprints: [{ id: "s3", name: "Sprint 3", committed: 13, completed: 8, finished: 3, carried: 2 }],
};
const widget: Widget = { id: "w1", dashboardId: "d1", kind: "sprint_history", title: "Sprint history", width: 1, position: 0, config: {} };

let asked: string[] = [];

/** Answers every report the widget asks for with the one history, and the rest with nothing. */
function answer(url: string): Response {
  asked.push(url);
  const body = url.includes("burndown") ? { sprints: [] } : history;
  return new Response(JSON.stringify(body), { status: 200, headers: { "Content-Type": "application/json" } });
}

function draws(source: ReportSource) {
  render(
    <QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>
      <ReportSourceProvider value={source}>
        <WidgetBody projectKey="FLOW" widget={widget} />
      </ReportSourceProvider>
    </QueryClientProvider>,
  );
}

describe("the sprint history widget", () => {
  beforeEach(() => {
    asked = [];
    vi.stubGlobal("fetch", vi.fn(async (url: string) => answer(url)));
  });
  afterEach(() => vi.unstubAllGlobals());

  it("offers a finished sprint's curve to somebody signed in, and asks the project for it", async () => {
    draws({});
    await userEvent.click(await screen.findByRole("button", { name: "Curve" }));
    expect(await screen.findByText("Nothing was written down for that sprint.")).toBeInTheDocument();
    expect(asked).toContain("/api/v1/projects/FLOW/reports/burndown?sprint=s3");
  });

  it("offers no curve on a shared link, which answers only for its widgets", async () => {
    draws({ share: "tok", readOnly: true });
    expect(await screen.findByText("Sprint 3")).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Curve" })).not.toBeInTheDocument();
    expect(asked.length).toBeGreaterThan(0);
    for (const url of asked) expect(url).toMatch(/^\/api\/v1\/shared\/tok\/widgets\/w1/);
  });
});
