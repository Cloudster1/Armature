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
  useMe: () => ({ data: { principal, organizations: [] } }),
}));
vi.mock("@/features/auth/SecondFactor", () => ({ EnrolmentRequired: () => <p>held to setting it up</p> }));
vi.mock("@/features/shell/NoOrganization", () => ({ NoOrganization: () => <p>no organization</p> }));

const { appRoute } = await import("./app");
const AppShell = (appRoute as unknown as { component: () => ReactNode }).component;

describe("the signed-in shell", () => {
  // A held session is refused every page, so the shell shows the setup instead
  // of a sidebar full of requests that would all come back refused.
  it("shows only the setup to a session its organization holds", () => {
    principal = { user: { id: "u1" }, org: { id: "o1", slug: "strict", name: "Strict" }, role: "member", mustEnrol: true };
    render(<AppShell />);
    expect(screen.getByText("held to setting it up")).toBeInTheDocument();
  });

  it("puts somebody with no organization in front of the choice first", () => {
    principal = { user: { id: "u1" }, mustEnrol: false };
    render(<AppShell />);
    expect(screen.getByText("no organization")).toBeInTheDocument();
    expect(screen.queryByText("held to setting it up")).toBeNull();
  });
});
