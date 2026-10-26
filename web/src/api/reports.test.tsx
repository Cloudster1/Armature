import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { renderHook, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";

import { useRemoveWidget, useUpdateWidget } from "./reports";

const fetchMock = vi.fn();

/** Answers held until the test lets each one through, in the order they were asked. */
let pending: Array<() => void> = [];

beforeEach(() => {
  pending = [];
  vi.stubGlobal("fetch", fetchMock);
  fetchMock.mockReset();
  fetchMock.mockImplementation(
    () =>
      new Promise((resolve) => {
        pending.push(() => resolve({ status: 200, ok: true, text: () => Promise.resolve('{"widget":{}}') }));
      }),
  );
});

afterEach(() => {
  vi.unstubAllGlobals();
});

/** One cache for the test, as one page has: mutations queue within a cache. */
function oneCache() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return ({ children }: { children: React.ReactNode }) => <QueryClientProvider client={client}>{children}</QueryClientProvider>;
}

/** Lets the n-th held request answer. */
function answer(n: number) {
  const release = pending[n];
  if (!release) throw new Error(`request ${n} was never sent`);
  release();
}

function sent(call: number) {
  const [url, init] = fetchMock.mock.calls[call] as [string, RequestInit];
  return { url, method: init.method, body: JSON.parse(String(init.body ?? "null")) };
}

describe("changes to a dashboard's widgets", () => {
  // Two quick picks in a widget's settings: the second must not overtake the first,
  // or the server keeps the older one (#337).
  it("go to the server one after another, so the last pick is the one kept", async () => {
    const { result } = renderHook(() => useUpdateWidget(), { wrapper: oneCache() });

    result.current.mutate({ id: "w1", config: { shape: "stacked" } });
    result.current.mutate({ id: "w1", config: { shape: "donut" } });

    await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(1));
    expect(sent(0).body.config.shape).toBe("stacked");
    await new Promise((settle) => setTimeout(settle, 20));
    expect(fetchMock, "the second save waits for the first to answer").toHaveBeenCalledTimes(1);

    answer(0);
    await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(2));
    expect(sent(1).body.config.shape).toBe("donut");
    answer(1);
  });

  it("queue across kinds of change, so a widget is not removed under its own save", async () => {
    const wrapper = oneCache();
    const { result: update } = renderHook(() => useUpdateWidget(), { wrapper });
    const { result: remove } = renderHook(() => useRemoveWidget(), { wrapper });

    update.current.mutate({ id: "w1", title: "Renamed" });
    remove.current.mutate("w1");

    await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(1));
    expect(sent(0).method).toBe("PATCH");
    answer(0);
    await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(2));
    expect(sent(1).method).toBe("DELETE");
    answer(1);
  });
});
