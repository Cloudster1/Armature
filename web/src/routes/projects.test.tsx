import { beforeEach, describe, expect, it, vi } from "vitest";
import { render, screen } from "@testing-library/react";
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

vi.mock("@tanstack/react-router", () => ({
  Link: ({ children }: { children: ReactNode }) => <a>{children}</a>,
  createRoute: (options: unknown) => options,
  useNavigate: () => vi.fn(),
}));
vi.mock("./app", () => ({ appRoute: {} }));
vi.mock("@/api/projects", () => ({
  useProjects: () => ({ data: { projects }, isLoading: false }),
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
const ProjectsPage = (projectsRoute as unknown as { component: () => ReactNode }).component;

function renderPage() {
  render(
    <ToastProvider>
      <ProjectsPage />
    </ToastProvider>,
  );
}

beforeEach(() => {
  administers = true;
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
