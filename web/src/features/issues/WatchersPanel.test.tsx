import { describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen } from "@testing-library/react";
import type { Watcher } from "@/api/watchers";

const addMutate = vi.fn();
const removeMutate = vi.fn();
let watchers: Watcher[] = [];
vi.mock("@/api/watchers", () => ({
  useWatchers: () => ({ data: { watchers }, isLoading: false }),
  useAddWatcher: () => ({ mutate: addMutate, error: null, isPending: false }),
  useRemoveWatcher: () => ({ mutate: removeMutate, error: null, isPending: false }),
}));
vi.mock("@/api/issues", () => ({
  useMembers: () => ({ data: { members: [{ id: "ben", name: "Ben", email: "ben@example.com", role: "member" }] } }),
}));

import { WatchersPanel } from "./WatchersPanel";

describe("WatchersPanel", () => {
  it("lists who is watching and lets me start", () => {
    watchers = [{ userId: "ada", name: "Ada", email: "ada@example.com", addedAt: "2026-01-01T00:00:00Z" }];
    render(<WatchersPanel issueKey="HLP-1" editable me="ben" />);
    expect(screen.getByText("ada@example.com")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Watch" }));
    expect(addMutate).toHaveBeenCalledWith({ key: "HLP-1" });
  });

  it("adds somebody by address and removes a watcher", () => {
    watchers = [{ userId: "ada", name: "Ada", email: "ada@example.com", addedAt: "2026-01-01T00:00:00Z" }];
    render(<WatchersPanel issueKey="HLP-1" editable me="ada" />);
    fireEvent.click(screen.getByRole("button", { name: "Add watcher" }));
    fireEvent.change(screen.getByLabelText("An address to add"), { target: { value: "carol@example.com" } });
    fireEvent.click(screen.getByRole("button", { name: "Add" }));
    expect(addMutate).toHaveBeenCalledWith({ key: "HLP-1", email: "carol@example.com" }, expect.anything());

    fireEvent.click(screen.getByRole("button", { name: "Stop Ada watching" }));
    expect(removeMutate).toHaveBeenCalledWith({ key: "HLP-1", userId: "ada" });
  });
});
