import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { meQueryKey } from "@/api/auth";
import type { Share } from "@/api/shares";
import { ToastProvider } from "@/components/ui";
import { ConfirmProvider } from "@/features/shell/ConfirmProvider";
import { atInstantIn } from "@/test/zone";
import { ShareDialog } from "./ShareDialog";

const json = (body: unknown) => new Response(JSON.stringify(body), { status: 200, headers: { "Content-Type": "application/json" } });

// The server keeps the links it is sent, so the row reads back what was asked for.
function serveShares(): { sent: () => Record<string, string> | undefined } {
  let sent: Record<string, string> | undefined;
  let shares: Share[] = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (_url: string, init?: RequestInit) => {
      if (init?.method !== "POST") return json({ shares });
      sent = JSON.parse(String(init.body));
      const share: Share = { id: "s1", dashboardId: "d1", name: sent!.name!, createdAt: "2026-10-01T12:00:00Z", expiresAt: sent!.expiresAt };
      shares = [share];
      return json({ share, url: "http://localhost/shared/token" });
    }),
  );
  return { sent: () => sent };
}

function openShareDialog(profileZone: string) {
  const client = new QueryClient();
  client.setQueryData(meQueryKey, { principal: { user: { id: "ada", timezone: profileZone, locale: "en-GB" } }, organizations: [] });
  render(
    <QueryClientProvider client={client}>
      <ToastProvider>
        <ConfirmProvider>
          <ShareDialog dashboardId="d1" dashboardName="Flow" query="" open onClose={() => {}} />
        </ConfirmProvider>
      </ToastProvider>
    </QueryClientProvider>,
  );
}

async function makeLinkExpiringOn(day: string) {
  fireEvent.change(screen.getByLabelText("Link name"), { target: { value: "Lobby screen" } });
  fireEvent.change(screen.getByLabelText(/Expires on/), { target: { value: day } });
  fireEvent.click(screen.getByRole("button", { name: "New link" }));
  return waitFor(() => expect(document.querySelector('[data-share="Lobby screen"]')).not.toBeNull());
}

describe("a share link's last day", () => {
  afterEach(() => {
    cleanup();
    vi.unstubAllGlobals();
  });

  // Each browser zone is on the other side of UTC from the profile's.
  const cases = [
    { profile: "Europe/Berlin", browser: "America/Chicago", expiresAt: "2026-10-10T21:59:59Z" },
    { profile: "America/Los_Angeles", browser: "Asia/Tokyo", expiresAt: "2026-10-11T06:59:59Z" },
  ];
  for (const { profile, browser, expiresAt } of cases) {
    it(`lasts to the end of that day in the profile's zone, ${profile}, and reads back as that day`, async () => {
      const server = serveShares();
      atInstantIn(browser, "2026-10-01T12:00:00Z");
      openShareDialog(profile);
      await makeLinkExpiringOn("2026-10-10");
      expect(Date.parse(server.sent()!.expiresAt!)).toBe(Date.parse(expiresAt));
      const row = document.querySelector<HTMLElement>('[data-share="Lobby screen"]')!;
      expect(within(row).getByText("10 Oct 2026")).toBeInTheDocument();
    });
  }
});
