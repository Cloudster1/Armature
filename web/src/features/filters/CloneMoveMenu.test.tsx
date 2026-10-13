import { describe, expect, it, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { ReactNode } from "react";
import type { FieldValue } from "@/api/fields";
import type { Issue } from "@/api/issues";

// Before a move is confirmed, the dialog names the answers that stay behind:
// those to the old project's own fields. Organization fields go along.

const state = { values: [] as FieldValue[] };

vi.mock("@tanstack/react-router", () => ({
  Link: ({ children }: { children: ReactNode }) => <a href="#">{children}</a>,
  useNavigate: () => vi.fn(),
}));

vi.mock("@/api/filters", async () => ({
  ...(await vi.importActual<typeof import("@/api/filters")>("@/api/filters")),
  useMoveIssue: () => ({ mutate: vi.fn(), isPending: false, error: null }),
  useCloneIssue: () => ({ mutate: vi.fn(), isPending: false, error: null }),
}));

vi.mock("@/api/issues", async () => ({
  ...(await vi.importActual<typeof import("@/api/issues")>("@/api/issues")),
  useStatuses: () => ({ data: { statuses: [] } }),
}));

vi.mock("@/api/projects", async () => ({
  ...(await vi.importActual<typeof import("@/api/projects")>("@/api/projects")),
  useProjects: () => ({ data: { projects: [{ key: "CP", name: "Customer Portal" }, { key: "OPS", name: "Operations" }] } }),
}));

vi.mock("@/api/fields", async () => ({
  ...(await vi.importActual<typeof import("@/api/fields")>("@/api/fields")),
  useIssueFields: () => ({ data: { values: state.values } }),
}));

const { ToastProvider } = await import("@/components/ui");
const { CloneMoveMenu, MoveDialog } = await import("./CloneMoveMenu");

const issue = { key: "CP-5", projectKey: "CP", summary: "Priced work" } as Issue;

function answer(name: string, org: boolean, value?: FieldValue["value"], display?: string): FieldValue {
  return {
    field: {
      id: `f-${name}`,
      projectId: org ? undefined : "p-cp",
      projectKey: org ? undefined : "CP",
      org,
      name,
      kind: "text",
      options: [],
      position: 0,
      createdAt: "",
      updatedAt: "",
    },
    value,
    display,
  };
}

function draw(values: FieldValue[]) {
  state.values = values;
  return render(
    <ToastProvider>
      <MoveDialog issue={issue} onClose={() => {}} />
    </ToastProvider>,
  );
}

describe("MoveDialog", () => {
  it("names each answer to a field of the old project before the move", () => {
    draw([answer("Cost", true, 120, "120"), answer("Platform", false, "Web", "Web"), answer("Customer", false)]);
    const dropped = document.querySelector("[data-move-dropped]");
    expect(dropped).not.toBeNull();
    const items = [...(dropped?.querySelectorAll("li") ?? [])].map((li) => li.textContent);
    expect(items).toEqual(["Platform: Web"]);
    expect(dropped?.textContent).not.toContain("Cost");
    expect(dropped?.textContent).not.toContain("Customer");
  });

  it("says nothing stays behind when only organization fields are answered", () => {
    draw([answer("Cost", true, 120, "120"), answer("Platform", false)]);
    expect(document.querySelector("[data-move-dropped]")).toBeNull();
    expect(screen.getByRole("button", { name: "Move" })).toBeInTheDocument();
  });
});

/** The query a download link asks the export for. */
function exportedQuery(): string | null {
  const href = document.querySelector('[data-action="download-csv"]')?.getAttribute("href") ?? "";
  return new URL(href, "http://armature.test").searchParams.get("q");
}

describe("CloneMoveMenu", () => {
  it("exports the issue that is open, and nothing else", async () => {
    render(
      <ToastProvider>
        <CloneMoveMenu issue={issue} editable />
      </ToastProvider>,
    );
    await userEvent.click(screen.getByRole("button", { name: "More about this issue" }));
    await userEvent.click(screen.getByRole("menuitem", { name: "Export as CSV" }));
    expect(document.querySelector("[data-export-dialog]")).not.toBeNull();
    expect(exportedQuery()).toBe("key = CP-5");
  });

  it("offers a reader the export but not clone or move", async () => {
    render(
      <ToastProvider>
        <CloneMoveMenu issue={issue} editable={false} />
      </ToastProvider>,
    );
    await userEvent.click(screen.getByRole("button", { name: "More about this issue" }));
    expect(screen.getByRole("menuitem", { name: "Export as CSV" })).toBeInTheDocument();
    expect(screen.queryByRole("menuitem", { name: "Clone" })).toBeNull();
    expect(screen.queryByRole("menuitem", { name: "Move to another project" })).toBeNull();
  });
});
