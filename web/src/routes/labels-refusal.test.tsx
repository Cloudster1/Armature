import { afterEach, describe, expect, it, vi } from "vitest";
import { act, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { ReactNode } from "react";
import type { Label } from "@/api/labels";
import { LABEL_PAGE_SIZE } from "@/config";
import { ConfirmProvider } from "@/features/shell/ConfirmProvider";
import { refusal, renderInShell, serve } from "@/test/shell";

vi.mock("@tanstack/react-router", () => ({
  Link: ({ children }: { children: ReactNode }) => <a>{children}</a>,
  createRoute: (options: object) => ({ ...options, useSearch: () => ({}) }),
  useNavigate: () => vi.fn(),
}));
vi.mock("./app", () => ({ appRoute: {} }));
vi.mock("@/api/access", () => ({ useAccess: () => ({ data: { canAdministerOrg: true } }) }));

const { labelsRoute } = await import("./labels");
const LabelsPage = (labelsRoute as unknown as { component: () => ReactNode }).component;

const label = (name: string): Label => ({ id: name, name, color: "blue", issueCount: 1, createdAt: "2026-10-10T08:00:00Z", updatedAt: "2026-10-10T08:00:00Z" });
// The label removed is the last row of a full page, far below a banner above the table.
const labels = Array.from({ length: LABEL_PAGE_SIZE }, (_, i) => label(`word-${String(i).padStart(2, "0")}`));
const last = labels.at(-1)!.name;
const refused = "That label is held by a workflow rule. Change the rule first, then remove the label.";

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("a refused removal on the labels page", () => {
  it("is said in a toast, not in a banner above the table", async () => {
    serve({
      [`GET /labels?limit=${LABEL_PAGE_SIZE}`]: [200, { labels, total: labels.length, limit: LABEL_PAGE_SIZE, offset: 0 }],
      [`DELETE /labels/${last}`]: [409, refusal("conflict", refused)],
    });
    renderInShell(
      <ConfirmProvider>
        <LabelsPage />
      </ConfirmProvider>,
    );

    await userEvent.click(await screen.findByRole("button", { name: `Remove label ${last}` }));
    await userEvent.click(screen.getByRole("button", { name: "Remove label" }));

    const said = await screen.findByText(refused);
    expect(said.closest("[data-toast]")).not.toBeNull();
    await act(() => new Promise((resolve) => setTimeout(resolve)));
    expect(screen.getAllByText(refused)).toHaveLength(1);
    expect([...document.querySelectorAll('[role="alert"]')].filter((el) => !el.closest("[data-toast]"))).toHaveLength(0);
  });
});
