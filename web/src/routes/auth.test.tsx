import { describe, expect, it, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import type { ReactNode } from "react";

const signup = { data: undefined as { open: boolean } | undefined };

vi.mock("@tanstack/react-router", () => ({
  Link: ({ children, to }: { children: ReactNode; to: string }) => (
    <a href={to}>{children}</a>
  ),
  createRoute: (options: unknown) => options,
  redirect: vi.fn(),
}));
vi.mock("./root", () => ({ rootRoute: {} }));
vi.mock("@/features/auth/AuthForms", () => ({
  LoginForm: () => null,
  SignupForm: () => <form data-signup-form />,
}));
vi.mock("@/api/client", () => ({ request: vi.fn() }));
vi.mock("@/api/auth", () => ({
  meQueryKey: ["auth", "me"],
  useSignupOpen: () => signup,
}));

const { SignupOffer, signupRoute } = await import("./auth");
const SignupPage = (signupRoute as unknown as { component: () => ReactNode })
  .component;

describe("sign-up where the installation allows it", () => {
  it("offers creating an account when sign-up is open", () => {
    signup.data = { open: true };
    render(<SignupOffer />);
    expect(
      screen.getByRole("link", { name: "Create one" }).getAttribute("href"),
    ).toBe("/signup");
  });

  it("points at an invitation instead when it is closed", () => {
    signup.data = { open: false };
    render(<SignupOffer />);
    expect(screen.queryByRole("link", { name: "Create one" })).toBeNull();
    expect(screen.getByText(/invite you/)).toBeTruthy();
  });

  it("does not show the form on the sign-up page when it is closed", () => {
    signup.data = { open: false };
    render(<SignupPage />);
    expect(screen.getByText("Sign-up is closed")).toBeTruthy();
    expect(document.querySelector("[data-signup-form]")).toBeNull();
  });
});
