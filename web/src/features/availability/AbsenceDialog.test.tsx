import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { ToastProvider } from "@/components/ui";
import { ConfirmProvider } from "@/features/shell/ConfirmProvider";
import type { Absence } from "@/api/availability";
import { MS_PER_DAY } from "@/config";
import { today } from "@/lib/week";
import { AbsenceDialog } from "./Absences";

// The editor against a stand-in for the server that lists as the real one does:
// with no days a month back and a year ahead, with only from everything after.

const DAYS_BEHIND = 31;
const DAYS_AHEAD = 365;
const FOURTEEN_MONTHS_DAYS = 426;

let stored: Absence[] = [];
let truncated = false;

function shift(days: number): string {
  return today(new Date(Date.now() + days * MS_PER_DAY));
}

function reply(status: number, body: unknown) {
  const text = body === undefined ? "" : JSON.stringify(body);
  return Promise.resolve({ status, ok: status < 300, text: () => Promise.resolve(text) });
}

function server(url: string, init: { method?: string; body?: string } = {}) {
  const { pathname, searchParams } = new URL(url, "http://armature.test");
  if (pathname === "/api/v1/absences" && (init.method ?? "GET") === "GET") {
    const from = searchParams.get("from") ?? shift(-DAYS_BEHIND);
    const to = searchParams.get("to") ?? (searchParams.has("from") ? null : shift(DAYS_AHEAD));
    const absences = stored.filter((a) => a.endsOn >= from && (to === null || a.startsOn <= to));
    return reply(200, { absences, from, to, truncated });
  }
  if (pathname === "/api/v1/absences" && init.method === "POST") {
    const input = JSON.parse(init.body ?? "{}") as Absence;
    const absence: Absence = { ...input, id: `a${stored.length + 1}`, userName: "Ada Away" };
    stored = [...stored, absence];
    return reply(201, { absence });
  }
  return reply(401, { error: { code: "unauthenticated", message: "Sign in first." } });
}

function openEditor() {
  render(
    <QueryClientProvider client={new QueryClient()}>
      <ToastProvider>
        <ConfirmProvider>
          <AbsenceDialog userId="u1" name="Ada" onClose={() => undefined} />
        </ConfirmProvider>
      </ToastProvider>
    </QueryClientProvider>,
  );
}

describe("the absences a person's editor lists", () => {
  beforeEach(() => {
    stored = [];
    truncated = false;
    vi.stubGlobal("fetch", vi.fn(server));
  });
  afterEach(() => vi.unstubAllGlobals());

  it("shows an absence fourteen months ahead straight after it is saved", async () => {
    openEditor();
    const first = await screen.findByLabelText("First day");
    const farAhead = shift(FOURTEEN_MONTHS_DAYS);
    await userEvent.type(first, farAhead);
    await userEvent.click(screen.getByRole("button", { name: "Record absence" }));
    await waitFor(() => expect(document.querySelector(`[data-absence="${farAhead}"]`)).not.toBeNull());
    expect(screen.queryByText(/is not away/)).toBeNull();
  });

  it("lists from a month back, the past month marked as past", async () => {
    stored = [{ id: "old", userId: "u1", userName: "Ada Away", startsOn: shift(-20), endsOn: shift(-19), halfDay: false }];
    openEditor();
    await waitFor(() => expect(document.querySelector(`[data-absence="${shift(-20)}"]`)).not.toBeNull());
    expect(screen.getByText("Past")).toBeInTheDocument();
  });

  it("says when the server cut the list short", async () => {
    stored = [{ id: "soon", userId: "u1", userName: "Ada Away", startsOn: shift(3), endsOn: shift(3), halfDay: false }];
    truncated = true;
    openEditor();
    expect(await screen.findByText(/More absences follow the last one listed\./)).toBeInTheDocument();
  });
});
