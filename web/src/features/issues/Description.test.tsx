import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { Doc } from "@/api/issues";
import { Description } from "./Description";

const STALE = "Somebody changed this issue since you opened it. Reload it and apply your edit again.";
const OPENED_AT = "2026-10-10T09:00:00.000001Z";
const THEIRS_AT = "2026-10-10T09:05:00.000002Z";

function paragraph(text: string): Doc {
  return { type: "doc", content: [{ type: "paragraph", content: [{ type: "text", text }] }] };
}

function reply(status: number, body: unknown) {
  return new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } });
}

const sent: Array<Record<string, unknown>> = [];
const fetchMock = vi.fn();

beforeEach(() => {
  localStorage.clear();
  sent.length = 0;
  fetchMock.mockReset();
  fetchMock.mockImplementation(async (_url: string, init?: RequestInit) => {
    if (init?.method === "PATCH") sent.push(JSON.parse(String(init.body)) as Record<string, unknown>);
    return reply(409, { error: { code: "stale_edit", message: STALE } });
  });
  vi.stubGlobal("fetch", fetchMock);
});

afterEach(() => vi.unstubAllGlobals());

describe("the description", () => {
  it("keeps my draft when somebody else saved first, shows their text beside it, and saves over it only once I have seen it", async () => {
    const client = new QueryClient({ defaultOptions: { mutations: { retry: false } } });
    const view = (doc: Doc, updatedAt: string) => (
      <QueryClientProvider client={client}>
        <Description issueKey="TAB-1" doc={doc} updatedAt={updatedAt} editable />
      </QueryClientProvider>
    );
    const { rerender } = render(view(paragraph("as it was"), OPENED_AT));

    await userEvent.click(screen.getByRole("button", { name: "Edit" }));
    const box = document.getElementById("issue-description")!;
    await userEvent.type(box, " and mine");
    await userEvent.click(screen.getByRole("button", { name: "Save description" }));

    expect(await screen.findByText(STALE)).toBeInTheDocument();
    expect(sent[0]).toMatchObject({ expectedUpdatedAt: OPENED_AT });
    expect(document.getElementById("issue-description")?.textContent).toContain("and mine");

    // The page refetches the issue after the refusal; its newer text arrives as props.
    rerender(view(paragraph("their words"), THEIRS_AT));
    const newer = await screen.findByRole("region", { name: "What it says now" });
    expect(newer.textContent).toContain("their words");
    expect(document.getElementById("issue-description")?.textContent).toContain("and mine");

    fetchMock.mockImplementation(async (_url: string, init?: RequestInit) => {
      if (init?.method === "PATCH") sent.push(JSON.parse(String(init.body)) as Record<string, unknown>);
      return reply(200, { issue: {} });
    });
    await userEvent.click(screen.getByRole("button", { name: "Save description" }));
    await waitFor(() => expect(sent).toHaveLength(2));
    expect(sent[1]).toMatchObject({ expectedUpdatedAt: THEIRS_AT });
    expect(JSON.stringify(sent[1].description)).toContain("and mine");
  });
});
