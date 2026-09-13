import { describe, expect, it, vi } from "vitest";
import { act, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";

vi.mock("@tanstack/react-router", () => ({
  useLocation: () => ({ pathname: "/projects/ALP" }),
}));

const { IssueDrawerProvider, useIssueDrawer, useIssueList, indexOf } = await import("./IssueDrawer");

/** A list of three that registers itself, and a readout of the panel's state. */
function List({ keys }: { keys: string[] }) {
  useIssueList(keys);
  const panel = useIssueDrawer();
  return (
    <div data-issue-list="">
      {keys.map((key) => (
        <button key={key} onClick={() => panel.open(key)}>
          {key}
        </button>
      ))}
      <output>{panel.current ?? "closed"}</output>
      <button onClick={() => panel.step(1)}>next</button>
      <button onClick={() => panel.step(-1)}>previous</button>
      <textarea aria-label="Comment" />
    </div>
  );
}

describe("IssueDrawerProvider", () => {
  it("steps through what the list registered, and stops at the ends", async () => {
    render(
      <IssueDrawerProvider>
        <List keys={["ALP-1", "ALP-2", "ALP-3"]} />
      </IssueDrawerProvider>,
    );
    await userEvent.click(screen.getByRole("button", { name: "ALP-2" }));
    expect(screen.getByRole("status")).toHaveTextContent("ALP-2");
    await userEvent.click(screen.getByRole("button", { name: "next" }));
    expect(screen.getByRole("status")).toHaveTextContent("ALP-3");
    await userEvent.click(screen.getByRole("button", { name: "next" }));
    expect(screen.getByRole("status")).toHaveTextContent("ALP-3");
    await userEvent.click(screen.getByRole("button", { name: "previous" }));
    expect(screen.getByRole("status")).toHaveTextContent("ALP-2");
  });

  it("walks with the arrow keys from the page, but not from a field", async () => {
    render(
      <IssueDrawerProvider>
        <List keys={["ALP-1", "ALP-2", "ALP-3"]} />
      </IssueDrawerProvider>,
    );
    await userEvent.click(screen.getByRole("button", { name: "ALP-1" }));
    await userEvent.keyboard("{ArrowDown}");
    expect(screen.getByRole("status")).toHaveTextContent("ALP-2");
    await userEvent.keyboard("{ArrowUp}");
    expect(screen.getByRole("status")).toHaveTextContent("ALP-1");

    await userEvent.click(screen.getByRole("textbox", { name: "Comment" }));
    await userEvent.keyboard("{ArrowDown}");
    expect(screen.getByRole("status")).toHaveTextContent("ALP-1");
  });

  it("forgets the list when it goes, and steps to the top when the issue left the list", async () => {
    const { rerender } = render(
      <IssueDrawerProvider>
        <List keys={["ALP-1", "ALP-2"]} />
      </IssueDrawerProvider>,
    );
    await userEvent.click(screen.getByRole("button", { name: "ALP-2" }));
    // The list was filtered and ALP-2 is no longer in it.
    await act(async () => rerender(
      <IssueDrawerProvider>
        <List keys={["ALP-1", "ALP-3"]} />
      </IssueDrawerProvider>,
    ));
    expect(indexOf(["ALP-1", "ALP-3"], "ALP-2")).toBe(-1);
    await userEvent.click(screen.getByRole("button", { name: "next" }));
    expect(screen.getByRole("status")).toHaveTextContent("ALP-1");
  });
});
