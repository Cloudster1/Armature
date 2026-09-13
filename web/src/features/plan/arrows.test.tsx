import { describe, expect, it, vi } from "vitest";
import { fireEvent, render } from "@testing-library/react";
import type { Issue } from "@/api/issues";
import type { PlanItem } from "@/api/plan";
import { makeScale } from "./scale";
import { DependencyArrows, arrowPath } from "./Timeline";
import type { Row } from "./views";

function item(key: string, start: string, due: string): PlanItem {
  return {
    issue: { id: key, key, summary: key, type: { id: "s", name: "Story", icon: "S", level: 0, isSubtask: false } } as Issue,
    depth: 0,
    start,
    due,
    derived: false,
    progress: { total: 0, done: 0 },
    children: [],
  } as unknown as PlanItem;
}

const a = item("P-1", "2026-03-02", "2026-03-06");
const b = item("P-2", "2026-03-09", "2026-03-13");
const rows: Row[] = [
  { kind: "issue", key: "P-1", item: a, hasChildren: false, collapsed: false },
  { kind: "issue", key: "P-2", item: b, hasChildren: false, collapsed: false },
];
const scale = makeScale(new Date("2026-03-01"), new Date("2026-03-20"), 20);
const dependency = { linkId: "l1", blockerKey: "P-1", blockedKey: "P-2" };

describe("arrowPath", () => {
  it("bends past both bars and puts the handle on the vertical leg's middle", () => {
    const { d, handle } = arrowPath(100, 17, 200, 51);
    expect(d).toBe("M 100 17 H 192 V 51 H 200");
    expect(handle).toEqual({ x: 192, y: 34 });
  });
});

describe("DependencyArrows", () => {
  it("draws an arrow per dependency with a wide hit path that selects it", () => {
    const onSelect = vi.fn();
    const { container } = render(
      <DependencyArrows dependencies={[dependency]} rows={rows} rowIndex={new Map([["P-1", 0], ["P-2", 1]])} scale={scale} height={68} selected={null} onSelect={onSelect} onRemove={() => {}} />,
    );
    expect(container.querySelectorAll("[data-plan-arrow]")).toHaveLength(1);
    fireEvent.click(container.querySelector('[data-plan-arrow-hit="P-1->P-2"]')!);
    expect(onSelect).toHaveBeenCalledWith(dependency);
  });

  it("marks the selected arrow", () => {
    const { container } = render(
      <DependencyArrows dependencies={[dependency]} rows={rows} rowIndex={new Map([["P-1", 0], ["P-2", 1]])} scale={scale} height={68} selected={dependency} onSelect={() => {}} onRemove={() => {}} />,
    );
    expect(container.querySelector('[data-plan-arrow="P-1->P-2"]')?.getAttribute("data-selected")).toBe("true");
  });

  it("offers to remove a selected arrow where the arrow is", () => {
    const onRemove = vi.fn();
    const { container } = render(
      <DependencyArrows dependencies={[dependency]} rows={rows} rowIndex={new Map([["P-1", 0], ["P-2", 1]])} scale={scale} height={68} selected={dependency} onSelect={() => {}} onRemove={onRemove} />,
    );
    const control = container.querySelector<HTMLButtonElement>('[data-plan-unlink="l1"]')!;
    expect(control.textContent).toContain("Remove dependency P-1 blocks P-2");
    // On the elbow's vertical leg, halfway between the two rows.
    const { handle } = arrowPath(scale.x("2026-03-06") + scale.pxPerDay, 17, scale.x("2026-03-09"), 51);
    expect(control.style.left).toBe(`${handle.x}px`);
    expect(control.style.top).toBe("34px");
    fireEvent.click(control);
    expect(onRemove).toHaveBeenCalledWith(dependency);
  });

  it("shows the control while the pointer is on an arrow and none is selected", () => {
    const { container } = render(
      <DependencyArrows dependencies={[dependency]} rows={rows} rowIndex={new Map([["P-1", 0], ["P-2", 1]])} scale={scale} height={68} selected={null} onSelect={() => {}} onRemove={() => {}} />,
    );
    expect(container.querySelector("[data-plan-unlink]")).toBeNull();
    fireEvent.pointerEnter(container.querySelector('[data-plan-arrow-hit="P-1->P-2"]')!);
    expect(container.querySelector('[data-plan-unlink="l1"]')).not.toBeNull();
  });

  it("skips a dependency whose ends are not both drawn", () => {
    const { container } = render(
      <DependencyArrows dependencies={[dependency]} rows={rows.slice(0, 1)} rowIndex={new Map([["P-1", 0]])} scale={scale} height={34} selected={null} onSelect={() => {}} onRemove={() => {}} />,
    );
    expect(container.querySelector("[data-plan-arrow]")).toBeNull();
  });
});
