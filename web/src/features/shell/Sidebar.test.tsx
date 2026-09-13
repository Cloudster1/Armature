import { beforeEach, describe, expect, it, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { ReactNode } from "react";

const state = { mode: "open" as "open" | "rail" };
const toggleMode = vi.fn(() => {
  state.mode = state.mode === "open" ? "rail" : "open";
});

vi.mock("@tanstack/react-router", () => ({
  Link: ({ children, to, ...rest }: { children: ReactNode; to: string } & Record<string, unknown>) => (
    <a href={String(to)} {...(rest as Record<string, string>)}>
      {children}
    </a>
  ),
  useNavigate: () => vi.fn(),
  useLocation: () => ({ pathname: "/" }),
  useParams: () => ({}),
}));
vi.mock("@/api/auth", () => ({
  useMe: () => ({ data: { principal: { user: { id: "u1", name: "Ada Lovelace" }, org: { name: "Acme" }, role: "owner" }, organizations: [] } }),
  useSwitchOrg: () => ({ mutate: vi.fn() }),
  useLogout: () => ({ mutate: vi.fn(), isPending: false }),
  useEraseMe: () => ({ mutate: vi.fn(), isPending: false }),
  MY_DATA_HREF: "/api/v1/auth/me/export",
}));
vi.mock("@/api/projects", () => ({ useProject: () => ({ data: undefined }), useProjects: () => ({ data: { projects: [] } }) }));
vi.mock("@/api/notifications", () => ({ useUnreadCount: () => ({ data: { unread: 3 } }) }));
vi.mock("./state", async () => {
  const actual = await vi.importActual<typeof import("./state")>("./state");
  return { ...actual, useSidebarMode: () => [state.mode, toggleMode], useLastProject: () => undefined };
});
vi.mock("./ConfirmProvider", () => ({ useConfirm: () => async () => true }));

const { Sidebar } = await import("./Sidebar");

beforeEach(() => {
  localStorage.clear();
  state.mode = "open";
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
