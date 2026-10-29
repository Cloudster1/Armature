import { describe, expect, it, vi } from "vitest";
import { render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { ProjectTemplate } from "@/api/projects";

const state = { templates: [] as ProjectTemplate[] };

vi.mock("@/api/projects", () => ({
  useProjectTemplates: () => ({ data: { templates: state.templates }, isLoading: false }),
}));

const { TemplateChooser, templateShape } = await import("./TemplateChooser");

const everything: ProjectTemplate["features"] = [
  "board",
  "sprints",
  "plan",
  "calendar",
  "resources",
  "milestones",
  "releases",
  "components",
  "hierarchy",
  "dashboard",
  "teams",
  "repositories",
  "automation",
  "import",
];

const kanban: ProjectTemplate = {
  key: "kanban",
  name: "Kanban",
  description: "Work flows continuously.",
  kind: "software",
  boardType: "kanban",
  planningMethod: "kanban",
  features: everything,
};
const scrum: ProjectTemplate = {
  key: "scrum",
  name: "Scrum",
  description: "Work is planned into sprints.",
  kind: "software",
  boardType: "scrum",
  planningMethod: "scrum",
  features: everything,
};
const tasks: ProjectTemplate = {
  key: "task-tracking",
  name: "Task tracking",
  description: "For work that is not software.",
  kind: "business",
  boardType: "kanban",
  planningMethod: "kanban",
  workflowName: "Simple task workflow",
  features: ["board", "plan", "calendar", "milestones", "dashboard", "hierarchy", "teams", "automation", "import"],
};

const stored: ProjectTemplate = {
  key: "5b1c0a52-0000-4000-8000-000000000001",
  name: "Client work",
  description: "For agency projects.",
  kind: "business",
  boardType: "kanban",
  planningMethod: "kanban",
  features: ["board", "plan"],
  builtin: false,
  archived: false,
  scheme: { id: "5b1c0a52-0000-4000-8000-000000000002", name: "Agency flow" },
  issueTypes: [
    { id: "t1", name: "Task", icon: "task", level: 0, retired: false },
    { id: "t2", name: "Bug", icon: "bug", level: 0, retired: false },
  ],
};

describe("TemplateChooser", () => {
  it("groups the stored templates after the built-ins and names their scheme and types", () => {
    state.templates = [kanban, scrum, stored];
    render(<TemplateChooser value="" onChange={() => {}} />);

    const groups = screen.getAllByRole("radiogroup");
    expect(groups.map((group) => group.getAttribute("aria-label"))).toEqual(["Template", "Made here"]);
    expect(within(groups[0]!).getAllByRole("radio")).toHaveLength(2);
    const card = within(groups[1]!).getByRole("radio", { name: /^Client work/ });
    expect(card).toHaveTextContent("Kanban board · Agency flow · Task, Bug");
  });

  it("hands back a stored template's id", async () => {
    state.templates = [kanban, stored];
    const onChange = vi.fn();
    render(<TemplateChooser value="" onChange={onChange} />);

    await userEvent.click(screen.getByRole("radio", { name: /^Client work/ }));
    expect(onChange).toHaveBeenCalledWith(stored.key);
  });

  it("does not offer an archived template", () => {
    state.templates = [kanban, { ...stored, archived: true }];
    render(<TemplateChooser value="" onChange={() => {}} />);

    expect(screen.queryByRole("radio", { name: /^Client work/ })).toBeNull();
    expect(screen.getAllByRole("radiogroup")).toHaveLength(1);
  });

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
    expect(templateShape(scrum)).toBe("Scrum board · the default workflows");
    expect(templateShape(tasks)).toBe("Kanban board · Simple task workflow · no sprints, resources, releases, components, repositories");
  });
});
