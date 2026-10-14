import { describe, expect, it, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import type { ReactNode } from "react";

let principal: Record<string, unknown>;

vi.mock("@tanstack/react-router", async (actual) => ({
  ...(await actual<typeof import("@tanstack/react-router")>()),
  createRoute: (options: unknown) => options,
}));
vi.mock("./root", () => ({ rootRoute: {} }));
vi.mock("@/api/auth", async (actual) => ({
  ...(await actual<typeof import("@/api/auth")>()),
  useMe: () => ({ data: { principal } }),
}));
vi.mock("@/features/auth/SecondFactor", () => ({ EnrolmentRequired: () => <p>held to setting it up</p> }));

const { appRoute } = await import("./app");
const AppShell = (appRoute as unknown as { component: () => ReactNode }).component;

describe("the signed-in shell", () => {
  // A held session is refused every page, so the shell shows the setup instead
  // of a sidebar full of requests that would all come back refused.
  it("shows only the setup to a session the installation holds", () => {
    principal = { user: { id: "u1" }, installation: { id: "o1", name: "Strict" }, role: "member", mustEnrol: true };
    render(<AppShell />);
    expect(screen.getByText("held to setting it up")).toBeInTheDocument();
  });
});
