import { beforeEach, describe, expect, it, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";

const create = vi.fn();
const withdraw = vi.fn();
const pending = { invites: [] as Array<{ id: string; email: string; role: string; expiresAt: string; createdAt: string }> };

vi.mock("@/api/invites", () => ({
  useCreateInvite: () => ({ mutate: create, isPending: false, error: null }),
  useInvites: () => ({ data: pending }),
  useWithdrawInvite: () => ({ mutate: withdraw, isPending: false, error: null }),
}));
vi.mock("@/features/shell/ConfirmProvider", () => ({ useConfirm: () => async () => true }));
vi.mock("@/components/ui", async () => {
  const actual = await vi.importActual<typeof import("@/components/ui")>("@/components/ui");
  return { ...actual, useToast: () => ({ success: vi.fn(), error: vi.fn(), info: vi.fn() }) };
});

const { InvitePanel } = await import("./InvitePanel");

beforeEach(() => {
  create.mockReset();
  withdraw.mockReset();
  pending.invites = [];
});

describe("InvitePanel", () => {
  it("invites the address as the chosen role", async () => {
    render(<InvitePanel />);
    await userEvent.type(screen.getByLabelText("Invite by email"), "grace@example.com");
    await userEvent.selectOptions(screen.getByLabelText("As"), "admin");
    await userEvent.click(screen.getByRole("button", { name: "Send invitation" }));
    expect(create).toHaveBeenCalledWith({ email: "grace@example.com", role: "admin" }, expect.anything());
  });

  // Without mail the link on screen is the only way the invitation goes anywhere.
  it("shows the link once, and says plainly when nothing was mailed", async () => {
    create.mockImplementation((_input, options) =>
      options.onSuccess({ invite: { id: "i1", email: "grace@example.com", role: "member" }, link: "https://app.test/invite#secret", mailed: false }),
    );
    render(<InvitePanel />);
    await userEvent.type(screen.getByLabelText("Invite by email"), "grace@example.com");
    await userEvent.click(screen.getByRole("button", { name: "Send invitation" }));

    expect(screen.getByText("Send this link to grace@example.com")).toBeTruthy();
    expect(screen.getByText(/Mail is not set up here/)).toBeTruthy();
    expect(screen.getByText("https://app.test/invite#secret")).toBeTruthy();
    await userEvent.click(screen.getByRole("button", { name: "Done" }));
    expect(screen.queryByText("https://app.test/invite#secret")).toBeNull();
  });

  it("says the invitation was mailed when it was", async () => {
    create.mockImplementation((_input, options) =>
      options.onSuccess({ invite: { id: "i1", email: "grace@example.com", role: "member" }, link: "https://app.test/invite#secret", mailed: true }),
    );
    render(<InvitePanel />);
    await userEvent.type(screen.getByLabelText("Invite by email"), "grace@example.com");
    await userEvent.click(screen.getByRole("button", { name: "Send invitation" }));
    expect(screen.getByText("Invitation mailed to grace@example.com")).toBeTruthy();
  });

  it("lists invitations waiting for an answer and withdraws one", async () => {
    pending.invites = [{ id: "i9", email: "late@example.com", role: "customer", expiresAt: "2030-01-01T00:00:00Z", createdAt: "2029-12-25T00:00:00Z" }];
    render(<InvitePanel />);
    expect(screen.getByText("late@example.com")).toBeTruthy();
    await userEvent.click(screen.getByRole("button", { name: "Withdraw the invitation for late@example.com" }));
    expect(withdraw).toHaveBeenCalledWith("i9");
  });
});
