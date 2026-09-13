import { describe, expect, it, vi, afterEach } from "vitest";
import { render, screen } from "@testing-library/react";

import { Avatar, PriorityBadge, StatusBadge, TypeBadge, relativeTime } from "./badges";
import { docToText, type Doc } from "@/api/issues";

describe("StatusBadge", () => {
  // Colour must follow the category, not the name: a workflow can call its
  // states anything, and the colour still has to mean the same thing.
  it("colours by category rather than by name", () => {
    const { rerender, container } = render(<StatusBadge name="Awaiting triage" category="todo" />);
    const todo = container.firstElementChild?.className ?? "";

    rerender(<StatusBadge name="Awaiting triage" category="done" />);
    const done = container.firstElementChild?.className ?? "";

    expect(todo).not.toBe(done);
    expect(screen.getByText("Awaiting triage")).toBeInTheDocument();
  });
});

describe("PriorityBadge", () => {
  it("names the priority for assistive technology", () => {
    render(<PriorityBadge priority="highest" />);
    expect(screen.getByText("Highest priority")).toBeInTheDocument();
  });
});

describe("TypeBadge", () => {
  it("exposes the type name rather than only a glyph", () => {
    render(<TypeBadge icon="bug" name="Bug" />);
    expect(screen.getByText("Bug")).toBeInTheDocument();
  });

  it("falls back to the task styling for an unknown icon", () => {
    render(<TypeBadge icon="something-new" name="Spike" />);
    expect(screen.getByText("Spike")).toBeInTheDocument();
  });
});

describe("Avatar", () => {
  it("shows initials and the full name", () => {
    render(<Avatar name="Ada Lovelace" />);
    expect(screen.getByText("AL")).toBeInTheDocument();
    expect(screen.getByText("Ada Lovelace")).toBeInTheDocument();
  });

  it("takes at most two initials", () => {
    render(<Avatar name="Ada Byron King Lovelace" />);
    expect(screen.getByText("AB")).toBeInTheDocument();
  });

  it("says unassigned when there is nobody", () => {
    render(<Avatar />);
    expect(screen.getByText("Unassigned")).toBeInTheDocument();
  });
});

describe("relativeTime", () => {
  afterEach(() => vi.useRealTimers());

  it("describes recent times in relative terms", () => {
    vi.useFakeTimers();
    vi.setSystemTime(new Date("2026-09-02T12:00:00Z"));

    expect(relativeTime("2026-09-02T11:59:30Z")).toBe("just now");
    expect(relativeTime("2026-09-02T11:45:00Z")).toBe("15m ago");
    expect(relativeTime("2026-09-02T09:00:00Z")).toBe("3h ago");
    expect(relativeTime("2026-08-30T12:00:00Z")).toBe("3d ago");
  });

  it("falls back to a date once it is far enough in the past", () => {
    vi.useFakeTimers();
    vi.setSystemTime(new Date("2026-09-02T12:00:00Z"));
    expect(relativeTime("2026-01-01T12:00:00Z")).toContain("2026");
  });
});

describe("docToText", () => {
  it("reads the text out of a document", () => {
    const doc: Doc = {
      type: "doc",
      content: [
        { type: "paragraph", content: [{ type: "text", text: "First line." }] },
        { type: "paragraph", content: [{ type: "text", text: "Second line." }] },
      ],
    };
    expect(docToText(doc)).toBe("First line.\nSecond line.");
  });

  it("handles nested marks and inline nodes", () => {
    const doc: Doc = {
      type: "doc",
      content: [
        {
          type: "paragraph",
          content: [
            { type: "text", text: "Bold " },
            { type: "text", text: "and plain" },
          ],
        },
      ],
    };
    expect(docToText(doc)).toBe("Bold and plain");
  });

  it("returns nothing for an absent or empty document", () => {
    expect(docToText(undefined)).toBe("");
    expect(docToText(null)).toBe("");
    expect(docToText({ type: "doc", content: [] })).toBe("");
  });
});
