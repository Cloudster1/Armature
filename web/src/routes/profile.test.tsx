import { beforeEach, describe, expect, it, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { ReactNode } from "react";
import type { NotificationPreferences } from "@/api/notifications";
import { ToastProvider } from "@/components/ui";
import { ConfirmProvider } from "@/features/shell/ConfirmProvider";

const save = vi.fn();
let preferences: NotificationPreferences;
const idle = { mutate: vi.fn(), isPending: false, error: null };

vi.mock("@tanstack/react-router", () => ({
  Link: ({ children, to }: { children: ReactNode; to: string }) => <a href={to}>{children}</a>,
  createRoute: (options: unknown) => options,
  useNavigate: () => vi.fn(),
}));
vi.mock("./app", () => ({ appRoute: {} }));
vi.mock("@/api/auth", () => ({
  LOCALES: [],
  MY_DATA_HREF: "/api/v1/auth/me/export",
  useMe: () => ({ data: { principal: { user: { id: "u1", name: "Ada", email: "ada@example.com", timezone: "UTC", locale: "en-GB" } }, organizations: [] } }),
  useUpdateProfile: () => idle,
  useUploadAvatar: () => idle,
  useRemoveAvatar: () => idle,
  useChangeMyPassword: () => idle,
  useEraseMe: () => idle,
  useSessions: () => ({ data: { sessions: [] } }),
  useEndSession: () => idle,
  useEndOtherSessions: () => idle,
}));
// Two-step sign-in has tests of its own, beside it.
vi.mock("@/features/auth/SecondFactor", () => ({ SecondFactorSettings: () => null }));
vi.mock("@/api/availability", () => ({
  useWorkingWeek: () => ({ data: undefined }),
  useAbsences: () => ({ data: undefined }),
}));
vi.mock("@/api/notifications", async (actual) => ({
  ...(await actual<typeof import("@/api/notifications")>()),
  useNotificationPreferences: () => ({ data: { preferences } }),
  useSaveNotificationPreferences: () => ({ mutate: save, isPending: false, error: null }),
}));

const { profileRoute } = await import("./profile");
const ProfilePage = (profileRoute as unknown as { component: () => ReactNode }).component;

function renderPage() {
  render(
    <ToastProvider>
      <ConfirmProvider>
        <ProfilePage />
      </ConfirmProvider>
    </ToastProvider>,
  );
}

beforeEach(() => {
  save.mockClear();
  preferences = { mail: {}, inapp: {}, digest: "off", watchOwn: true };
});

describe("the notification settings on the profile", () => {
  it("offers watching my own issues, and saves it turned off", async () => {
    renderPage();
    const watchOwn = screen.getByRole("checkbox", { name: /Watch the issues I report or am assigned/ });
    expect(watchOwn).toBeChecked();

    await userEvent.click(watchOwn);
    await userEvent.click(screen.getByRole("button", { name: "Save notification settings" }));
    expect(save).toHaveBeenCalledWith(expect.objectContaining({ watchOwn: false }), expect.anything());
  });

  it("shows it off when it was saved off", () => {
    preferences.watchOwn = false;
    renderPage();
    expect(screen.getByRole("checkbox", { name: /Watch the issues I report or am assigned/ })).not.toBeChecked();
  });
});
