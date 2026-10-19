import { beforeEach, describe, expect, it, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { ReactNode } from "react";
import { ApiError } from "@/api/client";
import type { Consent } from "@/api/oauth";

const leave = vi.fn();
const decide = vi.fn<(input: unknown, options: { onSuccess: (r: { redirectTo: string }) => void }) => void>();
const state = { consent: { data: undefined as unknown, error: null as unknown, isLoading: false } };

vi.mock("@tanstack/react-router", () => ({
  Link: ({ children, to }: { children: ReactNode; to: string }) => <a href={to}>{children}</a>,
  createRoute: (options: unknown) => options,
  redirect: vi.fn(),
}));
vi.mock("./root", () => ({ rootRoute: {} }));
vi.mock("./auth", () => ({
  AuthLayout: ({ title, children }: { title: string; children: ReactNode }) => (
    <div>
      <h1>{title}</h1>
      {children}
    </div>
  ),
}));
vi.mock("@/lib/leave", () => ({ leave }));
vi.mock("@/api/oauth", () => ({
  useConsent: (query: string) => ({ ...state.consent, query }),
  useDecideConsent: () => ({ mutate: decide, isPending: false, error: null }),
}));

const { oauthAuthorizeRoute } = await import("./oauth-authorize");
const ConsentPage = (oauthAuthorizeRoute as unknown as { component: () => ReactNode }).component;
const query = "response_type=code&client_id=stator&state=s";

function consent(overrides: Partial<Consent> = {}): Consent {
  return {
    client: { id: "stator", name: "Stator", clientUri: "https://stator.example.com" },
    redirectHost: "stator.example.com",
    pending: false,
    person: "Ada Lovelace",
    scopes: [
      { scope: "read", words: "See the projects and issues you can see" },
      { scope: "issue.write", words: "File and edit issues, and link pages to them" },
    ],
    mayAllow: true,
    mayCancel: true,
    ...overrides,
  };
}

beforeEach(() => {
  window.history.replaceState(null, "", `/oauth/authorize?${query}`);
  leave.mockReset();
  decide.mockReset();
  state.consent = { data: { consent: consent() }, error: null, isLoading: false };
});

describe("the consent page", () => {
  it("says who the app acts as and what it may do, and allows it", async () => {
    render(<ConsentPage />);
    expect(screen.getByRole("heading", { name: "Connect Stator" })).toBeTruthy();
    expect(screen.getByText(/wants to act as/).textContent).toContain("Ada Lovelace");
    expect(screen.getByText("File and edit issues, and link pages to them")).toBeTruthy();
    await userEvent.click(screen.getByRole("button", { name: "Allow" }));
    expect(decide).toHaveBeenCalledWith({ query, decision: "allow" }, expect.anything());
    decide.mock.calls[0]?.[1].onSuccess({ redirectTo: "https://stator.example.com/cb?code=c&state=s" });
    expect(leave).toHaveBeenCalledWith("https://stator.example.com/cb?code=c&state=s");
  });

  it("offers only Cancel with the reason when the person may not allow", async () => {
    state.consent.data = { consent: consent({ mayAllow: false, refusal: "Sending Armature's events to Stator needs an Armature administrator." }) };
    render(<ConsentPage />);
    expect(screen.queryByRole("button", { name: "Allow" })).toBeNull();
    expect(screen.getByText(/needs an Armature administrator/)).toBeTruthy();
    await userEvent.click(screen.getByRole("button", { name: "Cancel" }));
    expect(decide).toHaveBeenCalledWith({ query, decision: "deny" }, expect.anything());
  });

  it("asks an administrator to let a pending app in", () => {
    state.consent.data = { consent: consent({ pending: true }) };
    render(<ConsentPage />);
    expect(screen.getByText(/asks to be connected to Armature/)).toBeTruthy();
  });

  it("goes straight back to an app the person allowed already", () => {
    state.consent.data = { redirectTo: "https://stator.example.com/cb?code=c&state=s" };
    render(<ConsentPage />);
    expect(leave).toHaveBeenCalledWith("https://stator.example.com/cb?code=c&state=s");
    expect(screen.queryByRole("button", { name: "Allow" })).toBeNull();
  });

  it("never sends the browser to an app Armature does not know", () => {
    state.consent = {
      data: undefined,
      error: new ApiError(400, { code: "not_connected", message: "This app is not connected to Armature. Ask an Armature administrator to connect it." }),
      isLoading: false,
    };
    render(<ConsentPage />);
    expect(screen.getByRole("heading", { name: "This app cannot be connected" })).toBeTruthy();
    expect(screen.getByText(/Ask an Armature administrator/)).toBeTruthy();
    expect(leave).not.toHaveBeenCalled();
  });
});
