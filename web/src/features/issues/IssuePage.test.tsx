import { describe, expect, it, vi } from "vitest";
import { render } from "@testing-library/react";
import type { ReactNode } from "react";
import type { Issue } from "@/api/issues";
import type { Placement } from "@/api/arrange";

// This is the referee for the arrangement work: it says what the hardcoded
// page draws, so a page driven by data has to draw the same thing.

const nothing = () => ({ data: undefined, isLoading: false, error: null });
const mutation = () => ({ mutate: vi.fn(), mutateAsync: vi.fn(), isPending: false, error: null });

const state = {
  issue: {
    id: "i1",
    key: "CP-4",
    summary: "Fix the login loop",
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
  } as Issue,
  fields: [
    {
      field: { id: "f1", projectId: "p1", projectKey: "CP", org: false, name: "Customer", kind: "text", options: [], position: 0, createdAt: "", updatedAt: "" },
      value: null,
    },
  ],
};

vi.mock("@tanstack/react-router", () => ({
  Link: ({ children }: { children: ReactNode }) => <a href="#">{children}</a>,
  useNavigate: () => vi.fn(),
}));

vi.mock("@/api/issues", async () => ({
  ...(await vi.importActual<typeof import("@/api/issues")>("@/api/issues")),
  useIssue: () => ({ data: { issue: state.issue }, isLoading: false, error: null }),
  useComments: nothing, useHistory: nothing, useTransitions: nothing, useIssueHierarchy: nothing,
  useMembers: nothing, useIssues: nothing, useStatuses: nothing, useWorklogs: nothing,
  useAddComment: mutation, useTransitionIssue: mutation, useUpdateIssue: mutation,
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
  useIssueFields: () => ({ data: { values: state.fields }, isLoading: false }),
  useSetFieldValue: mutation,
}));

// The page draws what the server arranges; this is the arrangement the server
// answers with until a project says otherwise.
const builtIn: Placement[] = [
  { area: "main", slot: "description" },
  { area: "people", slot: "assignee" },
  { area: "people", slot: "reporter" },
  { area: "planning", slot: "parent" },
  { area: "planning", slot: "schedule" },
  { area: "planning", slot: "sprint" },
  { area: "planning", slot: "milestone" },
  { area: "planning", slot: "fixVersions" },
  { area: "planning", slot: "affectsVersions" },
  { area: "planning", slot: "components" },
  { area: "planning", slot: "estimate" },
  { area: "planning", slot: "team" },
  { area: "tracking", slot: "request" },
  { area: "tracking", slot: "goals" },
  { area: "tracking", slot: "priority" },
  { area: "tracking", slot: "labels" },
  { area: "tracking", slot: "time" },
  { area: "more", slot: "otherFields" },
  { area: "more", slot: "created" },
  { area: "more", slot: "resolved" },
];

vi.mock("@/api/arrange", async () => ({
  ...(await vi.importActual<typeof import("@/api/arrange")>("@/api/arrange")),
  useArrangements: () => ({
    data: { arrangements: [{ issueTypeId: "t1", issueTypeName: "Story", origin: { scope: "builtin", named: false }, places: builtIn }] },
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

// A project with no teams says so in words instead of offering a select, so
// the team row only carries its control when there is a team to pick.
vi.mock("@/api/teams", async () => ({
  ...(await vi.importActual<typeof import("@/api/teams")>("@/api/teams")),
  useTeams: () => ({ data: { teams: [{ id: "tm1", projectKey: "CP", name: "Platform" }] }, isLoading: false }),
  useSetIssueTeam: mutation,
}));

vi.mock("@/api/labels", async () => ({
  ...(await vi.importActual<typeof import("@/api/labels")>("@/api/labels")),
  useLabels: nothing, useSetIssueLabels: mutation,
}));

vi.mock("@/api/watchers", async () => ({
  ...(await vi.importActual<typeof import("@/api/watchers")>("@/api/watchers")),
  useWatchers: nothing, useAddWatcher: mutation, useRemoveWatcher: mutation,
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

const { IssuePage } = await import("./IssuePage");
const { ConfirmProvider } = await import("@/features/shell/ConfirmProvider");
const { ToastProvider } = await import("@/components/ui");

// The page asks to confirm and to toast, which the shell provides for it.
function renderPage() {
  return render(
    <ToastProvider>
      <ConfirmProvider>
        <IssuePage issueKey="CP-4" />
      </ConfirmProvider>
    </ToastProvider>,
  );
}

function groupTitles(): (string | null)[] {
  return [...document.querySelectorAll("[data-issue-group]")].map((group) => group.getAttribute("data-issue-group"));
}

// A group's own rows, not the ones a row draws inside itself: the time summary
// lists what is left and what was spent under its own label.
function rowsIn(title: string): (string | null)[] {
  const group = document.querySelector(`[data-issue-group="${title}"]`);
  return [...(group?.querySelectorAll(":scope > dl > div > dt") ?? [])].map((dt) => dt.textContent);
}

function groupOf(selector: string): string | null | undefined {
  return document.querySelector(selector)?.closest("[data-issue-group]")?.getAttribute("data-issue-group");
}

describe("the issue page", () => {
  it("holds the facts in four groups, in the order a reader learned them", () => {
    renderPage();
    expect(groupTitles()).toEqual(["People", "Planning", "Tracking", "Fields"]);
  });

  it("keeps each group's facts in their order", () => {
    renderPage();
    expect(rowsIn("People")).toEqual(["Assignee", "Reporter"]);
    expect(rowsIn("Planning")).toEqual([
      "Parent", "Scheduled", "Sprint", "Milestone", "Fix versions", "Affects versions", "Components", "Estimate", "Team",
    ]);
    expect(rowsIn("Tracking")).toEqual(["Priority", "Labels", "Time"]);
  });

  // The browser suite presses these by id, so where they live is a contract.
  it("leaves every control the browser suite presses where it was", () => {
    renderPage();
    for (const id of ["#issue-start", "#issue-due", "#issue-sprint", "#issue-milestone", "#issue-estimate", "#issue-team", "#issue-fix-versions", "#issue-affects-versions", "#issue-components"]) {
      expect(groupOf(id), id).toBe("Planning");
    }
    expect(groupOf("[data-labels]")).toBe("Tracking");
    expect(groupOf('[data-custom-field="Customer"]')).toBe("Fields");
  });

  it("puts a project's own fields before the dates it keeps itself", () => {
    renderPage();
    expect(rowsIn("Fields")).toEqual(["Customer", "Created"]);
  });

  // A fact with nothing to say draws no row at all, not an empty one.
  it("draws no row for a request, a goal or a resolution the issue does not have", () => {
    renderPage();
    expect(rowsIn("Tracking")).not.toContain("Request");
    expect(rowsIn("Tracking")).not.toContain("Goals");
    expect(rowsIn("Fields")).not.toContain("Resolved");
  });

  it("reads down the left column in the order the rail offers", () => {
    renderPage();
    const sections = [...document.querySelectorAll("[data-section]")].map((s) => s.getAttribute("data-section"));
    const rail = [...document.querySelectorAll("[data-rail]")].map((a) => a.getAttribute("data-rail"));
    expect(sections).toEqual(["description", "activity", "attachments", "work", "development", "children", "links", "watchers", "history"]);
    expect(rail).toEqual(sections);
  });
});
