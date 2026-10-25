import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";

const state = { admin: false };
const navigate = vi.fn();

vi.mock("@tanstack/react-router", () => ({ useNavigate: () => navigate }));
vi.mock("@/api/access", () => ({ useAccess: () => ({ data: { canAdministerOrg: state.admin } }) }));
vi.mock("@/api/issues", () => ({ useIssues: () => ({ data: { issues: [] } }) }));
vi.mock("@/api/projects", () => ({ useProject: () => ({ data: undefined }), useProjectSearch: () => ({ data: { projects: [] } }) }));
vi.mock("@/api/assistant", () => ({
  useAsk: () => ({ mutate: vi.fn(), reset: vi.fn(), isPending: false }),
  useAssistantStatus: () => ({ data: { configured: false } }),
}));

const { CommandPalette } = await import("./CommandPalette");

/** The palette's options for some words, by their labels. */
async function offered(words: string): Promise<string[]> {
  render(<CommandPalette open onClose={() => {}} onNewIssue={() => {}} onToggleSidebar={() => {}} />);
  await userEvent.type(screen.getByRole("combobox"), words);
  return Array.from(document.querySelectorAll("[data-palette-option]")).map((option) => option.textContent ?? "");
}

beforeEach(() => {
  state.admin = false;
  navigate.mockReset();
});
afterEach(cleanup);

describe("the palette's pages", () => {
  it("reaches every settings page an administrator has, the ones the old list left out included", async () => {
    state.admin = true;
    const all = await offered("settings");
    for (const label of ["Installation", "Automation", "Webhooks", "Fields", "Issue view", "Audit log", "Connected apps"]) {
      expect(all, label).toContain(`Settings / ${label}`);
    }
  });

  it("offers somebody who does not administer no settings page", async () => {
    for (const words of ["settings", "audit", "webhooks"]) {
      const all = await offered(words);
      expect(
        all.filter((label) => label.startsWith("Settings")),
        words,
      ).toEqual([]);
      cleanup();
    }
  });

  it("reaches the person's own pages and opens them where they live", async () => {
    const all = await offered("notification");
    expect(all).toContain("Notification settings");
    expect(all).toContain("Notifications");
    await userEvent.click(screen.getByText("Notification settings"));
    expect(navigate).toHaveBeenCalledWith({ to: "/settings/profile", search: undefined, hash: "notifications" });
  });

  it("offers Watching as the search for what the person watches", async () => {
    await offered("watching");
    await userEvent.click(screen.getByText("Watching"));
    expect(navigate).toHaveBeenCalledWith({ to: "/search", search: { q: "watcher = currentUser()" }, hash: undefined });
  });
});

// The suite's shared names (#309): the palette is Quick search, as in Stator and Exciter.
describe("the palette's name", () => {
  it("is Quick search", () => {
    render(<CommandPalette open onClose={() => {}} onNewIssue={() => {}} onToggleSidebar={() => {}} />);
    expect(screen.getByRole("dialog", { name: "Quick search" })).toBeInTheDocument();
  });
});
