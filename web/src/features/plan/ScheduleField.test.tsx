import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, fireEvent, render, screen } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { Issue } from "@/api/issues";
import { ScheduleField } from "./ScheduleField";

// A date typed by hand passes through years such as 0002 and 0020 on its way
// to 2026. Only the date the person meant may reach the server.

/** Long enough for every mutation that was started to have reached fetch. */
const SETTLE_MS = 50;

const fetchMock = vi.fn();

beforeEach(() => {
  fetchMock.mockReset();
  fetchMock.mockImplementation(() => Promise.resolve({ status: 200, ok: true, text: () => Promise.resolve('{"issue":{}}') }));
  vi.stubGlobal("fetch", fetchMock);
});

afterEach(() => {
  vi.unstubAllGlobals();
});

function saved(): unknown[] {
  return fetchMock.mock.calls.filter(([, init]) => init.method === "PUT").map(([, init]) => JSON.parse(init.body as string));
}

async function settle() {
  await act(() => new Promise((resolve) => setTimeout(resolve, SETTLE_MS)));
}

function renderField(issue: Partial<Issue>) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  render(
    <QueryClientProvider client={client}>
      <ScheduleField issue={{ key: "CP-1", ...issue } as Issue} />
    </QueryClientProvider>,
  );
}

describe("the schedule's date inputs", () => {
  it("save a complete date once, never the partial years typed on the way to it", async () => {
    renderField({});
    const start = screen.getByLabelText("Start date") as HTMLInputElement;
    start.focus();
    for (const typed of ["0002-03-04", "0020-03-04", "0202-03-04"]) {
      fireEvent.change(start, { target: { value: typed } });
    }
    await settle();
    expect(saved()).toEqual([]);
    // What has been typed so far stays in the box rather than snapping back.
    expect(start.value).toBe("0202-03-04");

    fireEvent.change(start, { target: { value: "2026-03-04" } });
    fireEvent.blur(start);
    await settle();
    expect(saved()).toEqual([{ startDate: "2026-03-04" }]);
  });

  it("save a cleared date when the input is left, not while it is being retyped", async () => {
    renderField({ dueDate: "2026-03-20" });
    const due = screen.getByLabelText("Due date") as HTMLInputElement;
    due.focus();
    fireEvent.change(due, { target: { value: "" } });
    await settle();
    expect(saved()).toEqual([]);

    fireEvent.blur(due);
    await settle();
    expect(saved()).toEqual([{ dueDate: null }]);
  });

  it("do not save a partial year when the input is left", async () => {
    renderField({});
    const start = screen.getByLabelText("Start date") as HTMLInputElement;
    start.focus();
    fireEvent.change(start, { target: { value: "0020-03-04" } });
    fireEvent.blur(start);
    await settle();
    expect(saved()).toEqual([]);
  });
});
