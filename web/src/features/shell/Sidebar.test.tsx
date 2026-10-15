import { beforeEach, describe, expect, it, vi } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { ReactNode } from "react";
import type { Build } from "@/api/meta";

const state = {
  mode: "open" as "open" | "rail",
  pathname: "/",
  params: {} as { projectKey?: string },
  projects: [] as Array<{ key: string; name: string }>,
  /** Every text the switcher asked the server for, in order. */
  searched: [] as string[],
  build: { version: "0.1.0", commit: "0123456789abcdef", builtAt: "2026-09-28T10:00:00Z" } as Build,
  /** What an older server listed beside the principal; nothing reads it now. */
  organizations: [] as Array<{ orgId: string; orgSlug: string; orgName: string; role: string }>,
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
  useMe: () => ({ data: { principal: { user: { id: "u1", name: "Ada Lovelace" }, installation: { id: "i1", name: "Acme" }, role: "owner" }, organizations: state.organizations } }),
  useLogout: () => ({ mutate: vi.fn(), isPending: false }),
  useEraseMe: () => ({ mutate: vi.fn(), isPending: false }),
  MY_DATA_HREF: "/api/v1/auth/me/export",
}));
vi.mock("@/api/projects", () => ({
  useProject: () => ({ data: undefined }),
  useProjectSearch: (q: string) => {
    if (state.searched.at(-1) !== q) state.searched.push(q);
    return { data: { projects: state.projects.filter((p) => `${p.name} ${p.key}`.toLowerCase().includes(q.toLowerCase())) } };
  },
}));
vi.mock("@/api/notifications", () => ({ useUnreadCount: () => ({ data: { unread: 3 } }) }));
vi.mock("@/api/meta", () => ({
  useMeta: () => ({
    data: { product: "armature", build: state.build, apiVersion: "1", capabilities: [] },
  }),
}));
vi.mock("./state", async () => {
  const actual = await vi.importActual<typeof import("./state")>("./state");
  return { ...actual, useSidebarMode: () => [state.mode, toggleMode], useLastProject: (key: string | undefined) => key };
});
vi.mock("./ConfirmProvider", () => ({ useConfirm: () => async () => true }));

const { Sidebar } = await import("./Sidebar");

beforeEach(() => {
  localStorage.clear();
  state.build = { version: "0.1.0", commit: "0123456789abcdef", builtAt: "2026-09-28T10:00:00Z" };
  state.mode = "open";
  state.pathname = "/";
  state.params = {};
  state.projects = [];
  state.searched = [];
  state.organizations = [];
  navigate.mockReset();
});

describe("the shell's chrome", () => {
  it("heads the sidebar with the installation's name, and offers nothing to switch to", () => {
    state.organizations = [
      { orgId: "o1", orgSlug: "acme", orgName: "Acme", role: "owner" },
      { orgId: "o2", orgSlug: "other", orgName: "Other Corp", role: "member" },
    ];
    render(<Sidebar onNewIssue={() => {}} onAsk={() => {}} />);
    expect(screen.getByText("Acme")).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /Acme/ })).toBeNull();
    expect(screen.queryByText("Other Corp")).toBeNull();
  });

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

  it("names the version alone when the server keeps the commit back", () => {
    state.build = { version: "0.1.0" };
    render(<Sidebar onNewIssue={() => {}} />);
    const stamp = document.querySelector("[data-build]")!;
    expect(stamp.textContent).toBe("0.1.0");
    expect(stamp.getAttribute("title")).toBeNull();
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

  // An installation of thousands is never listed whole: what is typed goes to the server.
  it("asks the server for what is typed, once the typing pauses", async () => {
    state.projects = [
      { key: "AAA", name: "Apollo" },
      { key: "BBB", name: "Borealis" },
    ];
    render(<Sidebar onNewIssue={() => {}} />);
    await userEvent.click(document.querySelector<HTMLElement>("[data-project-switcher]")!);
    await userEvent.type(screen.getByLabelText("Filter projects"), "bor");
    await waitFor(() => expect(state.searched.at(-1)).toBe("bor"));
    expect(state.searched).toEqual(["", "bor"]);
    expect(document.querySelector('[data-project-option="AAA"]')).toBeNull();
    expect(document.querySelector('[data-project-option="BBB"]')).not.toBeNull();
  });
});
