import { describe, expect, it, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";

const members = [
  { id: "u-ada", name: "Ada Lovelace", email: "ada@armature.test", role: "owner" },
  { id: "u-bob", name: "Bob Builder", email: "bob@armature.test", role: "member" },
];

const asked: unknown[] = [];
vi.mock("@/api/people", () => ({
  useMembers: (params: unknown) => {
    asked.push(params);
    return { data: { members, total: 120, limit: 50, offset: 50 }, isLoading: false };
  },
}));
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
    render(<MemberList show={() => {}} />);
    expect(screen.getByText("bob@armature.test")).toBeTruthy();
    expect(screen.queryByRole("button", { name: /Remove/ })).toBeNull();
    expect(screen.queryByText(/organization/i)).toBeNull();
  });

  it("asks for one page of what was searched, and says how many matched", async () => {
    const show = vi.fn();
    render(<MemberList q="ada" offset={50} show={show} />);
    expect(asked.at(-1)).toEqual({ q: "ada", offset: 50 });
    expect(screen.getByText("120 members")).toBeTruthy();
    expect(screen.getByText("51 to 100 of 120")).toBeTruthy();
    await userEvent.click(screen.getByRole("button", { name: "Next page" }));
    expect(show).toHaveBeenCalledWith({ q: "ada", offset: 100 });
  });
});
