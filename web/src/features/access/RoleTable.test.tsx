import { beforeEach, describe, expect, it, vi } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { Assignment } from "@/api/access";

const page = {
  rows: [] as Assignment[],
  total: 0,
  asked: [] as Array<{ q?: string; role?: string; offset?: number; projectKey?: string }>,
};

function grant(id: string, userName: string, projectKey?: string): Assignment {
  return { id, role: "user", userId: id, userName, projectKey } as Assignment;
}

vi.mock("@/api/access", () => ({
  permissionWords: (permission: string) => permission,
  useAssignments: (params: (typeof page.asked)[number]) => {
    page.asked.push(params);
    return { data: { assignments: page.rows, total: page.total, limit: 50, offset: params.offset ?? 0 }, isLoading: false, error: null };
  },
  useGrantRole: () => ({ mutate: vi.fn(), isPending: false, error: null }),
  useGroups: () => ({ data: { groups: [] } }),
  useRevokeRole: () => ({ mutate: vi.fn(), error: null }),
  useRoleName: () => (role: string) => (role === "user" ? "User" : "Scrum master"),
  useRoles: () => ({
    data: {
      roles: [
        { role: "user", name: "User", permissions: [], orgWideOnly: false },
        { role: "scrum_master", name: "Scrum master", permissions: [], orgWideOnly: false },
      ],
    },
  }),
}));
vi.mock("@/api/issues", () => ({ useMembers: () => ({ data: { members: [] } }) }));
vi.mock("@/features/projects/ProjectPicker", () => ({ ProjectPicker: () => null }));
vi.mock("@/features/shell/ConfirmProvider", () => ({ useConfirm: () => async () => true }));

const { RoleTable } = await import("./RoleTable");

beforeEach(() => {
  page.rows = [grant("g1", "Ada Lovelace"), grant("g2", "Bob Builder", "OPS")];
  page.total = 120;
  page.asked = [];
});

// The Roles tab holds every grant in the installation; it shows one page and finds the rest by typing.
describe("the Roles tab's grants", () => {
  it("shows one page and says which of the whole list it is", () => {
    render(<RoleTable />);
    expect(page.asked.at(-1)).toEqual({ projectKey: undefined, q: "", role: "", offset: 0 });
    expect(screen.getByText("1 to 2 of 120 grants")).toBeInTheDocument();
    expect(screen.getByText("1 to 50 of 120")).toBeInTheDocument();
  });

  it("turns the page, and starts a new search on its first page", async () => {
    render(<RoleTable />);
    await userEvent.click(screen.getByRole("button", { name: "Next page" }));
    expect(page.asked.at(-1)).toMatchObject({ offset: 50 });

    await userEvent.type(screen.getByLabelText("Find grants"), "bob");
    await waitFor(() => expect(page.asked.at(-1)).toMatchObject({ q: "bob", offset: 0 }));
  });

  it("narrows to one role", async () => {
    render(<RoleTable />);
    await userEvent.selectOptions(screen.getByLabelText("Only this role"), "Scrum master");
    expect(page.asked.at(-1)).toMatchObject({ role: "scrum_master", offset: 0 });
  });

  it("asks only for the grants that answer in a project when it is a project's", () => {
    render(<RoleTable projectKey="OPS" />);
    expect(page.asked.at(-1)).toMatchObject({ projectKey: "OPS" });
  });

  it("says when nothing matches rather than that nobody holds anything", async () => {
    render(<RoleTable />);
    page.rows = [];
    page.total = 0;
    await userEvent.type(screen.getByLabelText("Find grants"), "nobody");
    await waitFor(() => expect(screen.getByText("No grant matches that")).toBeInTheDocument());
    expect(screen.queryByText("Nobody has been given anything yet")).toBeNull();
  });
});
