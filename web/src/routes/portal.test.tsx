import { describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import type { ReactNode } from "react";
import type { Desk, MyRequests, PortalFollower, PortalRequest, Request as RequestRead } from "@/api/desk";

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

function request(key: string, summary: string, extra: Partial<PortalRequest> = {}): PortalRequest {
  return { key, summary, status: { name: "Waiting for support", category: "todo" }, sharedWithCompany: false, updatedAt: "2026-10-10T08:00:00Z", ...extra };
}

let lists: MyRequests = { requests: [], shared: [] };
let desks: Desk[] = [];
let opened: RequestRead | undefined;
let followers: PortalFollower[] = [];
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
    useRequestWatchers: () => ({ data: { watchers: followers } }),
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
const typeOf = {
  id: "rt1",
  projectId: "p",
  projectKey: "HLP",
  name: "Report a problem",
  issueTypeId: "t",
  issueTypeName: "Bug",
  priority: "high" as const,
  position: 0,
  category: "",
  detailsTemplate: "",
};

describe("the portal's lists", () => {
  it("shows what colleagues shared under the company, with who raised it", () => {
    lists = {
      requests: [request("HLP-1", "My own laptop")],
      shared: [request("HLP-2", "The VPN is down", { company: globex, sharedWithCompany: true, reporter: { id: "ada", name: "Ada Reporter" } })],
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
    await waitFor(() =>
      expect(raise).toHaveBeenCalledWith(expect.objectContaining({ requestTypeId: "rt1", summary: "The VPN is down", shareWithCompany: true })),
    );
  });

  it("offers nothing to share with a customer in no company", () => {
    desks = [{ projectId: "p", projectKey: "HLP", name: "Help", requestTypes: [typeOf], repliesByMail: false }];
    render(<RaisePage />);
    expect(screen.queryByLabelText(/Share with/)).toBeNull();
  });
});

describe("a shared request's page", () => {
  const issue = () => request("HLP-2", "The VPN is down", { company: globex, sharedWithCompany: true, reporter: { id: "ada", name: "Ada Reporter" } });
  const friend = { userId: "f1", name: "Friend", email: "friend@elsewhere.test" };

  it("shows a colleague who raised it, and not who follows it", () => {
    followers = [friend];
    opened = { request: issue(), comments: [], throughCompany: true };
    render(<RequestPage />);
    expect(screen.getByText("Raised by Ada Reporter and shared with Globex.")).toBeInTheDocument();
    expect(screen.queryByTestId("followers")).toBeNull();
    expect(screen.queryByText("friend@elsewhere.test")).toBeNull();
  });

  it("shows the followers to whoever is on the request", () => {
    followers = [friend];
    opened = { request: issue(), comments: [] };
    render(<RequestPage />);
    expect(within(screen.getByTestId("followers")).getByText("friend@elsewhere.test")).toBeInTheDocument();
  });

  it("names a follower the portal gives no address for", () => {
    followers = [friend, { userId: "a1", name: "Desk Agent" }];
    opened = { request: issue(), comments: [] };
    render(<RequestPage />);
    const listed = screen.getByTestId("followers");
    expect(within(listed).getByText("friend@elsewhere.test")).toBeInTheDocument();
    expect(listed.querySelector('[data-request-watcher="Desk Agent"]')).toHaveTextContent("Desk Agent");
  });

  it("reads the request type, the team and the conversation from the portal's answer", () => {
    followers = [];
    opened = {
      request: { ...issue(), requestTypeName: "Report a problem", team: { id: "t1", name: "Field support" }, assignee: { id: "ag", name: "Grace Agent" } },
      comments: [
        {
          id: "c1",
          author: { id: "ag", name: "Grace Agent" },
          body: { type: "doc", content: [{ type: "paragraph", content: [{ type: "text", text: "We are on it." }] }] },
          createdAt: "2026-10-10T09:00:00Z",
        },
      ],
    };
    render(<RequestPage />);
    expect(screen.getByText(/Report a problem/)).toBeInTheDocument();
    expect(screen.getByText("· with Field support")).toBeInTheDocument();
    expect(screen.getByText("We are on it.")).toBeInTheDocument();
  });
});
