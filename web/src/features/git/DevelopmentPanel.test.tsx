import { describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen, within } from "@testing-library/react";
import type { CIRun, CIStatus, Development, Repository } from "@/api/git";

const createBranch = vi.fn();
const state = {
  development: { branchName: "CP-4-fix-the-thing", branches: [], pullRequests: [], commits: [], runs: [] } as Development,
  repositories: [] as Repository[],
};

vi.mock("@/api/git", async () => {
  const actual = await vi.importActual<typeof import("@/api/git")>("@/api/git");
  return {
    ...actual,
    useDevelopment: () => ({ data: state.development, isLoading: false }),
    useRepositories: () => ({ data: { repositories: state.repositories } }),
    useCreateBranch: () => ({ mutate: createBranch, isPending: false, error: null }),
  };
});

const { DevelopmentPanel } = await import("./DevelopmentPanel");

function repository(over: Partial<Repository> = {}): Repository {
  return {
    id: "r1", projectId: "p", projectKey: "CP", host: "github", name: "acme/portal",
    url: "https://github.com/acme/portal", apiBaseUrl: "https://api.github.com", defaultBranch: "main",
    hasToken: false, commitCount: 0, openPullRequestCount: 0, createdAt: "", updatedAt: "", ...over,
  };
}

function run(name: string, status: CIStatus): CIRun {
  return {
    id: `run-${name}`, repository: "acme/portal", externalId: `check_run:${name}`, name, status,
    url: `https://github.com/acme/portal/runs/${name}`, sha: "abc1234567", startedAt: "",
  };
}

