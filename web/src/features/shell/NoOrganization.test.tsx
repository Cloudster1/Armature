import { beforeEach, describe, expect, it, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";

const switchOrg = vi.fn();
const logout = vi.fn();
const me = {
  organizations: [] as Array<{
    orgId: string;
    orgSlug: string;
    orgName: string;
    role: string;
  }>,
};

vi.mock("@tanstack/react-router", () => ({ useNavigate: () => vi.fn() }));
vi.mock("@/api/auth", () => ({
  useMe: () => ({
    data: {
      principal: { user: { id: "u1", name: "Bob" } },
      organizations: me.organizations,
    },
  }),
  useSwitchOrg: () => ({ mutate: switchOrg, isPending: false }),
  useLogout: () => ({ mutate: logout, isPending: false }),
}));

const { NoOrganization } = await import("./NoOrganization");

beforeEach(() => {
  switchOrg.mockReset();
  logout.mockReset();
});

describe("NoOrganization", () => {
  it("offers the organizations the person still belongs to", async () => {
    me.organizations = [
      { orgId: "o2", orgSlug: "other", orgName: "Other Corp", role: "member" },
    ];
    render(<NoOrganization />);
    expect(screen.getByText("Choose an organization")).toBeTruthy();
    await userEvent.click(screen.getByRole("button", { name: "Other Corp" }));
    expect(switchOrg).toHaveBeenCalledWith("other");
    await userEvent.click(screen.getByRole("button", { name: "Sign out" }));
    expect(logout).toHaveBeenCalled();
  });
});
