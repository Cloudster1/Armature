import { beforeEach, describe, expect, it, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { RoleMatrix } from "./RoleMatrix";

const update = vi.fn();
const remove = vi.fn();
const roles = vi.fn();
vi.mock("@/api/access", () => ({
  useRoles: () => ({ data: { roles: roles() }, isLoading: false, error: null }),
  usePermissions: () => ({
    data: {
      permissions: [
        { permission: "read", words: "See projects" },
        { permission: "sprint.manage", words: "Plan sprints" },
        { permission: "org.administer", words: "Administer the organization" },
      ],
    },
  }),
  useAccess: () => ({ data: { canAdministerOrg: true } }),
  useUpdateRole: () => ({ mutate: update, isPending: false, error: null }),
  useDeleteRole: () => ({ mutate: remove, isPending: false, error: null }),
  useCreateRole: () => ({ mutate: vi.fn(), isPending: false, error: null }),
}));
vi.mock("@/features/shell/ConfirmProvider", () => ({ useConfirm: () => () => Promise.resolve(true) }));
vi.mock("@/components/ui", async () => {
  const actual = await vi.importActual<typeof import("@/components/ui")>("@/components/ui");
  return { ...actual, useToast: () => ({ success: vi.fn(), info: vi.fn(), error: vi.fn() }) };
});

const admin = { role: "global_administrator", name: "Global administrator", description: "", orgWideOnly: true, builtin: true, permissions: ["read", "sprint.manage", "org.administer"], inUse: 1 };
const planner = { role: "sprint_planner", name: "Sprint planner", description: "Plans.", orgWideOnly: false, builtin: false, permissions: ["read"], inUse: 2 };

describe("the role matrix", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    roles.mockReturnValue([admin, planner]);
  });

  it("draws a box at every crossing and saves the whole list when one is ticked", async () => {
    render(<RoleMatrix />);
    const cell = screen.getByLabelText("Sprint planner may plan sprints") as HTMLInputElement;
    expect(cell.checked).toBe(false);
    await userEvent.click(cell);
    expect(update).toHaveBeenCalledWith({ role: "sprint_planner", permissions: ["read", "sprint.manage"] });

    await userEvent.click(screen.getByLabelText("Global administrator may plan sprints"));
    expect(update).toHaveBeenLastCalledWith({ role: "global_administrator", permissions: ["read", "org.administer"] });
  });

  it("never lets the global administrator stop administering", () => {
    render(<RoleMatrix />);
    const fixed = screen.getByLabelText("Global administrator may administer the organization") as HTMLInputElement;
    expect(fixed.checked).toBe(true);
    expect(fixed.disabled).toBe(true);
  });

  it("deletes a role of the organization's own after asking, and never a built-in one", async () => {
    render(<RoleMatrix />);
    await userEvent.click(screen.getByRole("button", { name: "Actions for Sprint planner" }));
    await userEvent.click(screen.getByRole("menuitem", { name: "Delete" }));
    expect(remove).toHaveBeenCalledWith("sprint_planner", expect.anything());

    await userEvent.click(screen.getByRole("button", { name: "Actions for Global administrator" }));
    expect((screen.getByRole("menuitem", { name: "Delete" }) as HTMLButtonElement).disabled).toBe(true);
  });
});
