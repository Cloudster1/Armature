import { describe, expect, it, vi } from "vitest";
import { render, screen } from "@testing-library/react";

const members = [
  { id: "u-ada", name: "Ada Lovelace", email: "ada@armature.test", role: "owner" },
  { id: "u-bob", name: "Bob Builder", email: "bob@armature.test", role: "member" },
];

vi.mock("@/api/issues", () => ({ useMembers: () => ({ data: { members }, isLoading: false }) }));
vi.mock("@/api/auth", () => ({
  useMe: () => ({ data: { principal: { user: { id: "u-ada", name: "Ada Lovelace" }, role: "owner" } } }),
}));
vi.mock("./InvitePanel", () => ({ InvitePanel: () => null }));
vi.mock("@/features/shell/ConfirmProvider", () => ({ useConfirm: () => async () => true }));
vi.mock("@/components/ui", async () => {
  const actual = await vi.importActual<typeof import("@/components/ui")>("@/components/ui");
  return { ...actual, useToast: () => ({ success: vi.fn(), error: vi.fn(), info: vi.fn() }) };
});

const { MemberList } = await import("./MemberList");

describe("MemberList", () => {
  // Letting somebody go switches their account off, which the users page does
  // with the rest of what an account is; this list only shows who is here.
  it("lists everyone, and takes nobody out of an organization", () => {
    render(<MemberList />);
    expect(screen.getByText("bob@armature.test")).toBeTruthy();
    expect(screen.queryByRole("button", { name: /Remove/ })).toBeNull();
    expect(screen.queryByText(/organization/i)).toBeNull();
  });
});
