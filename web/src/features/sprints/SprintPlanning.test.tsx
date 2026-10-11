import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { render, screen, waitFor, within } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { ReactNode } from "react";
import type { Issue } from "@/api/issues";
import type { Sprint } from "@/api/sprints";

// The links are only addresses here; what they point at is the router's job.
vi.mock("@tanstack/react-router", () => ({
  Link: ({ children }: { children: ReactNode }) => <a href="#">{children}</a>,
}));
vi.mock("@/features/shell/ConfirmProvider", () => ({ useConfirm: () => vi.fn() }));

const { SprintPlanning } = await import("./SprintPlanning");

const PAGE = 200;
// Drawing a page and a bit of backlog rows is slow in jsdom, more so beside the
// rest of the suite, so the page is given longer than the default to settle.
const SETTLED = { timeout: 5_000 };

function issue(n: number, over: Partial<Issue> = {}): Issue {
  return {
    id: `id-${n}`,
    key: `LONG-${n}`,
    summary: `work ${n}`,
    type: { id: "task", name: "Task", icon: "task", level: 0, isSubtask: false },
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
    ...over,
  };
}

function sprint(id: string, name: string): Sprint {
  return { id, name, projectId: "p", projectKey: "LONG", state: "future", position: 0, createdAt: "", updatedAt: "" };
}

const committed = issue(10, { summary: "old committed work", sprintId: "s3" });
// The newest page of the whole project: none of it is in a sprint, and the
// committed work is too old to be on it.
const newest = Array.from({ length: PAGE }, (_, i) => issue(300 - i));
// A long backlog is one page and a bit, so reading only the first loses work.
const longBacklog = Array.from({ length: PAGE + 50 }, (_, i) => issue(1000 + i));
let backlog: Issue[] = [];

const requested: URL[] = [];

function page(issues: Issue[], total: number, offset = 0) {
  return { issues, total, limit: PAGE, offset };
}

function answer(url: URL): unknown {
  const path = url.pathname.replace("/api/v1", "");
  if (path === "/projects/LONG/sprints") return { sprints: [sprint("s3", "Sprint 3"), sprint("s4", "Sprint 4")] };
  if (path === "/projects/LONG/plan") return { items: [], sprints: [], warnings: [] };
  if (path === "/projects/LONG/teams") return { teams: [] };
  if (path === "/projects/LONG/issues") {
    const sprintId = url.searchParams.get("sprint");
    const offset = Number(url.searchParams.get("offset") ?? 0);
    if (sprintId === "s3") return page([committed], 1);
    if (sprintId === "s4") return page([], 0);
    if (sprintId === "none") return page(backlog.slice(offset, offset + PAGE), backlog.length, offset);
    return page(newest, 400);
  }
  throw new Error(`nothing answers ${url}`);
}

function mount() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={client}>
      <SprintPlanning projectKey="LONG" />
    </QueryClientProvider>,
  );
}

function sprintCard(name: string): HTMLElement {
  const card = document.querySelector<HTMLElement>(`[data-sprint="${name}"]`);
  if (!card) throw new Error(`no card for ${name}`);
  return card;
}

describe("the sprint planning page", () => {
  beforeEach(() => {
    requested.length = 0;
    backlog = longBacklog.slice(0, 1);
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

  it("draws a sprint's issues from the sprint's own read, however old they are", async () => {
    mount();
    await waitFor(() => expect(within(sprintCard("Sprint 3")).getByText("old committed work")).toBeInTheDocument(), SETTLED);
    expect(requested.some((url) => url.searchParams.get("sprint") === "s3")).toBe(true);
  });

  it("offers to delete only a sprint the server says is empty", async () => {
    mount();
    await waitFor(() => expect(within(sprintCard("Sprint 4")).getByRole("button", { name: "Delete" })).toBeInTheDocument(), SETTLED);
    await waitFor(() => expect(within(sprintCard("Sprint 3")).getByText("old committed work")).toBeInTheDocument(), SETTLED);
    expect(within(sprintCard("Sprint 3")).queryByRole("button", { name: "Delete" })).toBeNull();
  });

  it("reads the whole backlog, ranked and without the done work", async () => {
    backlog = longBacklog;
    mount();
    const section = await screen.findByTestId("backlog");
    await waitFor(() => expect(section).toHaveTextContent(`Backlog (${backlog.length})`), SETTLED);

    const reads = requested.filter((url) => url.searchParams.get("sprint") === "none");
    expect(reads.map((url) => url.searchParams.get("offset") ?? "0")).toEqual(["0", String(PAGE)]);
    const first = reads[0]!.searchParams;
    expect(first.getAll("category")).toEqual(["todo", "in_progress"]);
    expect(first.get("orderBy")).toBe("rank");
    expect(first.get("order")).toBe("asc");
    expect(first.get("team")).toBe("none");

    const rows = [...section.querySelectorAll("[data-issue]")].map((row) => row.getAttribute("data-issue"));
    expect(rows.slice(0, 2)).toEqual(["LONG-1000", "LONG-1001"]);
  });
});
