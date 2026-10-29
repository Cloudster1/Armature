import { beforeEach, describe, expect, it, vi } from "vitest";
import { render, screen, within } from "@testing-library/react";
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
let saving: { mutate: () => void; isPending: boolean; error: Error | null } = idle;

const navigate = vi.fn();
let pathname = "/projects/CP/settings";
let administers = true;

vi.mock("@tanstack/react-router", () => ({
  Link: ({ children }: { children: ReactNode }) => <a>{children}</a>,
  Outlet: () => <p data-child>a tab</p>,
  createRoute: (options: object) => ({ ...options, useParams: () => ({ projectKey: "CP" }) }),
  useLocation: ({ select }: { select: (location: { pathname: string }) => string }) => select({ pathname }),
  useNavigate: () => navigate,
}));
vi.mock("./project", () => ({ projectRoute: {} }));
vi.mock("@/api/projects", () => ({
  useProject: () => ({ data: { project: shown } }),
  useUpdateProject: () => saving,
  useArchiveProject: () => ({ mutate: archive, isPending: false, error: null }),
  useRestoreProject: () => ({ mutate: restore, isPending: false, error: null }),
}));
vi.mock("@/api/people", () => ({ useMemberSearch: () => ({ data: { members: [], total: 0, limit: 20, offset: 0 } }) }));
vi.mock("@/api/access", () => ({ useAccess: () => ({ data: {} }), canAdminister: () => administers }));
// A stand-in for the sections with forms of their own, to see they are closed too.
vi.mock("@/features/projects/PlanningSettings", () => ({ PlanningSettings: () => <button type="button">Planning control</button> }));
vi.mock("@/features/projects/ShareSettings", () => ({ ShareSettings: () => null }));

const { projectSettingsRoute, projectSettingsIndexRoute } = await import("./project-settings");
const SettingsPage = (projectSettingsIndexRoute as unknown as { component: () => ReactNode }).component;
const Layout = (projectSettingsRoute as unknown as { component: () => ReactNode }).component;

function renderPage() {
  render(
    <ToastProvider>
      <SettingsPage />
    </ToastProvider>,
  );
}

beforeEach(() => {
  pathname = "/projects/CP/settings";
  administers = true;
  navigate.mockReset();
  shown = live;
  saving = idle;
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

  // On a phone Save is a screen below the top of the form.
  it("says a refused save beside Save, below the fields", () => {
    const refused = "Another project is already called Portal. Choose another name.";
    saving = { ...idle, error: new Error(refused) };
    renderPage();
    const save = screen.getByRole("button", { name: "Save changes" });
    // The features switch is made by the same hook, so the mock refuses it too; only the form's own is looked at.
    const said = within(save.closest("form") as HTMLElement).getByRole("alert");
    expect(said).toHaveTextContent(refused);
    expect(said.nextElementSibling?.contains(save)).toBe(true);
    expect(screen.getByLabelText("Link name").compareDocumentPosition(said) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
  });
});

describe("the tabs of a project's settings", () => {
  const tabs = () => screen.getAllByRole("tab").map((tab) => tab.textContent);

  it("lists General and the tabs of the features the project uses, and the ones every project has", () => {
    shown = { ...live, features: ["automation", "import"] } as Project;
    render(<Layout />);
    expect(tabs()).toEqual(["General", "Workflow", "Fields", "Issue view", "Automation", "Import"]);
    expect(screen.getByRole("tab", { name: "General" })).toHaveAttribute("aria-selected", "true");
  });

  it("lists every tab for a project that uses every feature", () => {
    shown = { ...live, features: ["components", "repositories", "automation", "import"] } as Project;
    render(<Layout />);
    expect(tabs()).toEqual(["General", "Workflow", "Fields", "Issue view", "Components", "Repositories", "Automation", "Import"]);
  });

  it("marks the tab the address names, and opens another on a click", async () => {
    shown = { ...live, features: ["import"] } as Project;
    pathname = "/projects/CP/settings/fields";
    render(<Layout />);
    expect(screen.getByRole("tab", { name: "Fields" })).toHaveAttribute("aria-selected", "true");
    await userEvent.click(screen.getByRole("tab", { name: "Import" }));
    expect(navigate).toHaveBeenCalledWith({ to: "/projects/CP/settings/import" });
  });

  it("shows the tab the address names", () => {
    render(<Layout />);
    expect(document.querySelector("[data-child]")).not.toBeNull();
  });

  it("tells the member who does not administer the project to ask, and mounts no tab", () => {
    administers = false;
    render(<Layout />);
    expect(screen.getByText("Only the project's administrators change its settings")).toBeInTheDocument();
    expect(screen.queryAllByRole("tab")).toHaveLength(0);
    expect(document.querySelector("[data-child]")).toBeNull();
  });

  it("answers a tab whose feature is off with the switch's notice and mounts nothing", () => {
    pathname = "/projects/CP/settings/import";
    render(<Layout />);
    expect(document.querySelector("[data-feature-off='import']")).not.toBeNull();
    expect(document.querySelector("[data-child]")).toBeNull();
  });
});
