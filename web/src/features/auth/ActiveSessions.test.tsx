import { beforeEach, describe, expect, it, vi } from "vitest";
import { render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { ActiveSession } from "@/api/auth";
import { ToastProvider } from "@/components/ui";
import { ConfirmProvider } from "@/features/shell/ConfirmProvider";
import { ActiveSessions } from "./ActiveSessions";

const end = vi.fn();
const endOthers = vi.fn();
let sessions: ActiveSession[];

vi.mock("@/api/auth", () => ({
  useSessions: () => ({ data: { sessions }, error: null }),
  useEndSession: () => ({ mutate: end, isPending: false, error: null }),
  useEndOtherSessions: () => ({ mutate: endOthers, isPending: false, error: null }),
}));
vi.mock("@/lib/format", () => ({ useFormat: () => ({ relative: (iso: string) => `since ${iso}` }) }));

const here: ActiveSession = {
  id: "s1",
  userAgent: "Mozilla/5.0 (X11; Linux x86_64; rv:131.0) Gecko/20100101 Firefox/131.0",
  ip: "192.0.2.1",
  orgName: "Acme",
  proof: "password",
  createdAt: "2026-10-12T08:00:00Z",
  lastSeenAt: "2026-10-13T09:00:00Z",
  current: true,
};
const library: ActiveSession = {
  ...here,
  id: "s2",
  userAgent: "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/129.0.0.0 Safari/537.36",
  ip: "198.51.100.7",
  lastSeenAt: "2026-10-01T17:00:00Z",
  current: false,
};

function show() {
  render(
    <ToastProvider>
      <ConfirmProvider>
        <ActiveSessions />
      </ConfirmProvider>
    </ToastProvider>,
  );
}

beforeEach(() => {
  vi.clearAllMocks();
  sessions = [library, here];
});

describe("where you are signed in", () => {
  it("names each device with its address, organization and last activity", () => {
    show();
    const other = document.querySelector('[data-session="s2"]') as HTMLElement;
    expect(within(other).getByText("Chrome on Windows")).toBeTruthy();
    expect(other.textContent).toContain("198.51.100.7 · Acme · last active since 2026-10-01T17:00:00Z");
    expect(within(other).queryByText("This device")).toBeNull();
  });

  it("tags this device, which is signed out from the menu rather than here", () => {
    show();
    const current = document.querySelector('[data-session="current"]') as HTMLElement;
    expect(within(current).getByText("Firefox on Linux")).toBeTruthy();
    expect(within(current).getByText("This device")).toBeTruthy();
    expect(current.textContent).toContain("active now");
    expect(within(current).queryByRole("button", { name: "Sign out" })).toBeNull();
  });

  it("asks before signing a device out, and leaves it be when the answer is no", async () => {
    show();
    const other = document.querySelector('[data-session="s2"]') as HTMLElement;
    await userEvent.click(within(other).getByRole("button", { name: "Sign out" }));
    expect(screen.getByRole("dialog", { name: "Sign out Chrome on Windows?" })).toBeTruthy();
    await userEvent.click(screen.getByRole("button", { name: "Cancel" }));
    expect(end).not.toHaveBeenCalled();

    await userEvent.click(within(other).getByRole("button", { name: "Sign out" }));
    await userEvent.click(within(screen.getByRole("dialog")).getByRole("button", { name: "Sign out Chrome on Windows" }));
    expect(end).toHaveBeenCalledWith("s2", expect.anything());
  });

  it("signs out everywhere else after saying how many end", async () => {
    show();
    await userEvent.click(screen.getByRole("button", { name: "Sign out everywhere else" }));
    const dialog = screen.getByRole("dialog", { name: "Sign out everywhere else?" });
    expect(dialog.textContent).toContain("The one other session ends. This one stays signed in.");
    await userEvent.click(within(dialog).getByRole("button", { name: "Sign out everywhere else" }));
    expect(endOthers).toHaveBeenCalled();
  });

  it("offers nothing to end when this is the only session", () => {
    sessions = [here];
    show();
    expect(screen.getByRole("button", { name: "Sign out everywhere else" })).toBeDisabled();
    expect(screen.getByText("You are signed in here and nowhere else.")).toBeTruthy();
  });
});