describe("DevelopmentPanel", () => {
  // A project with nothing connected has nothing to say, and says nothing
  // rather than showing an empty panel on every issue.
  it("is absent when the project has no repositories", () => {
    state.repositories = [];
    const { container } = render(<DevelopmentPanel issueKey="CP-4" projectKey="CP" />);
    expect(container).toBeEmptyDOMElement();
  });

  it("lists every repository, and says why a read only one cannot be chosen", () => {
    state.repositories = [repository(), repository({ id: "r2", name: "acme/api", hasToken: true })];
    render(<DevelopmentPanel issueKey="CP-4" projectKey="CP" />);
    fireEvent.click(screen.getByRole("button", { name: "Create branch" }));

    const select = screen.getByLabelText("Repository") as HTMLSelectElement;
    expect(select.value).toBe("r2");
    const readOnly = screen.getByRole("option", { name: /acme\/portal/ }) as HTMLOptionElement;
    expect(readOnly.disabled).toBe(true);
    expect(readOnly.textContent).toContain("read only");
  });

  // With nowhere to write, the form says what to do rather than failing.
  it("says how to make writing possible when no repository has a token", () => {
    state.repositories = [repository()];
    render(<DevelopmentPanel issueKey="CP-5" projectKey="CP" />);
    fireEvent.click(screen.getByRole("button", { name: "Create branch" }));
    expect(screen.getByTestId("no-token")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Create" })).toBeDisabled();
  });

  it("stays open after a branch is made and offers the next repository", () => {
    state.repositories = [
      repository({ id: "r1", name: "acme/portal", hasToken: true }),
      repository({ id: "r2", name: "acme/api", hasToken: true, defaultBranch: "develop" }),
    ];
    createBranch.mockImplementation((input: { repositoryId: string; name: string }, opts: { onSuccess: (r: unknown) => void }) => {
      const repo = state.repositories.find((r) => r.id === input.repositoryId)!;
      opts.onSuccess({ branch: { id: `b-${repo.id}`, repository: repo.name, name: input.name, commitCount: 0, createdAt: "" } });
    });
    render(<DevelopmentPanel issueKey="CP-4" projectKey="CP" />);
    fireEvent.click(screen.getByRole("button", { name: "Create branch" }));
    fireEvent.click(screen.getByRole("button", { name: "Create" }));

    expect(screen.getByText("Made on acme/portal.", { exact: false })).toBeInTheDocument();
    expect((screen.getByLabelText("Repository") as HTMLSelectElement).value).toBe("r2");
    expect((screen.getByLabelText("Start from") as HTMLInputElement).value).toBe("develop");
    expect(screen.getByRole("option", { name: /acme\/portal/ }).textContent).toContain("has CP-4-fix-the-thing");

    fireEvent.click(screen.getByRole("button", { name: "Create" }));
    expect(screen.getAllByTestId("branch-made")).toHaveLength(2);
    expect(screen.getByRole("button", { name: "Done" })).toBeInTheDocument();
    createBranch.mockReset();
  });

  it("shows a pull request with how its CI came out", () => {
    state.repositories = [repository()];
    state.development = {
      branchName: "CP-4-fix",
      branches: [],
      commits: [],
      runs: [],
      pullRequests: [{
        id: "q1", repository: "acme/portal", number: 7, title: "CP-4 fix", state: "open", openedAt: "",
        ci: { status: "failure", checks: [run("build", "failure")] },
      }],
    };
    render(<DevelopmentPanel issueKey="CP-4" projectKey="CP" />);
    expect(screen.getByText("CP-4 fix", { exact: false })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "CI failed" })).toHaveAttribute("data-ci", "failure");
    expect(screen.getByText("open")).toBeInTheDocument();
  });

  // The badge adds every check up; the list says which one to go and look at.
  it("lists the checks behind a pull request's CI", () => {
    state.repositories = [repository()];
    state.development = {
      branchName: "CP-4-fix",
      branches: [],
      commits: [],
      runs: [],
      pullRequests: [{
        id: "q1", repository: "acme/portal", number: 7, title: "CP-4 fix", state: "open", openedAt: "",
        ci: { status: "failure", checks: [run("lint", "success"), run("test", "failure")] },
      }],
    };
    render(<DevelopmentPanel issueKey="CP-4" projectKey="CP" />);
    const badge = screen.getByRole("button", { name: "CI failed" });
    expect(badge).toHaveAttribute("aria-expanded", "false");
    expect(screen.queryByText("lint passed")).not.toBeInTheDocument();

    fireEvent.click(badge);
    expect(badge).toHaveAttribute("aria-expanded", "true");
    const list = screen.getByTestId("ci-checks");
    expect(within(list).getByText("lint passed")).toHaveAttribute("data-ci", "success");
    expect(within(list).getByText("test failed")).toHaveAttribute("data-ci", "failure");
    expect(within(list).getByText("test failed")).toHaveAttribute("href", "https://github.com/acme/portal/runs/test");
  });

  it("gives a commit the same combined answer as a pull request", () => {
    state.repositories = [repository()];
    state.development = {
      branchName: "CP-4-fix",
      branches: [],
      pullRequests: [],
      runs: [run("lint", "success"), run("test", "pending")],
      commits: [{
        id: "c1", repository: "acme/portal", sha: "abc1234567", message: "CP-4 fix", committedAt: new Date().toISOString(),
        ci: { status: "pending", checks: [run("lint", "success"), run("test", "pending")] },
      }],
    };
    render(<DevelopmentPanel issueKey="CP-4" projectKey="CP" />);
    expect(screen.getByRole("button", { name: "CI running" })).toHaveAttribute("data-ci", "pending");
    expect(screen.queryByText("lint passed")).not.toBeInTheDocument();
  });

  it("names the branch before making it, starting from the repository's default branch", () => {
    state.repositories = [repository({ hasToken: true, defaultBranch: "develop" })];
    state.development = { branchName: "CP-4-fix-the-thing", branches: [], commits: [], runs: [], pullRequests: [] };
    render(<DevelopmentPanel issueKey="CP-4" projectKey="CP" />);
    fireEvent.click(screen.getByRole("button", { name: "Create branch" }));

    const name = screen.getByLabelText("Branch name") as HTMLInputElement;
    expect(name.value).toBe("CP-4-fix-the-thing");
    expect((screen.getByLabelText("Start from") as HTMLInputElement).value).toBe("develop");

    fireEvent.change(name, { target: { value: "feature/CP-4-thing" } });
    fireEvent.click(screen.getByRole("button", { name: "Create" }));
    expect(createBranch).toHaveBeenCalledWith(
      { repositoryId: "r1", name: "feature/CP-4-thing", from: "develop" },
      expect.anything(),
    );
  });

  it("shows how far a branch has come, and that it merged", () => {
    state.repositories = [repository()];
    state.development = {
      branchName: "CP-4-fix",
      commits: [],
      runs: [],
      pullRequests: [],
      branches: [
        { id: "b1", repository: "acme/portal", name: "CP-4-fix", headSha: "abcdef1234567", commitCount: 3, pushedAt: new Date().toISOString(), createdAt: "" },
        { id: "b2", repository: "acme/portal", name: "CP-4-old", commitCount: 0, mergedAt: "2026-09-01T00:00:00Z", createdAt: "" },
      ],
    };
    render(<DevelopmentPanel issueKey="CP-4" projectKey="CP" />);
    expect(screen.getByText("abcdef1")).toHaveAttribute("data-branch-head", "abcdef1234567");
    expect(screen.getByText("3 commits", { exact: false })).toBeInTheDocument();
    expect(screen.getByText("merged")).toBeInTheDocument();
    expect(document.querySelector('[data-branch="CP-4-old"]')).toHaveAttribute("data-branch-merged", "true");
  });
});
