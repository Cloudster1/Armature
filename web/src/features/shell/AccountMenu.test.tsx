import { beforeEach, describe, expect, it, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { ConnectedTool } from "@/api/installation";

const state = { tools: [] as ConnectedTool[], admin: false };
const navigate = vi.fn();

vi.mock("@tanstack/react-router", () => ({ useNavigate: () => navigate }));
vi.mock("@/api/auth", () => ({
  useLogout: () => ({ mutate: vi.fn(), isPending: false }),
  useMe: () => ({ data: { principal: { user: { id: "u1", name: "Ada Lovelace", email: "ada@armature.test" }, role: state.admin ? "admin" : "member" } } }),
}));
vi.mock("@/api/access", () => ({ useAccess: () => ({ data: { canAdministerOrg: state.admin } }) }));
vi.mock("@/api/installation", () => ({ useConnectedTools: () => ({ data: state.tools }) }));

const { AccountMenu } = await import("./AccountMenu");

const stator: ConnectedTool = { product: "stator", name: "Stator", url: "https://stator.example.test" };
const exciter: ConnectedTool = { product: "exciter", name: "Exciter", url: "https://exciter.example.test" };

async function open() {
  render(<AccountMenu userName="Ada Lovelace" />);
  await userEvent.click(screen.getByRole("button", { name: "Your account" }));
  return screen.getByRole("menu", { name: "Your account" });
}

const PERSONAL = ["Your profile", "Notification settings", "Watching", "Themes", "Access tokens"];

beforeEach(() => {
  state.tools = [];
  state.admin = false;
  navigate.mockReset();
});

describe("the avatar menu's Switch to group", () => {
  it("lists both connected tools as plain links to their homes, under the account entries", async () => {
    state.tools = [stator, exciter];
    const menu = await open();
    expect(screen.getByText("Switch to")).toBeInTheDocument();
    for (const tool of [stator, exciter]) {
      const link = screen.getByRole("menuitem", { name: tool.name });
      expect(link.tagName).toBe("A");
      expect(link).toHaveAttribute("href", tool.url);
      expect(link).not.toHaveAttribute("target");
      expect(link.querySelector("svg")?.getAttribute("data-icon")).toBe(tool.product);
    }
    const words = Array.from(menu.querySelectorAll('[role="menuitem"]')).map((item) => item.textContent);
    expect(words).toEqual([...PERSONAL, "Stator", "Exciter", "Sign out"]);
  });

  it("lists only the one connected", async () => {
    state.tools = [exciter];
    await open();
    expect(screen.getByRole("menuitem", { name: "Exciter" })).toHaveAttribute("href", exciter.url);
    expect(screen.queryByRole("menuitem", { name: "Stator" })).toBeNull();
  });

  it("leaves the group out with none connected", async () => {
    await open();
    expect(screen.queryByText("Switch to")).toBeNull();
    expect(screen.getAllByRole("menuitem").map((item) => item.textContent)).toEqual([...PERSONAL, "Sign out"]);
  });
});

describe("the avatar menu", () => {
  it("heads itself with the person's name, address and seat", async () => {
    await open();
    const header = document.querySelector("[data-account-header]")!;
    expect(header.textContent).toContain("Ada Lovelace");
    expect(header.textContent).toContain("ada@armature.test");
    expect(header.querySelector("[data-account-role]")?.textContent).toBe("Member");
  });

  it("offers Admin to an administrator, after the other tools and before Sign out", async () => {
    state.admin = true;
    state.tools = [stator];
    await open();
    expect(screen.getAllByRole("menuitem").map((item) => item.textContent)).toEqual([...PERSONAL, "Stator", "Admin", "Sign out"]);
    await userEvent.click(screen.getByRole("menuitem", { name: "Admin" }));
    expect(navigate).toHaveBeenCalledWith({ to: "/admin" });
  });

  it("opens each personal page where it lives, the notification settings and Watching included", async () => {
    await open();
    await userEvent.click(screen.getByRole("menuitem", { name: "Notification settings" }));
    expect(navigate).toHaveBeenLastCalledWith({ to: "/settings/profile", search: undefined, hash: "notifications" });
    await userEvent.click(screen.getByRole("button", { name: "Your account" }));
    await userEvent.click(screen.getByRole("menuitem", { name: "Watching" }));
    expect(navigate).toHaveBeenLastCalledWith({ to: "/search", search: { q: "watcher = currentUser()" }, hash: undefined });
  });
});
