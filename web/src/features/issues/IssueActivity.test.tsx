import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, fireEvent, render, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { Activity } from "./IssueActivity";

// Ctrl+Enter pressed twice, or held, while the first comment is still on its
// way must not post the same words twice.

/** Long enough for every mutation that was started to have reached fetch. */
const SETTLE_MS = 50;

const posts: Array<() => void> = [];
const fetchMock = vi.fn();

function reply(body: unknown) {
  return { status: 200, ok: true, text: () => Promise.resolve(JSON.stringify(body)) };
}

beforeEach(() => {
  posts.length = 0;
  fetchMock.mockReset();
  fetchMock.mockImplementation((url: string, init: { method: string }) => {
    if (init.method === "POST") {
      // The server answers only when the test says so: the post stays pending.
      return new Promise((resolve) => posts.push(() => resolve(reply({ comment: { id: `c${posts.length}` } }))));
    }
    if (url.endsWith("/comments")) return Promise.resolve(reply({ comments: [] }));
    if (url.endsWith("/members")) return Promise.resolve(reply({ members: [] }));
    return Promise.resolve(reply({ project: { key: "CP", kind: "software" } }));
  });
  vi.stubGlobal("fetch", fetchMock);
});

afterEach(() => {
  vi.unstubAllGlobals();
  localStorage.clear();
});

function posted() {
  return fetchMock.mock.calls.filter(([, init]) => init.method === "POST").length;
}

function submitWithKeyboard(box: HTMLElement) {
  fireEvent.keyDown(box, { key: "Enter", ctrlKey: true });
}

describe("adding a comment", () => {
  it("ignores another submit while the first comment is pending, and takes the next one after", async () => {
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    render(
      <QueryClientProvider client={client}>
        <Activity issueKey="CP-1" />
      </QueryClientProvider>,
    );
    const box = document.getElementById("new-comment")!;
    await userEvent.type(box, "Looks right to me.");

    submitWithKeyboard(box);
    submitWithKeyboard(box);
    submitWithKeyboard(box);
    await waitFor(() => expect(posted()).toBeGreaterThan(0));
    await act(() => new Promise((resolve) => setTimeout(resolve, SETTLE_MS)));
    expect(posted()).toBe(1);

    await act(async () => posts[0]!());
    await waitFor(() => expect(box.textContent).toBe(""));

    await userEvent.type(box, "One more thing.");
    submitWithKeyboard(box);
    await waitFor(() => expect(posted()).toBe(2));
  });
});
