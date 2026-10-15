import { describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import type { ReactNode } from "react";
import type { Desk, MyRequests, Request as RequestRead } from "@/api/desk";
import type { Issue } from "@/api/issues";

vi.mock("@tanstack/react-router", () => ({
  Link: ({ children, to, params }: { children: ReactNode; to: string; params?: Record<string, string> }) => {
    const href = Object.entries(params ?? {}).reduce((path, [name, value]) => path.replace(`$${name}`, value), to);
    return <a href={href}>{children}</a>;
  },
  Navigate: () => null,
  createRoute: (options: object) => ({ ...options, useParams: () => ({ issueKey: "HLP-2" }) }),
}));
vi.mock("./app", () => ({ appRoute: {} }));
vi.mock("@/features/attachments/AttachmentPanel", () => ({ AttachmentPanel: () => null }));
vi.mock("@/lib/format", () => ({ useFormat: () => ({ relative: () => "just now" }) }));
vi.mock("@/api/auth", () => ({ useMe: () => ({ data: { principal: { user: { id: "me" } } } }) }));
vi.mock("@/api/attachments", async () => {
  const actual = await vi.importActual<typeof import("@/api/attachments")>("@/api/attachments");
  return { ...actual, useUploadAttachment: () => ({ mutateAsync: vi.fn() }), useUploadLimit: () => 1024 * 1024 };
});

function request(key: string, summary: string, extra: Partial<Issue> = {}): Issue {
  return {
    id: key, key, summary, projectId: "p", projectKey: "HLP", priority: "medium", timeSpentMinutes: 0, labels: [], fixVersions: [], affectsVersions: [], components: [],
    type: { id: "t", name: "Task", icon: "task", level: 0, isSubtask: false },
    status: { id: "s", name: "Waiting for support", category: "todo" },
    createdAt: "2026-10-10T08:00:00Z", updatedAt: "2026-10-10T08:00:00Z",
    ...extra,
  } as Issue;
}

let lists: MyRequests = { requests: [], shared: [] };
let desks: Desk[] = [];
let opened: RequestRead | undefined;
const raise = vi.fn();
vi.mock("@/api/desk", async () => {
  const actual = await vi.importActual<typeof import("@/api/desk")>("@/api/desk");
  return {
    ...actual,
    useMyRequests: () => ({ data: lists, isLoading: false }),
    useDesks: () => ({ data: { desks } }),
    useRaiseRequest: () => ({ mutateAsync: raise, isPending: false, error: null }),
    usePortalArticles: () => ({ data: { articles: [] } }),
    useRequest: () => ({ data: opened, isLoading: false, error: null }),
    useRequestWatchers: () => ({ data: { watchers: [{ userId: "f1", name: "Friend", email: "friend@elsewhere.test" }] } }),
    useReply: () => ({ mutate: vi.fn(), isPending: false, error: null }),
    useFollow: () => ({ mutate: vi.fn(), isPending: false, error: null }),
    useUnfollow: () => ({ mutate: vi.fn(), isPending: false, error: null }),
  };
});

const { portalRoute, portalNewRoute, portalRequestRoute } = await import("./portal");
const RequestPage = (portalRequestRoute as unknown as { component: () => ReactNode }).component;
const MyRequestsPage = (portalRoute as unknown as { component: () => ReactNode }).component;
const RaisePage = (portalNewRoute as unknown as { component: () => ReactNode }).component;

const globex = { id: "c1", name: "Globex" };
const typeOf = { id: "rt1", projectId: "p", projectKey: "HLP", name: "Report a problem", issueTypeId: "t", issueTypeName: "Bug", priority: "high" as const, position: 0, category: "", detailsTemplate: "" };

describe("the portal's lists", () => {
  it("shows what colleagues shared under the company, with who raised it", () => {
    lists = {
      requests: [request("HLP-1", "My own laptop")],
      shared: [request("HLP-2", "The VPN is down", { company: globex, sharedWithCompany: true, reporter: { id: "ada", name: "Ada Reporter", email: "ada@globex.test" } })],
    };
    render(<MyRequestsPage />);
    const shared = screen.getByText("Shared by Globex").closest("section") as HTMLElement;
    expect(within(shared).getByText("The VPN is down")).toBeInTheDocument();
    expect(within(shared).getByText("Ada Reporter")).toBeInTheDocument();
    expect(within(shared).queryByText("My own laptop")).toBeNull();
  });

  it("has no shared section when nothing was shared", () => {
    lists = { requests: [request("HLP-1", "My own laptop")], shared: [] };
    render(<MyRequestsPage />);
    expect(screen.queryByText(/Shared by/)).toBeNull();
  });
});

describe("raising a request", () => {
  it("offers to share it with the customer's company, and sends what was chosen", async () => {
    desks = [{ projectId: "p", projectKey: "HLP", name: "Help", requestTypes: [typeOf], repliesByMail: false, company: globex }];
    raise.mockResolvedValue({ request: { key: "HLP-3" } });
    render(<RaisePage />);
    fireEvent.click(screen.getByLabelText("Share with Globex"));
    fireEvent.change(screen.getByLabelText(/Summary/), { target: { value: "The VPN is down" } });
    fireEvent.click(screen.getByRole("button", { name: "Send request" }));
    await waitFor(() => expect(raise).toHaveBeenCalledWith(expect.objectContaining({ requestTypeId: "rt1", summary: "The VPN is down", shareWithCompany: true })));
  });

  it("offers nothing to share with a customer in no company", () => {
    desks = [{ projectId: "p", projectKey: "HLP", name: "Help", requestTypes: [typeOf], repliesByMail: false }];
    render(<RaisePage />);
    expect(screen.queryByLabelText(/Share with/)).toBeNull();
  });
});

describe("a shared request's page", () => {
  const issue = () => request("HLP-2", "The VPN is down", { company: globex, sharedWithCompany: true, reporter: { id: "ada", name: "Ada Reporter", email: "ada@globex.test" } });

  it("shows a colleague who raised it, and not who follows it", () => {
    opened = { issue: issue(), comments: [], throughCompany: true };
    render(<RequestPage />);
    expect(screen.getByText("Raised by Ada Reporter and shared with Globex.")).toBeInTheDocument();
    expect(screen.queryByTestId("followers")).toBeNull();
    expect(screen.queryByText("friend@elsewhere.test")).toBeNull();
  });

  it("shows the followers to whoever is on the request", () => {
    opened = { issue: issue(), comments: [] };
    render(<RequestPage />);
    expect(within(screen.getByTestId("followers")).getByText("friend@elsewhere.test")).toBeInTheDocument();
  });
});
