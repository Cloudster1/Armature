import { beforeEach, describe, expect, it, vi } from "vitest";
import { render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { ReactNode } from "react";
import type { AuditEntry, AuditFilter } from "@/api/audit";

// The administrative changes that reach the log by the act itself or off the stream.
const recorded = [
  "auth.login",
  "auth.session_ended",
  "auth.sessions_ended",
  "group.deleted",
  "installation.renamed",
  "invite.created",
  "invite.revoked",
  "org.created",
  "org.second_factor_required",
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

const signedOut: AuditEntry = {
  id: "a2",
  action: "auth.session_ended",
  targetType: "session",
  targetId: "s2",
  actorId: "u1",
  actorName: "Ada Admin",
  data: { userAgent: "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/129.0.0.0 Safari/537.36" },
  ip: "192.0.2.1",
  createdAt: "2026-10-11T09:00:00Z",
};

const ruled: AuditEntry = {
  id: "a3",
  action: "org.second_factor_required",
  targetType: "org",
  targetId: "o1",
  actorId: "u1",
  actorName: "Ada Admin",
  data: { rule: "administrators" },
  createdAt: "2026-10-12T09:00:00Z",
};

const asked: AuditFilter[] = [];

vi.mock("@tanstack/react-router", () => ({ createRoute: (options: unknown) => options }));
vi.mock("./app", () => ({ appRoute: {} }));
vi.mock("@/lib/format", () => ({ useFormat: () => ({ dateTime: (iso: string) => `at ${iso}` }) }));
vi.mock("@/api/audit", () => ({
  auditExportHref: () => "/api/v1/audit/export",
  useAuditLog: (filter: AuditFilter) => {
    asked.push(filter);
    const entries = [rotated, signedOut, ruled].filter((e) => !filter.action || e.action === filter.action);
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

  // Stored rows and scripts keep the old keys; the page reads them as the installation's.
  it("reads what was stored under the organization as the installation's, and filters by the stored key", async () => {
    render(<AuditPage />);
    const select = screen.getByLabelText("Action");
    const words = within(select)
      .getAllByRole("option")
      .map((o) => o.textContent ?? "");
    expect(words).toContain("installation.created");
    expect(words).toContain("installation.second_factor_required");
    expect(words.join(" ")).not.toMatch(/\borg\./);
    await userEvent.selectOptions(select, "installation.second_factor_required");
    expect(asked.at(-1)?.action).toBe("org.second_factor_required");
    const row = document.querySelector('[data-audit-row="org.second_factor_required"]') as HTMLElement;
    expect(row.textContent).toContain("installation.second_factor_required");
    expect(row.textContent).not.toContain("org.");
  });

  it("names the device a session was signed out on, not its user agent", async () => {
    render(<AuditPage />);
    await userEvent.selectOptions(screen.getByLabelText("Action"), "auth.session_ended");
    const row = document.querySelector('[data-audit-row="auth.session_ended"]') as HTMLElement;
    expect(row.textContent).toContain("device: Chrome on Windows · from 192.0.2.1");
    expect(row.textContent).not.toContain("Mozilla");
  });
});
