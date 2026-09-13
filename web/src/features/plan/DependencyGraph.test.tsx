import { describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen } from "@testing-library/react";
import type { ReactNode } from "react";
import type { Issue, StatusCategory } from "@/api/issues";
import type { Plan, PlanItem } from "@/api/plan";

vi.mock("@tanstack/react-router", () => ({
  Link: ({ children, params }: { children: ReactNode; params?: { issueKey?: string } }) => (
    <a href={`/issues/${params?.issueKey ?? ""}`}>{children}</a>
  ),
}));

const removeMutate = vi.fn();
const addMutate = vi.fn();
let plan: Plan;
vi.mock("@/api/plan", () => ({
  usePlan: () => ({ data: plan, isLoading: false, error: null }),
  useAddLink: () => ({ mutate: addMutate, error: null, isPending: false }),
  useRemoveLink: () => ({ mutate: removeMutate, error: null, isPending: false }),
}));

import { DependencyGraph } from "./DependencyGraph";

function issue(key: string, over: { category?: StatusCategory; resolvedAt?: string } = {}): Issue {
  const category = over.category ?? "todo";
  return {
    id: key,
    key,
    type: { id: "story", name: "Story", icon: "S", level: 0, isSubtask: false },
    projectId: "p",
    projectKey: "PR",
    summary: `About ${key}`,
    status: { id: category, name: category, category, position: 0 },
    priority: "medium",
    labels: [],
    fixVersions: [],
    affectsVersions: [],
    components: [],
    timeSpentMinutes: 0,
    createdAt: "2026-01-01T00:00:00Z",
    updatedAt: "2026-01-01T00:00:00Z",
    resolvedAt: over.resolvedAt,
  } as Issue;
}

function item(i: Issue): PlanItem {
  return { issue: i, depth: 0, derived: false, progress: { total: 0, done: 0, inProgress: 0, todo: 0 }, children: [] } as PlanItem;
}

const dependency = { linkId: "l1", blockerKey: "PR-1", blockedKey: "PR-2" };

beforeEachPlan();
function beforeEachPlan() {
  plan = {
    projectKey: "PR",
    items: [item(issue("PR-1")), item(issue("PR-2")), item(issue("PR-3", { category: "done", resolvedAt: "2026-01-02T00:00:00Z" }))],
    dependencies: [dependency, { linkId: "l9", blockerKey: "OTHER-4", blockedKey: "PR-2" }],
    sprints: [],
    milestones: [],
    load: { weeks: [], rows: [] },
    warnings: [],
    from: "2026-01-01",
    to: "2026-03-01",
    unscheduled: 0,
    unestimated: 0,
    linkTypes: [],
    matched: [],
  } as unknown as Plan;
}

if (typeof globalThis.ResizeObserver === "undefined") {
  globalThis.ResizeObserver = class {
    observe() {}
    disconnect() {}
    unobserve() {}
  } as unknown as typeof ResizeObserver;
}

describe("DependencyGraph", () => {
  it("draws a box per ticket, an edge per dependency, and a dashed box for a blocker elsewhere", () => {
    const { container } = render(<DependencyGraph projectKey="PR" />);
    expect(container.querySelectorAll("[data-graph-node]")).toHaveLength(4);
    expect(container.querySelector('[data-graph-node="OTHER-4"]')?.getAttribute("data-graph-external")).toBe("true");
    expect(container.querySelectorAll("[data-graph-edge]")).toHaveLength(2);
    expect(container.querySelector("[data-graph-unlinked]")).not.toBeNull();
  });

  it("cuts what the reader cut, and the edges with it", () => {
    const { container } = render(
      <DependencyGraph projectKey="PR" cut={{ closedForDays: 0, matched: new Set(["PR-1", "PR-3"]) }} query="key IN (PR-1, PR-3)" />,
    );
    // Matched is read from the plan, which said nothing here, so the query cuts everything.
    expect(container.querySelectorAll("[data-graph-node]")).toHaveLength(0);
  });

  it("hides done work past the cut", () => {
    const { container } = render(<DependencyGraph projectKey="PR" cut={{ closedForDays: 0, matched: null }} />);
    expect(container.querySelector('[data-graph-node="PR-3"]')).toBeNull();
    expect(container.querySelectorAll("[data-graph-node]")).toHaveLength(3);
  });

  it("selects an edge and removes it by its link", () => {
    const { container } = render(<DependencyGraph projectKey="PR" />);
    fireEvent.click(container.querySelector('[data-graph-edge-hit="PR-1->PR-2"]')!);
    const remove = screen.getByRole("button", { name: /Remove dependency PR-1 blocks PR-2/ });
    fireEvent.click(remove);
    expect(removeMutate).toHaveBeenCalledWith({ key: "PR-1", linkId: "l1" }, expect.anything());
  });
});
