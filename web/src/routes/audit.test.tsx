import { beforeEach, describe, expect, it, vi } from "vitest";
import { render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { ReactNode } from "react";
import type { AuditEntry, AuditFilter } from "@/api/audit";

// The administrative changes that reach the log by the act itself or off the stream.
const recorded = [
  "auth.login",
  "group.deleted",
  "invite.created",
  "invite.revoked",
  "role.defined",
  "role.redefined",
  "role.removed",
  "rule.created",
  "rule.deleted",
  "rule.updated",
  "webhook.created",
  "webhook.deleted",
  "webhook.secret_rotated",
  "webhook.updated",
];

const rotated: AuditEntry = {
  id: "a1",
  action: "webhook.secret_rotated",
  targetType: "webhook",
  targetId: "w1",
  actorId: "u1",
  actorName: "Ada Admin",
  data: { name: "Elsewhere", address: "https://hooks.example", webhookId: "w1" },
  createdAt: "2026-10-10T09:00:00Z",
};

const asked: AuditFilter[] = [];

vi.mock("@tanstack/react-router", () => ({ createRoute: (options: unknown) => options }));
vi.mock("./app", () => ({ appRoute: {} }));
vi.mock("@/lib/format", () => ({ useFormat: () => ({ dateTime: (iso: string) => `at ${iso}` }) }));
vi.mock("@/api/audit", () => ({
  auditExportHref: () => "/api/v1/audit/export",
  useAuditLog: (filter: AuditFilter) => {
    asked.push(filter);
    const entries = filter.action && filter.action !== rotated.action ? [] : [rotated];
    return { data: { entries, actions: recorded }, isLoading: false, error: null };
  },
}));

const { auditRoute } = await import("./audit");
const AuditPage = (auditRoute as unknown as { component: () => ReactNode }).component;

beforeEach(() => {
  asked.length = 0;
});

describe("the audit page", () => {
  it("offers every administrative kind the log holds as a filter", () => {
    render(<AuditPage />);
    const options = within(screen.getByLabelText("Action"))
      .getAllByRole("option")
      .map((o) => (o as HTMLOptionElement).value);
    for (const action of recorded) expect(options).toContain(action);
  });

  it("narrows the log to a rotated secret, and shows the webhook without its secret", async () => {
    render(<AuditPage />);
    await userEvent.selectOptions(screen.getByLabelText("Action"), "webhook.secret_rotated");
    expect(asked.at(-1)?.action).toBe("webhook.secret_rotated");
    const row = document.querySelector('[data-audit-row="webhook.secret_rotated"]') as HTMLElement;
    expect(within(row).getByText("Ada Admin")).toBeTruthy();
    expect(row.textContent).toContain("name: Elsewhere");
    expect(row.textContent).toContain("address: https://hooks.example");
    expect(row.textContent).not.toMatch(/secret:/);
  });
});
