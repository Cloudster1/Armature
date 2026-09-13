import { afterEach, describe, expect, it, vi } from "vitest";
import { render, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider, useMutation, useQuery } from "@tanstack/react-query";
import { useEffect } from "react";

import { Settled } from "./Settled";

// The router is only asked whether it is between pages.
let routerStatus: "idle" | "pending" = "idle";
vi.mock("@tanstack/react-router", () => ({
  useRouter: () => ({ state: { status: routerStatus } }),
  useRouterState: ({ select }: { select: (s: { status: string }) => unknown }) => select({ status: routerStatus }),
}));

function answerAfter<T>(value: T, ms: number) {
  return () => new Promise<T>((resolve) => setTimeout(() => resolve(value), ms));
}

// The second query only exists once the first has answered, which is the
// shape an issue page has: the issue, then its history.
function Second() {
  const second = useQuery({ queryKey: ["second"], queryFn: answerAfter("two", 20) });
  return <p>{second.data ?? "waiting for two"}</p>;
}

function First() {
  const first = useQuery({ queryKey: ["first"], queryFn: answerAfter("one", 20) });
  if (!first.data) return <p>waiting for one</p>;
  return <Second />;
}

// A page that acts as soon as it shows, the way a form does on submit.
function Acting() {
  const act = useMutation({ mutationFn: answerAfter("done", 30) });
  useEffect(() => act.mutate(), []); // eslint-disable-line react-hooks/exhaustive-deps
  return <p>{act.data ?? "acting"}</p>;
}

/** Every value the body's data-settled took, in order, as long as the observer runs. */
function watchSettled() {
  const seen: string[] = [];
  const observer = new MutationObserver(() => seen.push(document.body.dataset.settled ?? ""));
  observer.observe(document.body, { attributes: true, attributeFilter: ["data-settled"] });
  return { seen, stop: () => observer.disconnect() };
}

function mount(ui: React.ReactNode) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={client}>
      {ui}
      <Settled />
    </QueryClientProvider>,
  );
}

describe("Settled", () => {
  afterEach(() => {
    delete document.body.dataset.settled;
    routerStatus = "idle";
  });

  it("is never settled between a query answering and the query that answer leads to", async () => {
    const watch = watchSettled();
    const view = mount(<First />);

    await waitFor(() => expect(view.getByText("two")).toBeInTheDocument());
    await waitFor(() => expect(document.body.dataset.settled).toBe("true"));
    watch.stop();

    // Busy from the first fetch, and settled exactly once: at the end.
    expect(watch.seen[0]).toBe("false");
    expect(watch.seen.filter((v) => v === "true")).toHaveLength(1);
    expect(watch.seen.at(-1)).toBe("true");
  });

  it("is busy while a mutation is in flight, since what it does next has not begun", async () => {
    const watch = watchSettled();
    const view = mount(<Acting />);
    await waitFor(() => expect(view.getByText("done")).toBeInTheDocument());
    await waitFor(() => expect(document.body.dataset.settled).toBe("true"));
    watch.stop();
    expect(watch.seen[0]).toBe("false");
    expect(watch.seen.filter((v) => v === "true")).toHaveLength(1);
  });

  it("is settled once nothing is asked for", async () => {
    mount(<p>nothing to fetch</p>);
    await waitFor(() => expect(document.body.dataset.settled).toBe("true"));
  });

  it("is busy while the router is between pages", async () => {
    routerStatus = "pending";
    mount(<p>on the way</p>);
    await waitFor(() => expect(document.body.dataset.settled).toBe("false"));
  });
});
