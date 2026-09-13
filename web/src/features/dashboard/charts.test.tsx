import { describe, expect, it } from "vitest";
import { render, screen } from "@testing-library/react";
import { hours, share } from "@/api/reports";
import { BarList, LineChart, Progress, pathOf, stackedPaths } from "./charts";

describe("report words", () => {
  it("says hours the way a person does", () => {
    expect(hours(0.5)).toBe("30m");
    expect(hours(3.25)).toBe("3.3h");
    expect(hours(72)).toBe("3.0d");
  });

  it("shares are whole percentages and never divide by nothing", () => {
    expect(share(1, 4)).toBe(25);
    expect(share(2, 3)).toBe(67);
    expect(share(3, 0)).toBe(0);
  });
});

describe("charts", () => {
  it("sizes bars against the largest and names each row", () => {
    render(<BarList rows={[{ label: "To Do", count: 2 }, { label: "Done", count: 4 }]} />);
    const todo = screen.getByText("To Do").closest("[data-bar]");
    expect(todo).toHaveAttribute("data-bar", "To Do");
    const fill = todo?.querySelector("span span") as HTMLElement;
    expect(fill.style.width).toBe("50%");
  });

  it("says what is empty rather than drawing nothing", () => {
    render(<BarList rows={[]} empty="No issues yet" />);
    expect(screen.getByText("No issues yet")).toBeInTheDocument();
  });

  it("shows progress as a fraction of the whole", () => {
    render(<Progress done={1} total={4} />);
    expect(screen.getByText("1/4")).toBeInTheDocument();
  });
});

describe("LineChart", () => {
  const remaining = { name: "remaining", tone: "text-chart-1", points: [{ x: 0, y: 8 }, { x: 1, y: 5 }, { x: 2, y: 3 }] };
  const ideal = { name: "ideal", tone: "text-ink-muted", points: [{ x: 0, y: 8 }, { x: 4, y: 0 }], dashed: true };
  const scope = { name: "scope", tone: "text-ink-subtle", points: [{ x: 0, y: 8 }, { x: 2, y: 10 }], step: true };

  it("draws one path per series with a command per sample", () => {
    render(<LineChart series={[remaining, ideal]} />);
    const path = document.querySelector('[data-series="remaining"] path')!;
    expect(path.getAttribute("d")!.split(" ").filter((c) => /^[MLHV]/.test(c))).toHaveLength(3);
    expect(document.querySelector('[data-series="ideal"] path')).toHaveAttribute("stroke-dasharray");
    expect(path).not.toHaveAttribute("stroke-dasharray");
  });

  it("scales every series against the tallest point of any of them", () => {
    render(<LineChart series={[remaining, scope]} />);
    // The scope reaches 10, so the remaining's 8 sits at 20% from the top, not at the top.
    const d = document.querySelector('[data-series="remaining"] path')!.getAttribute("d")!;
    expect(d.startsWith("M0 20")).toBe(true);
  });

  it("holds a stepped series until the next sample", () => {
    expect(pathOf([{ x: 0, y: 10 }, { x: 50, y: 40 }], true)).toBe("M0 10 H50 V40");
    expect(pathOf([{ x: 0, y: 10 }, { x: 50, y: 40 }], false)).toBe("M0 10 L50 40");
  });

  it("says what is empty", () => {
    render(<LineChart series={[{ name: "remaining", tone: "", points: [] }]} empty="No day yet" />);
    expect(screen.getByText("No day yet")).toBeInTheDocument();
  });
});

describe("stackedPaths", () => {
  it("stacks each band on the ones below and closes the shape", () => {
    const paths = stackedPaths([
      { name: "To Do", tone: "a", values: [2, 2, 0] },
      { name: "Done", tone: "b", values: [0, 1, 3] },
    ]);
    expect(paths.map((p) => p.name)).toEqual(["To Do", "Done"]);
    // The top band starts at the first band's height (2 of a max total 3) and ends at 3 of 3.
    expect(paths[1]!.d.startsWith("M0 33.33")).toBe(true);
    expect(paths[1]!.d).toContain("L100 0");
    expect(paths[1]!.d.endsWith("Z")).toBe(true);
  });
  it("draws nothing for a single step", () => {
    expect(stackedPaths([{ name: "x", tone: "a", values: [1] }])).toEqual([]);
  });
});
