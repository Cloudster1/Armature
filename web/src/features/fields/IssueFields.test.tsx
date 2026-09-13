import { describe, expect, it, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import type { FieldValue } from "@/api/fields";

const setValue = vi.fn();
const state = { values: [] as FieldValue[] };

vi.mock("@/api/fields", () => ({
  useIssueFields: () => ({ data: { values: state.values } }),
  useSetFieldValue: () => ({ mutate: setValue, error: null }),
}));

const { IssueFields } = await import("./IssueFields");

function value(over: Partial<FieldValue["field"]>, answer?: FieldValue["value"], display?: string): FieldValue {
  return {
    field: {
      id: "f1",
      projectId: "p",
      projectKey: "CP",
      org: false,
      name: "Platform",
      kind: "select",
      options: ["Web", "Mobile"],
      position: 0,
      createdAt: "",
      updatedAt: "",
      ...over,
    },
    value: answer,
    display,
  };
}

describe("IssueFields", () => {
  it("draws nothing for a project without fields", () => {
    state.values = [];
    const { container } = render(<dl><IssueFields issueKey="CP-1" editable /></dl>);
    expect(container.querySelector("dt")).toBeNull();
  });

  it("offers the field's options with none chosen", () => {
    state.values = [value({})];
    render(<dl><IssueFields issueKey="CP-1" editable /></dl>);
    const select = screen.getByLabelText("Platform") as HTMLSelectElement;
    expect(select.value).toBe("");
    expect([...select.options].map((o) => o.textContent)).toEqual(["None", "Web", "Mobile"]);
  });

  // The option was removed after it was chosen; the answer must not vanish.
  it("keeps showing a choice the project no longer offers", () => {
    state.values = [value({}, "Desk", "Desk")];
    render(<dl><IssueFields issueKey="CP-1" editable /></dl>);
    const select = screen.getByLabelText("Platform") as HTMLSelectElement;
    expect(select.value).toBe("Desk");
    const stale = [...select.options].find((o) => o.value === "Desk");
    expect(stale?.disabled).toBe(true);
    expect(stale?.textContent).toContain("no longer offered");
  });

  it("shows a reader the answer as text", () => {
    state.values = [value({}, "Web", "Web")];
    render(<dl><IssueFields issueKey="CP-1" editable={false} /></dl>);
    expect(screen.queryByRole("combobox")).toBeNull();
    expect(screen.getByText("Web")).toBeInTheDocument();
  });
});
