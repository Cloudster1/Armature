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
  useNavigate: () => navigate,
}));

const login = vi.fn();
const signup = vi.fn();
const switchOrg = vi.fn(() => Promise.resolve({}));
vi.mock("@/api/auth", async () => {
  const actual = await vi.importActual<typeof import("@/api/auth")>("@/api/auth");
  return {
    ...actual,
    useLogin: () => login(),
    useSignup: () => signup(),
    useSwitchOrg: () => ({ mutateAsync: switchOrg, isPending: false, error: null }),
  };
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
    expect(navigate).toHaveBeenCalledWith({ href: "/invite" });

    expect(safeNext("//evil.example")).toBeUndefined();
    expect(safeNext("https://evil.example")).toBeUndefined();
    expect(safeNext("/projects")).toBe("/projects");
  });

  it("asks somebody in several organizations which one they came for", async () => {
    const mutate = vi.fn((_input, options) => options?.onSuccess?.());
    mockLogin({ mutate });
    me.mockResolvedValue({
      principal: { user: { id: "u1" }, org: { slug: "first" } },
      organizations: [
        { orgId: "o1", orgSlug: "first", orgName: "First Corp", role: "member" },
        { orgId: "o2", orgSlug: "second", orgName: "Second Corp", role: "admin" },
      ],
    });

    wrap(<LoginForm />);
    await userEvent.click(screen.getByRole("button", { name: "Sign in" }));
    await screen.findByText("Which organization?");
    expect(navigate).not.toHaveBeenCalled();

    await userEvent.click(screen.getByRole("button", { name: /Second Corp/ }));
    expect(switchOrg).toHaveBeenCalledWith("second");
    await waitFor(() => expect(navigate).toHaveBeenCalledWith({ to: "/" }));
  });

  it("goes straight in with only one organization", async () => {
    const mutate = vi.fn((_input, options) => options?.onSuccess?.());
    mockLogin({ mutate });
    me.mockResolvedValue({ principal: { user: { id: "u1" }, org: { slug: "only" } }, organizations: [{ orgId: "o1", orgSlug: "only", orgName: "Only", role: "owner" }] });

    wrap(<LoginForm />);
    await userEvent.click(screen.getByRole("button", { name: "Sign in" }));
    await waitFor(() => expect(navigate).toHaveBeenCalledWith({ to: "/" }));
    expect(screen.queryByText("Which organization?")).toBeNull();
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

  it("submits every field the API needs to create an organization", async () => {
    const mutate = vi.fn();
    mockSignup({ mutate });

    wrap(<SignupForm />);
    await userEvent.type(screen.getByLabelText("Your name"), "Grace Hopper");
    await userEvent.type(screen.getByLabelText("Work email"), "grace@example.com");
    await userEvent.type(screen.getByLabelText("Password"), "compiler pioneer 1952");
    await userEvent.type(screen.getByLabelText("Repeat password"), "compiler pioneer 1952");
    await userEvent.type(screen.getByLabelText("Organization name"), "Remington Rand");
    await userEvent.click(screen.getByRole("button", { name: "Create your organization" }));

    await waitFor(() => expect(mutate).toHaveBeenCalledTimes(1));
    expect(mutate.mock.calls[0]?.[0]).toEqual({
      name: "Grace Hopper",
      email: "grace@example.com",
      password: "compiler pioneer 1952",
      orgName: "Remington Rand",
    });
  });

  it("navigates into the app once the account exists", async () => {
    const mutate = vi.fn((_input, options) => options?.onSuccess?.());
    mockSignup({ mutate });

    wrap(<SignupForm />);
    await userEvent.type(screen.getByLabelText("Your name"), "A");
    await userEvent.click(screen.getByRole("button", { name: "Create your organization" }));

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
    await userEvent.click(screen.getByRole("button", { name: "Create your organization" }));

    expect(screen.getByText(PASSWORD_MISMATCH)).toBeTruthy();
    expect(mutate).not.toHaveBeenCalled();

    await userEvent.clear(screen.getByLabelText("Repeat password"));
    await userEvent.type(screen.getByLabelText("Repeat password"), "compiler pioneer 1952");
    await userEvent.click(screen.getByRole("button", { name: "Create your organization" }));
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
