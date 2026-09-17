import { describe, expect, it, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";

const remove = vi.fn();
const members = [
  { id: "u-ada", name: "Ada Lovelace", email: "ada@armature.test", role: "owner" },
  { id: "u-bob", name: "Bob Builder", email: "bob@armature.test", role: "member" },
];

vi.mock("@/api/issues", () => ({ useMembers: () => ({ data: { members }, isLoading: false }) }));
vi.mock("@/api/auth", () => ({
  useMe: () => ({ data: { principal: { user: { id: "u-ada", name: "Ada Lovelace" }, role: "owner" } } }),
  useRemoveMember: () => ({ mutate: remove, isPending: false }),
}));
vi.mock("./InvitePanel", () => ({ InvitePanel: () => null }));
vi.mock("@/features/shell/ConfirmProvider", () => ({ useConfirm: () => async () => true }));
vi.mock("@/components/ui", async () => {
  const actual = await vi.importActual<typeof import("@/components/ui")>("@/components/ui");
  return { ...actual, useToast: () => ({ success: vi.fn(), error: vi.fn(), info: vi.fn() }) };
});

const { MemberList } = await import("./MemberList");

describe("MemberList", () => {
  // The person looking cannot remove themselves; the last owner is refused by
  // the server, but the button on your own row would only ever say so.
  it("lists everyone and lets an administrator remove anyone but themselves", async () => {
    render(<MemberList />);
    expect(screen.getByText("bob@armature.test")).toBeTruthy();
    expect(screen.getByRole("button", { name: "Remove Ada Lovelace from the organization" })).toHaveProperty("disabled", true);
    await userEvent.click(screen.getByRole("button", { name: "Remove Bob Builder from the organization" }));
    expect(remove).toHaveBeenCalledWith("u-bob", expect.anything());
  });
});
