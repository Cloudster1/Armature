import { describe, expect, it, vi, beforeEach } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { ReactNode } from "react";

import { LoginForm, PASSWORD_MISMATCH, SignupForm, safeNext } from "./AuthForms";
import { ApiError } from "@/api/client";

// The router is only used for the post-login redirect, which is not what these
// tests are about.
const navigate = vi.fn();
vi.mock("@tanstack/react-router", () => ({
  Link: ({ children, to, className }: { children: ReactNode; to: string; className?: string }) => (
    <a href={to} className={className}>
      {children}
    </a>
  ),
  useNavigate: () => navigate,
}));

const login = vi.fn();
const signup = vi.fn();
vi.mock("@/api/auth", async () => {
  const actual = await vi.importActual<typeof import("@/api/auth")>("@/api/auth");
  return {
    ...actual,
    useLogin: () => login(),
    useSignup: () => signup(),
  };
});

const finish = vi.fn();
vi.mock("@/api/secondFactor", async () => {
  const actual = await vi.importActual<typeof import("@/api/secondFactor")>("@/api/secondFactor");
  return { ...actual, useLoginSecondFactor: () => finish() };
});

const me = vi.fn();
vi.mock("@/api/client", async () => {
  const actual = await vi.importActual<typeof import("@/api/client")>("@/api/client");
  return { ...actual, request: (path: string) => (path === "/auth/me" ? me() : Promise.reject(new Error(path))) };
});

function wrap(ui: ReactNode) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(<QueryClientProvider client={client}>{ui}</QueryClientProvider>);
}

function mockLogin(overrides: Partial<ReturnType<typeof baseLogin>> = {}) {
  login.mockReturnValue({ ...baseLogin(), ...overrides });
}

function baseLogin() {
  return { mutate: vi.fn(), isPending: false, error: null as unknown };
}

