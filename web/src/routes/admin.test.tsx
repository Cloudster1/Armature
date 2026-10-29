import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen } from "@testing-library/react";
import type { ReactNode } from "react";

const state = { admin: false, narrow: false };
const request = vi.fn();

vi.mock("@tanstack/react-router", () => ({
  Link: ({ children, to, ...rest }: { children: ReactNode; to: string } & Record<string, unknown>) => (
    <a href={to} data-admin-card={rest["data-admin-card"] as string} data-admin-link={rest["data-admin-link"] as string}>
      {children}
    </a>
  ),
  Outlet: () => <p data-child>a child page</p>,
  createRoute: (options: unknown) => options,
  notFound: () => new Error("not found"),
  redirect: (options: unknown) => options,
  useLocation: () => ({ pathname: "/admin" }),
  useNavigate: () => vi.fn(),
}));
vi.mock("./app", () => ({ appRoute: {} }));
vi.mock("@/api/client", () => ({ request: (...args: unknown[]) => request(...args) }));
vi.mock("@/api/auth", () => ({ useMe: () => ({ data: { principal: { installation: { name: "Acme" } } } }) }));
vi.mock("@/api/access", () => ({
  accessQueryOptions: () => ({ queryKey: ["access", "me"], queryFn: () => ({}) }),
  useAccess: () => ({ data: { canAdministerOrg: state.admin } }),
}));
vi.mock("@/features/shell/state", () => ({ useNarrow: () => state.narrow }));
vi.mock("@/features/shell/nav", () => ({
  NavItem: ({ children, to, ...rest }: { children: ReactNode; to: string } & Record<string, unknown>) => (
    <a href={to} data-admin-link={rest["data-admin-link"] as string}>
      {children}
    </a>
  ),
}));

const { adminRoute, adminIndexRoute, movedAddress, settingsMovedRoute, settingsMovedDeepRoute, MOVED_SECTIONS } = await import("./admin");
const { adminPlaces } = await import("@/features/shell/places");
const Layout = (adminRoute as unknown as { component: () => ReactNode }).component;
const Index = (adminIndexRoute as unknown as { component: () => ReactNode }).component;
const guard = (adminRoute as unknown as { beforeLoad: (args: unknown) => Promise<void> }).beforeLoad;

afterEach(() => {
  cleanup();
  request.mockReset();
  state.admin = false;
  state.narrow = false;
});

describe("the Admin page's guard", () => {
  it("waits for the person's access before anything draws", async () => {
    const ensureQueryData = vi.fn().mockResolvedValue({});
    await guard({ context: { queryClient: { ensureQueryData } } });
    expect(ensureQueryData).toHaveBeenCalledWith(expect.objectContaining({ queryKey: ["access", "me"] }));
  });

  it("refuses a member and mounts no child, so nothing is fetched", () => {
    render(<Layout />);
    expect(screen.getByText("Only administrators can open this page")).toBeTruthy();
    expect(document.querySelector("[data-child]")).toBeNull();
    expect(document.querySelector("[data-admin-nav]")).toBeNull();
    expect(request).not.toHaveBeenCalled();
  });

  it("gives an administrator the navigation beside the page, or a menu on a narrow screen", () => {
    state.admin = true;
    const { unmount } = render(<Layout />);
    expect(document.querySelector("[data-child]")).not.toBeNull();
    expect(document.querySelectorAll("[data-admin-nav] [data-admin-link]")).toHaveLength(adminPlaces.length + 1);
    unmount();
    state.narrow = true;
    render(<Layout />);
    expect(document.querySelector("[data-admin-nav]")).toBeNull();
    expect(document.querySelector("[data-admin-menu]")).not.toBeNull();
  });
});

describe("the Admin index", () => {
  it("groups every admin place under its section", () => {
    state.admin = true;
    render(<Index />);
    const sections = Array.from(document.querySelectorAll("[data-admin-section]")).map((s) => [
      s.getAttribute("data-admin-section"),
      Array.from(s.querySelectorAll("[data-admin-card]")).map((a) => a.getAttribute("data-admin-card")),
    ]);
    expect(sections).toEqual([
      ["people", ["Installation", "Access", "Users"]],
      ["work", ["Holidays", "Workflows", "Issue types", "Labels", "Automation", "Fields", "Issue view"]],
      ["integrations", ["Connected apps", "Webhooks"]],
      ["records", ["Audit log"]],
    ]);
  });
});

describe("the old /settings addresses", () => {
  it("send every moved page on to /admin, with its search and hash", () => {
    for (const section of Object.keys(MOVED_SECTIONS)) {
      const target = section === "organization" ? "installation" : section;
      expect(movedAddress(section, "", "?tab=members", "top"), section).toBe(`/admin/${target}?tab=members#top`);
    }
  });

  it("keeps what lies below a section", () => {
    expect(movedAddress("workflows", "schemes", "?q=lean")).toBe("/admin/workflows/schemes?q=lean");
    expect(movedAddress("workflows", "w-1/design")).toBe("/admin/workflows/w-1/design");
  });

  it("covers every place the Admin page lists", () => {
    for (const place of adminPlaces) {
      expect(movedAddress(place.to.replace("/admin/", ""))).toBe(place.to);
    }
  });

  it("leaves the personal pages and anything unknown alone", () => {
    for (const section of ["profile", "tokens", "themes", "second-factor", "constructor", "toString"]) {
      expect(movedAddress(section), section).toBeUndefined();
    }
  });

  it("replaces the history entry and is not found for an unknown section", () => {
    const location = { searchStr: "?tab=members", hash: "" };
    const route = settingsMovedRoute as unknown as { beforeLoad: (a: unknown) => void };
    const deep = settingsMovedDeepRoute as unknown as { beforeLoad: (a: unknown) => void };
    const thrown = (fn: () => void) => {
      try {
        fn();
      } catch (e) {
        return e;
      }
    };
    expect(thrown(() => route.beforeLoad({ params: { section: "access" }, location }))).toEqual({ href: "/admin/access?tab=members", replace: true });
    expect(thrown(() => deep.beforeLoad({ params: { section: "workflows", _splat: "schemes" }, location }))).toEqual({
      href: "/admin/workflows/schemes?tab=members",
      replace: true,
    });
    expect(String(thrown(() => route.beforeLoad({ params: { section: "nothing" }, location })))).toContain("not found");
  });
});
