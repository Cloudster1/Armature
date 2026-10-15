import { beforeEach, describe, expect, it, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { ReactNode } from "react";
import type { Project } from "@/api/projects";
import { ToastProvider } from "@/components/ui";

const archive = vi.fn();
const restore = vi.fn();
const idle = { mutate: vi.fn(), isPending: false, error: null };
const live = { id: "p1", key: "CP", name: "Portal", description: "", kind: "software", features: ["board"] } as unknown as Project;
const archived = { ...live, archivedAt: "2026-10-01T08:00:00Z" } as Project;
let shown: Project = live;

vi.mock("@tanstack/react-router", () => ({
  Link: ({ children }: { children: ReactNode }) => <a>{children}</a>,
  createRoute: (options: object) => ({ ...options, useParams: () => ({ projectKey: "CP" }) }),
}));
vi.mock("./project", () => ({ projectRoute: {} }));
vi.mock("@/api/projects", () => ({
  useProject: () => ({ data: { project: shown } }),
  useUpdateProject: () => idle,
  useArchiveProject: () => ({ mutate: archive, isPending: false, error: null }),
  useRestoreProject: () => ({ mutate: restore, isPending: false, error: null }),
}));
vi.mock("@/api/people", () => ({ useMemberSearch: () => ({ data: { members: [], total: 0, limit: 20, offset: 0 } }) }));
vi.mock("@/api/access", () => ({ useAccess: () => ({ data: undefined }), canAdminister: () => true }));
// A stand-in for the sections with forms of their own, to see they are closed too.
vi.mock("@/features/projects/PlanningSettings", () => ({ PlanningSettings: () => <button type="button">Planning control</button> }));
vi.mock("@/features/projects/ShareSettings", () => ({ ShareSettings: () => null }));

const { projectSettingsRoute } = await import("./project-settings");
const SettingsPage = (projectSettingsRoute as unknown as { component: () => ReactNode }).component;

function renderPage() {
  render(
    <ToastProvider>
      <SettingsPage />
    </ToastProvider>,
  );
}

beforeEach(() => {
  shown = live;
  archive.mockReset().mockImplementation((_key: string, options?: { onSuccess?: () => void }) => options?.onSuccess?.());
  restore.mockReset();
});

describe("the settings of a project", () => {
  it("archives at once, and the toast's Undo restores the project", async () => {
    renderPage();
    await userEvent.click(screen.getByRole("button", { name: "Archive project" }));
    expect(screen.queryByRole("dialog")).toBeNull();
    expect(archive).toHaveBeenCalledWith("CP", expect.anything());
    expect(screen.getByRole("status")).toHaveTextContent("Archived Portal");

    await userEvent.click(screen.getByRole("button", { name: "Undo" }));
    expect(restore).toHaveBeenCalledWith("CP", expect.anything());
  });

  it("says an archived project is archived, closes its forms and offers Restore", async () => {
    shown = archived;
    renderPage();
    expect(screen.getByText("Portal is archived. Restore it to change its settings.")).toBeInTheDocument();
    expect(screen.getByLabelText(/Project name/)).toBeDisabled();
    expect(screen.getByRole("button", { name: "Save changes" })).toBeDisabled();
    expect(screen.getByRole("switch", { name: "Board" })).toBeDisabled();
    expect(screen.getByRole("button", { name: "Planning control" })).toBeDisabled();
    expect(screen.queryByRole("button", { name: "Archive project" })).toBeNull();

    await userEvent.click(screen.getByRole("button", { name: "Restore project" }));
    expect(restore).toHaveBeenCalledWith("CP", expect.anything());
  });
});
