import { beforeEach, describe, expect, it, vi } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { ReactNode } from "react";
import type { Project } from "@/api/projects";
import { ToastProvider } from "@/components/ui";

const archive = vi.fn();
const restore = vi.fn();
let administers = true;

function project(key: string, name: string, archivedAt?: string): Project {
  return { id: key, key, name, kind: "software", archivedAt, openIssueCount: 0, issueCount: 0, updatedAt: "2026-10-10T08:00:00Z" } as unknown as Project;
}
const projects = [project("LIVE", "Live work"), project("OLD", "Old work", "2026-10-01T08:00:00Z")];
const navigate = vi.fn();
const page = {
  search: {} as { q?: string; offset?: number; archived?: boolean },
  rows: projects,
  total: projects.length,
  asked: [] as unknown[],
};

vi.mock("@tanstack/react-router", () => ({
  Link: ({ children }: { children: ReactNode }) => <a>{children}</a>,
  createRoute: (options: object) => ({ ...options, useSearch: () => page.search }),
  useNavigate: () => navigate,
}));
vi.mock("./app", () => ({ appRoute: {} }));
vi.mock("@/api/projects", () => ({
  useProjects: (params: unknown) => {
    page.asked.push(params);
    return { data: { projects: page.rows, total: page.total, limit: 50, offset: page.search.offset ?? 0 }, isLoading: false };
  },
  useArchiveProject: () => ({ mutate: archive, isPending: false }),
  useRestoreProject: () => ({ mutate: restore, isPending: false }),
  useCreateProject: () => ({ mutate: vi.fn(), isPending: false, error: null }),
  useKeyCheck: () => ({ data: undefined }),
}));
vi.mock("@/api/access", () => ({ useAccess: () => ({ data: { canCreateProject: false } }), canAdminister: () => administers }));
vi.mock("@/features/projects/TemplateChooser", () => ({ TemplateChooser: () => null }));
vi.mock("@/features/projects/StatusStrip", () => ({ HealthTag: () => null }));
vi.mock("@/features/issues/badges", () => ({ RelativeTime: () => null }));

const { projectsRoute } = await import("./projects");
const route = projectsRoute as unknown as { component: () => ReactNode; validateSearch: (search: Record<string, unknown>) => unknown };
const ProjectsPage = route.component;

function renderPage() {
  render(
    <ToastProvider>
      <ProjectsPage />
    </ToastProvider>,
  );
}

beforeEach(() => {
  administers = true;
  page.search = {};
  page.rows = projects;
  page.total = projects.length;
  page.asked = [];
  navigate.mockReset();
  archive.mockReset().mockImplementation((_key: string, options?: { onSuccess?: () => void }) => options?.onSuccess?.());
  restore.mockReset();
});

describe("the projects list", () => {
  it("offers Restore on an archived row, and only that", async () => {
    renderPage();
    await userEvent.click(screen.getByRole("button", { name: "Actions for Old work" }));
    expect(screen.queryByRole("menuitem", { name: "Archive" })).toBeNull();
    expect(screen.queryByRole("menuitem", { name: "Settings" })).toBeNull();
    await userEvent.click(screen.getByRole("menuitem", { name: "Restore" }));
    expect(restore).toHaveBeenCalledWith("OLD", expect.anything());
  });

  it("offers nothing on any row to somebody who does not administer the project", () => {
    administers = false;
    renderPage();
    expect(screen.queryByRole("button", { name: /Actions for/ })).toBeNull();
  });

  it("archives at once, and the toast's Undo restores the project", async () => {
    renderPage();
    await userEvent.click(screen.getByRole("button", { name: "Actions for Live work" }));
    await userEvent.click(screen.getByRole("menuitem", { name: "Archive" }));
    expect(screen.queryByRole("dialog")).toBeNull();
    expect(archive).toHaveBeenCalledWith("LIVE", expect.anything());
    expect(screen.getByRole("status")).toHaveTextContent("Archived Live work");

    await userEvent.click(screen.getByRole("button", { name: "Undo" }));
    expect(restore).toHaveBeenCalledWith("LIVE");
  });
});

describe("the projects page in the address", () => {
  it("keeps the search, the page and the archived switch, and drops what is not one", () => {
    expect(route.validateSearch({ q: "port", offset: 50, archived: true })).toEqual({ q: "port", offset: 50, archived: true });
    expect(route.validateSearch({ q: "", offset: 0, archived: false })).toEqual({});
    expect(route.validateSearch({ offset: -50, archived: "yes" })).toEqual({});
    expect(route.validateSearch({ q: 42 })).toEqual({ q: "42" });
  });

  it("asks for the page the address names and counts the whole list", () => {
    page.search = { q: "work", offset: 50, archived: true };
    page.total = 120;
    renderPage();
    expect(page.asked.at(-1)).toEqual({ q: "work", offset: 50, includeArchived: true });
    expect(screen.getByText("120 projects")).toBeInTheDocument();
    expect(screen.getByText("51 to 100 of 120")).toBeInTheDocument();
  });

  it("turns the page in the address, keeping the search", async () => {
    page.search = { q: "work" };
    page.total = 120;
    renderPage();
    await userEvent.click(screen.getByRole("button", { name: "Next page" }));
    expect(navigate).toHaveBeenLastCalledWith({ to: "/projects", search: { q: "work", offset: 50, archived: undefined }, replace: false });
  });

  // A new search starts on its first page, written once the typing pauses rather than per letter.
  it("writes the search into the address once typing pauses, back at the first page", async () => {
    page.search = { offset: 50 };
    page.total = 120;
    renderPage();
    await userEvent.type(screen.getByLabelText("Find projects"), "live");
    await waitFor(() =>
      expect(navigate).toHaveBeenCalledWith({ to: "/projects", search: { q: "live", offset: undefined, archived: undefined }, replace: false }),
    );
    expect(navigate).toHaveBeenCalledTimes(1);
  });

  it("says when nothing matches the search", () => {
    page.search = { q: "nothing" };
    page.rows = [];
    page.total = 0;
    renderPage();
    expect(screen.getByText("No project matches that")).toBeInTheDocument();
    expect(screen.queryByText("No projects yet")).toBeNull();
  });
});
