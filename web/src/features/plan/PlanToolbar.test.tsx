import { describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen } from "@testing-library/react";
import { asReader } from "@/test/reader";
import { PlanToolbar } from "./PlanToolbar";
import { NO_FILTERS, meter } from "./views";

function toolbar(props: Partial<Parameters<typeof PlanToolbar>[0]> = {}) {
  render(
    <PlanToolbar
      view="management"
      onView={() => {}}
      query=""
      onQuery={() => {}}
      filters={NO_FILTERS}
      onFilters={() => {}}
      closedForDays={null}
      onClosedForDays={() => {}}
      numbers={meter([])}
      milestones={[]}
      teams={[]}
      zoom="fit"
      onZoom={() => {}}
      showZoom
      {...props}
    />,
    { wrapper: asReader("en-GB") },
  );
}

describe("the plan's toolbar", () => {
  it("offers the auto plan only to someone who may move the issues", () => {
    toolbar();
    expect(screen.queryByRole("button", { name: "Auto plan" })).toBeNull();
  });

  it("asks for the proposal when pressed", () => {
    const onAutoPlan = vi.fn();
    toolbar({ onAutoPlan });
    fireEvent.click(screen.getByRole("button", { name: "Auto plan" }));
    expect(onAutoPlan).toHaveBeenCalledOnce();
  });

  it("cannot be pressed twice while the proposal is read", () => {
    toolbar({ onAutoPlan: () => {}, autoPlanning: true });
    expect((screen.getByRole("button", { name: "Planning..." }) as HTMLButtonElement).disabled).toBe(true);
  });
});