describe("LoginForm", () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  // An invitation sends somebody to sign in and expects them back on its page.
  it("goes back where it was sent from after signing in, and only within the app", async () => {
    const mutate = vi.fn((_input, options) => options?.onSuccess?.());
    mockLogin({ mutate });
    wrap(<LoginForm next="/invite" />);
    await userEvent.click(screen.getByRole("button", { name: "Sign in" }));
    await waitFor(() => expect(navigate).toHaveBeenCalledWith({ href: "/invite" }));

    expect(safeNext("//evil.example")).toBeUndefined();
    expect(safeNext("https://evil.example")).toBeUndefined();
    expect(safeNext("/projects")).toBe("/projects");
  });

  it("offers a way back in for somebody who forgot their password", () => {
    mockLogin();
    wrap(<LoginForm />);
    expect(screen.getByRole("link", { name: "Forgot your password?" }).getAttribute("href")).toBe("/forgot-password");
  });

  // The provider knew them and the tracker did not: the page says who can
  // change that, not just that it failed.
  it("tells somebody the provider vouched for that an administrator can let them in", () => {
    window.history.replaceState({}, "", "/login?sso=not_a_member");
    mockLogin({ mutate: vi.fn() });
    wrap(<LoginForm />);
    expect(screen.getByText(/nobody here has let you in yet/)).toBeTruthy();
    expect(screen.getByText(/Users page/)).toBeTruthy();
    window.history.replaceState({}, "", "/login");
  });

  // One installation: whatever an older server listed, there is nothing to choose.
  it("goes straight in without asking which organization", async () => {
    const mutate = vi.fn((_input, options) => options?.onSuccess?.());
    mockLogin({ mutate });
    me.mockResolvedValue({
      principal: { user: { id: "u1" }, org: { slug: "first" } },
      organizations: [
        { orgId: "o1", orgSlug: "first", orgName: "First Corp", role: "member" },
        { orgId: "o2", orgSlug: "second", orgName: "Second Corp", role: "admin" },
      ],
    });

    wrap(<LoginForm next="/issues/SEC-1" />);
    await userEvent.click(screen.getByRole("button", { name: "Sign in" }));
    await waitFor(() => expect(navigate).toHaveBeenCalledWith({ href: "/issues/SEC-1" }));
    expect(screen.queryByText("Which organization?")).toBeNull();
  });

  it("offers single sign-on as one link, with no organization to name", () => {
    mockLogin();
    wrap(<LoginForm next="/issues/SEC-1" />);
    const sso = screen.getByRole("link", { name: "Continue with single sign-on" });
    expect(sso.getAttribute("href")).toBe("/api/v1/auth/oidc/start?next=%2Fissues%2FSEC-1");
    expect(screen.queryByLabelText("Organization")).toBeNull();
  });

  // A password alone opens nothing for somebody with two-step sign-in: the
  // form asks for the code and signs in only once it is taken.
  it("asks for the app's code when the account has two-step sign-in", async () => {
    mockLogin({ mutate: vi.fn((_input, options) => options?.onSuccess?.({ challenge: "c1", expiresInSeconds: 300 })) });
    const finishing = vi.fn((_input, options) => options?.onSuccess?.());
    finish.mockReturnValue({ ...baseLogin(), mutate: finishing, reset: vi.fn() });

    wrap(<LoginForm />);
    await userEvent.click(screen.getByRole("button", { name: "Sign in" }));
    const code = await screen.findByLabelText("Code");
    expect(navigate).not.toHaveBeenCalled();
    expect(code).toHaveAttribute("autocomplete", "one-time-code");

    await userEvent.type(code, "123456");
    await userEvent.click(screen.getByRole("button", { name: "Verify" }));
    expect(finishing.mock.calls[0]?.[0]).toEqual({ challenge: "c1", code: "123456" });
    await waitFor(() => expect(navigate).toHaveBeenCalledWith({ to: "/" }));
  });

  it("takes a recovery code instead when the phone is not at hand", async () => {
    mockLogin({ mutate: vi.fn((_input, options) => options?.onSuccess?.({ challenge: "c1", expiresInSeconds: 300 })) });
    const finishing = vi.fn();
    finish.mockReturnValue({ ...baseLogin(), mutate: finishing, reset: vi.fn() });

    wrap(<LoginForm />);
    await userEvent.click(screen.getByRole("button", { name: "Sign in" }));
    await userEvent.click(await screen.findByRole("button", { name: "Use a recovery code" }));
    await userEvent.type(screen.getByLabelText("Recovery code"), "abcd-efgh-ijkl-mnop");
    await userEvent.click(screen.getByRole("button", { name: "Verify" }));
    expect(finishing.mock.calls[0]?.[0]).toEqual({ challenge: "c1", code: "abcd-efgh-ijkl-mnop" });
    expect(screen.getByRole("button", { name: "Use the code from your app" })).toBeInTheDocument();
  });

  it("goes back to the password once the code step has run out", async () => {
    const reset = vi.fn();
    mockLogin({ mutate: vi.fn((_input, options) => options?.onSuccess?.({ challenge: "c1", expiresInSeconds: 300 })), reset } as Partial<ReturnType<typeof baseLogin>>);
    finish.mockReturnValue({
      ...baseLogin(),
      reset: vi.fn(),
      error: new ApiError(410, { code: "challenge_invalid", message: "This sign-in has expired or had too many wrong codes. Sign in again with your password." }),
    });

    wrap(<LoginForm />);
    await userEvent.click(screen.getByRole("button", { name: "Sign in" }));
    expect(await screen.findByRole("alert")).toHaveTextContent("Sign in again with your password");
    await userEvent.click(screen.getByRole("button", { name: "Sign in again" }));
    expect(screen.getByLabelText("Password")).toHaveValue("");
    expect(reset).toHaveBeenCalled();
  });

  it("submits the credentials the user typed", async () => {
    const mutate = vi.fn();
    mockLogin({ mutate });

    wrap(<LoginForm />);
    await userEvent.type(screen.getByLabelText("Email"), "ada@example.com");
    await userEvent.type(screen.getByLabelText("Password"), "a long enough password");
    await userEvent.click(screen.getByRole("button", { name: "Sign in" }));

    await waitFor(() => expect(mutate).toHaveBeenCalledTimes(1));
    expect(mutate.mock.calls[0]?.[0]).toEqual({
      email: "ada@example.com",
      password: "a long enough password",
    });
  });

  it("shows the server's message when the credentials are rejected", () => {
    mockLogin({
      error: new ApiError(401, { code: "invalid_credentials", message: "invalid email or password" }),
    });

    wrap(<LoginForm />);
    expect(screen.getByRole("alert")).toHaveTextContent("invalid email or password");
  });

  it("attaches field level errors to their own field rather than the banner", () => {
    mockLogin({
      error: new ApiError(422, {
        code: "validation_failed",
        message: "Some fields need attention.",
        fields: { email: "That address is not valid." },
      }),
    });

    wrap(<LoginForm />);
    expect(screen.queryByRole("alert")).not.toBeInTheDocument();

    const email = screen.getByLabelText("Email");
    expect(email).toHaveAttribute("aria-invalid", "true");
    expect(screen.getByText("That address is not valid.")).toBeInTheDocument();
  });

  it("disables the button while the request is in flight", () => {
    mockLogin({ isPending: true });

    wrap(<LoginForm />);
    const button = screen.getByRole("button", { name: "Sign in" });
    expect(button).toBeDisabled();
    expect(button).toHaveAttribute("aria-busy", "true");
  });
});

