import { beforeEach, describe, expect, it, vi } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { ReactNode } from "react";
import type { Label } from "@/api/labels";

const navigate = vi.fn();
const page = {
  search: {} as { q?: string; offset?: number },
  rows: [] as Label[],
  total: 0,
  asked: [] as unknown[],
};

function label(name: string): Label {
  return { id: name, name, color: "blue", issueCount: 1, createdAt: "2026-10-10T08:00:00Z", updatedAt: "2026-10-10T08:00:00Z" };
}

vi.mock("@tanstack/react-router", () => ({
  Link: ({ children }: { children: ReactNode }) => <a>{children}</a>,
  createRoute: (options: object) => ({ ...options, useSearch: () => page.search }),
  useNavigate: () => navigate,
}));
vi.mock("./app", () => ({ appRoute: {} }));
vi.mock("@/api/access", () => ({ useAccess: () => ({ data: { canAdministerOrg: true } }) }));
vi.mock("@/features/shell/ConfirmProvider", () => ({ useConfirm: () => vi.fn() }));
vi.mock("@/api/labels", () => ({
  LABEL_COLORS: ["blue"],
  useLabels: (params: unknown) => {
    page.asked.push(params);
    return { data: { labels: page.rows, total: page.total, limit: 50, offset: page.search.offset ?? 0 }, isLoading: false, error: null };
  },
  useDeleteLabel: () => ({ mutate: vi.fn(), error: null }),
  useUpdateLabel: () => ({ mutate: vi.fn(), isPending: false, error: null }),
}));

const { labelsRoute } = await import("./labels");
const route = labelsRoute as unknown as { component: () => ReactNode; validateSearch: (search: Record<string, unknown>) => unknown };
const LabelsPage = route.component;

beforeEach(() => {
  page.search = {};
  page.rows = [label("backend"), label("urgent")];
  page.total = 120;
  page.asked = [];
  navigate.mockReset();
});

// The installation's words come a page at a time, with how many there are, never all at once.
describe("the labels page", () => {
  it("asks for the page the address names and says which of the whole list it shows", () => {
    page.search = { q: "end", offset: 50 };
    render(<LabelsPage />);
    expect(page.asked.at(-1)).toEqual({ q: "end", offset: 50 });
    expect(screen.getByText("51 to 100 of 120")).toBeInTheDocument();
  });

  it("turns the page in the address, keeping the search", async () => {
    page.search = { q: "end" };
    render(<LabelsPage />);
    await userEvent.click(screen.getByRole("button", { name: "Next page" }));
    expect(navigate).toHaveBeenLastCalledWith({ to: "/admin/labels", search: { q: "end", offset: 50 }, replace: false });
  });

  it("writes the search into the address once typing pauses, back at the first page", async () => {
    page.search = { offset: 50 };
    render(<LabelsPage />);
    await userEvent.type(screen.getByLabelText("Find labels"), "urg");
    await waitFor(() => expect(navigate).toHaveBeenCalledWith({ to: "/admin/labels", search: { q: "urg", offset: undefined }, replace: false }));
    expect(navigate).toHaveBeenCalledTimes(1);
  });

  it("says when nothing matches the search rather than that there are no labels", () => {
    page.search = { q: "nothing" };
    page.rows = [];
    page.total = 0;
    render(<LabelsPage />);
    expect(screen.getByText("No label matches that")).toBeInTheDocument();
    expect(screen.queryByText("No labels yet")).toBeNull();
  });

  it("keeps the search and the page in the address, and drops what is not one", () => {
    expect(route.validateSearch({ q: "end", offset: 50 })).toEqual({ q: "end", offset: 50 });
    expect(route.validateSearch({ q: "", offset: -1 })).toEqual({});
  });
});
