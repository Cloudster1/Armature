import { describe, expect, it, vi } from "vitest";
import { render, screen, within } from "@testing-library/react";
import type { ReactNode } from "react";

// The links are only addresses here; what they point at is the router's job.
vi.mock("@tanstack/react-router", () => ({
  Link: ({ children, params }: { children: ReactNode; params?: { issueKey?: string } }) => (
    <a href={`/issues/${params?.issueKey ?? ""}`}>{children}</a>
  ),
}));

const { Ancestry, ProgressBar } = await import("./hierarchy");
const { LEVEL_EPIC, LEVEL_STANDARD } = await import("@/api/issues");
import type { Issue, Progress } from "@/api/issues";

function progress(over: Partial<Progress> = {}): Progress {
  return { total: 0, done: 0, inProgress: 0, todo: 0, ...over };
}

function issue(key: string, summary: string, level: number): Issue {
  return {
    id: key,
    key,
    summary,
    type: { id: "t", name: level === LEVEL_EPIC ? "Epic" : "Story", icon: "epic", level, isSubtask: false },
    projectId: "p",
    projectKey: "P",
    status: { id: "s", name: "To Do", category: "todo", position: 0 },
    priority: "medium",
    createdAt: new Date().toISOString(),
    timeSpentMinutes: 0,
    labels: [],
    fixVersions: [],
    affectsVersions: [],
    components: [],
    updatedAt: new Date().toISOString(),
  };
}

describe("ProgressBar", () => {
  it("says nothing when there is nothing underneath", () => {
    const { container } = render(<ProgressBar progress={progress()} />);
    expect(container).toBeEmptyDOMElement();
  });

  it("reports the count as well as the bar, because a bar alone is not a number", () => {
    render(<ProgressBar progress={progress({ total: 4, done: 1, inProgress: 2, todo: 1 })} />);
    expect(screen.getByText("1/4 done")).toBeInTheDocument();
  });

  it("gives the bar an accessible label rather than leaving it to colour alone", () => {
    render(<ProgressBar progress={progress({ total: 4, done: 3, todo: 1 })} />);
    expect(screen.getByRole("progressbar", { name: "3 of 4 done" })).toBeInTheDocument();
  });

  it("sizes the done and in-progress segments by their share", () => {
    const { container } = render(
      <ProgressBar progress={progress({ total: 4, done: 1, inProgress: 1, todo: 2 })} />,
    );
    const segments = container.querySelectorAll("span[style]");
    expect(segments).toHaveLength(2);
    expect((segments[0] as HTMLElement).style.width).toBe("25%");
    expect((segments[1] as HTMLElement).style.width).toBe("25%");
  });
});

describe("Ancestry", () => {
  it("shows nothing for an issue with nothing above it", () => {
    const { container } = render(<Ancestry ancestors={[]} />);
    expect(container).toBeEmptyDOMElement();
  });

  it("reads from the top down and links each step", () => {
    render(
      <Ancestry
        ancestors={[
          issue("P-1", "Make signing in effortless", LEVEL_EPIC + 1),
          issue("P-2", "Password and recovery", LEVEL_EPIC),
        ]}
      />,
    );

    const nav = screen.getByRole("navigation", { name: "Parent issues" });
    const links = within(nav).getAllByRole("link");
    expect(links.map((l) => l.textContent)).toEqual([
      "Make signing in effortless",
      "Password and recovery",
    ]);
    expect(links[0]).toHaveAttribute("href", "/issues/P-1");
  });

  it("names the ancestor by its summary, not by a key nobody remembers", () => {
    render(<Ancestry ancestors={[issue("P-9", "Portal performance", LEVEL_STANDARD + 1)]} />);
    expect(screen.getByText("Portal performance")).toBeInTheDocument();
  });
});
