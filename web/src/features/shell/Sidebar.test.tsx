import { beforeEach, describe, expect, it, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { ReactNode } from "react";

const state = {
  mode: "open" as "open" | "rail",
  pathname: "/",
  params: {} as { projectKey?: string },
  projects: [] as Array<{ key: string; name: string }>,
};
const navigate = vi.fn();
const toggleMode = vi.fn(() => {
  state.mode = state.mode === "open" ? "rail" : "open";
});

vi.mock("@tanstack/react-router", () => ({
  Link: ({ children, to, ...rest }: { children: ReactNode; to: string } & Record<string, unknown>) => (
    <a href={String(to)} {...(rest as Record<string, string>)}>
      {children}
    </a>
  ),
  useNavigate: () => navigate,
  useLocation: () => ({ pathname: state.pathname }),
  useParams: () => state.params,
}));
vi.mock("@/api/auth", () => ({
  useMe: () => ({ data: { principal: { user: { id: "u1", name: "Ada Lovelace" }, org: { name: "Acme" }, role: "owner" }, organizations: [] } }),
  useSwitchOrg: () => ({ mutate: vi.fn() }),
  useLogout: () => ({ mutate: vi.fn(), isPending: false }),
  useEraseMe: () => ({ mutate: vi.fn(), isPending: false }),
  MY_DATA_HREF: "/api/v1/auth/me/export",
}));
vi.mock("@/api/projects", () => ({ useProject: () => ({ data: undefined }), useProjects: () => ({ data: { projects: state.projects } }) }));
vi.mock("@/api/notifications", () => ({ useUnreadCount: () => ({ data: { unread: 3 } }) }));
vi.mock("@/api/build", () => ({ useBuild: () => ({ data: { build: { version: "0.1.0", commit: "0123456789abcdef", builtAt: "2026-09-28T10:00:00Z" } } }) }));
vi.mock("./state", async () => {
  const actual = await vi.importActual<typeof import("./state")>("./state");
  return { ...actual, useSidebarMode: () => [state.mode, toggleMode], useLastProject: (key: string | undefined) => key };
});
vi.mock("./ConfirmProvider", () => ({ useConfirm: () => async () => true }));

const { Sidebar } = await import("./Sidebar");

beforeEach(() => {
  localStorage.clear();
  state.mode = "open";
  state.pathname = "/";
  state.params = {};
  state.projects = [];
  navigate.mockReset();
});

describe("the shell's chrome", () => {
  // The suite waits for a header and for the tokens link on every arrival.
  it("opens the settings group with the tokens link, and folds a group by its title", async () => {
    render(<Sidebar onNewIssue={() => {}} onAsk={() => {}} />);
    expect(document.querySelector("header")).not.toBeNull();
    expect(document.querySelector('a[href="/settings/tokens"]')).not.toBeNull();
    expect(document.querySelector('[data-sidebar-group="settings"]')?.getAttribute("data-open")).toBe("true");
    await userEvent.click(screen.getByRole("button", { name: "Settings" }));
    expect(document.querySelector('a[href="/settings/tokens"]')).toBeNull();
    expect(JSON.parse(localStorage.getItem("armature.sidebar-groups") ?? "{}")).toEqual({ settings: false });
  });

  it("draws Themes with a glyph of its own, not one the light and dark switch wears", () => {
    render(<Sidebar onNewIssue={() => {}} onAsk={() => {}} />);
    const glyph = document.querySelector('a[href="/settings/themes"] svg')?.getAttribute("data-icon");
    expect(glyph).toBe("palette");
    expect(["sun", "moon", "monitor"]).not.toContain(glyph);
  });

  it("keeps every pressed control exactly once in either mode", () => {
    const { unmount } = render(<Sidebar onNewIssue={() => {}} onAsk={() => {}} />);
    for (const action of ["sidebar", "sign-out", "theme", "guide", "inbox", "new-issue", "profile"]) {
      expect(document.querySelectorAll(`[data-action="${action}"]`), action).toHaveLength(1);
    }
    expect(document.querySelector("[data-unread-count]")?.getAttribute("data-unread-count")).toBe("3");
    unmount();

    state.mode = "rail";
    render(<Sidebar onNewIssue={() => {}} onAsk={() => {}} />);
    expect(document.querySelector("[data-sidebar]")).toBeNull();
    expect(document.querySelector("[data-rail]")).not.toBeNull();
    for (const action of ["sidebar", "theme", "guide", "inbox", "new-issue", "profile"]) {
      expect(document.querySelectorAll(`[data-action="${action}"]`), action).toHaveLength(1);
    }
  });
});

describe("the foot of the map", () => {
  it("names the build that is answering", () => {
    render(<Sidebar onNewIssue={() => {}} />);
    const stamp = document.querySelector("[data-build]")!;
    expect(stamp.textContent).toBe("0.1.0 (0123456)");
    expect(stamp.getAttribute("title")).toContain("commit 0123456789abcdef");
  });
});

describe("the project switcher", () => {
  // A sprint's id belongs to the project it was opened in; carried across, it
  // drew one project's sprint under the other project's name.
  it("keeps the kind of page and drops the ids that belong to the old project", async () => {
    state.params = { projectKey: "AAA" };
    state.projects = [
      { key: "AAA", name: "Apollo" },
      { key: "BBB", name: "Borealis" },
    ];
    const pages: Array<[string, string]> = [
      ["/projects/AAA/sprints/0b0e5a1c-9f6e-4a49-a3c1-6d1c0f2b7e11/board", "/projects/BBB/sprints"],
      ["/projects/AAA/board", "/projects/BBB/board"],
      ["/projects/AAA/plan", "/projects/BBB/plan"],
      ["/projects/AAA", "/projects/BBB"],
    ];
    for (const [from, to] of pages) {
      state.pathname = from;
      navigate.mockReset();
      const { unmount } = render(<Sidebar onNewIssue={() => {}} />);
      await userEvent.click(document.querySelector<HTMLElement>("[data-project-switcher]")!);
      await userEvent.click(document.querySelector<HTMLElement>('[data-project-option="BBB"]')!);
      expect(navigate, from).toHaveBeenCalledWith({ to });
      unmount();
    }
  });
});
