import { afterEach, describe, expect, it, vi } from "vitest";
import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import type { Issue } from "@/api/issues";
import { ConfirmProvider } from "@/features/shell/ConfirmProvider";
import { renderInShell, serve } from "@/test/shell";

const navigate = vi.fn(async () => {});
vi.mock("@tanstack/react-router", () => ({ useNavigate: () => navigate }));

const { DeleteIssueButton } = await import("./DeleteIssue");

const issue = { id: "i1", key: "CP-4", projectKey: "CP", summary: "Made by mistake" } as Issue;

afterEach(() => {
  vi.unstubAllGlobals();
  navigate.mockClear();
});

function deletes(fetchMock: ReturnType<typeof serve>) {
  return fetchMock.mock.calls.filter(([, init]) => init?.method === "DELETE").map(([url]) => url);
}

function draw() {
  const fetchMock = serve({ "DELETE /issues/CP-4": [204] });
  renderInShell(
    <ConfirmProvider>
      <DeleteIssueButton issue={issue} />
    </ConfirmProvider>,
  );
  fireEvent.click(screen.getByRole("button", { name: "Delete this issue" }));
  return fetchMock;
}

describe("deleting an issue from its page", () => {
  it("asks with the issue's key and sends nothing on No", async () => {
    const fetchMock = draw();

    const dialog = await screen.findByRole("dialog", { name: "Delete issue CP-4?" });
    expect(dialog).toHaveTextContent("CP-4 goes for good");
    fireEvent.click(within(dialog).getByRole("button", { name: "Cancel" }));

    await waitFor(() => expect(screen.queryByRole("dialog")).not.toBeInTheDocument());
    expect(deletes(fetchMock)).toEqual([]);
    expect(navigate).not.toHaveBeenCalled();
  });

  it("deletes it on Yes and leaves for the project", async () => {
    const fetchMock = draw();

    const dialog = await screen.findByRole("dialog", { name: "Delete issue CP-4?" });
    fireEvent.click(within(dialog).getByRole("button", { name: "Delete issue CP-4" }));

    await waitFor(() => expect(navigate).toHaveBeenCalledWith({ to: "/projects/$projectKey", params: { projectKey: "CP" } }));
    expect(deletes(fetchMock)).toHaveLength(1);
    expect(await screen.findByText("CP-4 was deleted.")).toBeInTheDocument();
  });
});
