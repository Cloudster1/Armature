import { describe, expect, it, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { ReactNode } from "react";
import type { ResourcePlan } from "@/api/resources";

vi.mock("@tanstack/react-router", () => ({
  Link: ({ children, to, params, ...rest }: { children: ReactNode; to: string; params?: Record<string, string> } & Record<string, unknown>) => (
    <a href={to.replace("$issueKey", params?.issueKey ?? "")} {...(rest as Record<string, string>)}>
      {children}
    </a>
  ),
}));

const { ResourceGrid } = await import("./ResourceGrid");
const { UnplacedWork } = await import("./UnplacedWork");

const plan: ResourcePlan = {
  projectKey: "FLOW",
  method: "kanban",
  grouping: "person",
  from: "2031-03-03T00:00:00Z",
  to: "2031-03-16T00:00:00Z",
  weeks: ["2031-03-03T00:00:00Z", "2031-03-10T00:00:00Z"],
  rows: [
    {
      kind: "person",
      id: "ada",
      name: "Ada",
      weeks: [
        {
          start: "2031-03-03T00:00:00Z",
          loadHours: 16,
          capacityHours: 24,
          nominalHours: 40,
          daysAway: 1,
          holidays: 1,
          issues: [{ key: "FLOW-1", summary: "Build it", hours: 16 }],
        },
        { start: "2031-03-10T00:00:00Z", loadHours: 50, capacityHours: 40, nominalHours: 40, issues: [{ key: "FLOW-2", summary: "Long day", hours: 50 }] },
      ],
    },
    { kind: "unassigned", name: "Unassigned", weeks: [{ start: "2031-03-03T00:00:00Z", loadHours: 0, issues: [] }, { start: "2031-03-10T00:00:00Z", loadHours: 0, issues: [] }] },
  ],
  unscheduled: [{ key: "FLOW-4", summary: "Due but not started", hours: 3 }],
  unestimated: [{ key: "FLOW-3", summary: "Nobody sized this" }],
  warnings: [],
};

describe("the resource grid", () => {
  it("draws a row per person and a cell per week, hatching the days off", () => {
    render(<ResourceGrid plan={plan} />);
    expect(screen.getAllByRole("row")).toHaveLength(3);
    const first = document.querySelector('[data-resource-row="Ada"] [data-resource-week="2031-03-03"]')!;
    expect(first.getAttribute("title")).toMatch(/16 of 24 h; 1 day away; 1 holiday$/);
    expect(first.querySelector("[data-days-off]")).not.toBeNull();
    const second = document.querySelector('[data-resource-row="Ada"] [data-resource-week="2031-03-10"]')!;
    expect(second.getAttribute("data-over")).toBe("true");
    expect(second.querySelector("[data-days-off]")).toBeNull();
  });

  it("lists a week's issues with their hours when its cell is pressed", async () => {
    render(<ResourceGrid plan={plan} />);
    await userEvent.click(document.querySelector('[data-resource-row="Ada"] [data-resource-week="2031-03-03"]')!);
    const panel = screen.getByRole("dialog");
    expect(panel.textContent).toContain("16 of 24 h; 1 day away; 1 holiday");
    const issue = panel.querySelector('[data-resource-issue="FLOW-1"]')!;
    expect(issue.textContent).toContain("16 h");
    expect(issue.querySelector("a")?.getAttribute("href")).toBe("/issues/FLOW-1");
    await userEvent.click(document.querySelector('[data-resource-row="Unassigned"] [data-resource-week="2031-03-03"]')!);
    expect(screen.getByRole("dialog").textContent).toContain("Nothing is scheduled this week.");
  });

  it("lists the work it cannot place below", () => {
    render(<UnplacedWork unscheduled={plan.unscheduled} unestimated={plan.unestimated} />);
    expect(document.querySelector('[data-resource-list="unscheduled"] [data-resource-issue="FLOW-4"]')?.textContent).toContain("3 h");
    expect(document.querySelector('[data-resource-list="unestimated"] [data-resource-issue="FLOW-3"]')?.textContent).not.toContain(" h");
  });
});
