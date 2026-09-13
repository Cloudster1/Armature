import { beforeEach, describe, expect, it, vi } from "vitest";
import { render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { ReactNode } from "react";
import type { Status } from "@/api/issues";
import type { Assignment, Origin, Scheme, WorkflowDetail } from "@/api/workflows";

vi.mock("@tanstack/react-router", () => ({
  Link: ({ children }: { children: ReactNode }) => <a href="/settings/workflows">{children}</a>,
}));

const setProjectScheme = vi.fn();
const setAssignment = vi.fn();
const state = {
  schemeId: undefined as string | undefined,
  assignments: [] as Assignment[],
  schemes: [] as Scheme[],
  canAdministerOrg: true,
  canMap: true,
};

vi.mock("@/api/access", () => ({
  useAccess: () => ({ data: { grants: [], projects: [], canAdministerOrg: state.canAdministerOrg, canCreateProject: true } }),
  canAdminister: () => state.canMap,
}));

const todo: Status = { id: "s-todo", name: "To Do", category: "todo", position: 0 };
const done: Status = { id: "s-done", name: "Done", category: "done", position: 1 };

vi.mock("@/api/issues", () => ({
  useStatuses: () => ({ data: { statuses: [todo, done] } }),
}));

/** A two-step workflow whose one transition carries the workflow's name, so the drawing says which it is. */
function detail(id: string, name: string): WorkflowDetail {
  return {
    workflow: {
      id,
      name,
      steps: [
        { id: `${id}-1`, status: todo, isInitial: true, position: 0 },
        { id: `${id}-2`, status: done, isInitial: false, position: 1 },
      ],
      transitions: [{ id: `${id}-t`, name: `Finish ${name}`, fromStepId: `${id}-1`, toStepId: `${id}-2`, position: 0 }],
    },
    rules: {},
  };
}

vi.mock("@/api/workflows", () => ({
  useProjectWorkflows: () => ({
    data: { projectKey: "PR", schemeId: state.schemeId, assignments: state.assignments },
    isLoading: false,
  }),
  useSchemes: () => ({ data: { schemes: state.schemes } }),
  useSetProjectScheme: () => ({ mutate: setProjectScheme, isPending: false, error: null }),
  useSetProjectAssignment: () => ({ mutate: setAssignment, isPending: false, error: null }),
  useWorkflows: () => ({
    data: {
      workflows: [
        { id: "Default software workflow", name: "Default software workflow", stepCount: 4, transitionCount: 5, schemeCount: 1, inDefault: true },
        { id: "Bug triage", name: "Bug triage", stepCount: 2, transitionCount: 1, schemeCount: 1, inDefault: false },
      ],
    },
  }),
  useWorkflow: (id: string) => ({ data: detail(id, id), isLoading: false, error: null }),
}));

const { ProjectWorkflows } = await import("./ProjectWorkflows");

function origin(over: Partial<Origin> = {}): Origin {
  return { scope: "tenant", schemeId: "s1", schemeName: "Default workflow scheme", named: false, ...over };
}

function assignment(typeName: string, workflowName: string, over: Partial<Origin> = {}): Assignment {
  return {
    issueTypeId: typeName,
    issueTypeName: typeName,
    workflowId: workflowName,
    workflowName,
    origin: origin(over),
  };
}

const organisation: Scheme = {
  id: "s1",
  name: "Default workflow scheme",
  isDefault: true,
  items: [],
  projectKeys: [],
};

const ownScheme: Scheme = {
  id: "s2",
  name: "Support workflows",
  isDefault: false,
  items: [],
  projectKeys: ["PR"],
};

describe("ProjectWorkflows", () => {
  beforeEach(() => {
    state.canAdministerOrg = true;
    state.canMap = true;
  });

  it("lets a project administrator decide one row and sends the workflow for that type", async () => {
    state.schemeId = undefined;
    state.schemes = [organisation];
    state.canAdministerOrg = false;
    state.assignments = [assignment("Bug", "Default software workflow"), assignment("Task", "Default software workflow")];
    setAssignment.mockClear();

    render(<ProjectWorkflows projectKey="PR" />);
    // No whole-scheme picker for somebody who administers only this project.
    expect(screen.queryByLabelText("Workflow scheme")).toBeNull();

    const bug = screen.getByLabelText("Workflow for Bug") as HTMLSelectElement;
    expect(bug.value).toBe("");
    await userEvent.selectOptions(bug, "Bug triage");
    expect(setAssignment).toHaveBeenCalledWith({ projectKey: "PR", issueTypeId: "Bug", workflowId: "Bug triage" });
  });

  it("sends null when a row is handed back to the organization", async () => {
    state.schemeId = "s2";
    state.schemes = [organisation, ownScheme];
    state.assignments = [
      assignment("Bug", "Bug triage", { scope: "project", schemeId: "s2", schemeName: "Support workflows", named: true }),
    ];
    setAssignment.mockClear();

    render(<ProjectWorkflows projectKey="PR" />);
    const bug = screen.getByLabelText("Workflow for Bug") as HTMLSelectElement;
    expect(bug.value).toBe("Bug triage");
    await userEvent.selectOptions(bug, "Follow the organization");
    expect(setAssignment).toHaveBeenCalledWith({ projectKey: "PR", issueTypeId: "Bug", workflowId: null });
  });

  it("shows the fallback's workflow on a row the project's fallback decides, with no way back per type", () => {
    state.schemeId = "s2";
    state.schemes = [organisation, ownScheme];
    state.assignments = [
      assignment("Task", "Bug triage", { scope: "project", schemeId: "s2", schemeName: "Support workflows", named: false }),
    ];

    render(<ProjectWorkflows projectKey="PR" />);
    const task = screen.getByLabelText("Workflow for Task") as HTMLSelectElement;
    expect(task.value).toBe("Bug triage");
    expect(within(task).queryByText("Follow the organization")).toBeNull();
  });

  it("shows no controls to somebody who may only look", () => {
    state.schemeId = undefined;
    state.schemes = [organisation];
    state.canAdministerOrg = false;
    state.canMap = false;
    state.assignments = [assignment("Task", "Default software workflow")];

    render(<ProjectWorkflows projectKey="PR" />);
    expect(screen.queryByLabelText("Workflow for Task")).toBeNull();
    expect(screen.queryByLabelText("Workflow scheme")).toBeNull();
  });

  it("says the project follows the organization when it has named no scheme", () => {
    state.schemeId = undefined;
    state.schemes = [organisation];
    state.assignments = [assignment("Task", "Default software workflow")];

    render(<ProjectWorkflows projectKey="PR" />);
    expect(screen.getByText("This project follows the organization.")).toBeInTheDocument();
    // It names the scheme rather than saying "the default", which is not a name.
    expect(
      screen.getByText(/Every issue type uses Default workflow scheme/),
    ).toBeInTheDocument();
  });

  it("names the workflow each issue type uses and who decided it", () => {
    state.schemeId = "s2";
    state.schemes = [organisation, ownScheme];
    state.assignments = [
      assignment("Bug", "Bug triage", { scope: "project", schemeId: "s2", schemeName: "Support workflows", named: true }),
      assignment("Task", "Default software workflow"),
    ];

    render(<ProjectWorkflows projectKey="PR" />);

    const bug = screen.getByRole("row", { name: /Bug/ });
    expect(within(bug).getByRole("link", { name: "Bug triage" })).toBeInTheDocument();
    expect(within(bug).getByText("This project")).toBeInTheDocument();
    expect(within(bug).getByText(/Support workflows names this issue type/)).toBeInTheDocument();

    const task = screen.getByRole("row", { name: /Task/ });
    expect(within(task).getByText("Organization")).toBeInTheDocument();
    // The fallback is the interesting half: it is why the row is inherited.
    expect(
      within(task).getByText(/catches everything it does not name/),
    ).toBeInTheDocument();
  });

  it("draws the first row's workflow and follows a click to another row", async () => {
    state.schemeId = "s2";
    state.schemes = [organisation, ownScheme];
    state.assignments = [
      assignment("Bug", "Bug triage", { scope: "project", schemeId: "s2", schemeName: "Support workflows", named: true }),
      assignment("Task", "Default software workflow"),
    ];

    render(<ProjectWorkflows projectKey="PR" />);
    expect(document.querySelector('[data-workflow-graph="Bug triage"] [data-workflow-edge="Finish Bug triage"]')).not.toBeNull();
    expect(screen.getByRole("row", { name: /Bug/ })).toHaveAttribute("data-assignment-shown");

    await userEvent.click(within(screen.getByRole("row", { name: /Task/ })).getByText("Task"));
    expect(document.querySelector('[data-workflow-graph="Default software workflow"]')).not.toBeNull();
    expect(document.querySelector('[data-workflow-graph="Bug triage"]')).toBeNull();
    // The picture is for looking at: nothing on it is a button.
    expect(document.querySelectorAll("[data-workflow-graph] [role=\"button\"]")).toHaveLength(0);
  });

  // Handing the decision back is a deliberate null, not the absence of a value.
  it("hands the decision back to the organization when asked to", async () => {
    state.schemeId = "s2";
    state.schemes = [organisation, ownScheme];
    state.assignments = [];
    setProjectScheme.mockClear();

    render(<ProjectWorkflows projectKey="PR" />);
    await userEvent.click(screen.getByRole("button", { name: "Follow the organization" }));

    expect(setProjectScheme).toHaveBeenCalledWith({ projectKey: "PR", schemeId: null });
  });

  it("offers no way back when the project is already following the organization", () => {
    state.schemeId = undefined;
    state.schemes = [organisation, ownScheme];
    state.assignments = [];

    render(<ProjectWorkflows projectKey="PR" />);
    expect(screen.queryByRole("button", { name: "Follow the organization" })).toBeNull();
  });

  it("will not offer a scheme the project is already using", () => {
    state.schemeId = "s2";
    state.schemes = [organisation, ownScheme];
    state.assignments = [];

    render(<ProjectWorkflows projectKey="PR" />);
    const picker = screen.getByLabelText("Workflow scheme");
    expect(within(picker).queryByText("Support workflows")).toBeNull();
  });
});
