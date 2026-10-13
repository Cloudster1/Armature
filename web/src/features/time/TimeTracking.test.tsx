import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { meQueryKey } from "@/api/auth";
import type { Issue } from "@/api/issues";
import { ConfirmProvider } from "@/features/shell/ConfirmProvider";
import { atInstantIn } from "@/test/zone";
import { WorklogPanel } from "./TimeTracking";

const issue = { key: "FLOW-1", projectKey: "FLOW", timeSpentMinutes: 0 } as Issue;

// The server answers every read with nothing logged and nothing granted.
function answerWithNothing() {
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string) => {
      const body = url.endsWith("/worklogs") ? { worklogs: [] } : { permissions: { org: [], projects: {} } };
      return new Response(JSON.stringify(body), { status: 200, headers: { "Content-Type": "application/json" } });
    }),
  );
}

function openWorkLog(profileZone: string) {
  const client = new QueryClient();
  client.setQueryData(meQueryKey, { principal: { user: { id: "ada", timezone: profileZone, locale: "en-GB" } }, organizations: [] });
  render(
    <QueryClientProvider client={client}>
      <ConfirmProvider>
        <WorklogPanel issue={issue} editable me="ada" />
      </ConfirmProvider>
    </QueryClientProvider>,
  );
}

describe("the day work is logged on", () => {
  afterEach(() => {
    cleanup();
    vi.unstubAllGlobals();
  });

  it("starts at today in the profile's zone east of UTC, where UTC is still on yesterday", () => {
    answerWithNothing();
    atInstantIn("Pacific/Honolulu", "2026-10-09T23:00:00Z");
    openWorkLog("Europe/Berlin");
    expect(screen.getByLabelText("On")).toHaveValue("2026-10-10");
  });

  it("starts at today in the profile's zone west of UTC, where UTC is already on tomorrow", () => {
    answerWithNothing();
    atInstantIn("Asia/Tokyo", "2026-10-10T01:00:00Z");
    openWorkLog("America/Los_Angeles");
    expect(screen.getByLabelText("On")).toHaveValue("2026-10-09");
  });
});

describe("correcting a work log entry", () => {
  afterEach(() => {
    cleanup();
    vi.unstubAllGlobals();
  });

  it("sends the duration, the day and the note together, because the entry is replaced whole", async () => {
    const entry = { id: "w1", issueId: "i", author: { id: "ada", name: "Ada" }, minutes: 480, startedOn: "2026-10-05", note: "pairing", createdAt: "", updatedAt: "" };
    const fetchMock = vi.fn(async (url: string, init?: { method?: string; body?: string }) => {
      const body =
        init?.method === "PATCH"
          ? { worklog: { ...entry, ...JSON.parse(init.body!) } }
          : url.endsWith("/worklogs")
            ? { worklogs: [entry] }
            : { permissions: { org: [], projects: {} } };
      return new Response(JSON.stringify(body), { status: 200, headers: { "Content-Type": "application/json" } });
    });
    vi.stubGlobal("fetch", fetchMock);
    openWorkLog("Europe/Berlin");

    await userEvent.click(await screen.findByRole("button", { name: "Edit work log 1d" }));
    const duration = document.getElementById("worklog-w1-duration")!;
    await userEvent.clear(duration);
    await userEvent.type(duration, "48m");
    await userEvent.click(screen.getByRole("button", { name: "Save" }));

    await waitFor(() => expect(fetchMock.mock.calls.some(([, init]) => init?.method === "PATCH")).toBe(true));
    const [url, init] = fetchMock.mock.calls.find(([, init]) => init?.method === "PATCH")!;
    expect(url).toMatch(/\/worklogs\/w1$/);
    expect(JSON.parse(init!.body!)).toEqual({ minutes: 48, startedOn: "2026-10-05", note: "pairing" });
    await waitFor(() => expect(document.querySelector("[data-worklog-editor]")).toBeNull());
  });
});
