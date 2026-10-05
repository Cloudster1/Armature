import { describe, expect, it, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { PlanningMethod, ResourceGrouping } from "@/api/projects";
import { ResourceToolbar } from "./ResourceToolbar";

function toolbar(method: PlanningMethod, grouping: ResourceGrouping, canChange: boolean, onGrouping = vi.fn()) {
  render(
    <ResourceToolbar method={method} grouping={grouping} canChange={canChange} saving={false} onGrouping={onGrouping} onPrevious={vi.fn()} onToday={vi.fn()} onNext={vi.fn()} />,
  );
  return onGrouping;
}

describe("the resource toolbar", () => {
  it("lets an administrator of a kanban project switch between teams and people", async () => {
    const onGrouping = toolbar("kanban", "team", true);
    expect(screen.getByRole("button", { name: "Teams" })).toHaveAttribute("aria-pressed", "true");
    await userEvent.click(screen.getByRole("button", { name: "People" }));
    expect(onGrouping).toHaveBeenCalledWith("person");
  });

  it("shows anybody else the project's choice without a switch", () => {
    toolbar("kanban", "person", false);
    expect(screen.queryByRole("button", { name: "Teams" })).toBeNull();
    expect(screen.getByText("Planned by person")).toBeTruthy();
  });

  it("offers a scrum project no switch, whoever looks", () => {
    toolbar("scrum", "team", true);
    expect(screen.queryByRole("button", { name: "People" })).toBeNull();
    expect(screen.getByText("Planned by team")).toHaveAttribute("title", "A scrum project plans by team.");
  });
});
