import { beforeEach, describe, expect, it, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { ReactNode } from "react";
import { ApiError } from "@/api/client";

const navigate = vi.fn();
const accept = vi.fn(() => Promise.resolve({}));
const state = {
  preview: { data: undefined as unknown, error: null as unknown },
  me: { isLoading: false, data: undefined as unknown },
  acceptError: null as unknown,
};

vi.mock("@tanstack/react-router", () => ({
  Link: ({ children, to }: { children: ReactNode; to: string }) => <a href={to}>{children}</a>,
  createRoute: (options: unknown) => options,
  redirect: vi.fn(),
  useNavigate: () => navigate,
}));
vi.mock("./root", () => ({ rootRoute: {} }));
vi.mock("./auth", () => ({
  AuthLayout: ({ title, subtitle, children }: { title: string; subtitle?: string; children: ReactNode }) => (
    <div>
      <h1>{title}</h1>
      {subtitle && <p>{subtitle}</p>}
      {children}
    </div>
  ),
}));
vi.mock("@/api/auth", () => ({ useMe: () => state.me, useLogout: () => ({ mutate: vi.fn(), isPending: false }) }));
vi.mock("@/api/invites", () => ({
  useInvitePreview: () => state.preview,
  useAcceptInvite: () => ({ mutateAsync: accept, isPending: false, error: state.acceptError }),
}));

const { inviteRoute, readInviteToken, INVITE_TOKEN_KEY } = await import("./invite");
const InvitePage = (inviteRoute as unknown as { component: () => ReactNode }).component;
const invite = { orgName: "Acme", email: "grace@example.com", role: "member" };

beforeEach(() => {
  sessionStorage.clear();
  window.history.replaceState(null, "", "/invite#the-secret");
  navigate.mockReset();
  accept.mockReset();
  accept.mockImplementation(() => Promise.resolve({}));
  state.preview = { data: { invite }, error: null };
  state.me = { isLoading: false, data: undefined };
  state.acceptError = null;
});

describe("the invitation page", () => {
  it("takes the secret off the address bar and keeps it for after a sign-in", () => {
    expect(readInviteToken()).toBe("the-secret");
    expect(window.location.hash).toBe("");
    expect(sessionStorage.getItem(INVITE_TOKEN_KEY)).toBe("the-secret");
    expect(readInviteToken()).toBe("the-secret");
  });

  it("makes a new account with the name and the password typed twice", async () => {
    render(<InvitePage />);
    expect(screen.getByRole("heading", { name: "Join Acme" })).toBeTruthy();
    await userEvent.type(screen.getByLabelText("Your name"), "Grace Hopper");
    await userEvent.type(screen.getByLabelText("Password"), "compiler pioneer 1952");
    await userEvent.type(screen.getByLabelText("Repeat password"), "compiler pioneer 1952");
    await userEvent.click(screen.getByRole("button", { name: "Join Acme" }));

    expect(accept).toHaveBeenCalledWith({ token: "the-secret", name: "Grace Hopper", password: "compiler pioneer 1952" });
    await Promise.resolve();
    expect(sessionStorage.getItem(INVITE_TOKEN_KEY)).toBeNull();
    expect(navigate).toHaveBeenCalledWith({ to: "/" });
  });

  it("sends nothing while the two passwords differ", async () => {
    render(<InvitePage />);
    await userEvent.type(screen.getByLabelText("Your name"), "Grace Hopper");
    await userEvent.type(screen.getByLabelText("Password"), "compiler pioneer 1952");
    await userEvent.type(screen.getByLabelText("Repeat password"), "compiler pioneer 1953");
    await userEvent.click(screen.getByRole("button", { name: "Join Acme" }));
    expect(accept).not.toHaveBeenCalled();
  });

  it("lets the right person who is already signed in join with one press", async () => {
    state.me = { isLoading: false, data: { principal: { user: { email: "Grace@example.com" } } } };
    render(<InvitePage />);
    await userEvent.click(screen.getByRole("button", { name: "Join Acme" }));
    expect(accept).toHaveBeenCalledWith({ token: "the-secret" });
  });

  it("tells somebody signed in with another address to sign out first", () => {
    state.me = { isLoading: false, data: { principal: { user: { email: "someone@else.test" } } } };
    render(<InvitePage />);
    expect(screen.getByText(/this invitation is for grace@example.com/)).toBeTruthy();
    expect(screen.queryByRole("button", { name: "Join Acme" })).toBeNull();
  });

  it("sends an address that already has an account to sign in", () => {
    state.acceptError = new ApiError(409, { code: "sign_in_to_accept", message: "An account already uses this address." });
    render(<InvitePage />);
    expect(screen.getByRole("button", { name: "Sign in to accept" })).toBeTruthy();
  });

  it("says when the invitation is used up", () => {
    state.preview = { data: undefined, error: new ApiError(410, { code: "invite_invalid", message: "invitation is invalid or has expired" }) };
    render(<InvitePage />);
    expect(screen.getByText("This invitation cannot be used")).toBeTruthy();
    expect(screen.getByText(/Ask whoever invited you for a new one/)).toBeTruthy();
  });
});
