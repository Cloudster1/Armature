import { beforeEach, describe, expect, it, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { ReactNode } from "react";

const remove = vi.fn(() => Promise.resolve());
const principal = { role: "owner", org: { id: "o1", name: "Acme", slug: "acme" } };

vi.mock("@tanstack/react-router", () => ({
  Link: ({ children, to }: { children: ReactNode; to: string }) => <a href={to}>{children}</a>,
  createRoute: (options: unknown) => options,
  useNavigate: () => vi.fn(),
}));
vi.mock("./app", () => ({ appRoute: {} }));
vi.mock("@/api/auth", () => ({
  useMe: () => ({ data: { principal } }),
  useDeleteOrganization: () => ({ mutateAsync: remove, isPending: false, error: null }),
}));

const { organizationRoute } = await import("./organization");
const OrganizationPage = (organizationRoute as unknown as { component: () => ReactNode }).component;

beforeEach(() => {
  remove.mockClear();
  principal.role = "owner";
});

describe("the organization page", () => {
  it("deletes only once the owner has typed the address back", async () => {
    render(<OrganizationPage />);
    await userEvent.click(screen.getByRole("button", { name: "Delete organization" }));
    const confirmButton = screen.getByRole("button", { name: "Delete everything" });
    expect(confirmButton).toHaveProperty("disabled", true);

    await userEvent.type(screen.getByLabelText("Type acme to confirm"), "Acme");
    expect(confirmButton).toHaveProperty("disabled", true);

    await userEvent.clear(screen.getByLabelText("Type acme to confirm"));
    await userEvent.type(screen.getByLabelText("Type acme to confirm"), "acme");
    await userEvent.click(screen.getByRole("button", { name: "Delete everything" }));
    expect(remove).toHaveBeenCalledWith("acme");
  });

  it("does not offer deleting to somebody who does not own it", () => {
    principal.role = "admin";
    render(<OrganizationPage />);
    expect(screen.queryByRole("button", { name: "Delete organization" })).toBeNull();
    expect(screen.getByText("Only an owner of the organization can delete it.")).toBeTruthy();
  });
});
