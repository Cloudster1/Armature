import { describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { Status } from "@/api/issues";
import type { RuleType, WorkflowDetail } from "@/api/workflows";

const create = vi.fn();
const save = vi.fn();
const createStatus = vi.fn();
const state = { existing: undefined as WorkflowDetail | undefined };

const todo: Status = { id: "s-todo", name: "To Do", category: "todo", position: 0 };
const progress: Status = { id: "s-progress", name: "In Progress", category: "in_progress", position: 1 };
const done: Status = { id: "s-done", name: "Done", category: "done", position: 2 };

const catalogue: RuleType[] = [
  { kind: "validator", type: "validator.comment_required", label: "Comment required", description: "Needs a comment.", options: [] },
  {
    kind: "postfunction",
    type: "postfunction.add_comment",
    label: "Add a comment",
    description: "A fixed comment is added.",
    options: [{ name: "text", label: "Comment", kind: "text", required: true, choices: [] }],
  },
];

vi.mock("@/api/issues", () => ({
  useStatuses: () => ({ data: { statuses: [todo, progress, done] } }),
  useCreateStatus: () => ({ mutate: createStatus, isPending: false, error: null }),
}));

vi.mock("@/api/workflows", () => ({
  useWorkflow: () => ({ data: state.existing }),
  useRuleTypes: () => ({ data: { ruleTypes: catalogue } }),
  useCreateWorkflow: () => ({ mutate: create, isPending: false, error: null }),
  useSaveWorkflow: () => ({ mutate: save, isPending: false, error: null }),
}));

const { WorkflowDesigner } = await import("./WorkflowDesigner");

function node(name: string) {
  return document.querySelector(`[data-workflow-node="${name}"]`) as HTMLElement;
}

describe("WorkflowDesigner", () => {
  it("builds a workflow from the inspector alone and sends the picture with it", async () => {
    state.existing = undefined;
    create.mockClear();
    render(<WorkflowDesigner onDone={() => {}} onCancel={() => {}} />);

    await userEvent.type(screen.getByLabelText("Name"), "Lean");

    // The first status placed is where issues open.
    await userEvent.selectOptions(screen.getByLabelText("Add a status"), "To Do");
    await userEvent.click(screen.getByRole("button", { name: "Add status" }));
    expect(node("To Do")).toBeInTheDocument();
    expect(node("To Do").querySelector("[data-workflow-initial]")).not.toBeNull();

    // A status that is on the canvas is no longer on offer.
    expect(within(screen.getByLabelText("Add a status")).queryByRole("option", { name: "To Do" })).toBeNull();
    await userEvent.selectOptions(screen.getByLabelText("Add a status"), "Done");
    await userEvent.click(screen.getByRole("button", { name: "Add status" }));
    expect(node("Done").querySelector("[data-workflow-initial]")).toBeNull();

    // A transition drawn from To Do is named after where it goes until renamed.
    // A click, not a press: d3-drag wants a window jsdom's mousedown does not carry.
    fireEvent.click(screen.getByRole("button", { name: "To Do, where new issues open" }));
    await userEvent.selectOptions(screen.getByLabelText("Add a transition to"), "Done");
    await userEvent.click(screen.getByRole("button", { name: "Add transition" }));
    const name = screen.getByLabelText("Transition name") as HTMLInputElement;
    expect(name.value).toBe("Done");
    await userEvent.clear(name);
    await userEvent.type(name, "Finish");
    expect(document.querySelector('[data-workflow-edge="Finish"]')).not.toBeNull();

    // A rule with an option waits for the option before it can be added.
    await userEvent.selectOptions(screen.getByLabelText("Add a rule"), "postfunction.add_comment");
    expect(screen.getByRole("button", { name: "Add rule" })).toBeDisabled();
    await userEvent.type(screen.getByLabelText("Comment"), "Finished.");
    await userEvent.click(screen.getByRole("button", { name: "Add rule" }));
    await userEvent.selectOptions(screen.getByLabelText("Add a rule"), "validator.comment_required");
    await userEvent.click(screen.getByRole("button", { name: "Add rule" }));
    expect(screen.getByText("Add a comment (Comment: Finished.)")).toBeInTheDocument();

    await userEvent.click(screen.getByRole("button", { name: "Create workflow" }));
    expect(create).toHaveBeenCalledTimes(1);
    const input = create.mock.calls[0]![0];
    expect(input.name).toBe("Lean");
    expect(input.steps).toEqual([
      { statusId: todo.id, isInitial: true, layout: expect.objectContaining({ x: expect.any(Number) }) },
      { statusId: done.id, isInitial: false, layout: expect.objectContaining({ y: expect.any(Number) }) },
    ]);
    expect(input.transitions).toEqual([
      {
        id: undefined,
        name: "Finish",
        description: "",
        fromStatusId: todo.id,
        toStatusId: done.id,
        rules: [
          { kind: "postfunction", type: "postfunction.add_comment", config: { text: "Finished." } },
          { kind: "validator", type: "validator.comment_required", config: {} },
        ],
      },
    ]);
  });

  it("says what would stop a save before the server has to", async () => {
    state.existing = undefined;
    render(<WorkflowDesigner onDone={() => {}} onCancel={() => {}} />);
    expect(screen.getByText(/The workflow needs a name/)).toBeInTheDocument();
    expect(screen.getByText(/Put at least one status on the canvas/)).toBeInTheDocument();
  });

  it("opens a saved workflow with its rules and keeps its transition ids on save", async () => {
    state.existing = {
      workflow: {
        id: "w1",
        name: "Default",
        steps: [
          { id: "st1", status: todo, isInitial: true, position: 0, layout: { x: 40, y: 40 } },
          { id: "st2", status: progress, isInitial: false, position: 1 },
        ],
        transitions: [{ id: "t1", name: "Start", fromStepId: "st1", toStepId: "st2", position: 0 }],
      },
      rules: { t1: [{ id: "r1", kind: "postfunction", type: "postfunction.add_comment", config: { text: "Go." }, position: 0 }] },
    };
    save.mockClear();
    render(<WorkflowDesigner workflowId="w1" onDone={() => {}} onCancel={() => {}} />);

    expect(node("To Do").dataset.x).toBe("40");
    // The unplaced status was laid out, not dropped at the origin.
    expect(Number(node("In Progress").dataset.x)).toBeGreaterThan(40);

    fireEvent.click(screen.getByRole("button", { name: "Transition Start" }));
    const panel = document.querySelector("[data-edge-panel]") as HTMLElement;
    expect(within(panel).getByText("Add a comment (Comment: Go.)")).toBeInTheDocument();
    await userEvent.click(within(panel).getByRole("button", { name: /Remove rule Add a comment/ }));

    await userEvent.click(screen.getByRole("button", { name: "Save workflow" }));
    expect(save).toHaveBeenCalledWith(
      expect.objectContaining({
        id: "w1",
        transitions: [expect.objectContaining({ id: "t1", name: "Start", rules: [] })],
      }),
      expect.anything(),
    );
  });

  it("coins a status in the inspector and lands it on the canvas", async () => {
    state.existing = undefined;
    const coined: Status = { id: "s-review", name: "Reviewing", category: "in_progress", position: 3 };
    createStatus.mockImplementation((input: { name: string; category: string }, opts: { onSuccess: (r: unknown) => void }) => {
      expect(input).toEqual({ name: "Reviewing", category: "in_progress", description: undefined });
      opts.onSuccess({ status: coined });
    });
    render(<WorkflowDesigner onDone={() => {}} onCancel={() => {}} />);

    await userEvent.type(screen.getByLabelText("Status name"), "Reviewing");
    await userEvent.click(screen.getByRole("button", { name: "Create and add" }));

    expect(createStatus).toHaveBeenCalledTimes(1);
    expect(node("Reviewing")).toBeInTheDocument();
    // The new status is selected, so the panel is already about it.
    expect(document.querySelector('[data-node-panel="Reviewing"]')).toBeInTheDocument();
    createStatus.mockReset();
  });
});
