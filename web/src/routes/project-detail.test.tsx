import { beforeEach, describe, expect, it, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { ReactNode } from "react";
import type { Issue, IssueQuery } from "@/api/issues";

const FILED = 205;
const PAGE = 100;

const address: { search: { offset?: number } } = { search: {} };
const navigate = vi.fn();
const asked: IssueQuery[] = [];

vi.mock("@tanstack/react-router", () => ({
  Link: ({ children }: { children: ReactNode }) => <a>{children}</a>,
  createRoute: (options: object) => ({ ...options, useParams: () => ({ projectKey: "CP" }), useSearch: () => address.search }),
  useNavigate: () => navigate,
}));
vi.mock("./project", () => ({ projectRoute: {} }));
vi.mock("@/lib/format", () => ({ useFormat: () => ({ relative: () => "just now" }) }));
vi.mock("@/api/projects", () => ({ useProject: () => ({ data: { project: { name: "Portal", openIssueCount: FILED, issueCount: FILED } } }) }));
vi.mock("@/api/access", () => ({ useAccess: () => ({ data: undefined }), canAdminister: () => false }));
vi.mock("@/api/labels", () => ({ useLabelSearch: () => ({ data: undefined }) }));
vi.mock("@/api/milestones", () => ({ useMilestones: () => ({ data: { milestones: [] } }) }));
vi.mock("@/features/issues/CreateIssueDialog", () => ({ CreateIssueDialog: () => null }));
vi.mock("@/features/projects/StatusStrip", () => ({ StatusStrip: () => null }));
vi.mock("@/features/search/QueryInput", () => ({ QueryInput: () => null }));

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
    const shown = Math.max(0, Math.min(PAGE, FILED - offset));
    pages.set(offset, { issues: Array.from({ length: shown }, (_, i) => issue(offset + i + 1)), total: FILED, limit: PAGE, offset });
  }
  return pages.get(offset);
}

vi.mock("@/api/issues", async () => {
  const actual = await vi.importActual<typeof import("@/api/issues")>("@/api/issues");
  return {
    ...actual,
    useIssues: (query: IssueQuery) => {
      asked.push(query);
      return { data: page(query.offset ?? 0), error: null, isLoading: false };
    },
  };
});

const { listQuery, projectDetailRoute } = await import("./project-detail");
const ProjectDetail = (projectDetailRoute as unknown as { component: () => ReactNode }).component;
const validate = (projectDetailRoute as unknown as { validateSearch: (search: Record<string, unknown>) => unknown }).validateSearch;

beforeEach(() => {
  address.search = {};
  navigate.mockReset();
  asked.length = 0;
});

const lastCall = () => navigate.mock.lastCall?.[0] as { search: Record<string, unknown>; replace?: boolean };

describe("the project's issue list", () => {
  it("says how many of the whole list are shown", () => {
    render(<ProjectDetail />);
    expect(asked.at(-1)).toMatchObject({ project: "CP", limit: PAGE });
    expect(screen.getByText("1 to 100 of 205 issues")).toBeInTheDocument();
  });

  it("asks for the page the address names", () => {
    address.search = { offset: 200 };
    render(<ProjectDetail />);
    expect(asked.at(-1)).toMatchObject({ offset: 200 });
    expect(screen.getByText("201 to 205 of 205 issues")).toBeInTheDocument();
    expect(screen.getAllByRole("row")).toHaveLength(1 + FILED - 200);
  });

  it("keeps the page in the address, so a reload lands on it", async () => {
    render(<ProjectDetail />);
    await userEvent.click(screen.getByRole("button", { name: "Next page" }));
    expect(lastCall().search).toEqual({ offset: 100 });
    expect(validate({ offset: 100 })).toEqual({ offset: 100 });
    expect(validate({ offset: -5 })).toEqual({});
  });

  it("starts again at the first page when the list is narrowed", async () => {
    address.search = { offset: 100 };
    render(<ProjectDetail />);
    await userEvent.click(screen.getByRole("button", { name: "all" }));
    expect(lastCall().search).toEqual({});
    expect(lastCall().replace).toBe(true);

    navigate.mockReset();
    await userEvent.type(screen.getByLabelText("Search issues"), "l");
    expect(lastCall().search).toEqual({});
    expect(lastCall().replace).toBe(true);
  });
});

describe("listQuery", () => {
  it("says what the list shows as one query, for every page of the export", () => {
    expect(listQuery({ scope: "all", words: "", query: "" })).toBe("");
    expect(listQuery({ scope: "open", words: "", query: "" })).toBe("statusCategory in (todo, in_progress)");
    expect(listQuery({ scope: "mine", words: ' say "hi" ', query: "", label: "ux", milestone: "Beta 1" })).toBe(
      'assignee = currentUser() AND summary ~ "say \\"hi\\"" AND labels = "ux" AND milestone = "Beta 1"',
    );
  });

  it("keeps a query that stands alone as it is, and brackets one that does not", () => {
    expect(listQuery({ scope: "all", words: "", query: "priority = high ORDER BY due" })).toBe("priority = high ORDER BY due");
    expect(listQuery({ scope: "open", words: "", query: "priority = high OR type = Bug" })).toBe(
      "statusCategory in (todo, in_progress) AND (priority = high OR type = Bug)",
    );
  });

  it("offers the list to export", async () => {
    render(<ProjectDetail />);
    await userEvent.click(screen.getByRole("button", { name: "Export" }));
    const href = document.querySelector('[data-action="download-csv"]')?.getAttribute("href") ?? "";
    const params = new URL(href, "http://armature.test").searchParams;
    expect(params.get("project")).toBe("CP");
    expect(params.get("q")).toBe("statusCategory in (todo, in_progress)");
  });
});
