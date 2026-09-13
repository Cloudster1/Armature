import { describe, expect, it, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";

const confirmProposal = vi.fn();
vi.mock("@/api/assistant", () => ({ useConfirmProposal: () => ({ mutate: confirmProposal, isPending: false }) }));

const { AskAnswer } = await import("./AskAnswer");

describe("AskAnswer", () => {
  // The model may ask for a change; making it is the reader's own act.
  it("offers what the model proposed and does nothing until it is confirmed", async () => {
    const proposal = { tool: "create_issue", says: "File an issue in a project.", arguments: { projectKey: "WEB" } };
    render(<AskAnswer pending={false} error={null} answer={{ text: "I propose filing it." }} proposals={[proposal]} onGo={() => {}} />);

    expect(screen.getByText("I propose filing it.")).toBeTruthy();
    expect(screen.getByText("File an issue in a project.")).toBeTruthy();
    expect(confirmProposal).not.toHaveBeenCalled();

    await userEvent.click(screen.getByRole("button", { name: "Confirm" }));
    expect(confirmProposal).toHaveBeenCalledWith(proposal, expect.anything());
  });
});
