import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { ReactNode } from "react";
import type { Issue, IssueType } from "@/api/issues";

vi.mock("@tanstack/react-router", () => ({
  Link: ({ children }: { children: ReactNode }) => <a href="#">{children}</a>,
}));

const { ParentPicker } = await import("./hierarchy");

function type(id: string, name: string, level: number): IssueType {
  return { id, name, icon: id, level, isSubtask: level < 0, position: level };
}

const types = [type("epic", "Epic", 1), type("story", "Story", 0), type("subtask", "Subtask", -1)];

function issue(key: string, summary: string, level: number): Issue {
  const t = types.find((each) => each.level === level)!;
  return {
    id: key,
    key,
    summary,
    type: { id: t.id, name: t.name, icon: t.icon, level, isSubtask: t.isSubtask },
    projectId: "p",
    projectKey: "LONG",
    status: { id: "todo", name: "To Do", category: "todo", position: 0 },
    priority: "medium",
    timeSpentMinutes: 0,
    labels: [],
    fixVersions: [],
    affectsVersions: [],
    components: [],
    createdAt: "2026-01-01T00:00:00Z",
    updatedAt: "2026-01-01T00:00:00Z",
  };
}

// An epic far older than the newest page of the project, so only a search finds it.
const oldEpic = issue("LONG-3", "Portal performance", 1);
const recentEpic = issue("LONG-290", "Password and recovery", 1);

const requested: URL[] = [];

function answer(url: URL): unknown {
  const path = url.pathname.replace("/api/v1", "");
  if (path === "/issue-types") return { issueTypes: types };
  if (path === "/projects/LONG/issues") {
    const text = url.searchParams.get("text") ?? "";
    const found = [oldEpic, recentEpic].filter((each) => each.summary.toLowerCase().includes(text.toLowerCase()));
    const narrowed = url.searchParams.getAll("type").includes("epic") ? found : [];
    return { issues: text ? narrowed : narrowed.slice(1), total: narrowed.length, limit: 20, offset: 0 };
  }
  throw new Error(`nothing answers ${url}`);
}

describe("the parent picker", () => {
  beforeEach(() => {
    requested.length = 0;
    vi.stubGlobal(
      "fetch",
      vi.fn(async (input: string) => {
        const url = new URL(input, "http://armature.test");
        requested.push(url);
        return new Response(JSON.stringify(answer(url)), { status: 200, headers: { "Content-Type": "application/json" } });
      }),
    );
  });
  afterEach(() => vi.unstubAllGlobals());

  it("searches the project for a parent one level up rather than offering a capped list", async () => {
    render(
      <QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>
        <ParentPicker issue={issue("LONG-300", "Add password reset", 0)} />
      </QueryClientProvider>,
    );

    await userEvent.type(await screen.findByRole("searchbox", { name: "Find a parent" }), "portal");
    const picker = screen.getByRole("combobox", { name: "Parent" });
    await waitFor(() => expect(within(picker).getByRole("option", { name: "LONG-3 · Portal performance" })).toBeInTheDocument());

    const search = requested.filter((url) => url.searchParams.get("text") === "portal");
    expect(search.length).toBeGreaterThan(0);
    expect(search[0]!.searchParams.getAll("type")).toEqual(["epic"]);
  });
});
