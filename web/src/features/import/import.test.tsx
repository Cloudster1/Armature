import { describe, expect, it, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";

vi.mock("@/api/issues", () => ({
  useIssueTypes: () => ({ data: { issueTypes: [{ id: "1", name: "Story" }] } }),
  useStatuses: () => ({ data: { statuses: [{ id: "2", name: "To Do" }] } }),
  useMembers: () => ({ data: { members: [] } }),
}));

const { MappingCard } = await import("./MappingCard");
const { WordsCard } = await import("./WordsCard");

/** The nth of several elements, said out loud so a miss is not a type error. */
function nth(elements: HTMLElement[], n: number): HTMLElement {
  const found = elements[n];
  if (!found) throw new Error(`there is no element ${n} of ${elements.length}`);
  return found;
}

describe("mapping a file", () => {
  const columns = [
    { at: 0, name: "Summary", samples: ["Stand up the cluster"] },
    { at: 1, name: "Labels", samples: ["platform"] },
    { at: 2, name: "Labels", samples: ["cluster"] },
  ];

  // A Jira export names five columns Labels; a mapping by name could only ever
  // reach one of them.
  it("fills one target from two columns of the same name", async () => {
    const onChange = vi.fn();
    render(<MappingCard columns={columns} targets={["summary", "labels"]} mapping={{ summary: [0], labels: [1] }} onChange={onChange} />);

    const selects = screen.getAllByLabelText("What Labels becomes");
    await userEvent.selectOptions(nth(selects, 1), "labels");
    expect(onChange).toHaveBeenCalledWith({ summary: [0], labels: [1, 2] });
  });

  it("takes a column away from the target that held it", async () => {
    const onChange = vi.fn();
    render(<MappingCard columns={columns} targets={["summary", "labels"]} mapping={{ summary: [0], labels: [1, 2] }} onChange={onChange} />);

    const selects = screen.getAllByLabelText("What Labels becomes");
    await userEvent.selectOptions(nth(selects, 0), "");
    expect(onChange).toHaveBeenCalledWith({ summary: [0], labels: [2] });
  });
});

describe("the words in a file", () => {
  it("asks what a word this tracker does not know means here", async () => {
    const onChange = vi.fn();
    render(<WordsCard words={[{ target: "status", value: "Requested", count: 5, means: "" }]} values={{}} onChange={onChange} />);

    expect(screen.getByText("Requested")).toBeTruthy();
    await userEvent.selectOptions(screen.getByLabelText("What Requested means here"), "To Do");
    expect(onChange).toHaveBeenCalledWith({ status: { Requested: "To Do" } });
  });
});
