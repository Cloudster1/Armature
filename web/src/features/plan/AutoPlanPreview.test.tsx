import { describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen } from "@testing-library/react";
import type { ReactNode } from "react";
import type { Alignment, PlanMove } from "@/api/plan";
import { PLAN_ALIGN_MAX_MOVES } from "@/config";

vi.mock("@tanstack/react-router", () => ({
  Link: ({ children, to, params, ...rest }: { children: ReactNode; to: string; params?: Record<string, string> } & Record<string, unknown>) => (
    <a href={to.replace("$issueKey", params?.issueKey ?? "")} {...(rest as Record<string, string>)}>
      {children}
    </a>
  ),
}));

const { AutoPlanPreview } = await import("./AutoPlanPreview");

function move(n: number): PlanMove {
  return {
    issueKey: `PR-${n}`,
    from: { startDate: "2027-11-03T00:00:00Z", dueDate: "2027-11-04T00:00:00Z" },
    to: { startDate: "2027-11-08T00:00:00Z", dueDate: "2027-11-09T00:00:00Z" },
    workingDays: 2,
    after: "PR-1",
  };
}

function preview(alignment: Partial<Alignment>, props: Partial<Parameters<typeof AutoPlanPreview>[0]> = {}) {
  const handlers = { onApply: vi.fn(), onCancel: vi.fn(), onPreviewAgain: vi.fn(), onPlace: vi.fn() };
  render(
    <AutoPlanPreview alignment={{ projectKey: "PR", moves: [], unplaced: [], warnings: [], ...alignment }} locale="en-GB" applying={false} place={false} {...handlers} {...props} />,
  );
  return handlers;
}

describe("the auto plan's preview", () => {
  it("lists every move and the warnings left after it, and applies them", () => {
    const { onApply } = preview({
      moves: [move(2), move(3)],
      warnings: [{ issueKey: "PR-3", kind: "past-milestone", message: "this is due after Launch" }],
    });
    expect(screen.getByText(/2 issues would move/)).toBeTruthy();
    expect(document.querySelector('[data-auto-plan-move="PR-2"]')?.textContent).toContain("moves from 3 Nov 2027 to 8 Nov 2027, 2 working days, after PR-1");
    expect(document.querySelector('[data-auto-plan-warning-for="PR-3"]')?.textContent).toContain("due after Launch");
    fireEvent.click(screen.getByRole("button", { name: "Apply" }));
    expect(onApply).toHaveBeenCalledOnce();
  });

  it("says so when nothing needs to move, and offers nothing to apply", () => {
    preview({});
    expect(screen.getByText("Every issue already starts after the issues blocking it.")).toBeTruthy();
    expect(screen.queryByRole("button", { name: "Apply" })).toBeNull();
    expect(screen.getByRole("button", { name: "Close" })).toBeTruthy();
  });

  it("shows a refusal and offers to preview again", () => {
    const { onPreviewAgain } = preview({ moves: [move(2)] }, { refusal: "PR-2 was moved since these dates were read, so nothing was saved. Preview the auto plan again." });
    expect(screen.getByRole("alert").textContent).toContain("PR-2 was moved since");
    expect((screen.getByRole("button", { name: "Apply" }) as HTMLButtonElement).disabled).toBe(true);
    fireEvent.click(screen.getByRole("button", { name: "Preview again" }));
    expect(onPreviewAgain).toHaveBeenCalledOnce();
  });

  it("refuses more moves than are saved at once", () => {
    preview({ moves: Array.from({ length: PLAN_ALIGN_MAX_MOVES + 1 }, (_, i) => move(i + 2)) });
    expect(screen.getByRole("alert").textContent).toContain(`at most ${PLAN_ALIGN_MAX_MOVES} move at once`);
    expect((screen.getByRole("button", { name: "Apply" }) as HTMLButtonElement).disabled).toBe(true);
  });

  it("closes on Escape and on Cancel", () => {
    const { onCancel } = preview({ moves: [move(2)] });
    fireEvent.keyDown(window, { key: "Escape" });
    fireEvent.click(screen.getByRole("button", { name: "Cancel" }));
    expect(onCancel).toHaveBeenCalledTimes(2);
  });

  it("asks again with work without dates placed, and lists what it could not place", () => {
    const { onPlace } = preview({ unplaced: [{ issueKey: "PR-9", reason: "Not placed: it has no estimate." }] });
    fireEvent.click(screen.getByRole("checkbox", { name: /Also place issues without dates/ }));
    expect(onPlace).toHaveBeenCalledWith(true);
    expect(document.querySelector('[data-auto-plan-unplaced="PR-9"]')?.textContent).toContain("Not placed: it has no estimate.");
  });

  it("applies nothing while a new proposal is read", () => {
    preview({ moves: [move(2)] }, { reading: true, place: true });
    expect((screen.getByRole("button", { name: "Apply" }) as HTMLButtonElement).disabled).toBe(true);
    expect((screen.getByRole("checkbox", { name: /Also place/ }) as HTMLInputElement).checked).toBe(true);
  });
});
