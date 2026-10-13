import { beforeEach, describe, expect, it, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { FormEvent, ReactNode } from "react";
import type { Issue, IssueQuery } from "@/api/issues";

const MATCHED = 450;
const PAGE = 100;

const address: { search: { q: string; f?: string; offset?: number } } = { search: { q: "" } };
const navigate = vi.fn();
const asked: IssueQuery[] = [];

vi.mock("@tanstack/react-router", () => ({
  Link: ({ children }: { children: ReactNode }) => <a>{children}</a>,
  createRoute: (options: object) => ({ ...options, useSearch: () => address.search }),
  useNavigate: () => navigate,
}));
vi.mock("./app", () => ({ appRoute: {} }));
vi.mock("@/lib/format", () => ({ useFormat: () => ({ relative: () => "just now" }) }));
vi.mock("@/api/auth", () => ({ useMe: () => ({ data: undefined }) }));
vi.mock("@/api/filters", async () => ({
  exportHref: (await vi.importActual<typeof import("@/api/filters")>("@/api/filters")).exportHref,
  useSavedFilters: () => ({ data: { filters: [] } }),
  useBulkEdit: () => ({ mutate: vi.fn(), isPending: false, error: null }),
}));
vi.mock("@/features/filters/SavedFilterBar", () => ({ SavedFilterBar: () => null }));
vi.mock("@/features/search/QueryInput", () => ({
  QueryInput: ({ value, onSubmit }: { value: string; onSubmit: (query: string) => void }) => (
    <form
      onSubmit={(event: FormEvent<HTMLFormElement>) => {
        event.preventDefault();
        onSubmit(String(new FormData(event.currentTarget).get("q")));
      }}
    >
      <input name="q" aria-label="Query" defaultValue={value} />
    </form>
  ),
}));

function issue(n: number): Issue {
  return {
    id: `i${n}`,
    key: `CP-${n}`,
    summary: `Issue ${n}`,
    projectKey: "CP",
    type: { id: "t", name: "Task", icon: "task" },
    status: { id: "s", name: "To Do", category: "todo" },
    priority: "medium",
    labels: [],
    updatedAt: "2026-10-10T08:00:00Z",
  } as unknown as Issue;
}

// One answer per page, as the query cache gives, so an effect on the data runs once.
const pages = new Map<number, { issues: Issue[]; total: number; limit: number; offset: number }>();
function page(offset: number) {
  if (!pages.has(offset)) {
    const shown = Math.max(0, Math.min(PAGE, MATCHED - offset));
    pages.set(offset, { issues: Array.from({ length: shown }, (_, i) => issue(offset + i + 1)), total: MATCHED, limit: PAGE, offset });
  }
  return pages.get(offset);
}

vi.mock("@/api/issues", async () => {
  const actual = await vi.importActual<typeof import("@/api/issues")>("@/api/issues");
  return {
    ...actual,
    useMembers: () => ({ data: { members: [] } }),
    useIssues: (query: IssueQuery) => {
      asked.push(query);
      return { data: page(query.offset ?? 0), error: null, isLoading: false };
    },
  };
});

const { searchRoute } = await import("./search");
const SearchPage = (searchRoute as unknown as { component: () => ReactNode }).component;
const validate = (searchRoute as unknown as { validateSearch: (search: Record<string, unknown>) => unknown }).validateSearch;

beforeEach(() => {
  address.search = { q: "project = CP" };
  navigate.mockReset();
  asked.length = 0;
  localStorage.clear();
});

const lastSearch = () => (navigate.mock.lastCall?.[0] as { search: Record<string, unknown> }).search;

describe("the search page", () => {
  it("asks for the page the address names and says which of the result it is", () => {
    address.search = { q: "project = CP", offset: 100 };
    render(<SearchPage />);
    expect(asked.at(-1)).toMatchObject({ q: "project = CP", limit: PAGE, offset: 100 });
    expect(screen.getByTestId("search-count")).toHaveTextContent("101 to 200 of 450 issues");
    expect(screen.getByRole("navigation", { name: "Issue pages" })).toHaveTextContent("101 to 200 of 450");
  });

  it("keeps the page in the address, so a reload or a link lands on it", async () => {
    render(<SearchPage />);
    await userEvent.click(screen.getByRole("button", { name: "Next page" }));
    expect(lastSearch()).toMatchObject({ q: "project = CP", offset: 100 });
    expect(validate({ q: "project = CP", offset: 100 })).toEqual({ q: "project = CP", offset: 100 });
    expect(validate({ q: "project = CP", offset: "lots" })).toEqual({ q: "project = CP" });
  });

  it("starts a new query at its first page", async () => {
    address.search = { q: "project = CP", offset: 200 };
    render(<SearchPage />);
    const field = screen.getByLabelText("Query");
    await userEvent.clear(field);
    await userEvent.type(field, "type = Bug{Enter}");
    expect(lastSearch()).toEqual({ q: "type = Bug" });
  });

  it("starts a recent query at its first page", async () => {
    localStorage.setItem("armature.recentQueries", JSON.stringify(["status = Done"]));
    address.search = { q: "project = CP", offset: 200 };
    render(<SearchPage />);
    await userEvent.click(screen.getByRole("button", { name: "status = Done" }));
    expect(lastSearch()).toEqual({ q: "status = Done" });
  });

  it("steps back to the last page from a page past the end", () => {
    address.search = { q: "project = CP", offset: 900 };
    render(<SearchPage />);
    expect(lastSearch()).toMatchObject({ q: "project = CP", offset: 400 });
  });

  it("selects the page on screen, says so, and lets go on another page", async () => {
    const { rerender } = render(<SearchPage />);
    await userEvent.click(screen.getByLabelText("Select every issue on this page"));
    expect(screen.getByText("100 selected on this page")).toBeInTheDocument();
    address.search = { q: "project = CP", offset: 100 };
    rerender(<SearchPage />);
    expect(screen.queryByText(/selected on this page/)).not.toBeInTheDocument();
  });
});
