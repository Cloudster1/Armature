import { afterEach, describe, expect, it, vi } from "vitest";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { Repository } from "@/api/git";
import { ConfirmProvider } from "@/features/shell/ConfirmProvider";
import { renderInShell, serve } from "@/test/shell";
import { RepositoryList } from "./RepositoryList";

const portal: Repository = {
  id: "r1",
  projectId: "p1",
  projectKey: "CP",
  host: "github",
  name: "acme/portal",
  url: "https://github.com/acme/portal",
  apiBaseUrl: "https://api.github.com",
  defaultBranch: "main",
  hasToken: false,
  commitCount: 12,
  openPullRequestCount: 2,
  createdAt: "2026-10-01T08:00:00Z",
  updatedAt: "2026-10-01T08:00:00Z",
};

function show(canManage: boolean) {
  const fetchMock = serve({
    "GET /projects/CP/repositories": [200, { repositories: [portal] }],
    "DELETE /repositories/r1": [204],
  });
  renderInShell(
    <ConfirmProvider>
      <RepositoryList projectKey="CP" canManage={canManage} />
    </ConfirmProvider>,
  );
  return fetchMock;
}

const deletes = (fetchMock: ReturnType<typeof serve>) => fetchMock.mock.calls.filter(([, init]) => init?.method === "DELETE");

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("RepositoryList", () => {
  it("shows a member without project.administer the repositories and none of the controls", async () => {
    show(false);
    const name = await screen.findByText("acme/portal");
    expect(name.closest("[data-repository]")?.textContent).toContain("12 commits");
    expect(screen.queryByRole("button", { name: "Connect repository" })).toBeNull();
    expect(screen.queryByRole("button", { name: "Rotate secret" })).toBeNull();
    expect(screen.queryByRole("button", { name: "Disconnect" })).toBeNull();
    expect(screen.queryByLabelText("Add an access token")).toBeNull();
  });

  it("shows a project administrator every control", async () => {
    show(true);
    await screen.findByText("acme/portal");
    expect(screen.getByRole("button", { name: "Connect repository" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Rotate secret" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Disconnect" })).toBeInTheDocument();
    expect(screen.getByLabelText("Add an access token")).toBeInTheDocument();
  });

  it("asks before disconnecting, naming the repository and what goes, and sends nothing on No", async () => {
    const fetchMock = show(true);
    await userEvent.click(await screen.findByRole("button", { name: "Disconnect" }));
    const dialog = screen.getByRole("dialog", { name: "Disconnect repository acme/portal?" });
    expect(dialog.textContent).toContain("12 commits, its branches and its pull requests leave the Development section of every issue");
    await userEvent.click(within(dialog).getByRole("button", { name: "Cancel" }));
    expect(screen.queryByRole("dialog")).toBeNull();
    expect(deletes(fetchMock)).toHaveLength(0);

    await userEvent.click(screen.getByRole("button", { name: "Disconnect" }));
    await userEvent.click(within(screen.getByRole("dialog")).getByRole("button", { name: "Disconnect repository acme/portal" }));
    await waitFor(() => expect(deletes(fetchMock)).toHaveLength(1));
    expect(String(deletes(fetchMock)[0]?.[0])).toContain("/repositories/r1");
  });
});
