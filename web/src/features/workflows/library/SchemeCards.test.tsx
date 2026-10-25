import { beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, render, waitFor, within } from "@testing-library/react";
import type { Scheme } from "@/api/workflows";

const remove = vi.fn();
const confirm = vi.fn();
const schemes: Scheme[] = [
  { id: "s-used", name: "Busy scheme", isDefault: false, items: [], projectKeys: ["OPS", "WEB"] },
  { id: "s-idle", name: "Idle scheme", isDefault: false, items: [], projectKeys: [] },
];
vi.mock("@/api/workflows", () => ({
  useSchemes: () => ({ data: { schemes, total: schemes.length, offset: 0, limit: 20 } }),
  useDeleteScheme: () => ({ mutate: remove, isPending: false, error: null }),
  useSetDefaultScheme: () => ({ mutate: vi.fn(), isPending: false, error: null }),
}));
vi.mock("@/features/workflows/SchemeEditor", () => ({ SchemeEditor: () => null }));
vi.mock("@/features/shell/ConfirmProvider", () => ({ useConfirm: () => confirm }));

import { SchemeCards } from "./SchemeCards";

function card(container: HTMLElement, name: string) {
  return container.querySelector(`[data-scheme-card="${name}"]`) as HTMLElement;
}

beforeEach(() => {
  remove.mockReset();
  confirm.mockReset();
});

describe("deleting a workflow scheme", () => {
  it("is not offered for a scheme a project uses, which names the projects to move first", () => {
    const { container } = render(<SchemeCards q="" offset={0} onShow={vi.fn()} canConfigure />);
    const busy = card(container, "Busy scheme");

    expect(busy.querySelector('[data-action="delete"]')).toBeNull();
    expect(within(busy).getByText("To delete this scheme, move OPS, WEB to another scheme first.")).toBeInTheDocument();
  });

  it("asks about an unused scheme without promising a fallback, and deletes it on yes", async () => {
    confirm.mockResolvedValue(true);
    const { container } = render(<SchemeCards q="" offset={0} onShow={vi.fn()} canConfigure />);
    const idle = card(container, "Idle scheme");

    expect(within(idle).queryByText(/move .* to another scheme/)).toBeNull();
    fireEvent.click(idle.querySelector('[data-action="delete"]') as HTMLElement);

    await waitFor(() => expect(remove).toHaveBeenCalledWith("s-idle"));
    const asked = confirm.mock.calls[0]?.[0] as { body: string };
    expect(asked.body).toBe("Idle scheme goes. No project uses it, so nothing else changes.");
    expect(asked.body).not.toMatch(/fall back/);
  });
});
