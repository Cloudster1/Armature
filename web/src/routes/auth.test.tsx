import { describe, expect, it, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import type { ReactNode } from "react";

const signup = { data: undefined as { open: boolean; first: boolean } | undefined };

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
    signup.data = { open: true, first: false };
    render(<SignupOffer />);
    expect(
      screen.getByRole("link", { name: "Create one" }).getAttribute("href"),
    ).toBe("/signup");
  });

  it("points at an invitation instead when it is closed", () => {
    signup.data = { open: false, first: false };
    render(<SignupOffer />);
    expect(screen.queryByRole("link", { name: "Create one" })).toBeNull();
    expect(screen.getByText(/invite you/)).toBeTruthy();
  });

  it("says the first account sets the installation up and administers it", () => {
    signup.data = { open: true, first: true };
    render(<SignupPage />);
    expect(screen.getByText("Set up Armature")).toBeTruthy();
    expect(screen.getByText(/administer/)).toBeTruthy();
    expect(document.querySelector("[data-signup-form]")).not.toBeNull();
  });

  it("says a later account joins as a member, with no organization to make", () => {
    signup.data = { open: true, first: false };
    render(<SignupPage />);
    expect(screen.getByText("Create your account")).toBeTruthy();
    expect(screen.getByText(/as a member/)).toBeTruthy();
    expect(screen.queryByText(/organization/i)).toBeNull();
  });

  it("points at an administrator, not an organization, when it is closed", () => {
    signup.data = { open: false, first: false };
    render(<SignupOffer />);
    expect(screen.getByText("No account yet? Ask an administrator to invite you.")).toBeTruthy();
  });

  it("does not show the form on the sign-up page when it is closed", () => {
    signup.data = { open: false, first: false };
    render(<SignupPage />);
    expect(screen.getByText("Sign-up is closed")).toBeTruthy();
    expect(document.querySelector("[data-signup-form]")).toBeNull();
  });
});

describe("the door", () => {
  it("shows the logo above the product's name, on the backdrop the app wears", () => {
    signup.data = { open: true, first: false };
    render(<SignupPage />);
    const logo = document.querySelector("img[data-logo]")!;
    expect(logo.getAttribute("srcset")).toContain("/logo-512.webp 512w");
    expect(logo.getAttribute("alt")).toBe("");
    expect(logo.closest("[data-logo-head]")!.textContent).toBe("Armature");
    expect(logo.closest("[data-door]")!.className).toContain("bg-backdrop");
  });
});
