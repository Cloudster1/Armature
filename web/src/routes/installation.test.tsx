import { beforeEach, describe, expect, it, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { ReactNode } from "react";

const rename = vi.fn((_name: string) => Promise.resolve({ installation: { name: "" } }));
const addDemo = vi.fn(() => Promise.resolve({ project: { key: "LINE", name: "Production line 3" } }));
const navigate = vi.fn();
const toastSuccess = vi.fn();
const access = { canAdministerOrg: true };
const demo: { error: Error | null } = { error: null };
const addSuite = vi.fn();
const invalidate = vi.fn();
type Suite = { state: string; step: number; steps: number; message: string; taken: string; projects: string[] };
const suite: { demo: Suite | null; error: Error | null; loaded: boolean } = { demo: null, error: null, loaded: true };
const building = (state: string, step = 0): Suite => ({ state, step, steps: 4, message: "", taken: "2026-10-21", projects: ["ARM", "EXC", "STA"] });

vi.mock("@tanstack/react-router", () => ({
  Link: ({ children, to, params }: { children: ReactNode; to: string; params?: { projectKey: string } }) => (
    <a href={params ? to.replace("$projectKey", params.projectKey) : to}>{children}</a>
  ),
  createRoute: (options: unknown) => options,
  redirect: (options: unknown) => options,
  useNavigate: () => navigate,
}));
vi.mock("@tanstack/react-query", () => ({ useQueryClient: () => ({ invalidateQueries: invalidate }) }));
vi.mock("./app", () => ({ appRoute: {} }));
vi.mock("@/api/access", () => ({ useAccess: () => ({ data: access }) }));
vi.mock("@/api/installation", () => ({
  useInstallation: () => ({ data: { installation: { name: "Acme" } } }),
  useRenameInstallation: () => ({ mutateAsync: rename, isPending: false, error: null }),
  useAddDemoProject: () => ({ mutateAsync: addDemo, isPending: false, error: demo.error }),
  useSuiteDemo: () => ({ data: suite.loaded ? { demo: suite.demo } : undefined }),
  useAddSuiteDemo: () => ({ mutate: addSuite, isPending: false, error: suite.error }),
}));
vi.mock("@/components/ui", async () => {
  const actual = await vi.importActual<typeof import("@/components/ui")>("@/components/ui");
  return { ...actual, useToast: () => ({ success: toastSuccess, error: vi.fn(), info: vi.fn() }) };
});

const { installationRoute } = await import("./installation");
const { ApiError } = await import("@/api/client");
const InstallationPage = (installationRoute as unknown as { component: () => ReactNode }).component;

beforeEach(() => {
  rename.mockClear();
  addDemo.mockClear();
  navigate.mockClear();
  toastSuccess.mockClear();
  access.canAdministerOrg = true;
  demo.error = null;
  addSuite.mockClear();
  invalidate.mockClear();
  suite.demo = null;
  suite.error = null;
  suite.loaded = true;
});

describe("the installation page", () => {
  it("renames the installation", async () => {
    render(<InstallationPage />);
    const name = screen.getByLabelText("Name");
    expect((name as HTMLInputElement).value).toBe("Acme");

    await userEvent.clear(name);
    await userEvent.type(name, "  Acme Works ");
    await userEvent.click(screen.getByRole("button", { name: "Rename" }));
    expect(rename).toHaveBeenCalledWith("Acme Works");
    expect(toastSuccess).toHaveBeenCalledWith("Renamed to Acme Works");
  });

  it("does not rename to nothing", async () => {
    render(<InstallationPage />);
    await userEvent.clear(screen.getByLabelText("Name"));
    expect(screen.getByRole("button", { name: "Rename" })).toHaveProperty("disabled", true);
  });

  it("adds the demo project and opens it", async () => {
    render(<InstallationPage />);
    await userEvent.click(screen.getByRole("button", { name: "Add the demo project" }));
    expect(addDemo).toHaveBeenCalled();
    expect(navigate).toHaveBeenCalledWith({ to: "/projects/$projectKey", params: { projectKey: "LINE" } });
  });

  it("says why the demo was not added", () => {
    demo.error = new Error("The demo needs the project keys LINE and MAINT, and LINE is taken here.");
    render(<InstallationPage />);
    expect(screen.getByText(/LINE is taken here/)).toBeTruthy();
  });

  it("adds the suite demo", async () => {
    render(<InstallationPage />);
    await userEvent.click(screen.getByRole("button", { name: "Add the suite demo" }));
    expect(addSuite).toHaveBeenCalled();
  });

  it("offers the suite demo only once it knows nobody added it", () => {
    suite.loaded = false;
    render(<InstallationPage />);
    expect(screen.queryByRole("button", { name: "Add the suite demo" })).toBeNull();
  });

  it("follows the suite demo while the worker builds it, then links its projects", () => {
    suite.demo = building("running", 2);
    const { rerender } = render(<InstallationPage />);
    expect(screen.getByRole("status").textContent).toBe("Building the suite demo: step 2 of 4.");
    expect(screen.queryByRole("button", { name: "Add the suite demo" })).toBeNull();

    suite.demo = building("done", 4);
    rerender(<InstallationPage />);
    expect(screen.queryByRole("status")).toBeNull();
    for (const key of ["ARM", "EXC", "STA"]) {
      expect(screen.getByRole("link", { name: key }).getAttribute("href")).toBe(`/projects/${key}`);
    }
    expect(invalidate).toHaveBeenCalled();
  });

  it("says why the suite demo stopped, and offers it again", async () => {
    suite.demo = { ...building("failed", 1), message: "The suite demo stopped: create the EXC project. Look at the worker's log for why." };
    render(<InstallationPage />);
    expect(screen.getByText(/Look at the worker's log/)).toBeTruthy();
    await userEvent.click(screen.getByRole("button", { name: "Add the suite demo" }));
    expect(addSuite).toHaveBeenCalled();
  });

  it("says which key holds the suite demo off", () => {
    suite.error = new ApiError(422, {
      code: "validation_failed",
      message: "Some fields need attention.",
      fields: { key: "The project key EXC is taken already." },
    });
    render(<InstallationPage />);
    expect(screen.getByText("The project key EXC is taken already.")).toBeTruthy();
  });

  it("offers nothing that makes or deletes an installation", () => {
    render(<InstallationPage />);
    expect(screen.queryByRole("button", { name: /delete|create/i })).toBeNull();
    expect(screen.queryByText(/organization/i)).toBeNull();
  });

  it("shows a member the name and nothing to change", () => {
    access.canAdministerOrg = false;
    render(<InstallationPage />);
    expect(screen.getByText("Acme")).toBeTruthy();
    expect(screen.queryByRole("textbox")).toBeNull();
    expect(screen.queryByRole("button")).toBeNull();
  });
});
