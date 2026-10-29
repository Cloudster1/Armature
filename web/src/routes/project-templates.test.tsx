import { afterEach, describe, expect, it, vi } from "vitest";
import { screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { ReactNode } from "react";
import type { ProjectTemplate } from "@/api/projects";
import { ConfirmProvider } from "@/features/shell/ConfirmProvider";
import { renderInShell, serve } from "@/test/shell";

vi.mock("@tanstack/react-router", () => ({
  Link: ({ children }: { children: ReactNode }) => <a>{children}</a>,
  createRoute: (options: object) => options,
}));
vi.mock("./admin", () => ({ adminRoute: {} }));

const { projectTemplatesRoute } = await import("./project-templates");
const ProjectTemplatesPage = (projectTemplatesRoute as unknown as { component: () => ReactNode }).component;

function template(name: string, extra: Partial<ProjectTemplate> = {}): ProjectTemplate {
  return {
    key: name.toLowerCase(),
    name,
    description: `${name} description.`,
    kind: "software",
    boardType: "scrum",
    planningMethod: "scrum",
    features: ["board"],
    desk: false,
    builtin: true,
    archived: false,
    scheme: null,
    issueTypes: [],
    ...extra,
  };
}

const scrum = template("Scrum");
const clientWork = template("Client work", { key: "tpl-1", builtin: false, issueTypes: [{ id: "t", name: "Task", icon: "task", level: 0, retired: false }] });
const old = template("Old way", { key: "tpl-2", builtin: false, archived: true });

function show() {
  renderInShell(
    <ConfirmProvider>
      <ProjectTemplatesPage />
    </ConfirmProvider>,
  );
}

const card = (name: string) => document.querySelector<HTMLElement>(`[data-template-card="${name}"]`)!;

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("the project templates page", () => {
  it("offers Duplicate on a built-in card and Edit and Archive on a stored one", async () => {
    serve({ "GET /project-templates": [200, { templates: [scrum, clientWork] }] });
    show();
    await screen.findByText("Client work");
    expect(within(card("Scrum")).getByRole("button", { name: "Duplicate" })).toBeInTheDocument();
    expect(within(card("Scrum")).queryByRole("button", { name: "Edit" })).toBeNull();
    expect(within(card("Scrum")).getByText("Built-in")).toBeInTheDocument();
    expect(within(card("Client work")).getByRole("button", { name: "Edit" })).toBeInTheDocument();
    expect(within(card("Client work")).getByRole("button", { name: "Archive" })).toBeInTheDocument();
    expect(within(card("Client work")).queryByRole("button", { name: "Duplicate" })).toBeNull();
  });

  it("duplicates a built-in template by its key and opens the copy to edit", async () => {
    const copy = template("Scrum copy", { key: "tpl-9", builtin: false });
    const fetchMock = serve({
      "GET /project-templates": [200, { templates: [scrum] }],
      "POST /project-templates": [201, { template: copy }],
      "GET /issue-types": [200, { issueTypes: [] }],
      "GET /workflow-schemes?limit=200": [200, { schemes: [], total: 0, limit: 200, offset: 0 }],
    });
    show();
    await userEvent.click(await screen.findByRole("button", { name: "Duplicate" }));

    expect(await screen.findByRole("dialog", { name: "Edit Scrum copy" })).toBeInTheDocument();
    const post = fetchMock.mock.calls.find(([, init]) => init?.method === "POST")!;
    expect(JSON.parse(String(post[1]!.body))).toEqual({ from: "scrum" });
  });

  it("archives after a confirmation, and restores from the Archived view", async () => {
    const fetchMock = serve({
      "GET /project-templates": [200, { templates: [scrum, clientWork] }],
      "POST /project-templates/tpl-1/archive": [200, { template: { ...clientWork, archived: true } }],
      "GET /project-templates?archived=true": [200, { templates: [scrum, clientWork, old] }],
      "POST /project-templates/tpl-2/restore": [200, { template: { ...old, archived: false } }],
    });
    show();
    await screen.findByText("Client work");
    await userEvent.click(within(card("Client work")).getByRole("button", { name: "Archive" }));
    await userEvent.click(await screen.findByRole("button", { name: "Archive project template" }));
    await vi.waitFor(() => expect(fetchMock).toHaveBeenCalledWith(expect.stringContaining("/project-templates/tpl-1/archive"), expect.anything()));

    await userEvent.click(screen.getByRole("button", { name: "Archived" }));
    await screen.findByText("Old way");
    expect(screen.queryByText("Client work")).toBeNull();
    await userEvent.click(within(card("Old way")).getByRole("button", { name: "Restore" }));
    await vi.waitFor(() => expect(fetchMock).toHaveBeenCalledWith(expect.stringContaining("/project-templates/tpl-2/restore"), expect.anything()));
  });
});
