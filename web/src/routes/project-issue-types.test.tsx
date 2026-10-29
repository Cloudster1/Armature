import { afterEach, describe, expect, it, vi } from "vitest";
import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import type { ReactNode } from "react";
import { refusal, renderInShell, serve, type Routes } from "@/test/shell";

vi.mock("@tanstack/react-router", () => ({
  createRoute: (options: object) => ({ ...options, useParams: () => ({ projectKey: "CP" }) }),
}));
vi.mock("./project-settings", () => ({ projectSettingsRoute: {} }));

const { projectIssueTypesRoute } = await import("./project-issue-types");
const Tab = (projectIssueTypesRoute as unknown as { component: () => ReactNode }).component;

function type(id: string, name: string, level: number, extra: object = {}) {
  return { id, name, icon: "task", level, isSubtask: level < 0, position: 0, builtin: true, retired: false, ...extra };
}

const library = [
  type("epic", "Epic", 1),
  type("story", "Story", 0),
  type("task", "Task", 0),
  type("bug", "Bug", 0),
  type("chore", "Chore", 0, { builtin: false }),
];
// CP keeps Task, Bug and Epic, in that order, and a retired Spike its issues still use.
const kept = [
  type("task", "Task", 0, { issues: 3 }),
  type("bug", "Bug", 0, { issues: 0 }),
  type("epic", "Epic", 1, { issues: 0 }),
  type("spike", "Spike", 0, { issues: 1, retired: true }),
];

function routes(extra: Routes = {}): Routes {
  return {
    "GET /projects/CP/issue-types?retired=true": [200, { issueTypes: kept }],
    "GET /issue-types": [200, { issueTypes: library }],
    ...extra,
  };
}

function names(): string[] {
  return screen.getAllByRole("listitem").map((row) => row.getAttribute("data-project-type") ?? "");
}

afterEach(() => vi.unstubAllGlobals());

describe("the Issue types tab", () => {
  it("lists the project's types in its order, then the library's others unticked", async () => {
    serve(routes());
    renderInShell(<Tab />);

    expect(await screen.findByRole("checkbox", { name: "Allow Task" })).toBeChecked();
    expect(names()).toEqual(["Task", "Bug", "Epic", "Spike", "Story", "Chore"]);
    expect(screen.getByRole("checkbox", { name: "Allow Story" })).not.toBeChecked();
  });

  it("keeps a type in use ticked and says how many issues use it", async () => {
    serve(routes());
    renderInShell(<Tab />);

    const task = await screen.findByRole("checkbox", { name: "Allow Task" });
    expect(task).toBeDisabled();
    expect(within(task.closest("li")!).getByText("3 issues use it, so it stays.")).toBeInTheDocument();
    expect(screen.getByText("1 issue uses it, so it stays.")).toBeInTheDocument();
    expect(screen.getByRole("checkbox", { name: "Allow Bug" })).toBeEnabled();
  });

  it("does not offer a retired library type for adding", async () => {
    // The library lists only live types unless asked; Spike is retired and so is not among the unticked.
    serve(routes());
    renderInShell(<Tab />);

    await screen.findByRole("checkbox", { name: "Allow Task" });
    expect(
      screen
        .getAllByRole("checkbox")
        .filter((box) => !(box as HTMLInputElement).checked)
        .map((box) => box.getAttribute("aria-label")),
    ).toEqual(["Allow Story", "Allow Chore"]);
  });

  it("saves the ticked types in the order shown", async () => {
    const saved = [type("bug", "Bug", 0), type("task", "Task", 0)];
    const fetchMock = serve(routes({ "PUT /projects/CP/issue-types": [200, { issueTypes: saved }] }));
    renderInShell(<Tab />);

    fireEvent.click(await screen.findByRole("checkbox", { name: "Allow Epic" }));
    fireEvent.click(screen.getByRole("checkbox", { name: "Allow Story" }));
    fireEvent.click(screen.getByRole("button", { name: "Move Bug earlier" }));
    fireEvent.click(screen.getByRole("button", { name: "Save types" }));

    expect(await screen.findByText("Issue types saved")).toBeInTheDocument();
    const put = fetchMock.mock.calls.find(([, init]) => init?.method === "PUT")!;
    expect(JSON.parse(String(put[1]?.body))).toEqual({ typeIds: ["bug", "task", "spike", "story"] });
  });

  it("says the server's sentence when it refuses", async () => {
    const sentence = "Spike is still used by 1 issue; change its type first.";
    serve(routes({ "PUT /projects/CP/issue-types": [422, refusal("invalid", sentence)] }));
    renderInShell(<Tab />);

    fireEvent.click(await screen.findByRole("checkbox", { name: "Allow Bug" }));
    fireEvent.click(screen.getByRole("button", { name: "Save types" }));

    await waitFor(() => expect(screen.getByRole("alert")).toHaveTextContent(sentence));
  });

  it("will not save without a standard type", async () => {
    serve(routes({ "GET /projects/CP/issue-types?retired=true": [200, { issueTypes: [type("epic", "Epic", 1), type("bug", "Bug", 0)] }] }));
    renderInShell(<Tab />);

    const bug = await screen.findByRole("checkbox", { name: "Allow Bug" });
    expect(bug).toBeDisabled();
    expect(screen.getByText(/last standard type stays/)).toBeInTheDocument();
  });
});
