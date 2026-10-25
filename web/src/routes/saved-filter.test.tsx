import { beforeEach, describe, expect, it, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import type { ReactNode } from "react";
import type { SavedFilter } from "@/api/filters";

const navigate = vi.fn();
const answer: { data?: { filter: SavedFilter }; error: Error | null } = { error: null };
const asked: string[] = [];

vi.mock("@tanstack/react-router", () => ({
  Link: ({ children, to }: { children: ReactNode; to: string }) => <a href={to}>{children}</a>,
  createRoute: (options: object) => ({ ...options, useParams: () => ({ filterId: "f-1" }) }),
  useNavigate: () => navigate,
}));
vi.mock("./app", () => ({ appRoute: {} }));
vi.mock("@/api/filters", () => ({
  useSavedFilter: (id: string) => {
    asked.push(id);
    return answer;
  },
}));

const { savedFilterRoute } = await import("./saved-filter");
const SavedFilterPage = (savedFilterRoute as unknown as { component: () => ReactNode }).component;

const mine: SavedFilter = {
  id: "f-1",
  ownerId: "u-1",
  ownerName: "Ada",
  name: "My open work",
  query: "assignee = currentUser() AND statusCategory != done",
  shared: false,
  columns: [],
  starred: false,
  createdAt: "2026-10-01T08:00:00Z",
  updatedAt: "2026-10-01T08:00:00Z",
};

describe("a saved search by its address", () => {
  beforeEach(() => {
    navigate.mockClear();
    asked.length = 0;
    answer.data = undefined;
    answer.error = null;
  });

  // The address a subscription mail links to, /filters/<id>, runs the search.
  it("runs the saved search as it reads now", () => {
    answer.data = { filter: mine };
    render(<SavedFilterPage />);
    expect(asked).toContain("f-1");
    expect(navigate).toHaveBeenCalledWith({ to: "/search", search: { q: mine.query, f: "f-1" }, replace: true });
  });

  it("says so when the search is gone or no longer shared, and leads to the list", () => {
    answer.error = new Error("saved filter not found");
    render(<SavedFilterPage />);
    expect(screen.getByText("This saved search is not here")).toBeTruthy();
    expect(screen.getByText("Saved searches").closest("a")?.getAttribute("href")).toBe("/filters");
    expect(navigate).not.toHaveBeenCalled();
  });
});
