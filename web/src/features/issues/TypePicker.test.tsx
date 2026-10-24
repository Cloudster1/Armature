import { afterEach, describe, expect, it, vi } from "vitest";
import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import type { Issue } from "@/api/issues";
import { ConfirmProvider } from "@/features/shell/ConfirmProvider";
import { refusal, renderInShell, serve, type Routes } from "@/test/shell";
import { History } from "./IssueHistory";
import { TypeField } from "./TypePicker";

const todo = { id: "s-todo", name: "To Do", category: "todo", position: 0 };
const doing = { id: "s-doing", name: "In Progress", category: "in_progress", position: 1 };

const issue = {
  id: "i1",
  key: "CP-4",
  summary: "Fix the login loop",
  projectKey: "CP",
  type: { id: "task", name: "Task", icon: "task", level: 0, isSubtask: false },
  status: doing,
} as Issue;

const issueTypes = {
  issueTypes: [
    { id: "epic", name: "Epic", icon: "epic", level: 1, isSubtask: false, position: 0 },
    { id: "story", name: "Story", icon: "story", level: 0, isSubtask: false, position: 1 },
    { id: "task", name: "Task", icon: "task", level: 0, isSubtask: false, position: 2 },
    { id: "bug", name: "Bug", icon: "bug", level: 0, isSubtask: false, position: 3 },
    { id: "subtask", name: "Subtask", icon: "subtask", level: -1, isSubtask: true, position: 4 },
  ],
};

// Stories follow the default workflow; bugs follow one that has no In Progress.
const projectWorkflows = {
  projectKey: "CP",
  assignments: [
    { issueTypeId: "story", workflowId: "w-default" },
    { issueTypeId: "bug", workflowId: "w-triage" },
  ],
};

function workflow(id: string, statuses: (typeof todo)[]) {
  return {
    workflow: { id, name: id, transitions: [], steps: statuses.map((status, i) => ({ id: `${id}-${i}`, status, isInitial: i === 0, position: i })) },
    rules: {},
  };
}

const changed = { issue: { ...issue, type: { ...issue.type, id: "bug", name: "Bug" } } };

function routes(extra: Routes = {}): Routes {
  return {
    "GET /issue-types": [200, issueTypes],
    "GET /projects/CP/workflows": [200, projectWorkflows],
    "GET /workflows/w-default": [200, workflow("w-default", [todo, doing])],
    "GET /workflows/w-triage": [200, workflow("w-triage", [todo])],
    ...extra,
  };
}

function show(editable = true) {
  renderInShell(
    <ConfirmProvider>
      <TypeField issue={issue} editable={editable} />
    </ConfirmProvider>,
  );
}

async function choose(name: string) {
  fireEvent.click(await screen.findByRole("button", { name: "Issue type: Task" }));
  fireEvent.click(within(screen.getByRole("menu")).getByRole("menuitem", { name }));
}

function patches(fetchMock: ReturnType<typeof serve>) {
  return fetchMock.mock.calls.filter(([, init]) => init?.method === "PATCH").map(([, init]) => JSON.parse(String(init?.body)));
}

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("changing an issue's type", () => {
  it("offers only the other types at the issue's level", async () => {
    serve(routes());
    show();

    fireEvent.click(await screen.findByRole("button", { name: "Issue type: Task" }));
    const menu = within(screen.getByRole("menu"));
    expect(menu.getAllByRole("menuitem").map((item) => item.getAttribute("data-type-option"))).toEqual(["Story", "Bug"]);
    expect(menu.getByRole("menuitem", { name: "Bug" })).toBeInTheDocument();
  });

  it("offers nothing to somebody who may not edit the issue", async () => {
    const fetchMock = serve(routes());
    show(false);

    await waitFor(() => expect(fetchMock).toHaveBeenCalled());
    expect(screen.queryByRole("button", { name: "Issue type: Task" })).toBeNull();
    expect(screen.getByText("Task")).toBeInTheDocument();
  });

  it("changes at once when the status carries over", async () => {
    const fetchMock = serve(routes({ "PATCH /issues/CP-4": [200, changed] }));
    show();

    await choose("Story");

    expect(await screen.findByText("Changed to Story")).toBeInTheDocument();
    expect(screen.queryByRole("dialog")).toBeNull();
    expect(patches(fetchMock)).toEqual([{ typeId: "story" }]);
  });

  it("says where the issue lands, and changes nothing until that is confirmed", async () => {
    const fetchMock = serve(routes({ "PATCH /issues/CP-4": [200, changed] }));
    show();

    await choose("Bug");

    const dialog = await screen.findByRole("dialog", { name: "Change CP-4 to Bug?" });
    expect(dialog).toHaveTextContent("Its workflow has no In Progress, so it moves to To Do.");
    expect(patches(fetchMock)).toEqual([]);

    fireEvent.click(within(dialog).getByRole("button", { name: "Change CP-4 to Bug" }));
    expect(await screen.findByText("Changed to Bug")).toBeInTheDocument();
    expect(patches(fetchMock)).toEqual([{ typeId: "bug" }]);
  });

  it("leaves the type alone when the move is called off", async () => {
    const fetchMock = serve(routes());
    show();

    await choose("Bug");
    fireEvent.click(within(await screen.findByRole("dialog")).getByRole("button", { name: "Cancel" }));

    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
    expect(patches(fetchMock)).toEqual([]);
  });

  it("says why the server refused the change", async () => {
    const needsParent = "A Story needs a parent issue.";
    serve(routes({ "PATCH /issues/CP-4": [422, refusal("needs_parent", needsParent)] }));
    show();

    await choose("Story");

    expect(await screen.findByRole("alert")).toHaveTextContent(needsParent);
  });
});

describe("the history of a type change", () => {
  it("reads as a sentence", async () => {
    const entry = {
      id: "h1",
      issueId: "i1",
      actor: { id: "ada", name: "Ada" },
      changes: [{ field: "type", from: "Task", to: "Bug" }],
      createdAt: "2026-09-01T09:00:00Z",
    };
    serve({ "GET /issues/CP-4/history": [200, { history: [entry] }] });
    renderInShell(<History issueKey="CP-4" />);

    expect(await screen.findByText(/changed the type from Task to Bug/)).toBeInTheDocument();
  });
});
