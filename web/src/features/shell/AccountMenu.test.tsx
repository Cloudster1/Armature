import { beforeEach, describe, expect, it, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { ConnectedTool } from "@/api/installation";

const state = { tools: [] as ConnectedTool[] };
const navigate = vi.fn();

vi.mock("@tanstack/react-router", () => ({ useNavigate: () => navigate }));
vi.mock("@/api/auth", () => ({ useLogout: () => ({ mutate: vi.fn(), isPending: false }) }));
vi.mock("@/api/installation", () => ({ useConnectedTools: () => ({ data: state.tools }) }));

const { AccountMenu } = await import("./AccountMenu");

const stator: ConnectedTool = { product: "stator", name: "Stator", url: "https://stator.example.test" };
const exciter: ConnectedTool = { product: "exciter", name: "Exciter", url: "https://exciter.example.test" };

async function open() {
  render(<AccountMenu userName="Ada Lovelace" />);
  await userEvent.click(screen.getByRole("button", { name: "Your account" }));
  return screen.getByRole("menu", { name: "Your account" });
}

beforeEach(() => {
  state.tools = [];
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
    expect(words).toEqual(["Your profile", "Stator", "Exciter", "Sign out"]);
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
    expect(screen.getAllByRole("menuitem").map((item) => item.textContent)).toEqual(["Your profile", "Sign out"]);
  });
});
