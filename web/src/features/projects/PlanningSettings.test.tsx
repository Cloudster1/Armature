import { beforeEach, describe, expect, it, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { Project } from "@/api/projects";

const mutate = vi.fn();
vi.mock("@/api/projects", async () => {
  const actual = await vi.importActual<typeof import("@/api/projects")>("@/api/projects");
  return { ...actual, useUpdateProject: () => ({ mutate, isPending: false, error: null }) };
});
vi.mock("@/components/ui", async () => {
  const actual = await vi.importActual<typeof import("@/components/ui")>("@/components/ui");
  return { ...actual, useToast: () => ({ success: vi.fn(), info: vi.fn(), error: vi.fn() }) };
});

const { PlanningSettings, SCRUM_BY_TEAM } = await import("./PlanningSettings");

function settings(planningMethod: Project["planningMethod"], resourceGrouping: Project["resourceGrouping"]) {
  const project = { key: "FLOW", planningMethod, resourceGrouping } as Project;
  render(
    <QueryClientProvider client={new QueryClient()}>
      <PlanningSettings project={project} />
    </QueryClientProvider>,
  );
}

describe("the planning settings", () => {
  beforeEach(() => vi.clearAllMocks());

  it("let a kanban project plan by person", async () => {
    settings("kanban", "team");
    await userEvent.click(screen.getByRole("radio", { name: /^By person/ }));
    expect(mutate).toHaveBeenCalledWith({ key: "FLOW", resourceGrouping: "person" }, expect.anything());
  });

  it("show a scrum project the people option disabled, with why", async () => {
    settings("scrum", "team");
    const person = screen.getByRole("radio", { name: /^By person/ });
    expect(person).toBeDisabled();
    expect(person.textContent).toContain(SCRUM_BY_TEAM);
    await userEvent.click(screen.getByRole("radio", { name: /^Kanban/ }));
    expect(mutate).toHaveBeenCalledWith({ key: "FLOW", planningMethod: "kanban" }, expect.anything());
  });
});
