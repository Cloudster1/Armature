import { afterEach, describe, expect, it, vi } from "vitest";
import { screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { IssueType } from "@/api/issues";
import type { ProjectTemplate } from "@/api/projects";
import { refusal, renderInShell, serve } from "@/test/shell";
import { TemplateEditor } from "./TemplateEditor";

function type(name: string, extra: Partial<IssueType> = {}): IssueType {
  return { id: name.toLowerCase(), name, icon: "task", level: 0, isSubtask: false, position: 0, builtin: true, retired: false, ...extra };
}

const library = { issueTypes: [type("Task"), type("Bug", { icon: "bug" }), type("Epic", { icon: "epic", level: 1 })] };
const schemes = {
  schemes: [{ id: "s1", name: "Agency flow", isDefault: false, items: [], projectKeys: [] }],
  total: 1,
  limit: 200,
  offset: 0,
};
const GET_TYPES = "GET /issue-types";
const GET_SCHEMES = "GET /workflow-schemes?limit=200";

const stored: ProjectTemplate = {
  key: "tpl-1",
  name: "Client work",
  description: "For agencies.",
  kind: "software",
  boardType: "kanban",
  planningMethod: "kanban",
  features: ["board", "plan"],
  desk: false,
  builtin: false,
  archived: false,
  scheme: null,
  issueTypes: [
    { id: "task", name: "Task", icon: "task", level: 0, retired: false },
    { id: "bug", name: "Bug", icon: "bug", level: 0, retired: false },
  ],
};

function bodyOf(fetchMock: ReturnType<typeof serve>, call: string) {
  const sent = fetchMock.mock.calls.find(([url, init]) => `${init?.method ?? "GET"} ${String(url).replace(/^.*\/api\/v1/, "")}` === call);
  return sent ? JSON.parse(String(sent[1]!.body)) : undefined;
}

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("the template editor", () => {
  it("sends what was chosen when it changes a template", async () => {
    const fetchMock = serve({
      [GET_TYPES]: [200, library],
      [GET_SCHEMES]: [200, schemes],
      "PATCH /project-templates/tpl-1": [200, { template: stored }],
    });
    const onClose = vi.fn();
    renderInShell(<TemplateEditor template={stored} onClose={onClose} />);

    await screen.findByRole("checkbox", { name: "Allow Epic" });
    await userEvent.clear(screen.getByLabelText("Name"));
    await userEvent.type(screen.getByLabelText("Name"), "Agency work");
    await userEvent.selectOptions(screen.getByRole("combobox", { name: "Board" }), "scrum");
    await userEvent.selectOptions(screen.getByLabelText("Workflow scheme"), "s1");
    await userEvent.click(screen.getByRole("checkbox", { name: "Allow Bug" }));
    await userEvent.click(screen.getByRole("button", { name: "Move Task later" }));
    await userEvent.click(screen.getByRole("button", { name: "Save template" }));

    await vi.waitFor(() => expect(onClose).toHaveBeenCalled());
    expect(bodyOf(fetchMock, "PATCH /project-templates/tpl-1")).toEqual({
      name: "Agency work",
      description: "For agencies.",
      kind: "software",
      boardType: "scrum",
      planningMethod: "kanban",
      features: ["board", "plan"],
      desk: false,
      issueTypeIds: ["task"],
      workflowSchemeId: "s1",
    });
  });

  it("puts the order of the ticked types in what it sends, and asks for the default scheme", async () => {
    const fetchMock = serve({
      [GET_TYPES]: [200, library],
      [GET_SCHEMES]: [200, schemes],
      "PATCH /project-templates/tpl-1": [200, { template: stored }],
    });
    renderInShell(<TemplateEditor template={stored} onClose={() => {}} />);

    await screen.findByRole("checkbox", { name: "Allow Epic" });
    await userEvent.click(screen.getByRole("button", { name: "Move Bug earlier" }));
    await userEvent.click(screen.getByRole("button", { name: "Save template" }));

    await vi.waitFor(() => expect(bodyOf(fetchMock, "PATCH /project-templates/tpl-1")).toBeDefined());
    const sent = bodyOf(fetchMock, "PATCH /project-templates/tpl-1");
    expect(sent.issueTypeIds).toEqual(["bug", "task"]);
    expect(sent.defaultWorkflowScheme).toBe(true);
    expect(sent.workflowSchemeId).toBeUndefined();
  });

  it("offers the desk and its pages only for a service template", async () => {
    serve({ [GET_TYPES]: [200, library], [GET_SCHEMES]: [200, schemes] });
    renderInShell(<TemplateEditor template={stored} onClose={() => {}} />);

    await screen.findByRole("checkbox", { name: "Allow Epic" });
    expect(screen.queryByRole("switch", { name: "Set up a service desk" })).toBeNull();
    expect(screen.queryByRole("checkbox", { name: /Queues/ })).toBeNull();

    await userEvent.selectOptions(screen.getByLabelText("Kind"), "service");
    expect(screen.getByRole("switch", { name: "Set up a service desk" })).toBeInTheDocument();
    expect(screen.getByRole("checkbox", { name: /Queues/ })).toBeInTheDocument();

    await userEvent.selectOptions(screen.getByLabelText("Kind"), "business");
    expect(screen.queryByRole("switch", { name: "Set up a service desk" })).toBeNull();
    expect(screen.queryByRole("checkbox", { name: /Queues/ })).toBeNull();
  });

  it("will not save without a standard type, and says why", async () => {
    serve({ [GET_TYPES]: [200, library], [GET_SCHEMES]: [200, schemes] });
    renderInShell(<TemplateEditor template={stored} onClose={() => {}} />);

    await screen.findByRole("checkbox", { name: "Allow Epic" });
    await userEvent.click(screen.getByRole("checkbox", { name: "Allow Task" }));
    await userEvent.click(screen.getByRole("checkbox", { name: "Allow Bug" }));
    expect(screen.getByText(/No standard type is ticked/)).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Save template" })).toBeDisabled();
  });

  it("shows the server's refusal as its sentence", async () => {
    const sentence = "A template needs a standard issue type that is not retired, such as Task; add one.";
    serve({
      [GET_TYPES]: [200, library],
      [GET_SCHEMES]: [200, schemes],
      "PATCH /project-templates/tpl-1": [422, refusal("validation_failed", sentence)],
    });
    const onClose = vi.fn();
    renderInShell(<TemplateEditor template={stored} onClose={onClose} />);

    await screen.findByRole("checkbox", { name: "Allow Epic" });
    await userEvent.click(screen.getByRole("button", { name: "Save template" }));

    expect(await screen.findByText(sentence)).toBeInTheDocument();
    expect(onClose).not.toHaveBeenCalled();
  });

  it("makes a new template with the built-in types ticked", async () => {
    const fetchMock = serve({
      [GET_TYPES]: [200, library],
      [GET_SCHEMES]: [200, schemes],
      "POST /project-templates": [201, { template: stored }],
    });
    renderInShell(<TemplateEditor onClose={() => {}} />);

    await screen.findByRole("checkbox", { name: "Allow Epic" });
    await userEvent.type(screen.getByLabelText("Name"), "Fresh");
    await userEvent.click(screen.getByRole("button", { name: "Make template" }));

    await vi.waitFor(() => expect(bodyOf(fetchMock, "POST /project-templates")).toBeDefined());
    const sent = bodyOf(fetchMock, "POST /project-templates");
    expect(sent.name).toBe("Fresh");
    expect(sent.kind).toBe("software");
    expect(sent.issueTypeIds).toEqual(["task", "bug", "epic"]);
  });
});