function mockSignup(overrides: Partial<ReturnType<typeof baseLogin>> = {}) {
  signup.mockReturnValue({ ...baseLogin(), ...overrides });
}

describe("SignupForm", () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it("asks only for the person, with no organization to name", async () => {
    const mutate = vi.fn();
    mockSignup({ mutate });

    wrap(<SignupForm />);
    expect(screen.queryByLabelText(/organization/i)).toBeNull();
    await userEvent.type(screen.getByLabelText("Your name"), "Grace Hopper");
    await userEvent.type(screen.getByLabelText("Work email"), "grace@example.com");
    await userEvent.type(screen.getByLabelText("Password"), "compiler pioneer 1952");
    await userEvent.type(screen.getByLabelText("Repeat password"), "compiler pioneer 1952");
    await userEvent.click(screen.getByRole("button", { name: "Create your account" }));

    await waitFor(() => expect(mutate).toHaveBeenCalledTimes(1));
    expect(mutate.mock.calls[0]?.[0]).toEqual({
      name: "Grace Hopper",
      email: "grace@example.com",
      password: "compiler pioneer 1952",
    });
  });

  it("navigates into the app once the account exists", async () => {
    const mutate = vi.fn((_input, options) => options?.onSuccess?.());
    mockSignup({ mutate });

    wrap(<SignupForm />);
    await userEvent.type(screen.getByLabelText("Your name"), "A");
    await userEvent.click(screen.getByRole("button", { name: "Create your account" }));

    await waitFor(() => expect(navigate).toHaveBeenCalledWith({ to: "/" }));
  });

  it("sends nothing while the two passwords differ, and says so", async () => {
    const mutate = vi.fn();
    mockSignup({ mutate });

    wrap(<SignupForm />);
    await userEvent.type(screen.getByLabelText("Password"), "compiler pioneer 1952");
    await userEvent.type(screen.getByLabelText("Repeat password"), "compiler pioneer 1953");
    // Not said while the second copy is still being typed.
    expect(screen.queryByText(PASSWORD_MISMATCH)).toBeNull();
    await userEvent.click(screen.getByRole("button", { name: "Create your account" }));

    expect(screen.getByText(PASSWORD_MISMATCH)).toBeTruthy();
    expect(mutate).not.toHaveBeenCalled();

    await userEvent.clear(screen.getByLabelText("Repeat password"));
    await userEvent.type(screen.getByLabelText("Repeat password"), "compiler pioneer 1952");
    await userEvent.click(screen.getByRole("button", { name: "Create your account" }));
    await waitFor(() => expect(mutate).toHaveBeenCalledTimes(1));
  });

  it("surfaces a taken email against the form", () => {
    mockSignup({
      error: new ApiError(409, { code: "email_taken", message: "an account with that email already exists" }),
    });

    wrap(<SignupForm />);
    expect(screen.getByRole("alert")).toHaveTextContent("an account with that email already exists");
  });

  it("tells the browser the password minimum before the request is made", () => {
    mockSignup();
    wrap(<SignupForm />);
    expect(screen.getByLabelText("Password")).toHaveAttribute("minLength", "12");
  });

  // Autocomplete hints are what let a password manager offer to save the new
  // credentials, so they are worth asserting rather than assuming.
  it("marks the fields up for password managers", () => {
    mockSignup();
    wrap(<SignupForm />);
    expect(screen.getByLabelText("Work email")).toHaveAttribute("autocomplete", "username");
    expect(screen.getByLabelText("Password")).toHaveAttribute("autocomplete", "new-password");
    expect(screen.getByLabelText("Repeat password")).toHaveAttribute("autocomplete", "new-password");
  });
});
