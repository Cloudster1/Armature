import { describe, expect, it, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { ProjectTemplate } from "@/api/projects";

const state = { templates: [] as ProjectTemplate[] };

vi.mock("@/api/projects", () => ({
  useProjectTemplates: () => ({ data: { templates: state.templates }, isLoading: false }),
}));

const { TemplateChooser, templateShape } = await import("./TemplateChooser");

const everything: ProjectTemplate["features"] = ["board", "sprints", "plan", "calendar", "milestones", "releases", "components", "hierarchy", "dashboard", "teams", "repositories", "automation", "import"];

const kanban: ProjectTemplate = {
  key: "kanban",
  name: "Kanban",
  description: "Work flows continuously.",
  kind: "software",
  boardType: "kanban",
  features: everything,
};
const scrum: ProjectTemplate = {
  key: "scrum",
  name: "Scrum",
  description: "Work is planned into sprints.",
  kind: "software",
  boardType: "scrum",
  features: everything,
};
const tasks: ProjectTemplate = {
  key: "task-tracking",
  name: "Task tracking",
  description: "For work that is not software.",
  kind: "business",
  boardType: "kanban",
  workflowName: "Simple task workflow",
  features: ["board", "plan", "calendar", "milestones", "dashboard", "hierarchy", "teams", "automation", "import"],
};

describe("TemplateChooser", () => {
  // The server decides the default by putting it first; the chooser must not
  // have an opinion of its own that could disagree.
  it("selects the first template until the user picks", () => {
    state.templates = [kanban, scrum, tasks];
    render(<TemplateChooser value="" onChange={() => {}} />);

    expect(screen.getByRole("radio", { name: /^Kanban/ })).toHaveAttribute("aria-checked", "true");
    expect(screen.getByRole("radio", { name: /^Scrum/ })).toHaveAttribute("aria-checked", "false");
  });

  it("hands back the key of what was picked", async () => {
    state.templates = [kanban, scrum, tasks];
    const onChange = vi.fn();
    render(<TemplateChooser value="" onChange={onChange} />);

    await userEvent.click(screen.getByRole("radio", { name: /^Task tracking/ }));
    expect(onChange).toHaveBeenCalledWith("task-tracking");
  });

  it("says what each template brings", () => {
    expect(templateShape(scrum)).toBe("Scrum board · the organization's workflows");
    expect(templateShape(tasks)).toBe("Kanban board · Simple task workflow · no sprints, releases, components, repositories");
  });
});
