import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { ReactNode } from "react";

const markRead = vi.fn();
const notifications = [
  { id: "n1", kind: "mentioned", issueKey: "ALP-1", title: "Ada mentioned you on ALP-1", createdAt: "2026-10-10T08:00:00Z" },
  { id: "n2", kind: "absence", title: "Max recorded an absence for you", createdAt: "2026-10-09T08:00:00Z", readAt: "2026-10-09T09:00:00Z" },
];

vi.mock("@tanstack/react-router", () => ({
  Link: ({ children, to, hash, params, onClick, ...rest }: { children: ReactNode; to: string; hash?: string; params?: Record<string, string>; onClick?: () => void } & Record<string, unknown>) => (
    <a href={`${params?.issueKey ? to.replace("$issueKey", params.issueKey) : to}${hash ? `#${hash}` : ""}`} onClick={(e) => { e.preventDefault(); onClick?.(); }} {...(rest as Record<string, string>)}>
      {children}
    </a>
  ),
}));
vi.mock("@/api/notifications", () => ({
  useUnreadCount: () => ({ data: { unread: 1 } }),
  useInbox: () => ({ data: { notifications }, isLoading: false }),
  useMarkRead: () => ({ mutate: markRead, isPending: false }),
}));

vi.mock("@/lib/format", () => ({ useFormat: () => ({ relative: () => "a day ago" }) }));

const { NotificationsBell } = await import("./nav");

afterEach(cleanup);

describe("the bell", () => {
  it("opens the latest notifications with Mark all as read, See all and Notification settings", async () => {
    render(<NotificationsBell />);
    await userEvent.click(screen.getByRole("button", { name: "Notifications, 1 unread" }));
    const panel = screen.getByRole("dialog", { name: "Notifications" });
    expect(panel.querySelectorAll("[data-notification]")).toHaveLength(2);
    expect(panel.querySelector('[data-notification="mentioned"]')?.getAttribute("href")).toBe("/issues/ALP-1");
    expect(screen.getByRole("link", { name: "See all" })).toHaveAttribute("href", "/inbox");
    expect(screen.getByRole("link", { name: "Notification settings" })).toHaveAttribute("href", "/settings/profile#notifications");
    await userEvent.click(screen.getByRole("button", { name: "Mark all as read" }));
    expect(markRead).toHaveBeenCalledWith({ all: true });
  });

  it("marks one read as it is opened, and closes", async () => {
    render(<NotificationsBell />);
    await userEvent.click(screen.getByRole("button", { name: "Notifications, 1 unread" }));
    await userEvent.click(screen.getByText("Ada mentioned you on ALP-1"));
    expect(markRead).toHaveBeenCalledWith({ ids: ["n1"] });
    expect(screen.queryByRole("dialog", { name: "Notifications" })).toBeNull();
  });
});
