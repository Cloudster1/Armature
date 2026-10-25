import { beforeEach, describe, expect, it, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { ReactNode } from "react";
import { ApiError } from "@/api/client";
import { passwordMismatch } from "@/features/auth/AuthForms";
import { PASSWORD_RESET_TOKEN_KEY } from "@/config";

const navigate = vi.fn();
const ask = vi.fn();
const reset = vi.fn();
const state = { askError: null as unknown, resetError: null as unknown };

vi.mock("@tanstack/react-router", () => ({
  Link: ({ children, to }: { children: ReactNode; to: string }) => <a href={to}>{children}</a>,
  createRoute: (options: unknown) => options,
  redirect: vi.fn(),
  useNavigate: () => navigate,
}));
vi.mock("./root", () => ({ rootRoute: {} }));
vi.mock("./auth", () => ({
  redirectIfSignedIn: vi.fn(),
  AuthLayout: ({ title, children }: { title: string; children: ReactNode }) => (
    <div>
      <h1>{title}</h1>
      {children}
    </div>
  ),
}));
vi.mock("@/api/auth", () => ({
  useRequestPasswordReset: () => ({ mutate: ask, isPending: false, error: state.askError }),
  useResetPassword: () => ({ mutate: reset, isPending: false, error: state.resetError }),
}));

const { ForgotPasswordForm, resetLinkSent, readResetToken, resetPasswordRoute } = await import("./password-reset");
const ResetPasswordPage = (resetPasswordRoute as unknown as { component: () => ReactNode }).component;

beforeEach(() => {
  sessionStorage.clear();
  window.history.replaceState(null, "", "/reset-password#the-secret");
  navigate.mockReset();
  ask.mockReset();
  reset.mockReset();
  state.askError = null;
  state.resetError = null;
});

describe("asking for a password link", () => {
  // Known or not, the page says the same, or it would tell who has an account.
  it("gives one answer whatever the address", async () => {
    ask.mockImplementation((_email: string, options?: { onSuccess?: () => void }) => options?.onSuccess?.());
    render(<ForgotPasswordForm />);
    await userEvent.type(screen.getByLabelText("Email"), "nobody@example.com");
    await userEvent.click(screen.getByRole("button", { name: "Send me a link" }));

    expect(ask).toHaveBeenCalledWith("nobody@example.com", expect.anything());
    expect(screen.getByText(resetLinkSent())).toBeTruthy();
    expect(screen.queryByLabelText("Email")).toBeNull();
  });

  it("says what the server refused, such as mail not being set up", () => {
    state.askError = new ApiError(503, { code: "mail_unavailable", message: "Mail is not set up on this server." });
    render(<ForgotPasswordForm />);
    expect(screen.getByText("Mail is not set up on this server.")).toBeTruthy();
  });
});

describe("the page a password link opens", () => {
  it("takes the secret off the address bar and keeps it for a reload", () => {
    expect(readResetToken()).toBe("the-secret");
    expect(window.location.hash).toBe("");
    expect(sessionStorage.getItem(PASSWORD_RESET_TOKEN_KEY)).toBe("the-secret");
    expect(readResetToken()).toBe("the-secret");
  });

  it("sets the password and sends the person to sign in with it", async () => {
    reset.mockImplementation((_input: unknown, options?: { onSuccess?: () => void }) => options?.onSuccess?.());
    render(<ResetPasswordPage />);
    await userEvent.type(screen.getByLabelText("Password"), "a brand new adequate one");
    await userEvent.type(screen.getByLabelText("Repeat password"), "a brand new adequate one");
    await userEvent.click(screen.getByRole("button", { name: "Set new password" }));

    expect(reset).toHaveBeenCalledWith({ token: "the-secret", password: "a brand new adequate one" }, expect.anything());
    expect(navigate).toHaveBeenCalledWith({ to: "/login", search: { reset: "done" } });
    expect(sessionStorage.getItem(PASSWORD_RESET_TOKEN_KEY)).toBeNull();
  });

  it("sends nothing while the two passwords differ", async () => {
    render(<ResetPasswordPage />);
    await userEvent.type(screen.getByLabelText("Password"), "a brand new adequate one");
    await userEvent.type(screen.getByLabelText("Repeat password"), "a brand new adequate two");
    await userEvent.click(screen.getByRole("button", { name: "Set new password" }));

    expect(reset).not.toHaveBeenCalled();
    expect(screen.getByText(passwordMismatch())).toBeTruthy();
  });

  it("shows why a short password was refused", () => {
    state.resetError = new ApiError(422, { code: "validation_failed", message: "Password must be at least 12 characters." });
    render(<ResetPasswordPage />);
    expect(screen.getByText("Password must be at least 12 characters.")).toBeTruthy();
    expect(screen.getByRole("button", { name: "Set new password" })).toBeTruthy();
  });

  it("says a used or expired link is gone and where to get a new one", () => {
    const sentence = "This link has expired or was already used. Ask for a new one from the sign-in page.";
    state.resetError = new ApiError(410, { code: "reset_link_invalid", message: sentence });
    render(<ResetPasswordPage />);
    expect(screen.getByText(sentence)).toBeTruthy();
    expect(screen.getByRole("link", { name: "Ask for a new link" }).getAttribute("href")).toBe("/forgot-password");
    expect(screen.queryByRole("button", { name: "Set new password" })).toBeNull();
  });

  it("says when the link lost its secret on the way", () => {
    window.history.replaceState(null, "", "/reset-password");
    render(<ResetPasswordPage />);
    expect(screen.getByText("This link is incomplete")).toBeTruthy();
  });
});
