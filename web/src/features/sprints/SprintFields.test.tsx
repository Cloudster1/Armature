import { describe, expect, it, vi } from "vitest";
import { render, screen, within } from "@testing-library/react";
import type { Issue } from "@/api/issues";
import type { Sprint } from "@/api/sprints";
import { SprintField } from "./SprintFields";

function sprint(id: string, name: string, teamId?: string): Sprint {
  return {
    id,
    projectId: "p1",
    projectKey: "CP",
    teamId,
    name,
    state: "future",
    position: 0,
    createdAt: "2026-03-01T00:00:00Z",
    updatedAt: "2026-03-01T00:00:00Z",
  };
}

const sprints = [
  sprint("a1", "Alpha 1", "alpha"),
  sprint("b1", "Beta 1", "beta"),
  sprint("p1", "Project 1"),
];

vi.mock("@/api/sprints", async () => ({
  ...(await vi.importActual<typeof import("@/api/sprints")>("@/api/sprints")),
  useSprints: () => ({ data: { sprints }, isLoading: false, error: null }),
  useSetIssueSprint: () => ({ mutate: vi.fn(), isPending: false, error: null }),
}));

function issue(over: Partial<Issue> = {}): Issue {
  return { id: "i1", key: "CP-4", projectKey: "CP", ...over } as Issue;
}

function offered() {
  render(<SprintField issue={issueUnderTest} />);
  const select = screen.getByLabelText("Sprint");
  return within(select)
    .getAllByRole("option")
    .map((option) => option.textContent);
}

let issueUnderTest = issue();

// A sprint plans its own team's work, and the API refuses any other; offering
// another team's sprint would only ever produce that refusal.
describe("SprintField", () => {
  it("offers only the sprints of the issue's own team", () => {
    issueUnderTest = issue({ teamId: "alpha", team: { id: "alpha", name: "Alpha" } });
    expect(offered()).toEqual(["Backlog", "Alpha 1"]);
  });

  it("offers the project's own sprints for work no team has taken on", () => {
    issueUnderTest = issue();
    expect(offered()).toEqual(["Backlog", "Project 1"]);
  });
});
