import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { renderHook, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider, type QueryKey } from "@tanstack/react-query";

import { useDeleteWorklog, useLogWork, useUpdateIssue, useUpdateWorklog } from "./issues";
import { useSetIssueLabels } from "./labels";
import { boardQueryKey } from "./boards";
import { planQueryKey } from "./plan";
import { calendarQueryKey } from "./calendar";
import { resourcesQueryKey } from "./resources";
import { sprintsQueryKey } from "./sprints";
import { filtersQueryKey } from "./filters";
import { deskQueryKey } from "./desk";

// Every view beside which the issue panel docks, as each one keys its query.
// An edit made in the panel must leave none of them showing the old values.
const VIEWS: Record<string, QueryKey> = {
  board: [...boardQueryKey, "PR"],
  "sprint board": [...boardQueryKey, "sprint", "s1"],
  backlog: [...boardQueryKey, "backlog", "b1"],
  plan: [...planQueryKey, "PR", ""],
  calendar: [...calendarQueryKey, "PR", "2026-10"],
  resources: [...resourcesQueryKey, "PR", "2026-10-05", "2026-11-01"],
  "sprint planning": [...sprintsQueryKey, "PR", { includeClosed: false }],
  "saved filter": [...filtersQueryKey, "issues", "f1"],
  "desk queue": [...deskQueryKey, "queue", "PR", "open"],
};

const fetchMock = vi.fn();

beforeEach(() => {
  vi.stubGlobal("fetch", fetchMock);
  fetchMock.mockReset();
  fetchMock.mockImplementation((_url: string, init: RequestInit) =>
    Promise.resolve(
      init.method === "DELETE"
        ? { status: 204, ok: true, text: () => Promise.resolve("") }
        : { status: 200, ok: true, text: () => Promise.resolve("{}") },
    ),
  );
});

afterEach(() => {
  vi.unstubAllGlobals();
});

/** A cache holding a fresh answer for every view, as if each had just been shown. */
function seeded() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false, staleTime: Infinity } } });
  for (const key of Object.values(VIEWS)) client.setQueryData(key, { cached: true });
  const wrapper = ({ children }: { children: React.ReactNode }) => (
    <QueryClientProvider client={client}>{children}</QueryClientProvider>
  );
  return { client, wrapper };
}

function expectEveryViewStale(client: QueryClient) {
  for (const [view, key] of Object.entries(VIEWS)) {
    expect(client.getQueryState(key)?.isInvalidated, `the ${view} is read again`).toBe(true);
  }
}

describe("edits made in the issue panel", () => {
  it("leave no view showing the old summary, priority or assignee", async () => {
    const { client, wrapper } = seeded();
    const { result } = renderHook(() => useUpdateIssue(), { wrapper });

    result.current.mutate({ key: "PR-1", assigneeId: "u2" });

    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    expectEveryViewStale(client);
  });

  it("leave no view showing the old labels", async () => {
    const { client, wrapper } = seeded();
    const { result } = renderHook(() => useSetIssueLabels(), { wrapper });

    result.current.mutate({ key: "PR-1", labels: ["backend"] });

    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    expectEveryViewStale(client);
  });

  // Work logged brings the remaining time down, which the resources and the plan read.
  it.each([
    ["logged", () => useLogWork(), { key: "PR-1", minutes: 60 }],
    ["corrected", () => useUpdateWorklog(), { id: "w1", minutes: 30 }],
    ["removed", () => useDeleteWorklog(), "w1"],
  ] as const)("leave no view showing the old remaining time once work is %s", async (_what, use, input) => {
    const { client, wrapper } = seeded();
    const { result } = renderHook(() => use() as { mutate: (v: typeof input) => void; isSuccess: boolean }, { wrapper });

    result.current.mutate(input);

    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    expectEveryViewStale(client);
  });
});
