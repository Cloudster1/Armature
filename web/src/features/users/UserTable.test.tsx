import { afterEach, describe, expect, it, vi } from "vitest";
import { act, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { ManagedUser } from "@/api/users";
import { ConfirmProvider } from "@/features/shell/ConfirmProvider";
import { refusal, renderInShell, serve } from "@/test/shell";
import { PEOPLE_PAGE_SIZE } from "@/config";
import { UserTable } from "./UserTable";

vi.mock("@/api/auth", () => ({ useMe: () => ({ data: { principal: { user: { id: "me" } } } }) }));

const account = (id: string, name: string, more: Partial<ManagedUser> = {}): ManagedUser => ({
  id,
  email: `${id}@example.test`,
  name,
  role: "member",
  isActive: true,
  signsInWith: "password",
  managed: true,
  createdAt: "2026-10-01T08:00:00Z",
  secondFactor: false,
  ...more,
});

// The row acted on is the last of many, far below where a banner above the table would be.
const users = [...Array.from({ length: 20 }, (_, i) => account(`u${i}`, `Person ${i}`)), account("zed", "Zed Last", { isActive: false, secondFactor: true })];

const reactivateRefused = "Zed Last cannot be switched back on while the seat limit is reached. Free a seat first.";
const factorRefused = "Two-step sign-in is required for everyone. Change the rule before turning it off for one person.";

function show() {
  serve({
    [`GET /users?limit=${PEOPLE_PAGE_SIZE}`]: [200, { users, total: users.length, limit: PEOPLE_PAGE_SIZE, offset: 0 }],
    "PATCH /users/zed": [409, refusal("conflict", reactivateRefused)],
    "DELETE /users/zed/second-factor": [409, refusal("conflict", factorRefused)],
  });
  renderInShell(
    <ConfirmProvider>
      <UserTable show={() => {}} />
    </ConfirmProvider>,
  );
}

async function actOn(item: string) {
  await userEvent.click(await screen.findByRole("button", { name: "Actions for Zed Last" }));
  await userEvent.click(screen.getByRole("menuitem", { name: item }));
}

/** What the page says outside the toasts, where a banner above the table would be. */
const banners = () => [...document.querySelectorAll('[role="alert"]')].filter((el) => !el.closest("[data-toast]"));

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("a refused change to an account in the table", () => {
  it("is said in a toast, not in a banner above the table, when switching somebody back on", async () => {
    show();
    await actOn("Reactivate");
    const toast = await screen.findByText(reactivateRefused);
    expect(toast.closest("[data-toast]")).not.toBeNull();
    await act(() => new Promise((resolve) => setTimeout(resolve)));
    expect(screen.getAllByText(reactivateRefused)).toHaveLength(1);
    expect(banners()).toHaveLength(0);
  });

  it("is said in a toast when turning off somebody's two-step sign-in", async () => {
    show();
    await actOn("Turn off two-step sign-in");
    await userEvent.click(within(screen.getByRole("dialog")).getByRole("button", { name: /Turn off/ }));
    const toast = await screen.findByText(factorRefused);
    expect(toast.closest("[data-toast]")).not.toBeNull();
    expect(banners()).toHaveLength(0);
  });
});
