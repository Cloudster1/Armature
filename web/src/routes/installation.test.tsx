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

vi.mock("@tanstack/react-router", () => ({
  Link: ({ children, to }: { children: ReactNode; to: string }) => <a href={to}>{children}</a>,
  createRoute: (options: unknown) => options,
  redirect: (options: unknown) => options,
  useNavigate: () => navigate,
}));
vi.mock("./app", () => ({ appRoute: {} }));
vi.mock("@/api/access", () => ({ useAccess: () => ({ data: access }) }));
vi.mock("@/api/installation", () => ({
  useInstallation: () => ({ data: { installation: { name: "Acme" } } }),
  useRenameInstallation: () => ({ mutateAsync: rename, isPending: false, error: null }),
  useAddDemoProject: () => ({ mutateAsync: addDemo, isPending: false, error: demo.error }),
}));
vi.mock("@/components/ui", async () => {
  const actual = await vi.importActual<typeof import("@/components/ui")>("@/components/ui");
  return { ...actual, useToast: () => ({ success: toastSuccess, error: vi.fn(), info: vi.fn() }) };
});

const { installationRoute } = await import("./installation");
const InstallationPage = (installationRoute as unknown as { component: () => ReactNode }).component;

beforeEach(() => {
  rename.mockClear();
  addDemo.mockClear();
  navigate.mockClear();
  toastSuccess.mockClear();
  access.canAdministerOrg = true;
  demo.error = null;
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
