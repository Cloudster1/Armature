import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, render } from "@testing-library/react";
import type { ReactNode } from "react";

const state = { admin: false };

vi.mock("@tanstack/react-router", () => ({
  Link: ({ children, to, hash, search, ...rest }: { children: ReactNode; to: string; hash?: string; search?: Record<string, string> } & Record<string, unknown>) => {
    const query = search ? `?${new URLSearchParams(search)}` : "";
    const { className } = rest as { className?: string };
    return (
      <a href={`${to}${query}${hash ? `#${hash}` : ""}`} className={className} data-settings-link={rest["data-settings-link"] as string}>
        {children}
      </a>
    );
  },
  createRoute: (options: unknown) => options,
}));
vi.mock("./app", () => ({ appRoute: {} }));
vi.mock("@/api/auth", () => ({ useMe: () => ({ data: { principal: { installation: { name: "Acme" } } } }) }));
vi.mock("@/api/access", () => ({ useAccess: () => ({ data: { canAdministerOrg: state.admin } }) }));

const { settingsRoute } = await import("./settings");
const { axeViolations } = await import("@/test/axe");
const SettingsHub = (settingsRoute as unknown as { component: () => ReactNode }).component;

const links = (group: string) => Array.from(document.querySelectorAll(`[data-settings-group="${group}"] [data-settings-link]`)).map((a) => a.getAttribute("data-settings-link"));

afterEach(() => {
  cleanup();
  state.admin = false;
});

describe("the settings hub", () => {
  it("gives everyone the You card with their own pages", () => {
    render(<SettingsHub />);
    expect(links("You")).toEqual(["Your profile", "Notification settings", "Watching", "Themes", "Access tokens"]);
    expect(document.querySelector('[data-settings-group="Settings"]')).toBeNull();
    expect(document.querySelector('[data-settings-group="Everyone"]')).toBeNull();
  });

  it("gives an administrator the Settings card, Issue view among it", () => {
    state.admin = true;
    render(<SettingsHub />);
    expect(links("Settings")).toEqual(["Installation", "Access", "Users", "Holidays", "Workflows", "Labels", "Automation", "Fields", "Issue view", "Connected apps", "Webhooks", "Audit log"]);
    expect(document.querySelector('a[href="/settings/issue-view"]')).not.toBeNull();
  });
});

describe("the settings hub's accessibility", () => {
  it("breaks no rule, for an administrator or for anybody else", async () => {
    for (const admin of [true, false]) {
      state.admin = admin;
      const { unmount } = render(
        <main>
          <SettingsHub />
        </main>,
      );
      expect(await axeViolations(), admin ? "administrator" : "member").toEqual([]);
      unmount();
    }
  });
});
