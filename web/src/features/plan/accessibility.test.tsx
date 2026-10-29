import { describe, expect, it, vi } from "vitest";
import type { ReactNode } from "react";
import type { Issue } from "@/api/issues";
import type { Plan, PlanItem } from "@/api/plan";
import { axeViolations } from "@/test/axe";
import { renderInShell } from "@/test/shell";

function issue(key: string, summary: string, start: string, due: string): Issue {
  return {
    id: key,
    key,
    summary,
    type: { id: "t", name: "Story", icon: "story", level: 1, isSubtask: false },
    projectId: "p",
    projectKey: "ALP",
    status: { id: "s", name: "To Do", category: "todo", position: 0 },
    priority: "medium",
    startDate: start,
    dueDate: due,
    createdAt: "2027-01-01T00:00:00Z",
    updatedAt: "2027-01-01T00:00:00Z",
    timeSpentMinutes: 0,
    labels: [],
    fixVersions: [],
    affectsVersions: [],
    components: [],
  } as Issue;
}

function item(key: string, summary: string, start: string, due: string): PlanItem {
  return {
    issue: issue(key, summary, start, due),
    depth: 0,
    start,
    due,
    derived: false,
    progress: { total: 1, done: 0, inProgress: 0, todo: 1 },
    children: [],
  } as unknown as PlanItem;
}

const plan = {
  projectKey: "ALP",
  items: [item("ALP-1", "Lay the foundation", "2027-03-01", "2027-03-05"), item("ALP-2", "Raise the walls", "2027-03-04", "2027-03-12")],
  dependencies: [{ linkId: "l1", blockerKey: "ALP-1", blockedKey: "ALP-2" }],
  sprints: [],
  milestones: [],
  load: { weeks: [], rows: [] },
  holidays: [],
  warnings: [{ issueKey: "ALP-2", kind: "blocked-too-early", message: "ALP-2 starts before ALP-1, which blocks it, is done." }],
  from: "2027-02-22",
  to: "2027-03-19",
  unscheduled: 0,
  unestimated: 0,
  linkTypes: [],
  matched: [],
} as unknown as Plan;

const idle = { mutate: vi.fn(), mutateAsync: vi.fn(), isPending: false };

vi.mock("@tanstack/react-router", () => ({
  Link: ({ children, to, params, ...rest }: { children: ReactNode; to: string; params?: Record<string, string> } & Record<string, unknown>) => (
    <a href={params?.issueKey ? to.replace("$issueKey", params.issueKey) : to} {...(rest as Record<string, string>)}>
      {children}
    </a>
  ),
}));
vi.mock("@/api/plan", async () => ({
  ...(await vi.importActual<typeof import("@/api/plan")>("@/api/plan")),
  usePlan: () => ({ data: plan, isLoading: false, error: null }),
  useSchedule: () => idle,
  useAddLink: () => idle,
  useRemoveLink: () => idle,
}));
vi.mock("@/api/issues", async () => ({
  ...(await vi.importActual<typeof import("@/api/issues")>("@/api/issues")),
  useCreateIssue: () => idle,
  useSetParent: () => idle,
}));

// The library has more; the plan's add row offers what ALP allows, standard level and up.
vi.mock("@/api/issueTypes", () => ({
  useProjectIssueTypes: (key: string) => ({
    data: {
      issueTypes:
        key === "ALP"
          ? [
              { id: "t-task", name: "Task", icon: "task", level: 0, isSubtask: false },
              { id: "t-sub", name: "Subtask", icon: "subtask", level: -1, isSubtask: true },
            ]
          : [],
    },
  }),
}));

const { Timeline } = await import("./Timeline");

describe("the planning view", () => {
  // The rules axe can judge without a stylesheet; contrast is the browser suite's.
  it("breaks no accessibility rule, with a dependency and a warning on it", async () => {
    renderInShell(
      <main>
        <h1>Plan</h1>
        <Timeline projectKey="ALP" />
      </main>,
    );
    for (const key of ["ALP-1", "ALP-2"]) expect(document.querySelector(`[data-plan-bar="${key}"]`), key).not.toBeNull();
    expect(document.querySelector("[data-plan-arrow]")).not.toBeNull();
    expect(await axeViolations()).toEqual([]);
  });

  it("offers only the project's standard types in the row that adds an issue", () => {
    renderInShell(<Timeline projectKey="ALP" />);
    const picks = [...document.querySelectorAll("[data-plan-add-type]")];
    expect(picks.length).toBeGreaterThan(0);
    for (const pick of picks) expect([...pick.querySelectorAll("option")].map((o) => o.textContent)).toEqual(["Task"]);
  });
});
