import { describe as group, expect, it } from "vitest";
import { render, screen, within } from "@testing-library/react";
import { overBy, type Sprint, type SprintPlan } from "@/api/sprints";
import { SprintBands, describe, number } from "./SprintBands";
import { SprintCapacity } from "./SprintCapacity";
import { makeScale, day } from "./scale";

function sprint(over: Partial<Sprint> = {}): Sprint {
  return {
    id: "s1",
    projectId: "p",
    projectKey: "PR",
    name: "Sprint 1",
    state: "active",
    startsOn: "2026-03-02",
    endsOn: "2026-03-13",
    capacity: 16,
    position: 0,
    createdAt: "2026-03-01T00:00:00Z",
    updatedAt: "2026-03-01T00:00:00Z",
    ...over,
  };
}

function plan(over: Partial<SprintPlan> = {}): SprintPlan {
  return { sprint: sprint(), committed: 8, completed: 3, issues: 4, unestimated: 0, ...over };
}

group("overBy", () => {
  it("is nothing while the work fits", () => {
    expect(overBy(plan({ committed: 16 }))).toBe(0);
  });

  it("is the difference once it does not", () => {
    expect(overBy(plan({ committed: 21 }))).toBe(5);
  });

  // A team that has not said what fits cannot be over it.
  it("is nothing when nobody set a capacity", () => {
    expect(overBy(plan({ committed: 100, sprint: sprint({ capacity: undefined }) }))).toBe(0);
  });
});

group("describe", () => {
  it("reads as committed against capacity", () => {
    expect(describe(plan({ committed: 8 }))).toBe("8/16 pts");
  });

  it("gives only the total when there is no capacity to measure against", () => {
    expect(describe(plan({ committed: 8, sprint: sprint({ capacity: undefined }) }))).toBe("8 pts");
  });

  it("writes a half as a half rather than as 0.50", () => {
    expect(number(2.5)).toBe("2.5");
    expect(number(5)).toBe("5");
  });
});

group("SprintBands", () => {
  const scale = makeScale(day("2026-03-01"), day("2026-04-01"), 10);

  it("draws a band for each sprint that has dates", () => {
    render(<SprintBands scale={scale} sprints={[plan()]} />);
    expect(screen.getByText("Sprint 1")).toBeInTheDocument();
    expect(screen.getByText("8/16 pts")).toBeInTheDocument();
  });

  // A sprint nobody has dated has nothing to draw against a calendar.
  it("has nothing to draw for a sprint with no dates", () => {
    const { container } = render(
      <SprintBands
        scale={scale}
        sprints={[plan({ sprint: sprint({ startsOn: undefined, endsOn: undefined }) })]}
      />,
    );
    expect(container).toBeEmptyDOMElement();
  });

  it("places the band across the days the sprint covers", () => {
    render(<SprintBands scale={scale} sprints={[plan()]} />);
    const band = screen.getByText("Sprint 1").parentElement!;
    // 1 March is day zero, so a sprint starting on the 2nd begins one day in
    // and covers both its ends.
    expect(band.style.left).toBe("10px");
    expect(band.style.width).toBe("120px");
  });
});

group("SprintCapacity", () => {
  it("says how much is committed against how much fits", () => {
    render(<SprintCapacity sprints={[plan({ committed: 8 })]} />);
    expect(screen.getByText(/8 of 16 pts/)).toBeInTheDocument();
  });

  // A sprint of eight points with five unsized issues in it is not a sprint of
  // eight points, and the number alone would say otherwise.
  it("counts the unestimated work beside the total", () => {
    render(<SprintCapacity sprints={[plan({ unestimated: 5 })]} />);
    expect(screen.getByText(/5 unestimated/)).toBeInTheDocument();
  });

  it("marks the sprint the team is working in", () => {
    render(<SprintCapacity sprints={[plan()]} />);
    const row = screen.getByText("Sprint 1").closest("li")!;
    expect(within(row).getByText("Running")).toBeInTheDocument();
  });

  it("says nothing at all when there are no sprints", () => {
    const { container } = render(<SprintCapacity sprints={[]} />);
    expect(container).toBeEmptyDOMElement();
  });
});
