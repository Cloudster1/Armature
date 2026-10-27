import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { silentStart, trySilentSignIn } from "./silentSignIn";

const fetchMock = vi.fn();
const assign = vi.fn();

function offers(silentSignIn: boolean) {
  fetchMock.mockResolvedValue(new Response(JSON.stringify({ open: false, first: false, silentSignIn }), { status: 200 }));
}

beforeEach(() => {
  vi.stubGlobal("fetch", fetchMock);
  vi.stubGlobal("location", { ...window.location, assign });
});

afterEach(() => {
  vi.unstubAllGlobals();
  fetchMock.mockReset();
  assign.mockReset();
});

describe("a visit without a session", () => {
  it("asks the provider quietly, coming back to the page with its query", async () => {
    offers(true);
    expect(await trySilentSignIn("/issues/CP-12?tab=history")).toBe(true);
    expect(assign).toHaveBeenCalledWith("/api/v1/auth/oidc/start?prompt=none&next=%2Fissues%2FCP-12%3Ftab%3Dhistory");
  });

  it("goes to the sign-in page when the server says not to try", async () => {
    offers(false);
    expect(await trySilentSignIn("/")).toBe(false);
    expect(assign).not.toHaveBeenCalled();
  });

  it("goes to the sign-in page when the server cannot say", async () => {
    fetchMock.mockRejectedValue(new TypeError("offline"));
    expect(await trySilentSignIn("/")).toBe(false);
    expect(assign).not.toHaveBeenCalled();
  });

  it("names prompt=none and the page in the start address", () => {
    const start = new URL(silentStart("/board?sprint=4"), "http://app.test");
    expect(start.pathname).toBe("/api/v1/auth/oidc/start");
    expect(start.searchParams.get("prompt")).toBe("none");
    expect(start.searchParams.get("next")).toBe("/board?sprint=4");
  });
});
