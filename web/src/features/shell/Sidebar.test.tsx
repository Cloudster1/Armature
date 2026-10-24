import { beforeEach, describe, expect, it, vi } from "vitest";
import { act, render, screen, waitFor, within } from "@testing-library/react";
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
  admin: true,
};
const pushed = vi.fn();
const navigate = vi.fn();
const toggleMode = vi.fn(() => {
  state.mode = state.mode === "open" ? "rail" : "open";
});

vi.mock("@tanstack/react-router", () => ({
  Link: ({ children, to, search, hash, activeOptions, activeProps, params, ...rest }: { children: ReactNode; to: string } & Record<string, unknown>) => {
    void activeOptions;
    void activeProps;
    void params;
    const query = search ? `?${new URLSearchParams(search as Record<string, string>)}` : "";
    return (
      <a href={`${String(to)}${query}${hash ? `#${String(hash)}` : ""}`} {...(rest as Record<string, string>)}>
        {children}
      </a>
    );
  },
  useNavigate: () => navigate,
  useLocation: () => ({ pathname: state.pathname, href: state.pathname }),
  useParams: () => state.params,
  useRouter: () => ({ history: { push: pushed } }),
}));
vi.mock("@/api/access", () => ({ useAccess: () => ({ data: { canAdministerOrg: state.admin } }) }));
vi.mock("@/api/auth", () => ({
  useMe: () => ({ data: { principal: { user: { id: "u1", name: "Ada Lovelace", email: "ada@armature.test" }, installation: { id: "i1", name: "Acme" }, role: "owner" }, organizations: state.organizations } }),
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
vi.mock("@/api/installation", () => ({ useConnectedTools: () => ({ data: [] }) }));
vi.mock("@/api/notifications", () => ({
  useUnreadCount: () => ({ data: { unread: 3 } }),
  useInbox: () => ({ data: { notifications: [] }, isLoading: false }),
  useMarkRead: () => ({ mutate: vi.fn(), isPending: false }),
}));
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
  state.admin = true;
  window.innerWidth = 1280;
  window.sessionStorage.clear();
  navigate.mockReset();
  pushed.mockReset();
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

  it("puts the mark beside the installation's name, open or in the narrow drawer", () => {
    const { unmount } = render(<Sidebar onNewIssue={() => {}} />);
    const head = document.querySelector("[data-sidebar] [data-installation-name]")!.parentElement!;
    const mark = head.querySelector("img[data-logo-mark]")!;
    // Decorative: the name beside it is what a screen reader reads.
    expect(mark.getAttribute("alt")).toBe("");
    expect(mark.getAttribute("src")).toBe("/mark-96.webp");
    unmount();

    window.innerWidth = 375;
    render(<Sidebar onNewIssue={() => {}} drawerOpen onCloseDrawer={() => {}} />);
    const drawer = screen.getByRole("dialog", { name: "Navigation" });
    expect(drawer.querySelector("[data-installation-name]")!.parentElement!.querySelector("[data-logo-mark]")).not.toBeNull();
  });

  it("lists the work and no personal or installation pages, with Notifications counting what is unread", () => {
    render(<Sidebar onNewIssue={() => {}} onAsk={() => {}} />);
    const places = Array.from(document.querySelectorAll('[data-sidebar-group="work"] [data-place]')).map((a) => a.getAttribute("data-place"));
    expect(places).toEqual(["Home", "Search", "Notifications", "Saved searches", "Projects"]);
    expect(document.querySelector('[data-place="Notifications"]')?.getAttribute("href")).toBe("/inbox");
    expect(document.querySelector('[data-place="Notifications"] [data-unread-badge]')?.textContent).toContain("3");
    expect(document.querySelector('[data-sidebar-group="settings"]')).toBeNull();
    expect(document.querySelector('a[href="/settings/tokens"]')).toBeNull();
    expect(document.querySelector('[data-action="sign-out"]')).toBeNull();
    expect(screen.queryByText("Inbox")).toBeNull();
  });

  it("folds a group by its title and remembers it", async () => {
    render(<Sidebar onNewIssue={() => {}} onAsk={() => {}} />);
    expect(document.querySelector('[data-sidebar-group="work"]')?.getAttribute("data-open")).toBe("true");
    const title = screen.getByRole("button", { name: "Work & Plan" });
    await userEvent.click(title);
    // Kept, hidden, so the title still names what it folds.
    const folded = document.getElementById(title.getAttribute("aria-controls")!)!;
    expect(folded.hidden).toBe(true);
    expect(title.getAttribute("aria-expanded")).toBe("false");
    expect(within(document.querySelector("[data-sidebar]") as HTMLElement).queryByRole("link", { name: /Home/ })).toBeNull();
    expect(JSON.parse(localStorage.getItem("armature.sidebar-groups") ?? "{}")).toEqual({ work: false });
  });

  it("has the rail's toggle say whether the sidebar is out, and name it while it is", () => {
    const { unmount } = render(<Sidebar onNewIssue={() => {}} />);
    const toggle = document.querySelector('[data-action="sidebar"]')!;
    expect(toggle.getAttribute("aria-expanded")).toBe("true");
    expect(document.getElementById(toggle.getAttribute("aria-controls")!)?.hasAttribute("data-sidebar")).toBe(true);
    expect(document.querySelector("[data-rail]")!.hasAttribute("data-print-hide")).toBe(true);
    expect(document.querySelector("[data-sidebar]")!.hasAttribute("data-print-hide")).toBe(true);
    unmount();

    state.mode = "rail";
    render(<Sidebar onNewIssue={() => {}} />);
    const folded = document.querySelector('[data-action="sidebar"]')!;
    expect(folded.getAttribute("aria-expanded")).toBe("false");
    expect(folded.hasAttribute("aria-controls")).toBe(false);
  });

  it("keeps every pressed control exactly once in either mode", () => {
    const { unmount } = render(<Sidebar onNewIssue={() => {}} onAsk={() => {}} />);
    for (const action of ["sidebar", "theme", "guide", "inbox", "new-issue", "account"]) {
      expect(document.querySelectorAll(`[data-action="${action}"]`), action).toHaveLength(1);
    }
    expect(document.querySelector("[data-unread-count]")?.getAttribute("data-unread-count")).toBe("3");
    unmount();

    state.mode = "rail";
    render(<Sidebar onNewIssue={() => {}} onAsk={() => {}} />);
    expect(document.querySelector("[data-sidebar]")).toBeNull();
    expect(document.querySelector("[data-rail]")).not.toBeNull();
    for (const action of ["sidebar", "theme", "guide", "inbox", "new-issue", "account"]) {
      expect(document.querySelectorAll(`[data-action="${action}"]`), action).toHaveLength(1);
    }
  });
});

describe("the settings area", () => {
  it("swaps the sidebar for the settings' own, headed by the way back to the last page outside it", async () => {
    window.sessionStorage.setItem("armature.outside-settings", "/projects/AAA/board");
    state.pathname = "/settings/labels";
    render(<Sidebar onNewIssue={() => {}} />);
    expect(document.querySelector('[data-sidebar-area="settings"]')).not.toBeNull();
    expect(document.querySelector('[data-sidebar-group="work"]')).toBeNull();
    const you = Array.from(document.querySelectorAll('[data-sidebar-group="you"] [data-place]')).map((a) => a.getAttribute("data-place"));
    expect(you).toEqual(["Your profile", "Notification settings", "Watching", "Themes", "Access tokens"]);
    const settings = Array.from(document.querySelectorAll('[data-sidebar-group="settings"] [data-place]')).map((a) => a.getAttribute("data-place"));
    expect(settings).toEqual(["Installation", "Access", "Users", "Holidays", "Workflows", "Labels", "Automation", "Fields", "Issue view", "Connected apps", "Webhooks", "Audit log"]);
    expect(document.querySelector('[data-place="Notification settings"]')?.getAttribute("href")).toBe("/settings/profile#notifications");
    expect(document.querySelector('[data-place="Watching"]')?.getAttribute("href")).toBe("/search?q=watcher+%3D+currentUser%28%29");

    const back = screen.getByText("Back to Armature").closest("a")!;
    expect(back.getAttribute("href")).toBe("/projects/AAA/board");
    await userEvent.click(back);
    expect(pushed).toHaveBeenCalledWith("/projects/AAA/board");
  });

  it("shows somebody who does not administer only their own pages", () => {
    state.admin = false;
    state.pathname = "/settings";
    render(<Sidebar onNewIssue={() => {}} />);
    expect(document.querySelector('[data-sidebar-group="you"]')).not.toBeNull();
    expect(document.querySelector('[data-sidebar-group="settings"]')).toBeNull();
    expect(document.querySelector('a[href="/settings/audit"]')).toBeNull();
  });

  it("draws Themes with a glyph of its own, not one the light and dark switch wears", () => {
    state.pathname = "/settings";
    render(<Sidebar onNewIssue={() => {}} onAsk={() => {}} />);
    const glyph = document.querySelector('a[href="/settings/themes"] svg')?.getAttribute("data-icon");
    expect(glyph).toBe("palette");
    expect(["sun", "moon", "monitor"]).not.toContain(glyph);
  });

  it("goes home when nothing outside it was seen", () => {
    state.pathname = "/settings";
    render(<Sidebar onNewIssue={() => {}} />);
    expect(screen.getByText("Back to Armature").closest("a")?.getAttribute("href")).toBe("/");
  });
});

describe("a narrow screen", () => {
  it("draws neither rail nor sidebar, and the drawer holds the groups, the account's entries, the theme and the build", () => {
    window.innerWidth = 375;
    const { rerender } = render(<Sidebar onNewIssue={() => {}} drawerOpen={false} />);
    expect(document.querySelector("[data-rail]")).toBeNull();
    expect(document.querySelector("[data-sidebar]")).toBeNull();

    rerender(<Sidebar onNewIssue={() => {}} drawerOpen onCloseDrawer={() => {}} />);
    const drawer = screen.getByRole("dialog", { name: "Navigation" });
    expect(drawer.querySelector('[data-sidebar-group="work"]')).not.toBeNull();
    expect(drawer.querySelector('[data-sidebar-group="project"]')).not.toBeNull();
    const foot = drawer.querySelector("[data-drawer-foot]") as HTMLElement;
    for (const word of ["Your profile", "Notification settings", "Watching", "Themes", "Access tokens", "Settings", "Sign out"]) {
      expect(foot.textContent, word).toContain(word);
    }
    expect(foot.querySelector('[data-action="theme"]')).not.toBeNull();
    expect(foot.querySelector("[data-build]")).not.toBeNull();
  });

  it("holds the settings sidebar in the settings area", () => {
    window.innerWidth = 375;
    state.pathname = "/settings/users";
    render(<Sidebar onNewIssue={() => {}} drawerOpen onCloseDrawer={() => {}} />);
    const drawer = screen.getByRole("dialog", { name: "Navigation" });
    expect(drawer.getAttribute("data-sidebar-area")).toBe("settings");
    expect(drawer.querySelector('[data-sidebar-group="settings"]')).not.toBeNull();
    expect(drawer.querySelector('[data-action="back-from-settings"]')).not.toBeNull();
  });

  it("closes from its button and with Escape", async () => {
    window.innerWidth = 375;
    const close = vi.fn();
    render(<Sidebar onNewIssue={() => {}} drawerOpen onCloseDrawer={close} />);
    await userEvent.click(screen.getByRole("button", { name: "Close the navigation" }));
    await userEvent.keyboard("{Escape}");
    expect(close).toHaveBeenCalledTimes(2);
  });

  it("follows the width as the window is resized", () => {
    render(<Sidebar onNewIssue={() => {}} />);
    expect(document.querySelector("[data-rail]")).not.toBeNull();
    act(() => {
      window.innerWidth = 375;
      window.dispatchEvent(new Event("resize"));
    });
    expect(document.querySelector("[data-rail]")).toBeNull();
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

  // A box inside a menu item's button is activated by Space, which picks the item and closes the menu.
  it("keeps the filter box out of the menu's items, so a space is typed and not pressed", async () => {
    state.projects = [{ key: "CPO", name: "Customer Portal" }];
    render(<Sidebar onNewIssue={() => {}} />);
    await userEvent.click(document.querySelector<HTMLElement>("[data-project-switcher]")!);
    const box = screen.getByLabelText("Filter projects");
    expect(box.closest('[role="menuitem"], button')).toBeNull();
    await userEvent.type(box, "customer portal");
    expect(screen.getByRole("menu", { name: "Switch project" })).toBeInTheDocument();
    expect(box).toHaveValue("customer portal");
  });
});
