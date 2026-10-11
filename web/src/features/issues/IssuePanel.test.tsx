import { beforeEach, describe, expect, it, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { ReactNode } from "react";
import type { Doc, Issue } from "@/api/issues";
import type { Placement } from "@/api/arrange";

// A draft belongs to the issue it was started on. The panel steps between
// issues it already has, so nothing in it may carry over to the next one.

const nothing = () => ({ data: undefined, isLoading: false, error: null });
const mutation = () => ({ mutate: vi.fn(), mutateAsync: vi.fn(() => Promise.resolve({})), isPending: false, error: null });

/** Every write the page sends, whichever way it sends it. */
const sent = { update: vi.fn(), comment: vi.fn() };
function writes(spy: ReturnType<typeof vi.fn>) {
  return () => ({
    mutate: (vars: unknown) => spy(vars),
    mutateAsync: (vars: unknown) => {
      spy(vars);
      return Promise.resolve({});
    },
    isPending: false,
    error: null,
  });
}

function said(text: string): Doc {
  return { type: "doc", content: [{ type: "paragraph", content: [{ type: "text", text }] }] };
}

function issue(key: string, description?: Doc): Issue {
  return {
    id: key,
    key,
    summary: `Summary of ${key}`,
    description,
    type: { id: "t1", name: "Story", icon: "story", level: 0, isSubtask: false },
    projectId: "p1",
    projectKey: "CP",
    status: { id: "s1", name: "To Do", category: "todo", position: 0 },
    priority: "medium",
    labels: [],
    fixVersions: [],
    affectsVersions: [],
    components: [],
    timeSpentMinutes: 0,
    createdAt: "2026-09-01T09:00:00Z",
    updatedAt: "2026-09-02T09:00:00Z",
  } as Issue;
}

// Both are cached, so stepping between them never shows a skeleton.
const issues: Record<string, Issue> = {
  "CP-1": issue("CP-1", said("What A is about")),
  "CP-2": issue("CP-2"),
};

vi.mock("@tanstack/react-router", () => ({
  Link: ({ children }: { children: ReactNode }) => <a href="#">{children}</a>,
  useNavigate: () => vi.fn(),
  useLocation: () => ({ pathname: "/projects/CP" }),
}));

vi.mock("@/api/issues", async () => ({
  ...(await vi.importActual<typeof import("@/api/issues")>("@/api/issues")),
  useIssue: (key: string) => ({ data: { issue: issues[key] }, isLoading: false, error: null }),
  useComments: nothing, useHistory: nothing, useTransitions: nothing, useIssueHierarchy: nothing,
  useMembers: nothing, useIssues: nothing, useStatuses: nothing, useWorklogs: nothing,
  useAddComment: writes(sent.comment), useTransitionIssue: mutation, useUpdateIssue: writes(sent.update),
  useSetParent: mutation, useCreateIssue: mutation, useLogWork: mutation, useDeleteWorklog: mutation,
}));

vi.mock("@/api/access", async () => ({
  ...(await vi.importActual<typeof import("@/api/access")>("@/api/access")),
  useAccess: nothing,
  canWriteIssues: () => true,
}));

vi.mock("@/api/auth", async () => ({
  ...(await vi.importActual<typeof import("@/api/auth")>("@/api/auth")),
  useMe: () => ({ data: { principal: { user: { id: "u1" } } }, isLoading: false }),
}));

vi.mock("@/api/projects", async () => ({
  ...(await vi.importActual<typeof import("@/api/projects")>("@/api/projects")),
  useProject: () => ({ data: { project: { key: "CP", kind: "software" } }, isLoading: false }),
  useProjects: nothing,
}));

vi.mock("@/api/fields", async () => ({
  ...(await vi.importActual<typeof import("@/api/fields")>("@/api/fields")),
  useIssueFields: () => ({ data: { values: [] }, isLoading: false }),
  useSetFieldValue: mutation,
}));

const places: Placement[] = [{ area: "main", slot: "description" }];

vi.mock("@/api/arrange", async () => ({
  ...(await vi.importActual<typeof import("@/api/arrange")>("@/api/arrange")),
  useArrangements: () => ({
    data: { arrangements: [{ issueTypeId: "t1", issueTypeName: "Story", origin: { scope: "builtin", named: false }, places }] },
    isLoading: false,
  }),
}));

vi.mock("@/api/desk", async () => ({
  ...(await vi.importActual<typeof import("@/api/desk")>("@/api/desk")),
  useTimers: nothing, useCannedResponses: nothing, useAddNote: mutation, useRenderCanned: mutation,
}));

vi.mock("@/api/plan", async () => ({
  ...(await vi.importActual<typeof import("@/api/plan")>("@/api/plan")),
  useLinks: nothing, useLinkTypes: nothing, useSchedule: mutation, useAddLink: mutation, useRemoveLink: mutation,
}));

vi.mock("@/api/sprints", async () => ({
  ...(await vi.importActual<typeof import("@/api/sprints")>("@/api/sprints")),
  useSprints: nothing, useSetIssueSprint: mutation, useSetEstimate: mutation,
}));

vi.mock("@/api/milestones", async () => ({
  ...(await vi.importActual<typeof import("@/api/milestones")>("@/api/milestones")),
  useMilestones: nothing, useSetIssueMilestone: mutation,
}));

vi.mock("@/api/versions", async () => ({
  ...(await vi.importActual<typeof import("@/api/versions")>("@/api/versions")),
  useVersions: nothing, useComponents: nothing, useSetIssueVersions: mutation, useSetIssueComponents: mutation,
}));

vi.mock("@/api/teams", async () => ({
  ...(await vi.importActual<typeof import("@/api/teams")>("@/api/teams")),
  useTeams: nothing, useSetIssueTeam: mutation,
}));

vi.mock("@/api/labels", async () => ({
  ...(await vi.importActual<typeof import("@/api/labels")>("@/api/labels")),
  useLabels: nothing, useSetIssueLabels: mutation,
}));

vi.mock("@/api/watchers", async () => ({
  ...(await vi.importActual<typeof import("@/api/watchers")>("@/api/watchers")),
  useWatchers: nothing, useAddWatcher: mutation, useRemoveWatcher: mutation,
}));

vi.mock("@/api/remoteLinks", async () => ({
  ...(await vi.importActual<typeof import("@/api/remoteLinks")>("@/api/remoteLinks")),
  useRemoteLinks: nothing, useRemoveRemoteLink: mutation,
}));

vi.mock("@/api/attachments", async () => ({
  ...(await vi.importActual<typeof import("@/api/attachments")>("@/api/attachments")),
  useAttachments: nothing, useUploadAttachment: mutation, useDeleteAttachment: mutation,
}));

vi.mock("@/api/git", async () => ({
  ...(await vi.importActual<typeof import("@/api/git")>("@/api/git")),
  useDevelopment: nothing, useRepositories: nothing, useCreateBranch: mutation,
}));

vi.mock("@/api/filters", async () => ({
  ...(await vi.importActual<typeof import("@/api/filters")>("@/api/filters")),
  useCloneIssue: mutation, useMoveIssue: mutation,
}));

const { IssueDrawerProvider, useIssueDrawer, useIssueList } = await import("./IssueDrawer");
const { IssuePanel } = await import("./IssuePanel");
const { ConfirmProvider } = await import("@/features/shell/ConfirmProvider");
const { ToastProvider } = await import("@/components/ui");

/** The list beside the panel: it registers both issues and opens the first. */
function List() {
  useIssueList(["CP-1", "CP-2"]);
  const panel = useIssueDrawer();
  return (
    <div data-issue-list="">
      <button onClick={() => panel.open("CP-1")}>Open CP-1</button>
    </div>
  );
}

async function openPanel() {
  render(
    <ToastProvider>
      <ConfirmProvider>
        <IssueDrawerProvider>
          <List />
          <IssuePanel />
        </IssueDrawerProvider>
      </ConfirmProvider>
    </ToastProvider>,
  );
  await userEvent.click(screen.getByRole("button", { name: "Open CP-1" }));
  expect(shown()).toBe("CP-1");
}

/** The issue the panel's page is drawn for. */
function shown(): string | null | undefined {
  return document.querySelector("[data-issue-page]")?.getAttribute("data-issue-page");
}

async function step(to: "next" | "prev", expected: string) {
  await userEvent.click(document.querySelector<HTMLButtonElement>(`[data-action="panel-${to}"]`)!);
  expect(shown()).toBe(expected);
}

function descriptionBox(): HTMLTextAreaElement | null {
  return document.querySelector<HTMLTextAreaElement>("textarea#issue-description");
}

function commentBox(): HTMLTextAreaElement {
  return document.querySelector<HTMLTextAreaElement>("textarea#new-comment")!;
}

function keysWritten(spy: ReturnType<typeof vi.fn>): string[] {
  return spy.mock.calls.map(([vars]) => (vars as { key: string }).key);
}

describe("stepping through issues in the panel", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    // The markdown face is a plain textarea, which jsdom types into faithfully.
    localStorage.clear();
    localStorage.setItem("armature.editor", "markdown");
  });

  it("leaves a description draft on its own issue and saves it there", async () => {
    await openPanel();
    await step("next", "CP-2");
    await userEvent.click(screen.getByRole("button", { name: "Add description" }));
    await userEvent.type(descriptionBox()!, "What B is about");

    await step("prev", "CP-1");
    expect(descriptionBox()).toBeNull();
    expect(screen.queryByRole("button", { name: "Save description" })).not.toBeInTheDocument();
    expect(screen.getByText("What A is about")).toBeInTheDocument();

    await step("next", "CP-2");
    expect(descriptionBox()?.value).toBe("What B is about");
    await userEvent.click(screen.getByRole("button", { name: "Save description" }));
    expect(keysWritten(sent.update)).toEqual(["CP-2"]);
    expect(sent.update).toHaveBeenCalledWith(expect.objectContaining({ key: "CP-2", description: said("What B is about") }));
  });

  it("leaves a half-typed comment on its own issue and posts it there", async () => {
    await openPanel();
    await step("next", "CP-2");
    await userEvent.type(commentBox(), "Half a thought on B");

    await step("prev", "CP-1");
    expect(commentBox().value).toBe("");
    expect(screen.getByRole("button", { name: "Comment" })).toBeDisabled();
    await userEvent.type(commentBox(), "{Control>}{Enter}{/Control}");
    expect(sent.comment).not.toHaveBeenCalled();

    await step("next", "CP-2");
    expect(commentBox().value).toBe("Half a thought on B");
    await userEvent.click(screen.getByRole("button", { name: "Comment" }));
    expect(keysWritten(sent.comment)).toEqual(["CP-2"]);
  });

  it("forgets a description draft once it is cancelled", async () => {
    await openPanel();
    await step("next", "CP-2");
    await userEvent.click(screen.getByRole("button", { name: "Add description" }));
    await userEvent.type(descriptionBox()!, "Not worth keeping");
    await userEvent.click(screen.getByRole("button", { name: "Cancel" }));

    await step("prev", "CP-1");
    await step("next", "CP-2");
    expect(descriptionBox()).toBeNull();
    expect(screen.getByText("No description yet.")).toBeInTheDocument();
  });
});
