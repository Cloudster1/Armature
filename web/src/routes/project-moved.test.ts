import { describe, expect, it, vi } from "vitest";

vi.mock("@tanstack/react-router", () => ({
  createRoute: (options: unknown) => options,
  notFound: () => new Error("not found"),
  redirect: (options: unknown) => options,
}));
vi.mock("./project", () => ({ projectRoute: {} }));

const { projectMovedRoutes } = await import("./project-moved");
const { MOVED_PROJECT_PAGES, movedProjectAddress } = await import("@/features/projects/settingsTabs");

type Moved = { path: string; beforeLoad: (args: unknown) => void };

describe("a project's old setup addresses", () => {
  it("name every setup page but General, and Teams, which stays a work page of the project", () => {
    expect(MOVED_PROJECT_PAGES).toEqual(["/workflows", "/fields", "/issue-view", "/components", "/repositories", "/automation", "/import"]);
    expect((projectMovedRoutes as unknown as Moved[]).map((route) => route.path)).toEqual(MOVED_PROJECT_PAGES);
  });

  it.each(MOVED_PROJECT_PAGES)("send %s to its tab, keeping search and hash", (path) => {
    const route = (projectMovedRoutes as unknown as Moved[]).find((each) => each.path === path)!;
    let thrown: unknown;
    try {
      route.beforeLoad({ params: { projectKey: "CP" }, location: { searchStr: "?q=a&offset=20", hash: "top" } });
    } catch (e) {
      thrown = e;
    }
    expect(thrown).toEqual({ href: `/projects/CP/settings${path}?q=a&offset=20#top`, replace: true });
  });

  it("know nothing of an address that never moved", () => {
    expect(movedProjectAddress("CP", "board")).toBeUndefined();
    expect(movedProjectAddress("CP", "settings")).toBeUndefined();
    expect(movedProjectAddress("CP", "teams")).toBeUndefined();
    expect(movedProjectAddress("CP", "fields")).toBe("/projects/CP/settings/fields");
  });
});
