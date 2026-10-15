import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, fireEvent, render, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { ConfirmProvider } from "@/features/shell/ConfirmProvider";
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
    if (url.includes("/members?")) return Promise.resolve(reply({ members: [] }));
    if (url.endsWith("/access/me")) return Promise.resolve(reply(noGrants));
    return Promise.resolve(reply({ project: { key: "CP", kind: "software" } }));
  });
  vi.stubGlobal("fetch", fetchMock);
});

afterEach(() => {
  vi.unstubAllGlobals();
  localStorage.clear();
});

const noGrants = { permissions: { org: [], projects: {} } };

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
        <ConfirmProvider>
          <Activity issueKey="CP-1" />
        </ConfirmProvider>
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

describe("changing a comment", () => {
  const doc = (text: string) => ({ type: "doc", content: [{ type: "paragraph", content: [{ type: "text", text }] }] });
  const comments = [
    { id: "mine", issueId: "i", author: { id: "ada", name: "Ada" }, body: doc("I wrote this."), internal: false, createdAt: "2026-10-01T09:00:00Z" },
    { id: "theirs", issueId: "i", author: { id: "bob", name: "Bob" }, body: doc("Bob wrote this."), internal: false, createdAt: "2026-10-01T10:00:00Z" },
    { id: "note", issueId: "i", author: { id: "ada", name: "Ada" }, body: doc("Between agents."), internal: true, createdAt: "2026-10-01T11:00:00Z" },
  ];

  function serve(access: unknown) {
    fetchMock.mockImplementation((url: string, init: { method: string; body?: string }) => {
      if (init.method === "PATCH") {
        const sent = JSON.parse(init.body!) as { body: unknown };
        const id = url.slice(url.lastIndexOf("/") + 1);
        const before = comments.find((c) => c.id === id)!;
        return Promise.resolve(reply({ comment: { ...before, body: sent.body, editedAt: "2026-10-02T09:00:00Z" } }));
      }
      if (url.endsWith("/comments")) return Promise.resolve(reply({ comments }));
      if (url.includes("/members?")) return Promise.resolve(reply({ members: [] }));
      if (url.endsWith("/access/me")) return Promise.resolve(reply(access));
      return Promise.resolve(reply({ project: { key: "CP", kind: "software" } }));
    });
  }

  function open() {
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    render(
      <QueryClientProvider client={client}>
        <ConfirmProvider>
          <Activity issueKey="CP-1" me="ada" />
        </ConfirmProvider>
      </QueryClientProvider>,
    );
  }

  const item = (id: string) => document.querySelector(`[data-comment="${id}"]`)!;
  const action = (id: string, name: string) => item(id).querySelector(`[data-action="${name}"]`);

  it("offers Edit and Delete on the reader's own comments and nothing on somebody else's", async () => {
    serve(noGrants);
    open();
    await waitFor(() => expect(item("theirs")).not.toBeNull());
    await waitFor(() => expect(action("mine", "edit-comment")).not.toBeNull());
    expect(action("mine", "delete-comment")).not.toBeNull();
    expect(action("note", "edit-comment")).not.toBeNull();
    expect(action("theirs", "edit-comment")).toBeNull();
    expect(action("theirs", "delete-comment")).toBeNull();
  });

  it("lets a project administrator delete somebody else's comment but not rewrite it", async () => {
    serve({ permissions: { org: [], projects: { CP: ["project.administer"] } } });
    open();
    await waitFor(() => expect(action("theirs", "delete-comment")).not.toBeNull());
    expect(action("theirs", "edit-comment")).toBeNull();
  });

  it("saves an edit in place and keeps an internal note internal", async () => {
    serve(noGrants);
    open();
    await waitFor(() => expect(action("note", "edit-comment")).not.toBeNull());
    await userEvent.click(action("note", "edit-comment")!);
    const box = document.getElementById("comment-note")!;
    await userEvent.type(box, " Call them back.");
    await userEvent.click(item("note").querySelector('[data-action="save-comment"]')!);

    await waitFor(() => expect(document.getElementById("comment-note")).toBeNull());
    const patches = fetchMock.mock.calls.filter(([, init]) => init.method === "PATCH");
    expect(patches).toHaveLength(1);
    expect(patches[0]![0]).toMatch(/\/issues\/CP-1\/comments\/note$/);
    expect(JSON.stringify(JSON.parse(patches[0]![1].body))).toContain("Call them back.");
    expect(item("note").querySelector("[data-internal-note]")).not.toBeNull();
  });
});
