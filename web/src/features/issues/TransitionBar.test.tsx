import { afterEach, describe, expect, it, vi } from "vitest";
import { fireEvent, screen, waitFor } from "@testing-library/react";
import { renderInShell, serve } from "@/test/shell";
import { TransitionBar } from "./TransitionBar";

const close = { id: "tr-close", name: "Close", toStepId: "s-done", position: 1, needs: ["comment", "resolution"] };
const start = { id: "tr-start", name: "Start", toStepId: "s-doing", position: 0 };
const moved = { issue: { id: "i1", key: "CP-4" } };

afterEach(() => {
  vi.unstubAllGlobals();
});

function posted(fetchMock: ReturnType<typeof serve>) {
  return fetchMock.mock.calls.filter(([, init]) => init?.method === "POST").map(([, init]) => JSON.parse(String(init?.body)));
}

describe("the transition bar", () => {
  it("asks for what a transition needs and sends it with the move", async () => {
    const fetchMock = serve({
      "GET /issues/CP-4/transitions": [200, { transitions: [start, close] }],
      "POST /issues/CP-4/transitions": [200, moved],
    });
    renderInShell(<TransitionBar issueKey="CP-4" />);

    fireEvent.click(await screen.findByRole("button", { name: "Close" }));
    const dialog = await screen.findByRole("dialog", { name: "Close" });
    expect(posted(fetchMock)).toEqual([]);

    const go = dialog.querySelector<HTMLButtonElement>("[data-action=transition-go]");
    expect(go).toBeDisabled();
    fireEvent.change(screen.getByLabelText("Resolution"), { target: { value: "Won't do" } });
    expect(go).toBeDisabled();
    fireEvent.change(screen.getByLabelText("Comment"), { target: { value: "  Nobody asked for it.  " } });
    expect(go).toBeEnabled();
    fireEvent.click(go!);

    await waitFor(() => expect(posted(fetchMock)).toEqual([{ transitionId: "tr-close", comment: "Nobody asked for it.", resolution: "Won't do" }]));
    await waitFor(() => expect(screen.queryByRole("dialog")).not.toBeInTheDocument());
  });

  it("sends a transition that needs nothing straight away", async () => {
    const fetchMock = serve({
      "GET /issues/CP-4/transitions": [200, { transitions: [start, close] }],
      "POST /issues/CP-4/transitions": [200, moved],
    });
    renderInShell(<TransitionBar issueKey="CP-4" />);

    fireEvent.click(await screen.findByRole("button", { name: "Start" }));

    await waitFor(() => expect(posted(fetchMock)).toEqual([{ transitionId: "tr-start" }]));
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
  });

  it("asks once the server says what a move it offered without needs is missing", async () => {
    const message = "This transition needs a comment.";
    serve({
      "GET /issues/CP-4/transitions": [200, { transitions: [start] }],
      "POST /issues/CP-4/transitions": [422, { error: { code: "validation_failed", message, fields: { comment: message } } }],
    });
    renderInShell(<TransitionBar issueKey="CP-4" />);

    fireEvent.click(await screen.findByRole("button", { name: "Start" }));

    await screen.findByRole("dialog", { name: "Start" });
    expect(screen.getByLabelText("Comment")).toBeInTheDocument();
    expect(screen.queryByLabelText("Resolution")).not.toBeInTheDocument();
  });
});
