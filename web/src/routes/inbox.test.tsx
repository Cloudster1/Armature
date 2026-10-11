import { describe, expect, it, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import type { ReactNode } from "react";
import type { Notification } from "@/api/notifications";

vi.mock("@tanstack/react-router", () => ({
  Link: ({ children, to, params, ...rest }: { children: ReactNode; to: string; params?: Record<string, string> }) => {
    const href = Object.entries(params ?? {}).reduce((path, [name, value]) => path.replace(`$${name}`, value), to);
    const { className, onClick } = rest as { className?: string; onClick?: () => void };
    return (
      <a href={href} className={className} onClick={onClick}>
        {children}
      </a>
    );
  },
  createRoute: (options: unknown) => options,
}));
vi.mock("./app", () => ({ appRoute: {} }));
vi.mock("@/lib/format", () => ({ useFormat: () => ({ relative: () => "just now" }) }));

const items: Notification[] = [
  { id: "n1", kind: "absence", title: "Max Master recorded an absence for you", body: "Your profile lists the days.", link: "http://app.test/settings/profile", createdAt: "2026-10-10T08:00:00Z" },
  { id: "n2", kind: "assigned", issueKey: "CP-1", title: "Ada assigned CP-1 to you", link: "http://app.test/issues/CP-1", createdAt: "2026-10-10T07:00:00Z" },
];
vi.mock("@/api/notifications", async () => {
  const actual = await vi.importActual<typeof import("@/api/notifications")>("@/api/notifications");
  return {
    ...actual,
    useInbox: () => ({ data: { notifications: items }, isLoading: false }),
    useMarkRead: () => ({ mutate: vi.fn(), isPending: false }),
  };
});

const { inboxRoute } = await import("./inbox");
const { NOTIFICATION_KINDS } = await import("@/api/notifications");
const InboxPage = (inboxRoute as unknown as { component: () => ReactNode }).component;

describe("the inbox", () => {
  it("opens a notice about your absences on your profile, where they are listed", () => {
    render(<InboxPage />);
    const absence = screen.getByText("Max Master recorded an absence for you").closest("a") as HTMLAnchorElement;
    expect(absence.getAttribute("href")).toBe("/settings/profile");
    const issue = screen.getByText("Ada assigned CP-1 to you").closest("a") as HTMLAnchorElement;
    expect(issue.getAttribute("href")).toBe("/issues/CP-1");
  });

  it("offers absence notices among the kinds a person can turn off", () => {
    expect(NOTIFICATION_KINDS.find((k) => k.kind === "absence")?.label).toBe("Somebody else changes my absences");
  });
});
