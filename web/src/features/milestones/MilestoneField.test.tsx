import { afterEach, describe, expect, it, vi } from "vitest";
import { act, fireEvent, screen } from "@testing-library/react";
import type { Issue } from "@/api/issues";
import { refusal, renderInShell, serve } from "@/test/shell";
import { MilestoneField } from "./MilestoneField";

const issue = { id: "i1", key: "CP-4", summary: "Fix the login loop", projectKey: "CP", milestoneId: null } as unknown as Issue;
const closed = "That milestone is closed; reopen it before counting issues towards it.";

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("MilestoneField", () => {
  // The field shows its own refusal, so the shell's toast must not say it again.
  it("says a refusal once, beside itself", async () => {
    serve({
      "GET /projects/CP/milestones": [200, { milestones: [{ id: "m1", name: "Beta" }] }],
      "PUT /issues/CP-4/milestone": [409, refusal("conflict", closed)],
    });
    const { container } = renderInShell(<MilestoneField issue={issue} />);

    await screen.findByRole("option", { name: "Beta" });
    fireEvent.change(screen.getByLabelText("Milestone"), { target: { value: "m1" } });

    expect(await screen.findByText(closed)).toBeInTheDocument();
    await act(() => new Promise((resolve) => setTimeout(resolve)));
    expect(screen.getAllByText(closed)).toHaveLength(1);
    expect(container.ownerDocument.querySelector("[data-toast]")).toBeNull();
  });
});
