import { afterEach, describe, expect, it, vi } from "vitest";
import { screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { ReactNode } from "react";
import type { IssueType } from "@/api/issues";
import { ConfirmProvider } from "@/features/shell/ConfirmProvider";
import { refusal, renderInShell, serve } from "@/test/shell";

vi.mock("@tanstack/react-router", () => ({
  Link: ({ children }: { children: ReactNode }) => <a>{children}</a>,
  createRoute: (options: object) => options,
}));
vi.mock("./admin", () => ({ adminRoute: {} }));

const { issueTypesRoute } = await import("./issue-types");
const IssueTypesPage = (issueTypesRoute as unknown as { component: () => ReactNode }).component;

function type(name: string, extra: Partial<IssueType> = {}): IssueType {
  return { id: name.toLowerCase(), name, icon: "task", level: 0, isSubtask: false, position: 0, builtin: false, retired: false, ...extra };
}

const task = type("Task", { builtin: true });
const change = type("Change", { icon: "change", description: "Asked for, not broken." });
const incident = type("Incident", { icon: "incident", retired: true, retiredAt: "2026-10-20T09:00:00Z" });
const library = { issueTypes: [task, change, incident] };
const inUse = "Issues use Change, so it cannot be deleted. Retire it instead: its issues keep it and nothing new is offered it.";

function show() {
  renderInShell(
    <ConfirmProvider>
      <IssueTypesPage />
    </ConfirmProvider>,
  );
}

async function openMenu(name: string) {
  await userEvent.click(await screen.findByRole("button", { name: `Actions for ${name}` }));
}

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("the issue types page", () => {
  it("lists every type, retired ones too, with Built-in and Retired tags", async () => {
    serve({ "GET /issue-types?retired=true": [200, library] });
    show();
    // The description is drawn only in the table, so finding it means the rows are there.
    await screen.findByText("Asked for, not broken.");
    const row = (name: string) => document.querySelector<HTMLElement>(`[data-issue-type-row="${name}"]`)!;
    const taskRow = row("Task");
    expect(within(taskRow).getByText("Built-in")).toBeInTheDocument();
    expect(within(taskRow).queryByText("Retired")).toBeNull();
    const incidentRow = row("Incident");
    expect(within(incidentRow).getByText("Retired")).toBeInTheDocument();
    expect(within(incidentRow).queryByText("Built-in")).toBeNull();
  });

  it("offers a built-in type to edit but not to retire or delete", async () => {
    serve({ "GET /issue-types?retired=true": [200, library] });
    show();
    await openMenu("Task");
    expect(screen.getByRole("menuitem", { name: "Edit" })).toBeEnabled();
    expect(screen.getByRole("menuitem", { name: "Retire" })).toBeDisabled();
    expect(screen.getByRole("menuitem", { name: "Delete" })).toBeDisabled();
  });

  it("edits a type, sending only what changed", async () => {
    const fetchMock = serve({
      "GET /issue-types?retired=true": [200, library],
      "PATCH /issue-types/change": [200, { issueType: { ...change, name: "Change request", icon: "improvement" } }],
    });
    show();
    await openMenu("Change");
    await userEvent.click(screen.getByRole("menuitem", { name: "Edit" }));
    const dialog = await screen.findByRole("dialog");
    const name = within(dialog).getByLabelText("Name");
    await userEvent.clear(name);
    await userEvent.type(name, "Change request");
    await userEvent.click(within(dialog).getByRole("radio", { name: "Improvement" }));
    await userEvent.click(within(dialog).getByRole("button", { name: "Save type" }));

    await vi.waitFor(() => expect(fetchMock.mock.calls.some(([, init]) => init?.method === "PATCH")).toBe(true));
    const [, init] = fetchMock.mock.calls.find(([, init]) => init?.method === "PATCH")!;
    expect(JSON.parse(String(init!.body))).toEqual({ name: "Change request", icon: "improvement" });
  });

  it("keeps a built-in type's name and level out of reach while editing", async () => {
    serve({ "GET /issue-types?retired=true": [200, library] });
    show();
    await openMenu("Task");
    await userEvent.click(screen.getByRole("menuitem", { name: "Edit" }));
    const dialog = await screen.findByRole("dialog");
    expect(within(dialog).getByLabelText("Name")).toBeDisabled();
    expect(within(dialog).getByLabelText("Level")).toBeDisabled();
    expect(within(dialog).getByText(/keeps its name and level/)).toBeInTheDocument();
  });

  it("says why a type issues use cannot be deleted, and to retire it instead", async () => {
    serve({
      "GET /issue-types?retired=true": [200, library],
      "DELETE /issue-types/change": [409, refusal("conflict", inUse)],
    });
    show();
    await openMenu("Change");
    await userEvent.click(screen.getByRole("menuitem", { name: "Delete" }));
    await userEvent.click(await screen.findByRole("button", { name: "Delete issue type" }));

    const said = await screen.findByText(inUse);
    expect(said.closest("[data-toast]")).not.toBeNull();
  });
});
