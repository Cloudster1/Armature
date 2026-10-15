import { beforeEach, describe, expect, it, vi } from "vitest";
import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { Scheme } from "@/api/workflows";

const create = vi.fn();
const save = vi.fn();
const asked: Array<{ q?: string; limit?: number }> = [];
const rows = [
  { id: "w-default", name: "Default software workflow", stepCount: 4, transitionCount: 5, schemeCount: 1, inDefault: true },
  { id: "w-triage", name: "Bug triage", stepCount: 2, transitionCount: 1, schemeCount: 1, inDefault: false },
];

vi.mock("@/api/issues", () => ({ useIssueTypes: () => ({ data: { issueTypes: [{ id: "t-bug", name: "Bug" }] } }) }));
vi.mock("@/api/workflows", () => ({
  useCreateScheme: () => ({ mutate: create, isPending: false, error: null }),
  useSaveScheme: () => ({ mutate: save, isPending: false, error: null }),
  // The server answers what was typed; here, the rows whose name holds it.
  useWorkflows: (params: { q?: string; limit?: number } = {}) => {
    asked.push(params);
    const found = rows.filter((row) => row.name.toLowerCase().includes((params.q ?? "").toLowerCase()));
    return { data: { workflows: found, total: found.length, limit: 20, offset: 0 } };
  },
}));

const { SchemeEditor } = await import("./SchemeEditor");

beforeEach(() => {
  create.mockReset();
  save.mockReset();
  asked.length = 0;
});

describe("SchemeEditor", () => {
  // Taking the first of the library would map a scheme onto whatever happens to sort first.
  it("names no workflow on a new mapping until one is picked", async () => {
    render(<SchemeEditor onDone={() => {}} />);
    const workflow = screen.getByLabelText("Workflow") as HTMLSelectElement;
    expect(workflow.value).toBe("");
    expect(screen.getByRole("button", { name: "Create scheme" })).toBeDisabled();

    await userEvent.type(screen.getByLabelText("Scheme name"), "Support");
    await userEvent.selectOptions(workflow, "Bug triage");
    await userEvent.click(screen.getByRole("button", { name: "Create scheme" }));
    expect(create).toHaveBeenCalledWith({ name: "Support", items: [{ issueTypeId: null, workflowId: "w-triage" }] }, expect.anything());
  });

  it("asks the server for the workflows typed, a few at a time", async () => {
    render(<SchemeEditor onDone={() => {}} />);
    await userEvent.type(screen.getByLabelText("Find: Workflow"), "triage");
    await waitFor(() => expect(asked.at(-1)).toEqual({ q: "triage", limit: 20 }));
    const workflow = screen.getByLabelText("Workflow");
    expect(within(workflow).queryByText("Default software workflow")).toBeNull();
    expect(within(workflow).getByText("Bug triage")).toBeInTheDocument();
  });

  it("keeps an edited mapping's workflow by the name it carries, though the search found others", async () => {
    const scheme: Scheme = {
      id: "s1",
      name: "Support",
      isDefault: false,
      projectKeys: [],
      items: [{ workflowId: "w-old", workflowName: "Retired flow" }],
    };
    render(<SchemeEditor scheme={scheme} onDone={() => {}} />);
    const workflow = screen.getByLabelText("Workflow") as HTMLSelectElement;
    expect(workflow.value).toBe("w-old");
    expect(within(workflow).getByText("Retired flow")).toBeInTheDocument();

    await userEvent.click(screen.getByRole("button", { name: "Save scheme" }));
    expect(save).toHaveBeenCalledWith({ id: "s1", name: "Support", items: [{ issueTypeId: null, workflowId: "w-old" }] }, expect.anything());
  });
});
